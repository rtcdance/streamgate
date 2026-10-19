package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v4"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestHTTPAuth_RejectsAmbiguousAndConflictingCredentials(t *testing.T) {
	for _, tc := range []struct {
		name    string
		query   string
		headers []string
		changes jwt.MapClaims
		want    int
	}{
		{name: "access-only", want: 200},
		{name: "duplicate-access", headers: []string{"JWT", "JWT"}, want: 401},
		{name: "capability-only", query: "?playback_token=cap", headers: []string{}, want: 200},
		{name: "same-capability-header", query: "?playback_token=cap", headers: []string{"cap"}, want: 200},
		{name: "duplicate-capability", query: "?playback_token=cap&playback_token=cap", headers: []string{}, want: 401},
		{name: "empty-duplicate-capability", query: "?playback_token=&playback_token=cap", headers: []string{}, want: 401},
		{name: "malformed-query", query: "?playback_token=cap&playback_token=%ZZ", headers: []string{}, want: 401},
		{name: "matching-dual", query: "?playback_token=cap", want: 200},
		{name: "conflicting-session", query: "?playback_token=cap", changes: jwt.MapClaims{"session_id": "another-session"}, want: 401},
		{name: "conflicting-version", query: "?playback_token=cap", changes: jwt.MapClaims{"auth_epoch": 2}, want: 401},
		{name: "conflicting-restore", query: "?playback_token=cap", changes: jwt.MapClaims{"restore_epoch": "another-epoch"}, want: 401},
		{name: "expired-access", query: "?playback_token=cap", changes: jwt.MapClaims{"exp": time.Now().Add(-time.Hour).Unix()}, want: 401},
		{name: "wrong-purpose", query: "?playback_token=cap", changes: jwt.MapClaims{"token_type": "playback"}, want: 401},
		{name: "wrong-issuer", query: "?playback_token=cap", changes: jwt.MapClaims{"iss": "other"}, want: 401},
		{name: "invalid-access-with-valid-capability", query: "?playback_token=cap", headers: []string{"invalid"}, want: 401},
		{name: "duplicate-access-with-capability", query: "?playback_token=cap", headers: []string{"JWT", "JWT"}, want: 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			r.Use(PlaybackTokenAuth(PlaybackTokenAuthConfig{Validate: func(context.Context, string, string, string) (*PlaybackIdentity, error) {
				return &PlaybackIdentity{Wallet: baseAccessClaims(nil)["wallet_address"].(string), SessionID: "test-session", AuthEpoch: 1, RestoreEpoch: tokenKindTestRestoreEpoch}, nil
			}}, zap.NewNop()))
			r.Use(JWTAuthMiddleware(JWTAuthConfig{Secret: tokenKindTestSecret, Issuer: "streamgate", Audience: "streamgate-api"}, zap.NewNop()))
			r.GET("/media", func(c *gin.Context) { c.Status(200) })
			token := signWithClaims(t, tokenKindTestSecret, jwt.SigningMethodHS256, baseAccessClaims(tc.changes))
			headers := tc.headers
			if headers == nil {
				headers = []string{"JWT"}
			}
			req := httptest.NewRequest(http.MethodGet, "/media"+tc.query, http.NoBody)
			for _, header := range headers {
				if header == "JWT" {
					header = token
				}
				req.Header.Add("Authorization", "Bearer "+header)
			}
			result := httptest.NewRecorder()
			r.ServeHTTP(result, req)
			require.Equal(t, tc.want, result.Code)
		})
	}
}
