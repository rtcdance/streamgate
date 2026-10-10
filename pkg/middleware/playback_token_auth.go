package middleware

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// PlaybackTokenValidator validates a playback token and returns the wallet
// address it was issued to. Injected as a function type (not an interface) so
// pkg/middleware does not import pkg/service — routes.go wires the closure.
type PlaybackTokenValidator func(ctx context.Context, token, contentID, fingerprint string) (wallet string, err error)

// PlaybackTokenAuthConfig configures PlaybackTokenAuth. Validate is required;
// a nil validator fails closed (401) rather than trusting the token.
type PlaybackTokenAuthConfig struct {
	Validate PlaybackTokenValidator
}

// PlaybackTokenAuth validates the playback_token query param BEFORE
// JWTAuthMiddleware runs. HLS.js cannot send an Authorization header on media
// requests, so segment and sub-manifest URLs carry the token in the query
// string. The token must be cryptographically validated here — the mere
// presence of the parameter must never grant access. On success it sets
// playback_auth (which JWTAuthMiddleware and NFTGateMiddleware accept in lieu
// of a JWT / on-chain check) and wallet_address from the token's claims.
func PlaybackTokenAuth(config PlaybackTokenAuthConfig, logger *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.Query("playback_token")
		if token == "" {
			// No playback token: leave the decision to JWTAuthMiddleware.
			c.Next()
			return
		}
		if config.Validate == nil {
			logger.Error("PlaybackTokenAuth misconfigured: nil validator")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "playback auth unavailable", "code": "UNAUTHORIZED"})
			return
		}
		wallet, err := config.Validate(c.Request.Context(), token, c.Param("id"), c.GetHeader("X-Client-Fingerprint"))
		if err != nil {
			logger.Debug("playback token validation failed", zap.Error(err))
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid playback token", "code": "UNAUTHORIZED"})
			return
		}
		if wallet == "" {
			// GeneratePlaybackToken rejects empty wallets (A5); reject here too
			// rather than propagate an unattributable credential.
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "playback token has no wallet binding", "code": "UNAUTHORIZED"})
			return
		}
		c.Set("playback_auth", true)
		c.Set("wallet_address", wallet)
		c.Next()
	}
}
