package security

import (
	"testing"
	"time"
)

func TestSecurityHardening_ValidatePassword_Valid(t *testing.T) {
	policy := SecurityPolicy{
		PasswordMinLength:     12,
		PasswordRequireUpper:  true,
		PasswordRequireNumber: true,
		PasswordRequireSymbol: true,
	}

	sh := NewSecurityHardening(policy)

	validPassword := "SecurePass123!"
	err := sh.ValidatePassword(validPassword)
	if err != nil {
		t.Fatalf("ValidatePassword failed for valid password: %v", err)
	}
}

func TestSecurityHardening_ValidatePassword_TooShort(t *testing.T) {
	policy := SecurityPolicy{
		PasswordMinLength: 12,
	}

	sh := NewSecurityHardening(policy)

	shortPassword := "Short1!"
	err := sh.ValidatePassword(shortPassword)
	if err == nil {
		t.Error("Expected error for short password")
	}
}

func TestSecurityHardening_ValidatePassword_NoUppercase(t *testing.T) {
	policy := SecurityPolicy{
		PasswordMinLength:    12,
		PasswordRequireUpper: true,
	}

	sh := NewSecurityHardening(policy)

	noUpperPassword := "securepass123!"
	err := sh.ValidatePassword(noUpperPassword)
	if err == nil {
		t.Error("Expected error for password without uppercase")
	}
}

func TestSecurityHardening_ValidatePassword_NoNumber(t *testing.T) {
	policy := SecurityPolicy{
		PasswordMinLength:     12,
		PasswordRequireNumber: true,
	}

	sh := NewSecurityHardening(policy)

	noNumberPassword := "SecurePassword!"
	err := sh.ValidatePassword(noNumberPassword)
	if err == nil {
		t.Error("Expected error for password without number")
	}
}

func TestSecurityHardening_ValidatePassword_NoSymbol(t *testing.T) {
	policy := SecurityPolicy{
		PasswordMinLength:     12,
		PasswordRequireSymbol: true,
	}

	sh := NewSecurityHardening(policy)

	noSymbolPassword := "SecurePass123"
	err := sh.ValidatePassword(noSymbolPassword)
	if err == nil {
		t.Error("Expected error for password without symbol")
	}
}

func TestSecurityHardening_ValidateInput_Email(t *testing.T) {
	sh := NewSecurityHardening(SecurityPolicy{})

	validEmail := "user@example.com"
	err := sh.ValidateInput("email", validEmail)
	if err != nil {
		t.Fatalf("ValidateInput failed for valid email: %v", err)
	}

	invalidEmail := "invalid-email"
	err = sh.ValidateInput("email", invalidEmail)
	if err == nil {
		t.Error("Expected error for invalid email")
	}
}

func TestSecurityHardening_ValidateInput_Username(t *testing.T) {
	sh := NewSecurityHardening(SecurityPolicy{})

	validUsername := "user_123"
	err := sh.ValidateInput("username", validUsername)
	if err != nil {
		t.Fatalf("ValidateInput failed for valid username: %v", err)
	}

	invalidUsername := "ab" // Too short
	err = sh.ValidateInput("username", invalidUsername)
	if err == nil {
		t.Error("Expected error for invalid username")
	}
}

func TestSecurityHardening_ValidateInput_URL(t *testing.T) {
	sh := NewSecurityHardening(SecurityPolicy{})

	validURL := "https://example.com"
	err := sh.ValidateInput("url", validURL)
	if err != nil {
		t.Fatalf("ValidateInput failed for valid URL: %v", err)
	}

	invalidURL := "not-a-url"
	err = sh.ValidateInput("url", invalidURL)
	if err == nil {
		t.Error("Expected error for invalid URL")
	}
}

func TestSecurityHardening_EncodeOutput_HTML(t *testing.T) {
	sh := NewSecurityHardening(SecurityPolicy{})

	input := "<script>alert('XSS')</script>"
	encoded, err := sh.EncodeOutput("html", input)
	if err != nil {
		t.Fatalf("EncodeOutput failed: %v", err)
	}

	if encoded == input {
		t.Error("HTML encoding didn't encode special characters")
	}

	if encoded != "&lt;script&gt;alert(&#x27;XSS&#x27;)&lt;/script&gt;" {
		t.Errorf("HTML encoding incorrect. Got: %s", encoded)
	}
}

func TestSecurityHardening_EncodeOutput_URL(t *testing.T) {
	sh := NewSecurityHardening(SecurityPolicy{})

	input := "hello world&key=value"
	encoded, err := sh.EncodeOutput("url", input)
	if err != nil {
		t.Fatalf("EncodeOutput failed: %v", err)
	}

	if encoded == input {
		t.Error("URL encoding didn't encode special characters")
	}
}

func TestSecurityHardening_EncodeOutput_JSON(t *testing.T) {
	sh := NewSecurityHardening(SecurityPolicy{})

	input := `{"key": "value"}`
	encoded, err := sh.EncodeOutput("json", input)
	if err != nil {
		t.Fatalf("EncodeOutput failed: %v", err)
	}

	if encoded == input {
		t.Error("JSON encoding didn't encode special characters")
	}
}

func TestSecurityHardening_RecordFailedLogin(t *testing.T) {
	policy := SecurityPolicy{
		MaxLoginAttempts: 3,
	}

	sh := NewSecurityHardening(policy)

	username := "user@example.com"

	// First two attempts should succeed
	err := sh.RecordFailedLogin(username)
	if err != nil {
		t.Fatalf("First failed login should not error: %v", err)
	}

	err = sh.RecordFailedLogin(username)
	if err != nil {
		t.Fatalf("Second failed login should not error: %v", err)
	}

	// Third attempt should lock account
	err = sh.RecordFailedLogin(username)
	if err == nil {
		t.Error("Expected error when max attempts exceeded")
	}
}

func TestSecurityHardening_RecordSuccessfulLogin(t *testing.T) {
	policy := SecurityPolicy{
		MaxLoginAttempts: 3,
	}

	sh := NewSecurityHardening(policy)

	username := "user@example.com"

	sh.RecordFailedLogin(username)
	sh.RecordFailedLogin(username)

	if sh.GetFailedLoginCount(username) != 2 {
		t.Errorf("Expected 2 failed attempts, got %d", sh.GetFailedLoginCount(username))
	}

	sh.RecordSuccessfulLogin(username)

	if sh.GetFailedLoginCount(username) != 0 {
		t.Errorf("Expected 0 failed attempts after successful login, got %d", sh.GetFailedLoginCount(username))
	}
}

func TestSecurityHardening_IsAccountLocked(t *testing.T) {
	policy := SecurityPolicy{
		MaxLoginAttempts: 2,
		LockoutDuration:  1 * time.Second,
	}

	sh := NewSecurityHardening(policy)

	username := "user@example.com"

	if sh.IsAccountLocked(username) {
		t.Error("Account should not be locked initially")
	}

	sh.RecordFailedLogin(username)
	sh.RecordFailedLogin(username)

	if !sh.IsAccountLocked(username) {
		t.Error("Account should be locked after max attempts")
	}

	time.Sleep(1100 * time.Millisecond)

	if sh.IsAccountLocked(username) {
		t.Error("Account should be unlocked after lockout duration")
	}
}

func TestSecurityHardening_GetFailedLoginCount(t *testing.T) {
	sh := NewSecurityHardening(SecurityPolicy{})

	username := "user@example.com"

	if sh.GetFailedLoginCount(username) != 0 {
		t.Errorf("Expected 0 failed attempts initially, got %d", sh.GetFailedLoginCount(username))
	}

	sh.RecordFailedLogin(username)
	if sh.GetFailedLoginCount(username) != 1 {
		t.Errorf("Expected 1 failed attempt, got %d", sh.GetFailedLoginCount(username))
	}

	sh.RecordFailedLogin(username)
	if sh.GetFailedLoginCount(username) != 2 {
		t.Errorf("Expected 2 failed attempts, got %d", sh.GetFailedLoginCount(username))
	}
}

func TestSecurityHardening_ResetFailedAttempts(t *testing.T) {
	policy := SecurityPolicy{
		MaxLoginAttempts: 3,
	}

	sh := NewSecurityHardening(policy)

	username := "user@example.com"

	sh.RecordFailedLogin(username)
	sh.RecordFailedLogin(username)

	if sh.GetFailedLoginCount(username) != 2 {
		t.Errorf("Expected 2 failed attempts, got %d", sh.GetFailedLoginCount(username))
	}

	sh.ResetFailedAttempts(username)

	if sh.GetFailedLoginCount(username) != 0 {
		t.Errorf("Expected 0 failed attempts after reset, got %d", sh.GetFailedLoginCount(username))
	}
}

func TestSecurityHardening_AddValidator(t *testing.T) {
	sh := NewSecurityHardening(SecurityPolicy{})

	err := sh.AddValidator("phone", `^\d{10}$`)
	if err != nil {
		t.Fatalf("AddValidator failed: %v", err)
	}

	err = sh.ValidateInput("phone", "1234567890")
	if err != nil {
		t.Fatalf("ValidateInput failed for valid phone: %v", err)
	}

	err = sh.ValidateInput("phone", "123")
	if err == nil {
		t.Error("Expected error for invalid phone")
	}
}

func TestSecurityHardening_AddEncoder(t *testing.T) {
	sh := NewSecurityHardening(SecurityPolicy{})

	sh.AddEncoder("custom", func(s string) string {
		return "ENCODED:" + s
	})

	encoded, err := sh.EncodeOutput("custom", "test")
	if err != nil {
		t.Fatalf("EncodeOutput failed: %v", err)
	}

	if encoded != "ENCODED:test" {
		t.Errorf("Expected 'ENCODED:test', got '%s'", encoded)
	}
}

func TestSecurityHardening_GetLockedAccountCount(t *testing.T) {
	policy := SecurityPolicy{
		MaxLoginAttempts: 2,
		LockoutDuration:  1 * time.Second,
	}

	sh := NewSecurityHardening(policy)

	for i := 1; i <= 3; i++ {
		username := "user" + string(rune(i))
		sh.RecordFailedLogin(username)
		sh.RecordFailedLogin(username)
	}

	if sh.GetLockedAccountCount() != 3 {
		t.Errorf("Expected 3 locked accounts, got %d", sh.GetLockedAccountCount())
	}
}

func TestSecurityHardening_GetFailedAttemptCount(t *testing.T) {
	sh := NewSecurityHardening(SecurityPolicy{})

	for i := 1; i <= 3; i++ {
		username := "user" + string(rune(i))
		sh.RecordFailedLogin(username)
		sh.RecordFailedLogin(username)
	}

	if sh.GetFailedAttemptCount() != 6 {
		t.Errorf("Expected 6 total failed attempts, got %d", sh.GetFailedAttemptCount())
	}
}

func BenchmarkSecurityHardening_ValidatePassword(b *testing.B) {
	sh := NewSecurityHardening(SecurityPolicy{})
	password := "SecurePass123!"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sh.ValidatePassword(password)
	}
}

func BenchmarkSecurityHardening_ValidateInput(b *testing.B) {
	sh := NewSecurityHardening(SecurityPolicy{})
	email := "user@example.com"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sh.ValidateInput("email", email)
	}
}

func BenchmarkSecurityHardening_EncodeOutput(b *testing.B) {
	sh := NewSecurityHardening(SecurityPolicy{})
	input := "<script>alert('XSS')</script>"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sh.EncodeOutput("html", input)
	}
}
