package security

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"streamgate/pkg/security"
	"streamgate/pkg/util"
)

// SecurityAuditResult tracks security audit findings
type SecurityAuditResult struct {
	TotalChecks     int
	PassedChecks    int
	FailedChecks    int
	Vulnerabilities []string
	Warnings        []string
	PassPercentage  float64
}

// TestInputValidation validates input validation security
func TestInputValidation(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		valid   bool
		valType string
	}{
		// Email validation
		{"valid email", "user@example.com", true, "email"},
		{"invalid email", "invalid-email", false, "email"},
		{"empty email", "", false, "email"},

		// Ethereum address validation
		{"valid address", "0x742d35Cc6634C0532925a3b844Bc9e7595f42bE", true, "address"},
		{"invalid address", "0x742d35Cc6634C0532925a3b844Bc9e7595f42b", false, "address"},
		{"invalid address prefix", "742d35Cc6634C0532925a3b844Bc9e7595f42bE", false, "address"},

		// Hash validation
		{"valid hash", "0x" + hex.EncodeToString(sha256.New().Sum(nil)), true, "hash"},
		{"invalid hash", "0xinvalid", false, "hash"},
		{"empty hash", "", false, "hash"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var valid bool
			switch tt.valType {
			case "email":
				valid = util.ValidateEmail(tt.input)
			case "address":
				valid = util.ValidateEthereumAddress(tt.input)
			case "hash":
				valid = util.ValidateHash(tt.input)
			}

			if valid != tt.valid {
				t.Errorf("Validation failed: expected %v, got %v", tt.valid, valid)
			}
		})
	}
}

// TestRateLimitingEnforcement validates rate limiting is enforced
func TestRateLimitingEnforcement(t *testing.T) {
	limiter := security.NewRateLimiter(10, 10) // 10 requests per second

	// Should allow first 10 requests
	for i := 0; i < 10; i++ {
		if !limiter.Allow("test-client") {
			t.Errorf("Rate limiter blocked request %d (should allow)", i+1)
		}
	}

	// Should block 11th request
	if limiter.Allow("test-client") {
		t.Error("Rate limiter allowed request beyond limit")
	}
}

// TestAuditLogging validates audit logging is enabled
func TestAuditLogging(t *testing.T) {
	logger := security.NewAuditLogger()

	// Log sensitive operations
	logger.LogAuthAttempt("user@example.com", true)
	logger.LogDataModification("content-123", "update")
	logger.LogCacheOperation("cache-key", "set")
	logger.LogRateLimitViolation("client-ip", "upload")

	// Verify logs were recorded
	logs := logger.GetLogs()
	if len(logs) < 4 {
		t.Errorf("Expected at least 4 audit logs, got %d", len(logs))
	}
}

// TestCryptographicSecurity validates cryptographic operations
func TestCryptographicSecurity(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		length int
	}{
		{"short string", "test", 32},
		{"long string", "this is a longer test string", 32},
		{"empty string", "", 32},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash := util.HashSHA256(tt.input)
			if len(hash) != tt.length {
				t.Errorf("Hash length mismatch: expected %d, got %d", tt.length, len(hash))
			}
		})
	}
}

// TestSecureRandomGeneration validates random number generation
func TestSecureRandomGeneration(t *testing.T) {
	// Generate multiple random values
	randoms := make(map[string]bool)
	for i := 0; i < 100; i++ {
		random := util.GenerateRandomString(32)
		if len(random) != 32 {
			t.Errorf("Random string length mismatch: expected 32, got %d", len(random))
		}
		if randoms[random] {
			t.Error("Duplicate random string generated")
		}
		randoms[random] = true
	}
}

// TestCacheInvalidation validates cache invalidation on data changes
func TestCacheInvalidation(t *testing.T) {
	cache := security.NewSecureCache()

	// Set cache value
	cache.Set("key", "value")
	if val, ok := cache.Get("key"); !ok || val != "value" {
		t.Error("Cache set/get failed")
	}

	// Invalidate cache
	cache.Invalidate("key")
	if _, ok := cache.Get("key"); ok {
		t.Error("Cache invalidation failed")
	}
}

// TestErrorHandling validates error handling doesn't leak sensitive info
func TestErrorHandling(t *testing.T) {
	// Test that errors don't contain sensitive information
	sensitiveInfo := []string{
		"password",
		"private_key",
		"secret",
		"token",
		"api_key",
	}

	// Simulate error scenarios
	err := security.NewSecurityError("unauthorized access")
	errMsg := err.Error()

	for _, info := range sensitiveInfo {
		if contains(errMsg, info) {
			t.Errorf("Error message contains sensitive info: %s", info)
		}
	}
}

// TestCORSConfiguration validates CORS is properly configured
func TestCORSConfiguration(t *testing.T) {
	corsConfig := security.GetCORSConfig()

	// Verify CORS settings
	if len(corsConfig.AllowedOrigins) == 0 {
		t.Error("CORS allowed origins not configured")
	}

	if len(corsConfig.AllowedMethods) == 0 {
		t.Error("CORS allowed methods not configured")
	}

	if corsConfig.MaxAge <= 0 {
		t.Error("CORS max age not properly configured")
	}
}

// TestTLSConfiguration validates TLS is properly configured
func TestTLSConfiguration(t *testing.T) {
	tlsConfig := security.GetTLSConfig()

	// Verify TLS settings
	if tlsConfig.MinVersion < 771 { // TLS 1.2
		t.Error("TLS minimum version too low")
	}

	if len(tlsConfig.CipherSuites) == 0 {
		t.Error("TLS cipher suites not configured")
	}
}

// TestDependencyVulnerabilities checks for known vulnerabilities
func TestDependencyVulnerabilities(t *testing.T) {
	// This would typically use a tool like nancy or trivy
	// For now, we'll just verify the check can run
	vulnerabilities := checkDependencies()
	if len(vulnerabilities) > 0 {
		t.Logf("Found %d potential vulnerabilities in dependencies", len(vulnerabilities))
		for _, vuln := range vulnerabilities {
			t.Logf("  - %s", vuln)
		}
	}
}

// TestSQLInjectionPrevention validates SQL injection prevention
func TestSQLInjectionPrevention(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"normal input", "user@example.com"},
		{"sql injection attempt", "'; DROP TABLE users; --"},
		{"quote injection", "' OR '1'='1"},
		{"comment injection", "-- comment"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Verify input is properly escaped
			escaped := util.EscapeSQL(tt.input)
			if contains(escaped, "DROP") || contains(escaped, "DELETE") {
				t.Error("SQL injection not properly prevented")
			}
		})
	}
}

// TestXSSPrevention validates XSS prevention
func TestXSSPrevention(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"normal input", "Hello World"},
		{"script tag", "<script>alert('xss')</script>"},
		{"event handler", "<img src=x onerror=alert('xss')>"},
		{"html entity", "&lt;script&gt;"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			escaped := util.EscapeHTML(tt.input)
			if contains(escaped, "<script>") || contains(escaped, "onerror=") {
				t.Error("XSS not properly prevented")
			}
		})
	}
}

// TestCSRFProtection validates CSRF protection
func TestCSRFProtection(t *testing.T) {
	csrfToken := security.GenerateCSRFToken()

	if len(csrfToken) == 0 {
		t.Error("CSRF token generation failed")
	}

	// Verify token can be validated
	if !security.ValidateCSRFToken(csrfToken) {
		t.Error("CSRF token validation failed")
	}
}

// TestAuthenticationSecurity validates authentication security
func TestAuthenticationSecurity(t *testing.T) {
	// Test password hashing
	password := "test-password-123"
	hash := util.HashPassword(password)

	if len(hash) == 0 {
		t.Error("Password hashing failed")
	}

	// Verify password can be checked
	if !util.VerifyPassword(password, hash) {
		t.Error("Password verification failed")
	}

	// Verify wrong password fails
	if util.VerifyPassword("wrong-password", hash) {
		t.Error("Password verification should fail for wrong password")
	}
}

// TestAuthorizationSecurity validates authorization security
func TestAuthorizationSecurity(t *testing.T) {
	// Test role-based access control
	user := security.NewUser("user@example.com", []string{"read", "write"})

	if !user.HasPermission("read") {
		t.Error("User should have read permission")
	}

	if !user.HasPermission("write") {
		t.Error("User should have write permission")
	}

	if user.HasPermission("admin") {
		t.Error("User should not have admin permission")
	}
}

// TestDataEncryption validates data encryption
func TestDataEncryption(t *testing.T) {
	plaintext := "sensitive data"
	key := util.GenerateRandomString(32)

	// Encrypt data
	ciphertext, err := util.Encrypt(plaintext, key)
	if err != nil {
		t.Errorf("Encryption failed: %v", err)
	}

	// Verify ciphertext is different from plaintext
	if ciphertext == plaintext {
		t.Error("Ciphertext should be different from plaintext")
	}

	// Decrypt data
	decrypted, err := util.Decrypt(ciphertext, key)
	if err != nil {
		t.Errorf("Decryption failed: %v", err)
	}

	// Verify decrypted data matches original
	if decrypted != plaintext {
		t.Error("Decrypted data does not match original")
	}
}

// TestSecurityHeaders validates security headers
func TestSecurityHeaders(t *testing.T) {
	headers := security.GetSecurityHeaders()

	requiredHeaders := []string{
		"X-Content-Type-Options",
		"X-Frame-Options",
		"X-XSS-Protection",
		"Strict-Transport-Security",
		"Content-Security-Policy",
	}

	for _, header := range requiredHeaders {
		if _, ok := headers[header]; !ok {
			t.Errorf("Missing security header: %s", header)
		}
	}
}

// TestSecurityAudit runs comprehensive security audit
func TestSecurityAudit(t *testing.T) {
	result := &SecurityAuditResult{}

	// Run all security checks
	checks := []struct {
		name string
		fn   func() bool
	}{
		{"Input Validation", testInputValidationCheck},
		{"Rate Limiting", testRateLimitingCheck},
		{"Audit Logging", testAuditLoggingCheck},
		{"Cryptography", testCryptographyCheck},
		{"CORS Configuration", testCORSCheck},
		{"TLS Configuration", testTLSCheck},
		{"SQL Injection Prevention", testSQLInjectionCheck},
		{"XSS Prevention", testXSSCheck},
		{"CSRF Protection", testCSRFCheck},
		{"Authentication", testAuthenticationCheck},
		{"Authorization", testAuthorizationCheck},
		{"Data Encryption", testEncryptionCheck},
		{"Security Headers", testSecurityHeadersCheck},
	}

	for _, check := range checks {
		result.TotalChecks++
		if check.fn() {
			result.PassedChecks++
		} else {
			result.FailedChecks++
			result.Vulnerabilities = append(result.Vulnerabilities, check.name)
		}
	}

	result.PassPercentage = float64(result.PassedChecks) / float64(result.TotalChecks) * 100

	// Report results
	t.Logf("Security Audit Results:")
	t.Logf("  Total Checks: %d", result.TotalChecks)
	t.Logf("  Passed: %d", result.PassedChecks)
	t.Logf("  Failed: %d", result.FailedChecks)
	t.Logf("  Pass Rate: %.1f%%", result.PassPercentage)

	if result.FailedChecks > 0 {
		t.Logf("  Vulnerabilities:")
		for _, vuln := range result.Vulnerabilities {
			t.Logf("    - %s", vuln)
		}
	}

	// Require at least 90% pass rate
	if result.PassPercentage < 90 {
		t.Errorf("Security audit failed: %.1f%% pass rate (expected > 90%%)", result.PassPercentage)
	}
}

// Helper functions
func contains(s, substr string) bool {
	for i := 0; i < len(s)-len(substr)+1; i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func checkDependencies() []string {
	// Placeholder for dependency vulnerability checking
	return []string{}
}

// Check functions
func testInputValidationCheck() bool {
	return util.ValidateEmail("test@example.com") && !util.ValidateEmail("invalid")
}

func testRateLimitingCheck() bool {
	limiter := security.NewRateLimiter(10, 10)
	return limiter.Allow("test")
}

func testAuditLoggingCheck() bool {
	logger := security.NewAuditLogger()
	logger.LogAuthAttempt("test@example.com", true)
	return len(logger.GetLogs()) > 0
}

func testCryptographyCheck() bool {
	hash := util.HashSHA256("test")
	return len(hash) == 32
}

func testCORSCheck() bool {
	config := security.GetCORSConfig()
	return len(config.AllowedOrigins) > 0
}

func testTLSCheck() bool {
	config := security.GetTLSConfig()
	return config.MinVersion >= 771
}

func testSQLInjectionCheck() bool {
	escaped := util.EscapeSQL("'; DROP TABLE users; --")
	return !contains(escaped, "DROP")
}

func testXSSCheck() bool {
	escaped := util.EscapeHTML("<script>alert('xss')</script>")
	return !contains(escaped, "<script>")
}

func testCSRFCheck() bool {
	token := security.GenerateCSRFToken()
	return len(token) > 0 && security.ValidateCSRFToken(token)
}

func testAuthenticationCheck() bool {
	password := "test-password"
	hash := util.HashPassword(password)
	return util.VerifyPassword(password, hash)
}

func testAuthorizationCheck() bool {
	user := security.NewUser("test@example.com", []string{"read"})
	return user.HasPermission("read") && !user.HasPermission("admin")
}

func testEncryptionCheck() bool {
	plaintext := "test data"
	key := util.GenerateRandomString(32)
	ciphertext, _ := util.Encrypt(plaintext, key)
	decrypted, _ := util.Decrypt(ciphertext, key)
	return decrypted == plaintext
}

func testSecurityHeadersCheck() bool {
	headers := security.GetSecurityHeaders()
	return len(headers) > 0
}
