package gateway

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/rtcdance/streamgate/pkg/core/config"
	"github.com/rtcdance/streamgate/pkg/middleware"
	"github.com/rtcdance/streamgate/pkg/monitoring"
	"github.com/rtcdance/streamgate/pkg/service"
	"github.com/rtcdance/streamgate/pkg/util"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func RegisterAuthRoutes(router *gin.Engine, log *zap.Logger, cfg *config.Config, authService *service.AuthService, rateLimiter middleware.RateLimiter) {
	auth := router.Group(APIPrefix + "/auth")
	auth.Use(func(c *gin.Context) {
		key := c.ClientIP()
		if wallet := c.GetHeader("X-Wallet-Address"); wallet != "" {
			key = key + ":" + wallet
		}
		if !rateLimiter.Allow(c.Request.Context(), key) {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": "Rate limit exceeded",
				"code":  "RATE_LIMITED",
			})
			c.Abort()
			return
		}
		c.Next()
	})
	auth.POST("/challenge", handleAuthChallenge(cfg, authService))
	auth.POST("/login", handleAuthLogin(authService))
	auth.POST("/register", handleAuthRegister(authService, log))
	auth.POST("/refresh", handleAuthRefresh(authService))
	auth.POST("/logout", handleAuthLogout(authService, log))
	auth.POST("/verify", handleAuthVerify(authService))
	log.Info("Auth routes registered")
}

func handleAuthChallenge(cfg *config.Config, authService *service.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Address  string `json:"address"`
			Wallet   string `json:"wallet"`
			ChainID  int64  `json:"chain_id"`
			SignType string `json:"sign_type" binding:"omitempty,oneof=personal_sign eip712"`
		}
		if errs := BindAndValidate(c, &req); errs != nil {
			abortWithValidationError(c, errs)
			return
		}
		wallet := req.Address
		if wallet == "" {
			wallet = req.Wallet
		}
		// Validate wallet address format (Ethereum or Solana)
		if wallet != "" && !util.IsValidAddress(wallet) && !service.IsValidSolanaAddress(wallet) {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "invalid wallet address format")
			return
		}
		chainID := req.ChainID
		if chainID == 0 {
			chainID = cfg.Web3.ChainID
		}
		challenge, err := authService.GenerateWalletChallenge(c.Request.Context(), wallet, chainID, req.SignType)
		if err != nil {
			abortWithErrorDetail(c, http.StatusInternalServerError, ErrInternalError, internalErrMsg(c, err), err.Error())
			return
		}
		respondOK(c, gin.H{
			"challenge_id": challenge.ID, "message": challenge.Message,
			"nonce":      challenge.Nonce,
			"issued_at":  challenge.IssuedAt.Format(time.RFC3339),
			"expires_at": challenge.ExpiresAt.Format(time.RFC3339),
			"address":    challenge.WalletAddress,
			"wallet":     challenge.WalletAddress, "chain_id": challenge.ChainID,
			"signing_type": challenge.SigningType,
		})
	}
}

func handleAuthLogin(authService *service.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Address     string `json:"address"`
			Wallet      string `json:"wallet"`
			ChallengeID string `json:"challenge_id" binding:"required"`
			Signature   string `json:"signature" binding:"required"`
			ChainID     int64  `json:"chain_id"`
		}
		if errs := BindAndValidate(c, &req); errs != nil {
			abortWithValidationError(c, errs)
			return
		}
		wallet := req.Address
		if wallet == "" {
			wallet = req.Wallet
		}
		token, err := authService.AuthenticateWithWallet(c.Request.Context(), wallet, req.ChallengeID, req.Signature, req.ChainID)
		if err != nil {
			monitoring.AuthOperationsTotal.WithLabelValues("login", "failure").Inc()
			abortWithError(c, http.StatusUnauthorized, ErrUnauthorized, "authentication failed")
			return
		}
		monitoring.AuthOperationsTotal.WithLabelValues("login", "success").Inc()
		// Parse token to extract expires_at
		var expiresAt string
		if claims, err := authService.ParseToken(token); err == nil && claims.ExpiresAt != nil {
			expiresAt = claims.ExpiresAt.Format(time.RFC3339)
		}
		c.Header("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
		c.Header("Pragma", "no-cache")
		respondOK(c, gin.H{"token": token, "address": wallet, "wallet_address": wallet, "expires_at": expiresAt})
	}
}

func isValidUsername(username string) bool {
	for _, r := range username {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
	}
	return true
}

func handleAuthRegister(authService *service.AuthService, log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Username string `json:"username" binding:"required,min=3,max=50"`
			Password string `json:"password" binding:"required,min=8"`
			Email    string `json:"email" binding:"omitempty,email"`
		}
		if errs := BindAndValidate(c, &req); errs != nil {
			abortWithValidationError(c, errs)
			return
		}
		if !isValidUsername(req.Username) {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "username must contain only alphanumeric characters and underscores")
			return
		}
		if req.Email != "" && !util.IsValidEmail(req.Email) {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "invalid email format")
			return
		}
		if err := authService.Register(c.Request.Context(), req.Username, req.Password, req.Email); err != nil {
			// Sanitize error: never expose internal DB details to client.
			middleware.GetLogger(c, log).Warn("Registration failed", zap.String("username", req.Username), zap.Error(err))
			abortWithError(c, http.StatusConflict, ErrInvalidRequest, "username or email already exists")
			return
		}
		respondCreated(c, gin.H{"message": "user registered", "username": req.Username})
	}
}

func handleAuthRefresh(authService *service.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Token string `json:"token" binding:"required"`
		}
		if errs := BindAndValidate(c, &req); errs != nil {
			abortWithValidationError(c, errs)
			return
		}
		// Check if token is revoked before refreshing
		if claims, err := authService.ParseToken(req.Token); err == nil && claims.JTI != "" {
			if authService.IsTokenRevoked(c.Request.Context(), claims.JTI) {
				abortWithError(c, http.StatusUnauthorized, ErrTokenRevoked, "token has been revoked")
				return
			}
		}
		newToken, err := authService.RefreshToken(c.Request.Context(), req.Token)
		if err != nil {
			monitoring.AuthOperationsTotal.WithLabelValues("refresh", "failure").Inc()
			abortWithError(c, http.StatusUnauthorized, ErrUnauthorized, "token refresh failed")
			return
		}
		monitoring.AuthOperationsTotal.WithLabelValues("refresh", "success").Inc()
		var expiresAt string
		if claims, err := authService.ParseToken(newToken); err == nil && claims.ExpiresAt != nil {
			expiresAt = claims.ExpiresAt.Format(time.RFC3339)
		}
		c.Header("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
		c.Header("Pragma", "no-cache")
		respondOK(c, gin.H{"token": newToken, "expires_at": expiresAt})
	}
}

func handleAuthLogout(authService *service.AuthService, log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenStr := extractBearerToken(c)
		if tokenStr == "" {
			monitoring.AuthOperationsTotal.WithLabelValues("logout", "failure").Inc()
			abortWithError(c, http.StatusUnauthorized, ErrUnauthorized, "missing token")
			return
		}
		if err := authService.RevokeToken(c.Request.Context(), tokenStr); err != nil {
			middleware.GetLogger(c, log).Warn("failed to revoke token on logout", zap.Error(err))
		}
		monitoring.AuthOperationsTotal.WithLabelValues("logout", "success").Inc()
		respondOK(c, gin.H{"message": "logged out"})
	}
}

func handleAuthVerify(authService *service.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenStr := extractBearerToken(c)
		if tokenStr == "" {
			monitoring.AuthOperationsTotal.WithLabelValues("verify", "failure").Inc()
			abortWithError(c, http.StatusUnauthorized, ErrUnauthorized, "missing token")
			return
		}
		result, err := authService.VerifyToken(c.Request.Context(), tokenStr)
		if err != nil || !result.Valid {
			monitoring.AuthOperationsTotal.WithLabelValues("verify", "failure").Inc()
			code := ErrUnauthorized
			if err != nil && errors.Is(err, service.ErrTokenRevoked) {
				code = ErrTokenRevoked
			}
			abortWithError(c, http.StatusUnauthorized, code, "invalid token")
			return
		}
		monitoring.AuthOperationsTotal.WithLabelValues("verify", "success").Inc()
		respondOK(c, gin.H{"valid": true, "expires_at": result.ExpiresAt, "wallet_address": result.WalletAddress})
	}
}

// RegisterAuthProtectedRoutes registers JWT-protected authentication routes.
func RegisterAuthProtectedRoutes(router gin.IRouter, log *zap.Logger, authService *service.AuthService) {
	router.GET(APIPrefix+"/auth/profile", func(c *gin.Context) {
		wallet := middleware.GetWalletAddress(c)
		respondOK(c, gin.H{"wallet_address": wallet})
	})
	router.POST(APIPrefix+"/auth/change-password", func(c *gin.Context) {
		var req struct {
			OldPassword string `json:"old_password"`
			NewPassword string `json:"new_password"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "invalid request")
			return
		}
		if req.OldPassword == "" || req.NewPassword == "" {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "old_password and new_password are required")
			return
		}
		username := middleware.GetWalletAddress(c)
		// Wallet-authenticated users authenticate via signature, not password.
		if strings.HasPrefix(username, "0x") {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "wallet-authenticated users cannot change password")
			return
		}
		if err := authService.ChangePassword(c.Request.Context(), username, req.OldPassword, req.NewPassword); err != nil {
			abortWithErrorDetail(c, http.StatusUnauthorized, ErrUnauthorized, "password change failed", err.Error())
			return
		}
		respondOK(c, gin.H{"message": "password changed"})
	})
}

// extractBearerToken extracts the Bearer token from the Authorization header.
func extractBearerToken(c *gin.Context) string {
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		return ""
	}
	if strings.HasPrefix(authHeader, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
	}
	return ""
}
