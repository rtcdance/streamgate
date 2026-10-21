# StreamGate Security Guide

**Date**: 2025-01-28  
**Status**: Security Implementation Guide  
**Version**: 1.0.0

## Overview

This guide provides comprehensive documentation for the StreamGate security infrastructure, including encryption, key management, compliance framework, and security hardening.

## Table of Contents

1. [Encryption](#encryption)
2. [Key Management](#key-management)
3. [Compliance Framework](#compliance-framework)
4. [Security Hardening](#security-hardening)
5. [Best Practices](#best-practices)
6. [API Reference](#api-reference)
7. [Troubleshooting](#troubleshooting)

## Encryption

### Overview

The encryption module provides AES-256-GCM encryption for data protection with support for password-based key derivation.

### Features

- **Algorithm**: AES-256-GCM
- **Key Derivation**: PBKDF2 with SHA256
- **Nonce Size**: 12 bytes (GCM standard)
- **Iterations**: 100,000 (configurable)

### Usage

#### Basic Encryption/Decryption

```go
import "github.com/yourusername/streamgate/pkg/security"

// Create encryptor
encryptor := security.NewEncryptor(security.EncryptionConfig{})

// Generate key
key, err := encryptor.GenerateKey()
if err != nil {
    log.Fatal(err)
}

// Encrypt data
plaintext := "Sensitive data"
encrypted, err := encryptor.Encrypt(plaintext, key)
if err != nil {
    log.Fatal(err)
}

// Decrypt data
decrypted, err := encryptor.Decrypt(encrypted, key)
if err != nil {
    log.Fatal(err)
}
```

#### Password-Based Encryption

```go
// Encrypt with password
plaintext := "Secret message"
password := "secure-password-123"

encrypted, err := encryptor.EncryptString(plaintext, password)
if err != nil {
    log.Fatal(err)
}

// Decrypt with password
decrypted, err := encryptor.DecryptString(encrypted, password)
if err != nil {
    log.Fatal(err)
}
```

#### Password Hashing

```go
// Hash password
password := "user-password"
hash := encryptor.HashPassword(password)

// Verify password
if encryptor.VerifyPassword(password, hash) {
    // Password is correct
}
```

### Configuration

```go
config := security.EncryptionConfig{
    Algorithm:  "AES-256-GCM",
    KeySize:    32,           // 32 bytes for AES-256
    NonceSize:  12,           // 12 bytes for GCM
    Iterations: 100000,       // PBKDF2 iterations
}

encryptor := security.NewEncryptor(config)
```

## Key Management

### Overview

The key management module provides secure key generation, storage, rotation, and lifecycle management.

### Features

- **Key Generation**: Cryptographically secure random key generation
- **Key Rotation**: Automated key rotation with versioning
- **Key Revocation**: Secure key revocation and expiration
- **Key Metadata**: Tracking key creation, expiration, and status
- **Active Key Management**: Automatic active key tracking

### Usage

#### Key Generation

```go
import "github.com/yourusername/streamgate/pkg/security"

// Create key manager
km := security.NewKeyManager(24*time.Hour, 32)

// Generate key
keyID, err := km.GenerateKey()
if err != nil {
    log.Fatal(err)
}

// Get key
key, err := km.GetKey(keyID)
if err != nil {
    log.Fatal(err)
}
```

#### Key Rotation

```go
// Check if rotation is needed
if km.ShouldRotate() {
    newKeyID, err := km.RotateKey()
    if err != nil {
        log.Fatal(err)
    }
}

// Get active key
activeKeyID, key, err := km.GetActiveKey()
if err != nil {
    log.Fatal(err)
}
```

#### Key Lifecycle

```go
// List all keys
keys := km.ListKeys()

// Get key metadata
metadata, err := km.GetKeyMetadata(keyID)
if err != nil {
    log.Fatal(err)
}

// Revoke key
err = km.RevokeKey(keyID)
if err != nil {
    log.Fatal(err)
}

// Check if key is active
if km.IsKeyActive(keyID) {
    // Key is active
}
```

### Configuration

```go
// Create key manager with custom settings
rotationInterval := 24 * time.Hour  // Rotate daily
keySize := 32                        // AES-256

km := security.NewKeyManager(rotationInterval, keySize)
```

## Compliance Framework

### Overview

The compliance framework provides compliance checking, audit logging, and reporting for standards like GDPR, HIPAA, SOC2, and PCI-DSS.

### Features

- **Compliance Standards**: GDPR, HIPAA, SOC2, PCI-DSS, ISO27001
- **Compliance Checks**: Configurable compliance checks
- **Compliance Reports**: Automated compliance reporting
- **Audit Logging**: Comprehensive audit trail
- **Compliance Status**: Real-time compliance status

### Usage

#### Compliance Checks

```go
import "github.com/yourusername/streamgate/pkg/security"

// Create compliance framework
cf := security.NewComplianceFramework()

// Register compliance check
check := &security.ComplianceCheck{
    ID:          "check-1",
    Standard:    security.GDPR,
    Name:        "Data Encryption",
    Description: "Verify data is encrypted",
    Status:      "PASS",
}

err := cf.RegisterCheck(check)
if err != nil {
    log.Fatal(err)
}
```

#### Compliance Reports

```go
// Generate compliance report
report, err := cf.GenerateReport(security.GDPR)
if err != nil {
    log.Fatal(err)
}

// Check compliance status
if report.Status == "COMPLIANT" {
    // System is compliant
}

// Get compliance score
fmt.Printf("Compliance Score: %.2f%%\n", report.Score)
```

#### Audit Logging

```go
// Log audit event
err := cf.LogAuditEvent(
    "LOGIN",                    // Action
    "user-123",                 // Resource
    "user@example.com",         // User
    "SUCCESS",                  // Status
    "User logged in",           // Details
)
if err != nil {
    log.Fatal(err)
}

// Get audit log
logs := cf.GetAuditLog(100)

// Get audit log by resource
resourceLogs := cf.GetAuditLogByResource("user-123", 50)

// Get audit log by user
userLogs := cf.GetAuditLogByUser("user@example.com", 50)
```

#### Compliance Status

```go
// Get overall compliance status
status := cf.GetComplianceStatus()

for standard, complianceStatus := range status {
    fmt.Printf("%s: %s\n", standard, complianceStatus)
}
```

## Security Hardening

### Overview

The security hardening module provides password validation, input validation, output encoding, and account lockout protection.

### Features

- **Password Validation**: Configurable password strength requirements
- **Input Validation**: Email, username, URL, IPv4, UUID validation
- **Output Encoding**: HTML, URL, JSON, SQL encoding
- **Account Lockout**: Failed login attempt tracking and account lockout
- **Custom Validators**: Support for custom input validators
- **Custom Encoders**: Support for custom output encoders

### Usage

#### Password Validation

```go
import "github.com/yourusername/streamgate/pkg/security"

// Create security hardening with policy
policy := security.SecurityPolicy{
    PasswordMinLength:     12,
    PasswordRequireUpper:  true,
    PasswordRequireNumber: true,
    PasswordRequireSymbol: true,
}

sh := security.NewSecurityHardening(policy)

// Validate password
password := "SecurePass123!"
err := sh.ValidatePassword(password)
if err != nil {
    log.Fatal(err)
}
```

#### Input Validation

```go
// Validate email
err := sh.ValidateInput("email", "user@example.com")
if err != nil {
    log.Fatal(err)
}

// Validate username
err = sh.ValidateInput("username", "valid_user123")
if err != nil {
    log.Fatal(err)
}

// Validate URL
err = sh.ValidateInput("url", "https://example.com")
if err != nil {
    log.Fatal(err)
}
```

#### Output Encoding

```go
// Encode for HTML context
htmlEncoded, err := sh.EncodeOutput("html", "<script>alert('XSS')</script>")
if err != nil {
    log.Fatal(err)
}

// Encode for JSON context
jsonEncoded, err := sh.EncodeOutput("json", `{"key": "value"}`)
if err != nil {
    log.Fatal(err)
}

// Encode for URL context
urlEncoded, err := sh.EncodeOutput("url", "hello world&key=value")
if err != nil {
    log.Fatal(err)
}
```

#### Account Lockout

```go
// Record failed login
err := sh.RecordFailedLogin("user@example.com")
if err != nil {
    // Account locked
    log.Fatal(err)
}

// Record successful login
sh.RecordSuccessfulLogin("user@example.com")

// Check if account is locked
if sh.IsAccountLocked("user@example.com") {
    // Account is locked
}

// Get failed login count
count := sh.GetFailedLoginCount("user@example.com")

// Reset failed attempts
sh.ResetFailedAttempts("user@example.com")
```

#### Custom Validators

```go
// Add custom validator
err := sh.AddValidator("phone", `^\d{10}$`)
if err != nil {
    log.Fatal(err)
}

// Use custom validator
err = sh.ValidateInput("phone", "1234567890")
if err != nil {
    log.Fatal(err)
}
```

#### Custom Encoders

```go
// Add custom encoder
sh.AddEncoder("custom", func(s string) string {
    return "ENCODED:" + s
})

// Use custom encoder
encoded, err := sh.EncodeOutput("custom", "test")
if err != nil {
    log.Fatal(err)
}
```

## Best Practices

### Encryption

1. **Use Strong Keys**: Always use cryptographically secure random keys
2. **Rotate Keys**: Implement regular key rotation (e.g., daily)
3. **Secure Storage**: Store keys in secure vaults, not in code
4. **Unique Nonces**: Ensure each encryption uses a unique nonce
5. **Authenticated Encryption**: Use GCM mode for authenticated encryption

### Key Management

1. **Key Versioning**: Maintain multiple key versions for rotation
2. **Key Expiration**: Set expiration dates for keys
3. **Key Revocation**: Revoke compromised keys immediately
4. **Key Audit**: Log all key operations
5. **Key Backup**: Maintain secure backups of keys

### Compliance

1. **Regular Audits**: Conduct regular compliance audits
2. **Audit Logging**: Log all security-relevant events
3. **Compliance Reports**: Generate regular compliance reports
4. **Policy Enforcement**: Enforce compliance policies
5. **Documentation**: Maintain compliance documentation

### Security Hardening

1. **Strong Passwords**: Enforce strong password requirements
2. **Input Validation**: Validate all user inputs
3. **Output Encoding**: Encode all outputs for context
4. **Account Lockout**: Implement account lockout after failed attempts
5. **Session Management**: Implement secure session management

## API Reference

### Encryption API

```go
// Create encryptor
NewEncryptor(config EncryptionConfig) *Encryptor

// Generate key
GenerateKey() ([]byte, error)
GenerateKeyHex() (string, error)

// Derive key from password
DeriveKey(password string, salt []byte) ([]byte, error)

// Encrypt/Decrypt
Encrypt(plaintext string, key []byte) (*EncryptedData, error)
Decrypt(encrypted *EncryptedData, key []byte) (string, error)

// Password-based encryption
EncryptString(plaintext, password string) (*EncryptedData, error)
DecryptString(encrypted *EncryptedData, password string) (string, error)

// Password hashing
HashPassword(password string) string
VerifyPassword(password, hash string) bool
```

### Key Manager API

```go
// Create key manager
NewKeyManager(rotationInterval time.Duration, keySize int) *KeyManager

// Key operations
GenerateKey() (string, error)
GetKey(keyID string) ([]byte, error)
GetActiveKey() (string, []byte, error)
RotateKey() (string, error)

// Key management
ListKeys() []KeyMetadata
GetKeyMetadata(keyID string) (*KeyMetadata, error)
RevokeKey(keyID string) error
IsKeyActive(keyID string) bool

// Key statistics
GetKeyCount() int
GetActiveKeyCount() int
GetRotatedKeyCount() int
GetLastRotationTime() time.Time
GetRotationInterval() time.Duration
```

### Compliance Framework API

```go
// Create compliance framework
NewComplianceFramework() *ComplianceFramework

// Compliance checks
RegisterCheck(check *ComplianceCheck) error
RunCheck(checkID string) error
RunAllChecks(standard ComplianceStandard) ([]ComplianceCheck, error)

// Compliance reports
GenerateReport(standard ComplianceStandard) (*ComplianceReport, error)
GetReport(reportID string) (*ComplianceReport, error)
ListReports(standard ComplianceStandard) []*ComplianceReport

// Audit logging
LogAuditEvent(action, resource, user, status, details string) error
GetAuditLog(limit int) []AuditLogEntry
GetAuditLogByResource(resource string, limit int) []AuditLogEntry
GetAuditLogByUser(user string, limit int) []AuditLogEntry

// Compliance status
GetComplianceStatus() map[ComplianceStandard]string
GetCheckCount() int
GetReportCount() int
GetAuditLogCount() int
```

### Security Hardening API

```go
// Create security hardening
NewSecurityHardening(policy SecurityPolicy) *SecurityHardening

// Password validation
ValidatePassword(password string) error

// Input validation
ValidateInput(inputType, value string) error
AddValidator(name, pattern string) error

// Output encoding
EncodeOutput(context, value string) (string, error)
AddEncoder(name string, encoder func(string) string)

// Account lockout
RecordFailedLogin(username string) error
RecordSuccessfulLogin(username string)
IsAccountLocked(username string) bool
GetFailedLoginCount(username string) int
GetLockoutTime(username string) time.Time
ResetFailedAttempts(username string)

// Statistics
GetLockedAccountCount() int
GetFailedAttemptCount() int
GetPolicy() SecurityPolicy
UpdatePolicy(policy SecurityPolicy)
```

## Troubleshooting

### Encryption Issues

**Problem**: Decryption fails with "failed to decrypt"
- **Solution**: Verify the correct key is being used. Different keys will fail decryption.

**Problem**: Key size mismatch error
- **Solution**: Ensure key size matches configuration (32 bytes for AES-256)

**Problem**: Nonce is empty
- **Solution**: Ensure nonce is properly generated during encryption

### Key Management Issues

**Problem**: Key not found error
- **Solution**: Verify key ID is correct and key hasn't been revoked

**Problem**: No active key
- **Solution**: Generate at least one key before using GetActiveKey()

**Problem**: Key rotation not happening
- **Solution**: Check rotation interval and call RotateKey() when ShouldRotate() returns true

### Compliance Issues

**Problem**: Compliance report shows non-compliant
- **Solution**: Register and run all required compliance checks

**Problem**: Audit log is empty
- **Solution**: Ensure LogAuditEvent() is being called for security events

**Problem**: Compliance status not updating
- **Solution**: Call GenerateReport() to update compliance status

### Security Hardening Issues

**Problem**: Password validation fails
- **Solution**: Ensure password meets all policy requirements

**Problem**: Input validation fails
- **Solution**: Verify input format matches validator pattern

**Problem**: Account locked unexpectedly
- **Solution**: Check failed login count and lockout duration

## Performance Considerations

### Encryption Performance

- Encryption: ~1-2ms per operation
- Decryption: ~1-2ms per operation
- Key derivation: ~100-200ms (PBKDF2 with 100k iterations)

### Key Management Performance

- Key generation: <1ms
- Key rotation: <1ms
- Key lookup: <1ms

### Compliance Performance

- Report generation: ~10-50ms
- Audit logging: <1ms
- Compliance check: <1ms

## Security Considerations

1. **Key Storage**: Never store keys in code or configuration files
2. **Key Transmission**: Always transmit keys over secure channels (TLS)
3. **Key Backup**: Maintain secure backups of keys
4. **Key Destruction**: Securely destroy keys when no longer needed
5. **Audit Logging**: Log all security-relevant events
6. **Access Control**: Restrict access to security functions
7. **Regular Updates**: Keep security libraries up to date

## Conclusion

The StreamGate security infrastructure provides comprehensive encryption, key management, compliance, and hardening capabilities. Follow best practices and security considerations to ensure maximum security.

---

**Document Status**: Complete  
**Last Updated**: 2025-01-28  
**Version**: 1.0.0
