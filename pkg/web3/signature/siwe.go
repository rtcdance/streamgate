package signature

import (
	"fmt"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// SIWEMessage represents an EIP-4361 (Sign-In with Ethereum) message.
// See: https://eips.ethereum.org/EIPS/eip-4361
type SIWEMessage struct {
	Domain         string
	Address        string
	URI            string
	Version        string
	ChainID        int64
	Nonce          string
	IssuedAt       string
	ExpirationTime string
	NotBefore      string
	RequestID      string
	Resources      []string
}

// ValidateSIWEMessage checks that a parsed SIWE message matches the expected
// values for domain, address, nonce, and chain ID. This is critical for
// EIP-4361 compliance: it prevents phishing (domain binding), replay (nonce),
// and cross-chain attacks (chain ID binding).
//
// A zero-value expectedDomain skips domain validation (for backwards compatibility).
func ValidateSIWEMessage(msg *SIWEMessage, expectedDomain, expectedAddress, expectedNonce string, expectedChainID int64) error {
	if msg.Version != "1" {
		return fmt.Errorf("invalid SIWE version: got %q, want \"1\"", msg.Version)
	}
	if msg.Address == "" || !common.IsHexAddress(msg.Address) {
		return fmt.Errorf("invalid SIWE address: %q", msg.Address)
	}
	checksummed := common.HexToAddress(msg.Address).Hex()
	if msg.Address != checksummed {
		return fmt.Errorf("SIWE address is not EIP-55 checksummed: got %q, want %q", msg.Address, checksummed)
	}

	if expectedDomain != "" && !strings.EqualFold(msg.Domain, expectedDomain) {
		return fmt.Errorf("SIWE domain mismatch: got %q, want %q", msg.Domain, expectedDomain)
	}
	if expectedNonce != "" && msg.Nonce != expectedNonce {
		return fmt.Errorf("SIWE nonce mismatch")
	}
	if expectedChainID != 0 && msg.ChainID != expectedChainID {
		return fmt.Errorf("SIWE chain ID mismatch: got %d, want %d", msg.ChainID, expectedChainID)
	}
	return nil
}

// BuildSIWEMessage constructs an EIP-4361 formatted message string.
func BuildSIWEMessage(msg *SIWEMessage) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "%s wants you to sign in with your Ethereum account:\n", msg.Domain)
	fmt.Fprintf(&sb, "%s\n\n", msg.Address)

	sb.WriteString("Sign in to StreamGate\n\n")

	fmt.Fprintf(&sb, "URI: %s\n", msg.URI)
	fmt.Fprintf(&sb, "Version: %s\n", msg.Version)

	fmt.Fprintf(&sb, "Chain ID: %d\n", msg.ChainID)

	fmt.Fprintf(&sb, "Nonce: %s\n", msg.Nonce)

	fmt.Fprintf(&sb, "Issued At: %s", msg.IssuedAt)

	if msg.ExpirationTime != "" {
		fmt.Fprintf(&sb, "\nExpiration Time: %s", msg.ExpirationTime)
	}
	if msg.NotBefore != "" {
		fmt.Fprintf(&sb, "\nNot Before: %s", msg.NotBefore)
	}
	if msg.RequestID != "" {
		fmt.Fprintf(&sb, "\nRequest ID: %s", msg.RequestID)
	}

	if len(msg.Resources) > 0 {
		sb.WriteString("\nResources:")
		for _, r := range msg.Resources {
			fmt.Fprintf(&sb, "\n- %s", r)
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

	if !strings.HasSuffix(lines[0], "wants you to sign in with your Ethereum account:") {
		return nil, fmt.Errorf("invalid SIWE message: missing domain header")
	}
	msg.Domain = strings.TrimSuffix(lines[0], " wants you to sign in with your Ethereum account:")

	msg.Address = strings.TrimSpace(lines[1])
	if !common.IsHexAddress(msg.Address) {
		return nil, fmt.Errorf("invalid SIWE message: invalid Ethereum address format")
	}

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
		} else if line == "Resources:" {
		}
	}

	if msg.Version == "" {
		return nil, fmt.Errorf("invalid SIWE message: missing Version field")
	}
	if msg.Nonce == "" {
		return nil, fmt.Errorf("invalid SIWE message: missing Nonce field")
	}

	return msg, nil
}

type SIWEMessageOption func(*SIWEMessage)

func WithSIWEExpirationTime(t time.Time) SIWEMessageOption {
	return func(m *SIWEMessage) { m.ExpirationTime = t.UTC().Format(time.RFC3339) }
}

func WithSIWENotBefore(t time.Time) SIWEMessageOption {
	return func(m *SIWEMessage) { m.NotBefore = t.UTC().Format(time.RFC3339) }
}

func WithSIWERequestID(id string) SIWEMessageOption {
	return func(m *SIWEMessage) { m.RequestID = id }
}

func WithSIWEResources(res []string) SIWEMessageOption {
	return func(m *SIWEMessage) { m.Resources = res }
}

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
