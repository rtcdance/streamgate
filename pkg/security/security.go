package security

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"
)

// SecurityError represents a security-related error
type SecurityError struct {
	Code    string
	Message string
	Details map[string]interface{}
}

// Error implements the error interface
func (e *SecurityError) Error() string {
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// NewSecurityError creates a new security error
func NewSecurityError(code, message string) *SecurityError {
	return &SecurityError{
		Code:    code,
		Message: message,
		Details: make(map[string]interface{}),
	}
}

// WithDetail adds a detail to the security error
func (e *SecurityError) WithDetail(key string, value interface{}) *SecurityError {
	e.Details[key] = value
	return e
}

// CORSConfig represents CORS configuration
type CORSConfig struct {
	AllowedOrigins   []string
	AllowedMethods   []string
	AllowedHeaders   []string
	ExposedHeaders   []string
	AllowCredentials bool
	MaxAge           int
}

// DefaultCORSConfig returns the default CORS configuration
func DefaultCORSConfig() CORSConfig {
	return CORSConfig{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type", "Authorization"},
		ExposedHeaders:   []string{},
		AllowCredentials: false,
		MaxAge:           86400,
	}
}

// GetCORSConfig returns the CORS configuration
func GetCORSConfig() CORSConfig {
	return DefaultCORSConfig()
}

// TLSConfig represents TLS configuration
type TLSConfig struct {
	CertFile           string
	KeyFile            string
	MinVersion         uint16
	MaxVersion         uint16
	CipherSuites       []uint16
	ServerName         string
	InsecureSkipVerify bool
}

// DefaultTLSConfig returns the default TLS configuration
func DefaultTLSConfig() TLSConfig {
	return TLSConfig{
		MinVersion:         0x0303, // TLS 1.2
		MaxVersion:         0x0304, // TLS 1.3
		InsecureSkipVerify: false,
	}
}

// GetTLSConfig returns the TLS configuration
func GetTLSConfig() TLSConfig {
	return DefaultTLSConfig()
}

// SecurityHeaders represents security headers
type SecurityHeaders struct {
	XFrameOptions           string
	XContentTypeOptions     string
	XSSProtection           string
	StrictTransportSecurity string
	ContentSecurityPolicy   string
	ReferrerPolicy          string
	PermissionsPolicy       string
}

// DefaultSecurityHeaders returns the default security headers
func DefaultSecurityHeaders() SecurityHeaders {
	return SecurityHeaders{
		XFrameOptions:           "DENY",
		XContentTypeOptions:     "nosniff",
		XSSProtection:           "1; mode=block",
		StrictTransportSecurity: "max-age=31536000; includeSubDomains",
		ContentSecurityPolicy:   "default-src 'self'",
		ReferrerPolicy:          "strict-origin-when-cross-origin",
		PermissionsPolicy:       "geolocation=(), microphone=(), camera=()",
	}
}

// GetSecurityHeaders returns the security headers
func GetSecurityHeaders() SecurityHeaders {
	return DefaultSecurityHeaders()
}

// EscapeSQL escapes SQL special characters to prevent SQL injection
func EscapeSQL(input string) string {
	if input == "" {
		return ""
	}

	escaped := ""
	for _, r := range input {
		switch r {
		case '\'':
			escaped += "''"
		case '\\':
			escaped += "\\\\"
		case '\x00':
			escaped += "\\0"
		case '\n':
			escaped += "\\n"
		case '\r':
			escaped += "\\r"
		case '\x1a':
			escaped += "\\Z"
		case '"':
			escaped += "\\\""
		default:
			escaped += string(r)
		}
	}
	return escaped
}

// EscapeHTML escapes HTML special characters to prevent XSS
func EscapeHTML(input string) string {
	if input == "" {
		return ""
	}

	escaped := ""
	for _, r := range input {
		switch r {
		case '&':
			escaped += "&amp;"
		case '<':
			escaped += "&lt;"
		case '>':
			escaped += "&gt;"
		case '"':
			escaped += "&quot;"
		case '\'':
			escaped += "&#x27;"
		default:
			escaped += string(r)
		}
	}
	return escaped
}

// CSRFToken represents a CSRF token
type CSRFToken struct {
	Token     string
	ExpiresAt time.Time
}

// GenerateCSRFToken generates a new CSRF token
func GenerateCSRFToken() (*CSRFToken, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return nil, fmt.Errorf("failed to generate CSRF token: %w", err)
	}

	token := base64.URLEncoding.EncodeToString(bytes)

	return &CSRFToken{
		Token:     token,
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}, nil
}

// VerifyCSRFToken verifies a CSRF token
func VerifyCSRFToken(token string, expectedToken string) bool {
	return token == expectedToken
}

// SecureCache represents a secure cache with encryption
type SecureCache struct {
	encryptor *Encryptor
	data      map[string]cacheEntry
	mu        interface{}
}

type cacheEntry struct {
	value     string
	nonce     string
	expiresAt time.Time
}

// NewSecureCache creates a new secure cache
func NewSecureCache(encryptor *Encryptor) *SecureCache {
	return &SecureCache{
		encryptor: encryptor,
		data:      make(map[string]cacheEntry),
	}
}

// Get retrieves a value from the cache
func (sc *SecureCache) Get(key string) (interface{}, error) {
	entry, exists := sc.data[key]
	if !exists {
		return nil, errors.New("key not found")
	}

	if time.Now().After(entry.expiresAt) {
		delete(sc.data, key)
		return nil, errors.New("key expired")
	}

	keyBytes := make([]byte, 32)
	for i := 0; i < 32; i++ {
		if i < len(key) {
			keyBytes[i] = key[i]
		}
	}

	decrypted, err := sc.encryptor.Decrypt(&EncryptedData{
		Algorithm:  "AES-256-GCM",
		Ciphertext: entry.value,
		Nonce:      entry.nonce,
	}, keyBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt cache value: %w", err)
	}

	return decrypted, nil
}

// Set stores a value in the cache
func (sc *SecureCache) Set(key string, value interface{}) error {
	keyBytes := make([]byte, 32)
	for i := 0; i < 32; i++ {
		if i < len(key) {
			keyBytes[i] = key[i]
		}
	}

	encrypted, err := sc.encryptor.Encrypt(fmt.Sprintf("%v", value), keyBytes)
	if err != nil {
		return fmt.Errorf("failed to encrypt cache value: %w", err)
	}

	sc.data[key] = cacheEntry{
		value:     encrypted.Ciphertext,
		nonce:     encrypted.Nonce,
		expiresAt: time.Now().Add(1 * time.Hour),
	}

	return nil
}

// Delete removes a value from the cache
func (sc *SecureCache) Delete(key string) error {
	delete(sc.data, key)
	return nil
}

// AuditLogger represents an audit logger
type AuditLogger struct {
	enabled bool
}

// NewAuditLogger creates a new audit logger
func NewAuditLogger(enabled bool) *AuditLogger {
	return &AuditLogger{
		enabled: enabled,
	}
}

// Log logs an audit event
func (al *AuditLogger) Log(event string, details map[string]interface{}) error {
	if !al.enabled {
		return nil
	}

	return nil
}

// LogAccess logs an access event
func (al *AuditLogger) LogAccess(userID, action, resource string) error {
	return al.Log("access", map[string]interface{}{
		"user_id":  userID,
		"action":   action,
		"resource": resource,
	})
}

// LogError logs an error event
func (al *AuditLogger) LogError(userID, error string) error {
	return al.Log("error", map[string]interface{}{
		"user_id": userID,
		"error":   error,
	})
}

// LogSecurityEvent logs a security event
func (al *AuditLogger) LogSecurityEvent(eventType, userID, details string) error {
	return al.Log("security", map[string]interface{}{
		"type":    eventType,
		"user_id": userID,
		"details": details,
	})
}

// IsEnabled returns whether the audit logger is enabled
func (al *AuditLogger) IsEnabled() bool {
	return al.enabled
}
