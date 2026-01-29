package security

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

// SecurityPolicy defines security policies
type SecurityPolicy struct {
	ID                    string
	Name                  string
	Description           string
	PasswordMinLength     int
	PasswordRequireUpper  bool
	PasswordRequireNumber bool
	PasswordRequireSymbol bool
	SessionTimeout        time.Duration
	MaxLoginAttempts      int
	LockoutDuration       time.Duration
	EnableMFA             bool
	EnableAuditLogging    bool
}

// SecurityHardening provides security hardening utilities
type SecurityHardening struct {
	policy              SecurityPolicy
	failedAttempts      map[string]int
	lockedAccounts      map[string]time.Time
	mu                  sync.RWMutex
	inputValidators     map[string]*regexp.Regexp
	outputEncoders      map[string]func(string) string
}

// NewSecurityHardening creates a new security hardening instance
func NewSecurityHardening(policy SecurityPolicy) *SecurityHardening {
	if policy.PasswordMinLength == 0 {
		policy.PasswordMinLength = 12
	}
	if policy.SessionTimeout == 0 {
		policy.SessionTimeout = 30 * time.Minute
	}
	if policy.MaxLoginAttempts == 0 {
		policy.MaxLoginAttempts = 5
	}
	if policy.LockoutDuration == 0 {
		policy.LockoutDuration = 15 * time.Minute
	}

	sh := &SecurityHardening{
		policy:          policy,
		failedAttempts:  make(map[string]int),
		lockedAccounts:  make(map[string]time.Time),
		inputValidators: make(map[string]*regexp.Regexp),
		outputEncoders:  make(map[string]func(string) string),
	}

	sh.initializeValidators()
	sh.initializeEncoders()

	return sh
}

// ValidatePassword validates a password against the security policy
func (sh *SecurityHardening) ValidatePassword(password string) error {
	if len(password) < sh.policy.PasswordMinLength {
		return fmt.Errorf("password must be at least %d characters", sh.policy.PasswordMinLength)
	}

	if sh.policy.PasswordRequireUpper {
		if !regexp.MustCompile(`[A-Z]`).MatchString(password) {
			return fmt.Errorf("password must contain at least one uppercase letter")
		}
	}

	if sh.policy.PasswordRequireNumber {
		if !regexp.MustCompile(`[0-9]`).MatchString(password) {
			return fmt.Errorf("password must contain at least one number")
		}
	}

	if sh.policy.PasswordRequireSymbol {
		if !regexp.MustCompile(`[!@#$%^&*()_+\-=\[\]{};':"\\|,.<>\/?]`).MatchString(password) {
			return fmt.Errorf("password must contain at least one special character")
		}
	}

	return nil
}

// ValidateInput validates input against a validator
func (sh *SecurityHardening) ValidateInput(inputType, value string) error {
	sh.mu.RLock()
	validator, exists := sh.inputValidators[inputType]
	sh.mu.RUnlock()

	if !exists {
		return fmt.Errorf("validator not found: %s", inputType)
	}

	if !validator.MatchString(value) {
		return fmt.Errorf("invalid %s: %s", inputType, value)
	}

	return nil
}

// EncodeOutput encodes output for a specific context
func (sh *SecurityHardening) EncodeOutput(context, value string) (string, error) {
	sh.mu.RLock()
	encoder, exists := sh.outputEncoders[context]
	sh.mu.RUnlock()

	if !exists {
		return "", fmt.Errorf("encoder not found: %s", context)
	}

	return encoder(value), nil
}

// RecordFailedLogin records a failed login attempt
func (sh *SecurityHardening) RecordFailedLogin(username string) error {
	sh.mu.Lock()
	defer sh.mu.Unlock()

	sh.failedAttempts[username]++

	if sh.failedAttempts[username] >= sh.policy.MaxLoginAttempts {
		sh.lockedAccounts[username] = time.Now().Add(sh.policy.LockoutDuration)
		return fmt.Errorf("account locked due to too many failed login attempts")
	}

	return nil
}

// RecordSuccessfulLogin records a successful login
func (sh *SecurityHardening) RecordSuccessfulLogin(username string) {
	sh.mu.Lock()
	defer sh.mu.Unlock()

	sh.failedAttempts[username] = 0
	delete(sh.lockedAccounts, username)
}

// IsAccountLocked checks if an account is locked
func (sh *SecurityHardening) IsAccountLocked(username string) bool {
	sh.mu.RLock()
	defer sh.mu.RUnlock()

	lockTime, exists := sh.lockedAccounts[username]
	if !exists {
		return false
	}

	if time.Now().After(lockTime) {
		return false
	}

	return true
}

// GetFailedLoginCount returns the number of failed login attempts
func (sh *SecurityHardening) GetFailedLoginCount(username string) int {
	sh.mu.RLock()
	defer sh.mu.RUnlock()

	return sh.failedAttempts[username]
}

// GetLockoutTime returns the lockout time for an account
func (sh *SecurityHardening) GetLockoutTime(username string) time.Time {
	sh.mu.RLock()
	defer sh.mu.RUnlock()

	return sh.lockedAccounts[username]
}

// ResetFailedAttempts resets failed login attempts for an account
func (sh *SecurityHardening) ResetFailedAttempts(username string) {
	sh.mu.Lock()
	defer sh.mu.Unlock()

	sh.failedAttempts[username] = 0
	delete(sh.lockedAccounts, username)
}

// initializeValidators initializes input validators
func (sh *SecurityHardening) initializeValidators() {
	sh.inputValidators["email"] = regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
	sh.inputValidators["username"] = regexp.MustCompile(`^[a-zA-Z0-9_-]{3,32}$`)
	sh.inputValidators["url"] = regexp.MustCompile(`^https?://[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`)
	sh.inputValidators["ipv4"] = regexp.MustCompile(`^(\d{1,3}\.){3}\d{1,3}$`)
	sh.inputValidators["uuid"] = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
}

// initializeEncoders initializes output encoders
func (sh *SecurityHardening) initializeEncoders() {
	// HTML encoding
	sh.outputEncoders["html"] = func(s string) string {
		return strings.NewReplacer(
			"&", "&amp;",
			"<", "&lt;",
			">", "&gt;",
			"\"", "&quot;",
			"'", "&#x27;",
		).Replace(s)
	}

	// URL encoding
	sh.outputEncoders["url"] = func(s string) string {
		return strings.NewReplacer(
			" ", "%20",
			"&", "%26",
			"=", "%3D",
			"?", "%3F",
			"#", "%23",
		).Replace(s)
	}

	// JSON encoding
	sh.outputEncoders["json"] = func(s string) string {
		return strings.NewReplacer(
			"\"", "\\\"",
			"\\", "\\\\",
			"\n", "\\n",
			"\r", "\\r",
			"\t", "\\t",
		).Replace(s)
	}

	// SQL encoding (basic)
	sh.outputEncoders["sql"] = func(s string) string {
		return strings.ReplaceAll(s, "'", "''")
	}
}

// AddValidator adds a custom input validator
func (sh *SecurityHardening) AddValidator(name string, pattern string) error {
	regex, err := regexp.Compile(pattern)
	if err != nil {
		return fmt.Errorf("invalid regex pattern: %w", err)
	}

	sh.mu.Lock()
	defer sh.mu.Unlock()

	sh.inputValidators[name] = regex
	return nil
}

// AddEncoder adds a custom output encoder
func (sh *SecurityHardening) AddEncoder(name string, encoder func(string) string) {
	sh.mu.Lock()
	defer sh.mu.Unlock()

	sh.outputEncoders[name] = encoder
}

// GetPolicy returns the security policy
func (sh *SecurityHardening) GetPolicy() SecurityPolicy {
	return sh.policy
}

// UpdatePolicy updates the security policy
func (sh *SecurityHardening) UpdatePolicy(policy SecurityPolicy) {
	sh.mu.Lock()
	defer sh.mu.Unlock()

	sh.policy = policy
}

// GetLockedAccountCount returns the number of locked accounts
func (sh *SecurityHardening) GetLockedAccountCount() int {
	sh.mu.RLock()
	defer sh.mu.RUnlock()

	count := 0
	for _, lockTime := range sh.lockedAccounts {
		if time.Now().Before(lockTime) {
			count++
		}
	}
	return count
}

// GetFailedAttemptCount returns the total number of failed attempts
func (sh *SecurityHardening) GetFailedAttemptCount() int {
	sh.mu.RLock()
	defer sh.mu.RUnlock()

	count := 0
	for _, attempts := range sh.failedAttempts {
		count += attempts
	}
	return count
}
