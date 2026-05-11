package web3

import (
	"fmt"
	"strings"
	"time"
)

// SIWEMessage represents an EIP-4361 (Sign-In with Ethereum) message.
// See: https://eips.ethereum.org/EIPS/eip-4361
type SIWEMessage struct {
	Domain         string   // RFC 3986 authority that is requesting the signing
	Address        string   // Ethereum address performing the signing (EIP-55 mixed-case)
	URI            string   // RFC 3986 URI referring to the resource that is the subject of the signing
	Version        string   // Current version of the message, must be "1"
	ChainID        int64    // EIP-155 Chain ID to which the session is bound
	Nonce          string   // Randomized token to prevent replay attacks
	IssuedAt       string   // ISO 8601 datetime when the message was generated
	ExpirationTime string   // ISO 8601 datetime when the message is no longer valid (optional)
	NotBefore      string   // ISO 8601 datetime when the message becomes valid (optional)
	RequestID      string   // System-specific ID for login (optional)
	Resources      []string // List of resources the session intends to access (optional)
}

// BuildSIWEMessage constructs an EIP-4361 formatted message string.
func BuildSIWEMessage(msg *SIWEMessage) string {
	var sb strings.Builder

	// Header line
	sb.WriteString(fmt.Sprintf("%s wants you to sign in with your Ethereum account:\n", msg.Domain))
	sb.WriteString(fmt.Sprintf("%s\n\n", msg.Address))

	// Statement (optional — we use a default)
	sb.WriteString("Sign in to StreamGate\n\n")

	// URI and version
	sb.WriteString(fmt.Sprintf("URI: %s\n", msg.URI))
	sb.WriteString(fmt.Sprintf("Version: %s\n", msg.Version))

	// Chain ID
	sb.WriteString(fmt.Sprintf("Chain ID: %d\n", msg.ChainID))

	// Nonce
	sb.WriteString(fmt.Sprintf("Nonce: %s\n", msg.Nonce))

	// Issued At
	sb.WriteString(fmt.Sprintf("Issued At: %s", msg.IssuedAt))

	// Optional fields
	if msg.ExpirationTime != "" {
		sb.WriteString(fmt.Sprintf("\nExpiration Time: %s", msg.ExpirationTime))
	}
	if msg.NotBefore != "" {
		sb.WriteString(fmt.Sprintf("\nNot Before: %s", msg.NotBefore))
	}
	if msg.RequestID != "" {
		sb.WriteString(fmt.Sprintf("\nRequest ID: %s", msg.RequestID))
	}

	// Resources
	if len(msg.Resources) > 0 {
		sb.WriteString("\nResources:")
		for _, r := range msg.Resources {
			sb.WriteString(fmt.Sprintf("\n- %s", r))
		}
	}

	return sb.String()
}

// ParseSIWEMessage parses an EIP-4361 formatted message string into a SIWEMessage struct.
func ParseSIWEMessage(message string) (*SIWEMessage, error) {
	lines := strings.Split(message, "\n")
	if len(lines) < 8 {
		return nil, fmt.Errorf("invalid SIWE message: too few lines (%d)", len(lines))
	}

	msg := &SIWEMessage{}

	// Line 0: "{domain} wants you to sign in with your Ethereum account:"
	if !strings.HasSuffix(lines[0], "wants you to sign in with your Ethereum account:") {
		return nil, fmt.Errorf("invalid SIWE message: missing domain header")
	}
	msg.Domain = strings.TrimSuffix(lines[0], " wants you to sign in with your Ethereum account:")

	// Line 1: Ethereum address
	msg.Address = strings.TrimSpace(lines[1])

	// Line 2: empty
	// Line 3: statement
	// Line 4: empty

	// Parse key-value fields
	for _, line := range lines[5:] {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "URI: ") {
			msg.URI = strings.TrimPrefix(line, "URI: ")
		} else if strings.HasPrefix(line, "Version: ") {
			msg.Version = strings.TrimPrefix(line, "Version: ")
		} else if strings.HasPrefix(line, "Chain ID: ") {
			var chainID int64
			_, _ = fmt.Sscanf(strings.TrimPrefix(line, "Chain ID: "), "%d", &chainID)
			msg.ChainID = chainID
		} else if strings.HasPrefix(line, "Nonce: ") {
			msg.Nonce = strings.TrimPrefix(line, "Nonce: ")
		} else if strings.HasPrefix(line, "Issued At: ") {
			msg.IssuedAt = strings.TrimPrefix(line, "Issued At: ")
		} else if strings.HasPrefix(line, "Expiration Time: ") {
			msg.ExpirationTime = strings.TrimPrefix(line, "Expiration Time: ")
		} else if strings.HasPrefix(line, "Not Before: ") {
			msg.NotBefore = strings.TrimPrefix(line, "Not Before: ")
		} else if strings.HasPrefix(line, "Request ID: ") {
			msg.RequestID = strings.TrimPrefix(line, "Request ID: ")
		} else if strings.HasPrefix(line, "- ") {
			msg.Resources = append(msg.Resources, strings.TrimPrefix(line, "- "))
		} else if line == "Resources:" { //nolint:SA9003 // Section header — resources follow on subsequent lines
		}
	}

	// Validate required fields
	if msg.Version == "" {
		return nil, fmt.Errorf("invalid SIWE message: missing Version field")
	}
	if msg.Nonce == "" {
		return nil, fmt.Errorf("invalid SIWE message: missing Nonce field")
	}

	return msg, nil
}

// SIWEMessageOption configures a SIWEMessage with optional fields.
type SIWEMessageOption func(*SIWEMessage)

// WithSIWEExpirationTime sets the expiration time.
func WithSIWEExpirationTime(t time.Time) SIWEMessageOption {
	return func(m *SIWEMessage) { m.ExpirationTime = t.UTC().Format(time.RFC3339) }
}

// WithSIWENotBefore sets the not-before time.
func WithSIWENotBefore(t time.Time) SIWEMessageOption {
	return func(m *SIWEMessage) { m.NotBefore = t.UTC().Format(time.RFC3339) }
}

// WithSIWERequestID sets the request ID.
func WithSIWERequestID(id string) SIWEMessageOption {
	return func(m *SIWEMessage) { m.RequestID = id }
}

// WithSIWEResources sets the resources list.
func WithSIWEResources(res []string) SIWEMessageOption {
	return func(m *SIWEMessage) { m.Resources = res }
}

// NewSIWEMessage creates a SIWE message with standard defaults for StreamGate.
func NewSIWEMessage(domain, address, uri string, chainID int64, nonce string, issuedAt time.Time, opts ...SIWEMessageOption) *SIWEMessage {
	msg := &SIWEMessage{
		Domain:    domain,
		Address:   address,
		URI:       uri,
		Version:   "1",
		ChainID:   chainID,
		Nonce:     nonce,
		IssuedAt:  issuedAt.UTC().Format(time.RFC3339),
		Resources: []string{uri},
	}
	for _, opt := range opts {
		opt(msg)
	}
	return msg
}
