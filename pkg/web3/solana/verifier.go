package solana

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"go.uber.org/zap"
)

type SolanaVerifier struct {
	logger     *zap.Logger
	rpcURLs    []string
	clients    []*rpc.Client
	currentIdx atomic.Uint32
}

func NewSolanaVerifier(logger *zap.Logger, rpcEndpoint ...string) *SolanaVerifier {
	urls := rpcEndpoint
	if len(urls) == 0 {
		urls = []string{}
	}
	clients := make([]*rpc.Client, 0, len(urls))
	for _, u := range urls {
		if u != "" {
			clients = append(clients, rpc.New(u))
		}
	}
	return &SolanaVerifier{
		logger:  logger,
		rpcURLs: urls,
		clients: clients,
	}
}

func (sv *SolanaVerifier) getRPCClient() *rpc.Client {
	if len(sv.clients) == 0 {
		return nil
	}
	idx := sv.currentIdx.Load() % uint32(len(sv.clients))
	return sv.clients[idx]
}

func (sv *SolanaVerifier) switchToNextRPC() {
	if len(sv.clients) <= 1 {
		return
	}
	newIdx := sv.currentIdx.Add(1) % uint32(len(sv.clients))
	sv.logger.Warn("Switching Solana RPC endpoint",
		zap.Int("new_index", int(newIdx)),
		zap.Int("total_endpoints", len(sv.clients)))
}

func (sv *SolanaVerifier) withRPCClient(fn func(*rpc.Client) error) error {
	if len(sv.clients) == 0 {
		return fmt.Errorf("solana RPC client not configured")
	}
	startIdx := sv.currentIdx.Load() % uint32(len(sv.clients))
	for i := 0; i < len(sv.clients); i++ {
		idx := (startIdx + uint32(i)) % uint32(len(sv.clients))
		client := sv.clients[idx]
		err := fn(client)
		if err == nil {
			return nil
		}
		sv.logger.Warn("Solana RPC call failed, trying next endpoint",
			zap.Int("endpoint_index", int(idx)),
			zap.Error(err))
		sv.switchToNextRPC()
	}
	return fmt.Errorf("all %d Solana RPC endpoints failed", len(sv.clients))
}

func (sv *SolanaVerifier) Close() {
	for _, c := range sv.clients {
		if c != nil {
			_ = c.Close()
		}
	}
}

// VerifySignature verifies a Solana ed25519 signature.
func (sv *SolanaVerifier) VerifySignature(address, message, signature string) (bool, error) {
	sv.logger.Debug("Verifying Solana signature",
		zap.String("address", address),
		zap.Int("message_length", len(message)))

	pubKey, err := solana.PublicKeyFromBase58(address)
	if err != nil {
		return false, fmt.Errorf("invalid Solana address: %w", err)
	}

	sigBytes, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return false, fmt.Errorf("failed to decode signature: %w", err)
	}

	if len(sigBytes) != 64 {
		return false, fmt.Errorf("invalid signature length: expected 64, got %d", len(sigBytes))
	}

	var sig [64]byte
	copy(sig[:], sigBytes)

	messageBytes, err := base64.StdEncoding.DecodeString(message)
	if err != nil {
		return false, fmt.Errorf("failed to decode message: %w", err)
	}

	verified := ed25519.Verify(pubKey[:], messageBytes, sig[:])

	if !verified {
		sv.logger.Warn("Solana signature verification failed", zap.String("address", address))
		return false, nil
	}

	sv.logger.Debug("Solana signature verified successfully", zap.String("address", address))
	return true, nil
}

func (sv *SolanaVerifier) VerifyTransaction(address string, transaction []byte, signature string) (bool, error) {
	sv.logger.Debug("Verifying Solana transaction",
		zap.String("address", address),
		zap.Int("tx_length", len(transaction)))

	pubKey, err := solana.PublicKeyFromBase58(address)
	if err != nil {
		return false, fmt.Errorf("invalid Solana address: %w", err)
	}

	sigBytes, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return false, fmt.Errorf("failed to decode signature: %w", err)
	}

	if len(sigBytes) != 64 {
		return false, fmt.Errorf("invalid signature length: expected 64, got %d", len(sigBytes))
	}

	var sig [64]byte
	copy(sig[:], sigBytes)

	verified := ed25519.Verify(pubKey[:], transaction, sig[:])

	if !verified {
		sv.logger.Warn("Solana transaction verification failed", zap.String("address", address))
		return false, nil
	}

	sv.logger.Debug("Solana transaction verified successfully", zap.String("address", address))
	return true, nil
}

func (sv *SolanaVerifier) SignMessage(message string, privateKey ed25519.PrivateKey) (string, error) {
	sv.logger.Debug("Signing Solana message", zap.Int("message_length", len(message)))

	messageBytes := []byte(message)
	signature := ed25519.Sign(privateKey, messageBytes)

	return base64.StdEncoding.EncodeToString(signature[:]), nil
}

func (sv *SolanaVerifier) GetPublicKeyFromPrivateKey(privateKey ed25519.PrivateKey) string {
	publicKey := make([]byte, ed25519.PublicKeySize)
	copy(publicKey, privateKey[32:])

	pubKey := solana.PublicKeyFromBytes(publicKey)
	return pubKey.String()
}

func (sv *SolanaVerifier) VerifyMessage(address, message, signature string) (bool, error) {
	return sv.VerifySignature(address, message, signature)
}

func (sv *SolanaVerifier) SignTransaction(transaction []byte, privateKey ed25519.PrivateKey) (string, error) {
	sv.logger.Debug("Signing Solana transaction", zap.Int("tx_length", len(transaction)))

	signature := ed25519.Sign(privateKey, transaction)

	return base64.StdEncoding.EncodeToString(signature[:]), nil
}

func (sv *SolanaVerifier) VerifyMultiSignature(addresses []string, message string, signatures []string) (bool, error) {
	if len(addresses) != len(signatures) {
		return false, fmt.Errorf("number of addresses (%d) does not match number of signatures (%d)",
			len(addresses), len(signatures))
	}

	for i, address := range addresses {
		verified, err := sv.VerifySignature(address, message, signatures[i])
		if err != nil {
			return false, fmt.Errorf("failed to verify signature %d: %w", i, err)
		}
		if !verified {
			return false, nil
		}
	}

	return true, nil
}

// VerifyOffchainMessage verifies a SIP-004 offchain message signature.
func (sv *SolanaVerifier) VerifyOffchainMessage(address, message, signature string) (bool, error) {
	encoded := base64.StdEncoding.EncodeToString(encodeSIP004Message(message))
	return sv.VerifySignature(address, encoded, signature)
}

func (sv *SolanaVerifier) SignOffchainMessage(message string, privateKey ed25519.PrivateKey) (string, error) {
	encoded := base64.StdEncoding.EncodeToString(encodeSIP004Message(message))
	return sv.SignMessage(encoded, privateKey)
}

func encodeSIP004Message(message string) []byte {
	msgBytes := []byte(message)
	var buf []byte
	buf = append(buf, 0xff)
	buf = append(buf, []byte("solana offchain")...)
	buf = append(buf, 0x00)
	buf = append(buf, 0x00)
	buf = append(buf, 0x00)
	buf = append(buf, encodeVarint(uint64(len(msgBytes)))...)
	buf = append(buf, msgBytes...)
	return buf
}

func encodeVarint(v uint64) []byte {
	var buf []byte
	for {
		b := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			b |= 0x80
		}
		buf = append(buf, b)
		if v == 0 {
			break
		}
	}
	return buf
}

type MetaplexAttribute struct {
	TraitType string `json:"trait_type"`
	Value     string `json:"value"`
}

type MetaplexProperties struct {
	Creators []MetaplexCreator `json:"creators"`
	Files    []MetaplexFile    `json:"files"`
}

type MetaplexCreator struct {
	Address  string `json:"address"`
	Share    int    `json:"share"`
	Verified bool   `json:"verified"`
}

type MetaplexFile struct {
	URI  string `json:"uri"`
	Type string `json:"type"`
}

func (sv *SolanaVerifier) VerifyMetaplexNFTOwnership(ctx context.Context, mintAddress, ownerAddress string) (bool, error) {
	sv.logger.Debug("Verifying Metaplex NFT ownership",
		zap.String("mint", mintAddress),
		zap.String("owner", ownerAddress))

	var result bool
	err := sv.withRPCClient(func(client *rpc.Client) error {
		verifier := NewMetaplexVerifier(client, sv.logger, nil)
		verified, e := verifier.VerifyNFTOwnership(ctx, mintAddress, ownerAddress)
		if e != nil {
			return e
		}
		result = verified
		return nil
	})
	return result, err
}

func (sv *SolanaVerifier) FetchMetaplexMetadata(ctx context.Context, mintAddress string) (*MetaplexMetadata, error) {
	sv.logger.Debug("Fetching Metaplex metadata", zap.String("mint", mintAddress))

	var result *MetaplexMetadata
	err := sv.withRPCClient(func(client *rpc.Client) error {
		verifier := NewMetaplexVerifier(client, sv.logger, nil)
		meta, e := verifier.GetMetadata(ctx, mintAddress)
		if e != nil {
			return e
		}
		result = meta
		return nil
	})
	return result, err
}

func (sv *SolanaVerifier) VerifyTokenAccount(ctx context.Context, tokenAccount, ownerAddress string) (bool, error) {
	sv.logger.Debug("Verifying token account",
		zap.String("token_account", tokenAccount),
		zap.String("owner", ownerAddress))

	var result bool
	err := sv.withRPCClient(func(client *rpc.Client) error {
		accountInfo, e := client.GetAccountInfo(ctx, solana.MustPublicKeyFromBase58(tokenAccount))
		if e != nil {
			return fmt.Errorf("failed to get token account info: %w", e)
		}
		if accountInfo == nil || accountInfo.Value == nil {
			result = false
			return nil
		}

		data := accountInfo.Value.Data.GetBinary()
		if len(data) < 64 {
			return fmt.Errorf("token account data too short")
		}

		actualOwner := solana.PublicKeyFromBytes(data[32:64])
		expectedOwner, e := solana.PublicKeyFromBase58(ownerAddress)
		if e != nil {
			return fmt.Errorf("invalid owner address: %w", e)
		}

		result = actualOwner.Equals(expectedOwner)
		return nil
	})
	return result, err
}

func (sv *SolanaVerifier) VerifyMintAuthority(ctx context.Context, mintAddress, authorityAddress string) (bool, error) {
	sv.logger.Debug("Verifying mint authority",
		zap.String("mint", mintAddress),
		zap.String("authority", authorityAddress))

	var result bool
	err := sv.withRPCClient(func(client *rpc.Client) error {
		accountInfo, e := client.GetAccountInfo(ctx, solana.MustPublicKeyFromBase58(mintAddress))
		if e != nil {
			return fmt.Errorf("failed to get mint account info: %w", e)
		}
		if accountInfo == nil || accountInfo.Value == nil {
			result = false
			return nil
		}

		data := accountInfo.Value.Data.GetBinary()
		if len(data) < 36 {
			return fmt.Errorf("mint account data too short")
		}

		mintAuthorityOpt := data[0:36]
		if mintAuthorityOpt[0] == 0 {
			result = false
			return nil
		}

		mintAuthority := solana.PublicKeyFromBytes(mintAuthorityOpt[4:36])
		expectedAuthority, e := solana.PublicKeyFromBase58(authorityAddress)
		if e != nil {
			return fmt.Errorf("invalid authority address: %w", e)
		}

		result = mintAuthority.Equals(expectedAuthority)
		return nil
	})
	return result, err
}

func (sv *SolanaVerifier) ParseSolanaAddress(address string) (solana.PublicKey, error) {
	if !strings.HasPrefix(address, "0x") {
		pubKey, err := solana.PublicKeyFromBase58(address)
		if err != nil {
			return solana.PublicKey{}, fmt.Errorf("invalid Solana address: %w", err)
		}
		return pubKey, nil
	}

	pubKeyBytes, err := hex.DecodeString(strings.TrimPrefix(address, "0x"))
	if err != nil {
		return solana.PublicKey{}, fmt.Errorf("invalid hex address: %w", err)
	}

	if len(pubKeyBytes) != 32 {
		return solana.PublicKey{}, fmt.Errorf("invalid address length: expected 32, got %d", len(pubKeyBytes))
	}

	return solana.PublicKeyFromBytes(pubKeyBytes), nil
}

func (sv *SolanaVerifier) IsValidAddress(address string) bool {
	_, err := sv.ParseSolanaAddress(address)
	return err == nil
}

func (sv *SolanaVerifier) DeriveTokenAccountAddress(walletAddress, mintAddress string) (string, error) {
	sv.logger.Debug("Deriving token account address",
		zap.String("wallet", walletAddress),
		zap.String("mint", mintAddress))

	walletPubKey, err := sv.ParseSolanaAddress(walletAddress)
	if err != nil {
		return "", fmt.Errorf("invalid wallet address: %w", err)
	}

	mintPubKey, err := sv.ParseSolanaAddress(mintAddress)
	if err != nil {
		return "", fmt.Errorf("invalid mint address: %w", err)
	}

	tokenProgramID := solana.MustPublicKeyFromBase58("TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA")

	seed := [][]byte{
		walletPubKey[:],
		mintPubKey[:],
	}

	pda, _, err := solana.FindProgramAddress(seed, tokenProgramID)
	if err != nil {
		return "", fmt.Errorf("failed to derive token account address: %w", err)
	}

	return pda.String(), nil
}

func (sv *SolanaVerifier) VerifyPDASignature(pdaAddress string, seeds []string, programID string) (bool, error) {
	sv.logger.Debug("Verifying PDA signature",
		zap.String("pda", pdaAddress),
		zap.Int("seeds_count", len(seeds)),
		zap.String("program", programID))

	seedBytes := make([][]byte, len(seeds))
	for i, seed := range seeds {
		seedBytes[i] = []byte(seed)
	}

	programPubKey, err := solana.PublicKeyFromBase58(programID)
	if err != nil {
		return false, fmt.Errorf("invalid program ID: %w", err)
	}

	expectedPDA, _, err := solana.FindProgramAddress(seedBytes, programPubKey)
	if err != nil {
		return false, fmt.Errorf("failed to derive PDA: %w", err)
	}

	providedPDA, err := solana.PublicKeyFromBase58(pdaAddress)
	if err != nil {
		return false, fmt.Errorf("invalid PDA address: %w", err)
	}

	if expectedPDA != providedPDA {
		sv.logger.Warn("PDA verification failed",
			zap.String("expected", expectedPDA.String()),
			zap.String("provided", providedPDA.String()))
		return false, nil
	}

	sv.logger.Debug("PDA verified successfully", zap.String("pda", pdaAddress))
	return true, nil
}
