package service

import (
	"testing"
)

func FuzzParseJWT(f *testing.F) {
	f.Add("eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwidXNlcm5hbWUiOiJ0ZXN0In0.fake_signature")
	f.Add("")
	f.Add("not-a-jwt")
	f.Add("eyJhbGciOiJIUzI1NiJ9.invalid.payload")
	f.Add("aaaa.bbbb.cccc")

	verifier := NewJWTVerifier("test-secret-that-is-at-least-32-chars")

	f.Fuzz(func(t *testing.T, token string) {
		_, _ = verifier.ParseToken(token)
	})
}

func FuzzParseJWTShortSecret(f *testing.F) {
	f.Add("eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.payload.sig")
	f.Add("")
	f.Add("a.b.c")

	f.Fuzz(func(t *testing.T, token string) {
		func() {
			defer func() { _ = recover() }()
			v := NewJWTVerifier("short")
			_, _ = v.ParseToken(token)
		}()
	})
}
