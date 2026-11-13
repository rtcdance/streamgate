package service

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	stg "github.com/rtcdance/streamgate/pkg/storage"
	"github.com/rtcdance/streamgate/pkg/web3"
	walletsig "github.com/rtcdance/streamgate/pkg/web3/signature"
	"time"
)

func (s *AuthService) validateWalletAdmission(challenge *stg.WalletChallenge, walletAddress string, chainID int64) (string, error) {
	var normalizedAddress string
	if isSolanaChain(challenge.ChainID) {
		// Solana path: base58 address, no hex normalization
		if !IsValidSolanaAddress(walletAddress) {
			return "", fmt.Errorf("invalid Solana wallet address: %s", walletAddress)
		}
		normalizedAddress = walletAddress
	} else {
		// EVM path: hex address normalization
		if !common.IsHexAddress(walletAddress) {
			return "", fmt.Errorf("invalid wallet address: %s", walletAddress)
		}
		normalizedAddress = common.HexToAddress(walletAddress).Hex()
	}

	if challenge.WalletAddress != normalizedAddress {
		return "", ErrInvalidCredential
	}
	if challenge.ChainID != 0 && chainID != challenge.ChainID {
		return "", ErrChainIDMismatch
	}
	if !challenge.UsedAt.IsZero() {
		return "", stg.ErrChallengeUsed
	}
	if time.Now().UTC().After(challenge.ExpiresAt) {
		return "", ErrChallengeExpired
	}

	// For SIWE challenges, validate the parsed message against expected values.
	// This ensures domain binding (phishing protection), nonce match, and
	// address consistency per EIP-4361.
	if challenge.SigningType == "siwe" {
		parsedSIWE, parseErr := web3.ParseSIWEMessage(challenge.Message)
		if parseErr != nil {
			return "", fmt.Errorf("failed to parse SIWE message: %w", parseErr)
		}
		if parsedSIWE.URI != s.siweURI || parsedSIWE.IssuedAt != challenge.IssuedAt.Format(time.RFC3339) || parsedSIWE.ExpirationTime != challenge.ExpiresAt.Format(time.RFC3339) {
			return "", ErrInvalidCredential
		}
		if validateErr := web3.ValidateSIWEMessage(parsedSIWE,
			s.siweDomain,
			normalizedAddress,
			challenge.Nonce,
			challenge.ChainID,
		); validateErr != nil {
			return "", fmt.Errorf("SIWE validation failed: %w", validateErr)
		}
	}

	return normalizedAddress, nil
}

func (s *AuthService) verifyWalletChallengeProof(ctx context.Context, challenge *stg.WalletChallenge, normalizedAddress, signature string) (*walletsig.AccountProof, error) {
	var err error

	var proof *walletsig.AccountProof
	// Route to the correct signature verifier based on chain type and signing type
	var valid bool
	if isSolanaChain(challenge.ChainID) {
		verifier, ok := s.signatureVerifier.(ChainAwareSignatureVerifier)
		if !ok || verifier == nil {
			return nil, ErrNotSupported
		}
		valid, err = verifier.VerifyOffchainMessage(ctx, normalizedAddress, challenge.Message, signature)
	} else if verifier, ok := s.signatureVerifier.(walletProofVerifier); ok {
		scheme := challenge.SigningType
		if scheme == "" {
			scheme = "personal_sign"
		}
		proof, err = verifier.VerifyWalletProof(ctx, challenge.ChainID, scheme, normalizedAddress, challenge.Message, signature)
		valid = err == nil && proof != nil
		if valid && (proof.Address.Hex() != normalizedAddress || proof.ChainID != challenge.ChainID || (proof.Kind != walletsig.AccountEOA && proof.Kind != walletsig.AccountContract) || len(proof.Signature) > s.contractWalletPolicy().MaxSignatureBytes) {
			return nil, ErrInvalidCredential
		}
		if valid && !s.freshWalletProof(proof) {
			return nil, walletsig.ErrWalletDependency
		}
	} else if challenge.SigningType == "eip712" {
		if s.eip712Verifier == nil {
			return nil, ErrNotSupported
		}
		// EIP-712 typed data verification: parse the stored JSON message
		var typedData web3.EIP712TypedData
		if err := json.Unmarshal([]byte(challenge.Message), &typedData); err != nil {
			return nil, fmt.Errorf("failed to parse stored EIP-712 typed data: %w", err)
		}
		valid, err = s.eip712Verifier.VerifyTypedData(normalizedAddress, &typedData, signature)
	} else {
		if s.signatureVerifier == nil {
			return nil, ErrNotSupported
		}
		// Default: EIP-191 personal_sign
		valid, err = s.signatureVerifier.VerifySignature(ctx, normalizedAddress, challenge.Message, signature)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to verify wallet signature: %w", err)
	}
	if !valid {
		return nil, ErrInvalidCredential
	}

	return proof, nil
}
