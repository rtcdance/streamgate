//go:build fullchain

package e2e_test

import (
	"bytes"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const e2eBase = "http://localhost:18080"
const e2eAnvilAddr = "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266"

func e2eLogin(t *testing.T) string {
	t.Helper()
	key := e2eKey(t)
	resp, err := http.Post(e2eBase+"/api/v1/auth/challenge", "application/json",
		bytes.NewReader([]byte(fmt.Sprintf(`{"address":"%s"}`, e2eAnvilAddr))))
	require.NoError(t, err)
	defer resp.Body.Close()
	var cr struct {
		ChallengeID string `json:"challenge_id"`
		Message     string `json:"message"`
	}
	json.NewDecoder(resp.Body).Decode(&cr)
	prefixed := fmt.Sprintf("\x19Ethereum Signed Message:\n%d%s", len(cr.Message), cr.Message)
	hash := crypto.Keccak256([]byte(prefixed))
	sig, err := crypto.Sign(hash, key)
	require.NoError(t, err)
	sig[64] += 27
	resp2, err := http.Post(e2eBase+"/api/v1/auth/login", "application/json",
		bytes.NewReader([]byte(fmt.Sprintf(`{"address":"%s","challenge_id":"%s","signature":"0x%s"}`,
			e2eAnvilAddr, cr.ChallengeID, common.Bytes2Hex(sig)))))
	require.NoError(t, err)
	defer resp2.Body.Close()
	var lr struct {
		Token string `json:"token"`
	}
	json.NewDecoder(resp2.Body).Decode(&lr)
	require.NotEmpty(t, lr.Token)
	return lr.Token
}

func e2eKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	k, err := crypto.HexToECDSA("ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	require.NoError(t, err)
	return k
}

func TestE2EAuthLogoutVerifyWorkflow(t *testing.T) {
	token := e2eLogin(t)
	for name, tc := range map[string]struct {
		auth string
		code int
	}{
		"Valid":   {"Bearer " + token, 200},
		"Missing": {"", 401},
		"Invalid": {"Bearer invalid", 401},
	} {
		t.Run("Verify_"+name, func(t *testing.T) {
			req, _ := http.NewRequest("POST", e2eBase+"/api/v1/auth/verify", http.NoBody)
			if tc.auth != "" {
				req.Header.Set("Authorization", tc.auth)
			}
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()
			assert.Equal(t, tc.code, resp.StatusCode)
		})
	}
}

func TestE2EAuthNFTStreamingWorkflow(t *testing.T) {
	token := e2eLogin(t)
	require.NotEmpty(t, token)

	t.Run("ContentRequiresAuth", func(t *testing.T) {
		resp, err := http.Get(e2eBase + "/api/v1/content")
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, 401, resp.StatusCode)
	})

	t.Run("ContentWithAuth", func(t *testing.T) {
		req, _ := http.NewRequest("GET", e2eBase+"/api/v1/content", http.NoBody)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		t.Logf("Content response: %s", string(body))
	})
}
