package e2e_test

import (
	"testing"
	"time"

	"streamgate/pkg/security"
)

func TestSecurityE2E_UserRegistrationFlow(t *testing.T) {
	// Initialize security components
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

	// Step 1: User provides credentials
	email := "newuser@example.com"
	password := "SecurePass123!"

	// Step 2: Validate email format
	err := sh.ValidateInput("email", email)
	if err != nil {
		t.Fatalf("Email validation failed: %v", err)
	}

	// Step 3: Validate password strength
	err = sh.ValidatePassword(password)
	if err != nil {
		t.Fatalf("Password validation failed: %v", err)
	}

	// Step 4: Hash password
	_ = encryptor.HashPassword(password)

	// Step 5: Generate encryption key for user data
	keyID, _ := km.GenerateKey()
	key, _ := km.GetKey(keyID)

	// Step 6: Encrypt user profile data
	profileData := `{"name":"John Doe","email":"newuser@example.com"}`
	encrypted, _ := encryptor.Encrypt(profileData, key)

	// Step 7: Log audit event
	cf.LogAuditEvent("USER_REGISTRATION", email, "system", "SUCCESS", "User registered successfully")

	// Step 8: Register compliance check
	check := &security.ComplianceCheck{
		ID:       "user-data-encryption-" + email,
		Standard: security.GDPR,
		Name:     "User Data Encryption",
		Status:   "PASS",
	}
	cf.RegisterCheck(check)

	// Verify registration completed successfully
	if cf.GetAuditLogCount() != 1 {
		t.Error("Audit log should have registration event")
	}

	// Verify data can be decrypted
	decrypted, _ := encryptor.Decrypt(encrypted, key)
	if decrypted != profileData {
		t.Error("Decrypted profile data doesn't match")
	}
}

func TestSecurityE2E_LoginFlow(t *testing.T) {
	// Initialize security components
	encryptor := security.NewEncryptor(security.EncryptionConfig{})
	cf := security.NewComplianceFramework()
	policy := security.SecurityPolicy{
		MaxLoginAttempts: 3,
		LockoutDuration:  1 * time.Second,
	}
	sh := security.NewSecurityHardening(policy)

	// Setup: User exists with password hash
	email := "user@example.com"
	password := "SecurePass123!"
	passwordHash := encryptor.HashPassword(password)

	// Scenario 1: Successful login
	if !encryptor.VerifyPassword(password, passwordHash) {
		t.Error("Password verification failed")
	}

	sh.RecordSuccessfulLogin(email)
	cf.LogAuditEvent("LOGIN", email, email, "SUCCESS", "User logged in")

	if cf.GetAuditLogCount() != 1 {
		t.Error("Audit log should have login event")
	}

	// Scenario 2: Failed login attempts
	wrongPassword := "WrongPass123!"
	if encryptor.VerifyPassword(wrongPassword, passwordHash) {
		t.Error("Wrong password should not verify")
	}

	for i := 0; i < 3; i++ {
		sh.RecordFailedLogin(email)
		cf.LogAuditEvent("LOGIN_FAILED", email, email, "FAILURE", "Failed login attempt")
	}

	if !sh.IsAccountLocked(email) {
		t.Error("Account should be locked after failed attempts")
	}

	if cf.GetAuditLogCount() != 4 { // 1 successful + 3 failed
		t.Errorf("Expected 4 audit log entries, got %d", cf.GetAuditLogCount())
	}

	// Scenario 3: Account unlock after lockout duration
	time.Sleep(1100 * time.Millisecond)

	if sh.IsAccountLocked(email) {
		t.Error("Account should be unlocked after lockout duration")
	}
}

func TestSecurityE2E_DataEncryptionFlow(t *testing.T) {
	// Initialize security components
	encryptor := security.NewEncryptor(security.EncryptionConfig{})
	km := security.NewKeyManager(24*time.Hour, 32)
	cf := security.NewComplianceFramework()

	// Step 1: Generate encryption key
	keyID, _ := km.GenerateKey()
	key, _ := km.GetKey(keyID)

	// Step 2: Encrypt multiple data items
	dataItems := []string{
		"Sensitive document 1",
		"Sensitive document 2",
		"Sensitive document 3",
	}

	encryptedItems := make([]*security.EncryptedData, len(dataItems))
	for i, data := range dataItems {
		encrypted, _ := encryptor.Encrypt(data, key)
		encryptedItems[i] = encrypted
		cf.LogAuditEvent("DATA_ENCRYPTED", "document-"+string(rune(i)), "system", "SUCCESS", "Data encrypted")
	}

	// Step 3: Verify all items can be decrypted
	for i, encrypted := range encryptedItems {
		decrypted, _ := encryptor.Decrypt(encrypted, key)
		if decrypted != dataItems[i] {
			t.Errorf("Decrypted data doesn't match for item %d", i)
		}
	}

	// Step 4: Verify audit trail
	if cf.GetAuditLogCount() != 3 {
		t.Errorf("Expected 3 audit log entries, got %d", cf.GetAuditLogCount())
	}

	// Step 5: Rotate key
	newKeyID, _ := km.RotateKey()
	newKey, _ := km.GetKey(newKeyID)

	// Step 6: Verify old data still decrypts with old key
	for i, encrypted := range encryptedItems {
		decrypted, _ := encryptor.Decrypt(encrypted, key)
		if decrypted != dataItems[i] {
			t.Errorf("Old data doesn't decrypt with old key for item %d", i)
		}
	}

	// Step 7: Encrypt new data with new key
	newData := "New sensitive data"
	newEncrypted, _ := encryptor.Encrypt(newData, newKey)
	newDecrypted, _ := encryptor.Decrypt(newEncrypted, newKey)

	if newDecrypted != newData {
		t.Error("New data doesn't decrypt with new key")
	}

	// Step 8: Verify key rotation in audit log
	cf.LogAuditEvent("KEY_ROTATED", "encryption-key", "system", "SUCCESS", "Encryption key rotated")

	if cf.GetAuditLogCount() != 4 {
		t.Errorf("Expected 4 audit log entries, got %d", cf.GetAuditLogCount())
	}
}

func TestSecurityE2E_ComplianceAuditFlow(t *testing.T) {
	// Initialize compliance framework
	cf := security.NewComplianceFramework()

	// Step 1: Register compliance checks for GDPR
	gdprChecks := []string{
		"Data Encryption",
		"Access Control",
		"Data Retention",
		"User Consent",
	}

	for i, checkName := range gdprChecks {
		check := &security.ComplianceCheck{
			ID:       "gdpr-check-" + string(rune(i)),
			Standard: security.GDPR,
			Name:     checkName,
			Status:   "PASS",
		}
		cf.RegisterCheck(check)
	}

	// Step 2: Log compliance audit events
	for i, checkName := range gdprChecks {
		cf.LogAuditEvent("COMPLIANCE_CHECK", "gdpr-"+string(rune(i)), "auditor", "SUCCESS", checkName+" verified")
	}

	// Step 3: Generate compliance report
	report, _ := cf.GenerateReport(security.GDPR)

	if report.Status != "COMPLIANT" {
		t.Errorf("Expected COMPLIANT status, got %s", report.Status)
	}

	if report.Score != 100 {
		t.Errorf("Expected score 100, got %f", report.Score)
	}

	// Step 4: Verify audit trail
	auditLogs := cf.GetAuditLog(10)
	if len(auditLogs) != 4 {
		t.Errorf("Expected 4 audit log entries, got %d", len(auditLogs))
	}

	// Step 5: Register HIPAA checks
	hipaaChecks := []string{
		"PHI Encryption",
		"Access Logging",
		"Data Integrity",
	}

	for i, checkName := range hipaaChecks {
		check := &security.ComplianceCheck{
			ID:       "hipaa-check-" + string(rune(i)),
			Standard: security.HIPAA,
			Name:     checkName,
			Status:   "PASS",
		}
		cf.RegisterCheck(check)
	}

	// Step 6: Generate HIPAA report
	hipaaReport, _ := cf.GenerateReport(security.HIPAA)

	if hipaaReport.Status != "COMPLIANT" {
		t.Errorf("Expected COMPLIANT status for HIPAA, got %s", hipaaReport.Status)
	}

	// Step 7: Verify compliance status
	status := cf.GetComplianceStatus()
	if status[security.GDPR] != "COMPLIANT" {
		t.Errorf("Expected GDPR COMPLIANT, got %s", status[security.GDPR])
	}

	if status[security.HIPAA] != "COMPLIANT" {
		t.Errorf("Expected HIPAA COMPLIANT, got %s", status[security.HIPAA])
	}
}

func TestSecurityE2E_InputValidationAndOutputEncoding(t *testing.T) {
	// Initialize security hardening
	sh := security.NewSecurityHardening(security.SecurityPolicy{})

	// Step 1: Validate various inputs
	testCases := []struct {
		inputType string
		value     string
		valid     bool
	}{
		{"email", "user@example.com", true},
		{"email", "invalid-email", false},
		{"username", "valid_user123", true},
		{"username", "ab", false},
		{"url", "https://example.com", true},
		{"url", "not-a-url", false},
		{"ipv4", "192.168.1.1", true},
		{"ipv4", "999.999.999.999", false},
	}

	for _, tc := range testCases {
		err := sh.ValidateInput(tc.inputType, tc.value)
		if tc.valid && err != nil {
			t.Errorf("Expected valid %s, got error: %v", tc.inputType, err)
		}
		if !tc.valid && err == nil {
			t.Errorf("Expected invalid %s, got no error", tc.inputType)
		}
	}

	// Step 2: Encode outputs for different contexts
	xssPayload := "<script>alert('XSS')</script>"

	htmlEncoded, _ := sh.EncodeOutput("html", xssPayload)
	if htmlEncoded == xssPayload {
		t.Error("HTML encoding didn't encode XSS payload")
	}

	jsonEncoded, _ := sh.EncodeOutput("json", xssPayload)
	if jsonEncoded == xssPayload {
		t.Error("JSON encoding didn't encode XSS payload")
	}

	// Step 3: Verify encoded outputs are safe
	if htmlEncoded != "&lt;script&gt;alert(&#x27;XSS&#x27;)&lt;/script&gt;" {
		t.Errorf("HTML encoding incorrect: %s", htmlEncoded)
	}
}

func TestSecurityE2E_FullSecurityStackIntegration(t *testing.T) {
	// Initialize all security components
	encryptor := security.NewEncryptor(security.EncryptionConfig{})
	km := security.NewKeyManager(24*time.Hour, 32)
	cf := security.NewComplianceFramework()
	policy := security.SecurityPolicy{
		PasswordMinLength:     12,
		PasswordRequireUpper:  true,
		PasswordRequireNumber: true,
		PasswordRequireSymbol: true,
		MaxLoginAttempts:      3,
		LockoutDuration:       1 * time.Second,
	}
	sh := security.NewSecurityHardening(policy)

	// Simulate complete user lifecycle
	users := []struct {
		email    string
		password string
	}{
		{"alice@example.com", "AlicePass123!"},
		{"bob@example.com", "BobPass456!"},
		{"charlie@example.com", "CharliePass789!"},
	}

	// Step 1: Register users
	for _, user := range users {
		// Validate inputs
		sh.ValidateInput("email", user.email)
		sh.ValidatePassword(user.password)

		// Hash password
		encryptor.HashPassword(user.password)

		// Generate encryption key
		km.GenerateKey()

		// Log audit event
		cf.LogAuditEvent("USER_REGISTRATION", user.email, "system", "SUCCESS", "User registered")
	}

	// Step 2: Users login
	for _, user := range users {
		sh.RecordSuccessfulLogin(user.email)
		cf.LogAuditEvent("LOGIN", user.email, user.email, "SUCCESS", "User logged in")
	}

	// Step 3: Encrypt user data
	keyID, _ := km.GenerateKey()
	key, _ := km.GetKey(keyID)

	for _, user := range users {
		userData := `{"email":"` + user.email + `","status":"active"}`
		encryptor.Encrypt(userData, key)
		cf.LogAuditEvent("DATA_ENCRYPTED", user.email, "system", "SUCCESS", "User data encrypted")
	}

	// Step 4: Register compliance checks
	for i := 1; i <= 5; i++ {
		check := &security.ComplianceCheck{
			ID:       "check-" + string(rune(i)),
			Standard: security.GDPR,
			Name:     "Check " + string(rune(i)),
			Status:   "PASS",
		}
		cf.RegisterCheck(check)
	}

	// Step 5: Generate compliance report
	report, _ := cf.GenerateReport(security.GDPR)

	// Verify all components worked together
	if cf.GetAuditLogCount() != 9 { // 3 registrations + 3 logins + 3 encryptions
		t.Errorf("Expected 9 audit log entries, got %d", cf.GetAuditLogCount())
	}

	if report.Status != "COMPLIANT" {
		t.Errorf("Expected COMPLIANT status, got %s", report.Status)
	}

	if km.GetKeyCount() != 4 { // 3 user keys + 1 data encryption key
		t.Errorf("Expected 4 keys, got %d", km.GetKeyCount())
	}
}

func BenchmarkSecurityE2E_UserRegistration(b *testing.B) {
	encryptor := security.NewEncryptor(security.EncryptionConfig{})
	km := security.NewKeyManager(24*time.Hour, 32)
	cf := security.NewComplianceFramework()
	sh := security.NewSecurityHardening(security.SecurityPolicy{})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		email := "user" + string(rune(i)) + "@example.com"
		password := "SecurePass123!"

		sh.ValidateInput("email", email)
		sh.ValidatePassword(password)
		encryptor.HashPassword(password)
		km.GenerateKey()
		cf.LogAuditEvent("USER_REGISTRATION", email, "system", "SUCCESS", "User registered")
	}
}

func BenchmarkSecurityE2E_LoginFlow(b *testing.B) {
	encryptor := security.NewEncryptor(security.EncryptionConfig{})
	sh := security.NewSecurityHardening(security.SecurityPolicy{})
	cf := security.NewComplianceFramework()

	password := "SecurePass123!"
	passwordHash := encryptor.HashPassword(password)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		email := "user@example.com"
		encryptor.VerifyPassword(password, passwordHash)
		sh.RecordSuccessfulLogin(email)
		cf.LogAuditEvent("LOGIN", email, email, "SUCCESS", "User logged in")
	}
}

func BenchmarkSecurityE2E_ComplianceReporting(b *testing.B) {
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
		cf.LogAuditEvent("COMPLIANCE_CHECK", "gdpr", "auditor", "SUCCESS", "Compliance check completed")
	}
}
