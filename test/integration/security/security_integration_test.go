package security_test

import (
	"testing"
	"time"

	"streamgate/pkg/security"
)

func TestSecurityIntegration_EncryptionWithKeyManager(t *testing.T) {
	// Setup
	encryptor := security.NewEncryptor(security.EncryptionConfig{})
	km := security.NewKeyManager(24*time.Hour, 32)

	// Generate key
	keyID, err := km.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	// Get key
	key, err := km.GetKey(keyID)
	if err != nil {
		t.Fatalf("GetKey failed: %v", err)
	}

	// Encrypt data
	plaintext := "Sensitive data"
	encrypted, err := encryptor.Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	// Decrypt data
	decrypted, err := encryptor.Decrypt(encrypted, key)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if decrypted != plaintext {
		t.Errorf("Decrypted text doesn't match. Expected %s, got %s", plaintext, decrypted)
	}
}

func TestSecurityIntegration_KeyRotationWithEncryption(t *testing.T) {
	// Setup
	encryptor := security.NewEncryptor(security.EncryptionConfig{})
	km := security.NewKeyManager(24*time.Hour, 32)

	// Generate initial key and encrypt
	keyID1, _ := km.GenerateKey()
	key1, _ := km.GetKey(keyID1)
	plaintext := "Important data"
	encrypted1, _ := encryptor.Encrypt(plaintext, key1)

	// Rotate key
	keyID2, _ := km.RotateKey()
	key2, _ := km.GetKey(keyID2)

	// Old data should still decrypt with old key
	decrypted1, err := encryptor.Decrypt(encrypted1, key1)
	if err != nil {
		t.Fatalf("Decrypt with old key failed: %v", err)
	}

	if decrypted1 != plaintext {
		t.Errorf("Decrypted text doesn't match")
	}

	// New data should encrypt with new key
	encrypted2, _ := encryptor.Encrypt(plaintext, key2)
	decrypted2, _ := encryptor.Decrypt(encrypted2, key2)

	if decrypted2 != plaintext {
		t.Errorf("Decrypted text doesn't match")
	}

	// Verify active key is the new one
	activeKeyID, _, _ := km.GetActiveKey()
	if activeKeyID != keyID2 {
		t.Errorf("Active key should be rotated key")
	}
}

func TestSecurityIntegration_ComplianceWithAuditLogging(t *testing.T) {
	// Setup
	cf := security.NewComplianceFramework()
	sh := security.NewSecurityHardening(security.SecurityPolicy{})

	// Register compliance checks
	for i := 1; i <= 3; i++ {
		check := &security.ComplianceCheck{
			ID:       "check-" + string(rune(i)),
			Standard: security.GDPR,
			Name:     "Check " + string(rune(i)),
			Status:   "PASS",
		}
		cf.RegisterCheck(check)
	}

	// Log audit events
	cf.LogAuditEvent("COMPLIANCE_CHECK", "gdpr-checks", "admin", "SUCCESS", "GDPR checks passed")

	// Generate compliance report
	report, err := cf.GenerateReport(security.GDPR)
	if err != nil {
		t.Fatalf("GenerateReport failed: %v", err)
	}

	if report.Status != "COMPLIANT" {
		t.Errorf("Expected COMPLIANT status, got %s", report.Status)
	}

	// Verify audit log
	logs := cf.GetAuditLog(10)
	if len(logs) != 1 {
		t.Errorf("Expected 1 audit log entry, got %d", len(logs))
	}
}

func TestSecurityIntegration_PasswordValidationWithHardening(t *testing.T) {
	// Setup
	policy := security.SecurityPolicy{
		PasswordMinLength:     12,
		PasswordRequireUpper:  true,
		PasswordRequireNumber: true,
		PasswordRequireSymbol: true,
		MaxLoginAttempts:      3,
		LockoutDuration:       1 * time.Second,
	}

	sh := security.NewSecurityHardening(policy)
	encryptor := security.NewEncryptor(security.EncryptionConfig{})

	// Validate password
	validPassword := "SecurePass123!"
	err := sh.ValidatePassword(validPassword)
	if err != nil {
		t.Fatalf("ValidatePassword failed: %v", err)
	}

	// Hash password
	hash := encryptor.HashPassword(validPassword)

	// Verify password
	if !encryptor.VerifyPassword(validPassword, hash) {
		t.Error("Password verification failed")
	}

	// Test failed login attempts
	username := "user@example.com"
	for i := 0; i < 3; i++ {
		sh.RecordFailedLogin(username)
	}

	if !sh.IsAccountLocked(username) {
		t.Error("Account should be locked")
	}

	// Wait for lockout to expire
	time.Sleep(1100 * time.Millisecond)

	if sh.IsAccountLocked(username) {
		t.Error("Account should be unlocked after lockout duration")
	}
}

func TestSecurityIntegration_InputValidationWithOutputEncoding(t *testing.T) {
	// Setup
	sh := security.NewSecurityHardening(security.SecurityPolicy{})

	// Validate email input
	email := "user@example.com"
	err := sh.ValidateInput("email", email)
	if err != nil {
		t.Fatalf("ValidateInput failed: %v", err)
	}

	// Encode for HTML output
	htmlInput := "<script>alert('XSS')</script>"
	encoded, err := sh.EncodeOutput("html", htmlInput)
	if err != nil {
		t.Fatalf("EncodeOutput failed: %v", err)
	}

	if encoded == htmlInput {
		t.Error("HTML encoding didn't encode special characters")
	}

	// Validate URL
	url := "https://example.com"
	err = sh.ValidateInput("url", url)
	if err != nil {
		t.Fatalf("ValidateInput failed for URL: %v", err)
	}
}

func TestSecurityIntegration_FullSecurityStack(t *testing.T) {
	// Setup all security components
	encryptor := security.NewEncryptor(security.EncryptionConfig{})
	km := security.NewKeyManager(24*time.Hour, 32)
	cf := security.NewComplianceFramework()
	policy := security.SecurityPolicy{
		PasswordMinLength:     12,
		PasswordRequireUpper:  true,
		PasswordRequireNumber: true,
		PasswordRequireSymbol: true,
	}
	sh := security.NewSecurityHardening(policy)

	// 1. Validate user input
	username := "user@example.com"
	password := "SecurePass123!"

	err := sh.ValidateInput("email", username)
	if err != nil {
		t.Fatalf("Email validation failed: %v", err)
	}

	err = sh.ValidatePassword(password)
	if err != nil {
		t.Fatalf("Password validation failed: %v", err)
	}

	// 2. Hash password
	passwordHash := encryptor.HashPassword(password)

	// 3. Generate encryption key
	keyID, _ := km.GenerateKey()
	key, _ := km.GetKey(keyID)

	// 4. Encrypt sensitive data
	sensitiveData := "User profile data"
	encrypted, _ := encryptor.Encrypt(sensitiveData, key)

	// 5. Log audit event
	cf.LogAuditEvent("USER_REGISTRATION", username, "system", "SUCCESS", "User registered")

	// 6. Register compliance check
	check := &security.ComplianceCheck{
		ID:       "user-data-encryption",
		Standard: security.GDPR,
		Name:     "User Data Encryption",
		Status:   "PASS",
	}
	cf.RegisterCheck(check)

	// 7. Generate compliance report
	report, _ := cf.GenerateReport(security.GDPR)

	// Verify all components worked together
	if report.Status != "COMPLIANT" {
		t.Error("Compliance report should be compliant")
	}

	if cf.GetAuditLogCount() != 1 {
		t.Error("Audit log should have 1 entry")
	}

	// Verify encryption
	decrypted, _ := encryptor.Decrypt(encrypted, key)
	if decrypted != sensitiveData {
		t.Error("Decrypted data doesn't match")
	}

	// Verify password
	if !encryptor.VerifyPassword(password, passwordHash) {
		t.Error("Password verification failed")
	}
}

func TestSecurityIntegration_MultipleUsersWithCompliance(t *testing.T) {
	// Setup
	cf := security.NewComplianceFramework()
	sh := security.NewSecurityHardening(security.SecurityPolicy{
		MaxLoginAttempts: 3,
		LockoutDuration:  1 * time.Second,
	})

	// Simulate multiple users
	users := []string{"user1@example.com", "user2@example.com", "user3@example.com"}

	for _, user := range users {
		// Validate email
		err := sh.ValidateInput("email", user)
		if err != nil {
			t.Fatalf("Email validation failed for %s: %v", user, err)
		}

		// Log audit event
		cf.LogAuditEvent("LOGIN", user, user, "SUCCESS", "User logged in")
	}

	// Verify audit logs
	if cf.GetAuditLogCount() != 3 {
		t.Errorf("Expected 3 audit log entries, got %d", cf.GetAuditLogCount())
	}

	// Test failed login for one user
	failedUser := users[0]
	for i := 0; i < 3; i++ {
		sh.RecordFailedLogin(failedUser)
		cf.LogAuditEvent("LOGIN_FAILED", failedUser, failedUser, "FAILURE", "Failed login attempt")
	}

	if !sh.IsAccountLocked(failedUser) {
		t.Error("Account should be locked")
	}

	// Verify audit logs for failed user
	failedLogs := cf.GetAuditLogByUser(failedUser, 10)
	if len(failedLogs) != 4 { // 1 successful + 3 failed
		t.Errorf("Expected 4 audit log entries for failed user, got %d", len(failedLogs))
	}
}

func BenchmarkSecurityIntegration_EncryptDecrypt(b *testing.B) {
	encryptor := security.NewEncryptor(security.EncryptionConfig{})
	key, _ := encryptor.GenerateKey()
	plaintext := "Test data"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		encrypted, _ := encryptor.Encrypt(plaintext, key)
		encryptor.Decrypt(encrypted, key)
	}
}

func BenchmarkSecurityIntegration_KeyRotation(b *testing.B) {
	km := security.NewKeyManager(24*time.Hour, 32)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		km.RotateKey()
	}
}

func BenchmarkSecurityIntegration_ComplianceReporting(b *testing.B) {
	cf := security.NewComplianceFramework()

	for i := 1; i <= 10; i++ {
		check := &security.ComplianceCheck{
			ID:       "check-" + string(rune(i)),
			Standard: security.GDPR,
			Name:     "Check " + string(rune(i)),
			Status:   "PASS",
		}
		cf.RegisterCheck(check)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cf.GenerateReport(security.GDPR)
	}
}
