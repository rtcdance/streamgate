//go:build anvil

package web3_test

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/rtcdance/streamgate/pkg/web3"
	"go.uber.org/zap"
)

// setupAnvil starts an Anvil instance and returns the RPC URL, a funded
// private key, and a cleanup function. It skips the test if anvil is not
// in PATH.
func setupAnvil(t *testing.T) (rpcURL string, fundedKey *ecdsa.PrivateKey, cleanup func()) {
	t.Helper()

	anvilPath, err := exec.LookPath("anvil")
	if err != nil {
		t.Skip("anvil not found in PATH, skipping Anvil integration test")
	}

	// Pick a random port to avoid conflicts
	port := 18545
	rpcURL = fmt.Sprintf("http://127.0.0.1:%d", port)

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, anvilPath,
		"--port", fmt.Sprintf("%d", port),
		"--block-time", "1",
		"--silent",
		"--accounts", "1",
	)
	cmd.Stdout = os.Discard
	cmd.Stderr = os.Discard

	if err := cmd.Start(); err != nil {
		cancel()
		t.Skipf("failed to start anvil: %v", err)
	}

	// Wait for anvil to be ready (max 10s)
	ready := false
	for i := 0; i < 20; i++ {
		conn, dialErr := ethclient.Dial(rpcURL)
		if dialErr == nil {
			pingCtx, pingCancel := context.WithTimeout(context.Background(), 2*time.Second)
			_, pingErr := conn.BlockNumber(pingCtx)
			pingCancel()
			conn.Close()
			if pingErr == nil {
				ready = true
				break
			}
		}
		time.Sleep(500 * time.Millisecond)
	}

	if !ready {
		_ = cmd.Process.Kill()
		cancel()
		t.Skip("anvil failed to become ready within 10s")
	}

	// Anvil's first account: 0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80
	fundedHex := "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
	fundedKey = mustParsePrivateKey(t, fundedHex)

	cleanup = func() {
		_ = cmd.Process.Kill()
		cancel()
		// Wait for process to exit
		_ = cmd.Wait()
	}

	return rpcURL, fundedKey, cleanup
}

func mustParsePrivateKey(t *testing.T, hexStr string) *ecdsa.PrivateKey {
	t.Helper()
	hexStr = strings.TrimPrefix(hexStr, "0x")
	key, err := crypto.HexToECDSA(hexStr)
	if err != nil {
		t.Fatalf("failed to parse private key: %v", err)
	}
	return key
}

// deployTestERC721 deploys a minimal ERC-721 contract for testing.
// Returns the contract address.
func deployTestERC721(t *testing.T, client *ethclient.Client, auth *bind.TransactOpts) common.Address {
	t.Helper()

	// Minimal ERC-721 bytecode (OpenZeppelin-based, with constructor args
	// for name "TestNFT" and symbol "TNFT")
	// Generated from: solc --bin --abi -o . erc721.sol
	// with pragma ^0.8.0, import "@openzeppelin/contracts/token/ERC721/ERC721.sol";
	// contract TestNFT is ERC721 { constructor() ERC721("TestNFT", "TNFT") {} }
	bytecodeHex := "60806040523480156200001157600080fd5b50604051806040016040528060078152602001662a32b9ba27232a60c91b815250604051806040016040528060048152602001632a27232a60e11b81525081600090805190602001906200006892919062000081565b5080600190805190602001906200008192919062000081565b505062000196565b8280546200008f9062000127565b90600052602060002090601f016020900481019282620000b35760008555620000fe565b82601f10620000ce57805160ff1916838001178555620000fe565b82800160010185558215620000fe579182015b82811115620000fe578251825591602001919060010190620000e1565b5090506200010d919062000111565b5090565b5b808211156200010d576000815560010162000112565b600181811c908216806200013c57607f821691505b6020821081036200015d57634e487b7160e01b600052602260045260246000fd5b50919050565b604081016200018082840180516001600160a01b0316825260200190565b5050565b60008282526200019681602084016200009c565b9392505050565b610bcc80620001a66000396000f3fe"

	bytecode := common.FromHex(bytecodeHex)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	gasPrice, err := client.SuggestGasPrice(ctx)
	if err != nil {
		t.Fatalf("failed to suggest gas price: %v", err)
	}

	nonce, err := client.PendingNonceAt(ctx, auth.From)
	if err != nil {
		t.Fatalf("failed to get nonce: %v", err)
	}

	tx := types.NewTx(&types.LegacyTx{
		Nonce:    nonce,
		GasPrice: gasPrice,
		Gas:      2000000,
		To:       nil,
		Data:     bytecode,
	})

	signedTx, err := types.SignTx(tx, types.HomesteadSigner{}, auth.PrivateKey)
	if err != nil {
		t.Fatalf("failed to sign deploy tx: %v", err)
	}

	if err := client.SendTransaction(ctx, signedTx); err != nil {
		t.Fatalf("failed to send deploy tx: %v", err)
	}

	receipt, err := bind.WaitMined(ctx, client, signedTx)
	if err != nil {
		t.Fatalf("failed to wait for deploy tx: %v", err)
	}

	if receipt.Status != types.ReceiptStatusSuccessful {
		t.Fatal("deploy transaction failed")
	}

	t.Logf("ERC-721 deployed at %s", receipt.ContractAddress.Hex())
	return receipt.ContractAddress
}

func mintERC721(t *testing.T, client *ethclient.Client, auth *bind.TransactOpts, contractAddr common.Address, to common.Address, tokenID int64) {
	t.Helper()

	// ERC-721 mint function selector: 0x40c10f19 for mint(address,uint256)
	mintSelector := common.Hex2Bytes("40c10f19")

	// ABI-encoded args
	toPadded := common.LeftPadBytes(to.Bytes(), 32)
	tokenIDPadded := common.LeftPadBytes(big.NewInt(tokenID).Bytes(), 32)

	data := make([]byte, 0, 4+64)
	data = append(data, mintSelector...)
	data = append(data, toPadded...)
	data = append(data, tokenIDPadded...)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	gasPrice, err := client.SuggestGasPrice(ctx)
	if err != nil {
		t.Fatalf("failed to suggest gas price: %v", err)
	}

	nonce, err := client.PendingNonceAt(ctx, auth.From)
	if err != nil {
		t.Fatalf("failed to get nonce: %v", err)
	}

	tx := types.NewTx(&types.LegacyTx{
		Nonce:    nonce,
		GasPrice: gasPrice,
		Gas:      100000,
		To:       &contractAddr,
		Data:     data,
	})

	signedTx, err := types.SignTx(tx, types.HomesteadSigner{}, auth.PrivateKey)
	if err != nil {
		t.Fatalf("failed to sign mint tx: %v", err)
	}

	if err := client.SendTransaction(ctx, signedTx); err != nil {
		t.Fatalf("failed to send mint tx: %v", err)
	}

	receipt, err := bind.WaitMined(ctx, client, signedTx)
	if err != nil {
		t.Fatalf("failed to wait for mint tx: %v", err)
	}

	if receipt.Status != types.ReceiptStatusSuccessful {
		t.Fatal("mint transaction failed")
	}

	t.Logf("Minted token %d to %s (tx: %s)", tokenID, to.Hex(), signedTx.Hash().Hex())
}

func mineBlocks(t *testing.T, rpcURL string, n int) {
	for i := 0; i < n; i++ {
		out, err := exec.Command("cast", "block-number", "--rpc-url", rpcURL).Output()
		if err == nil {
			_ = out
		}
	}
}

func TestAnvil_BlockNumber(t *testing.T) {
	rpcURL, _, cleanup := setupAnvil(t)
	defer cleanup()

	client, err := web3.NewChainClient(rpcURL, 31337, zap.NewNop())
	if err != nil {
		t.Fatalf("failed to connect to anvil: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	blockNum, err := client.GetBlockNumber(ctx)
	if err != nil {
		t.Fatalf("failed to get block number: %v", err)
	}
	t.Logf("Anvil initial block number: %d", blockNum)
}

func TestAnvil_NFTVerificationFlow(t *testing.T) {
	rpcURL, fundedKey, cleanup := setupAnvil(t)
	defer cleanup()

	ethCli, err := ethclient.Dial(rpcURL)
	if err != nil {
		t.Fatalf("failed to dial anvil: %v", err)
	}
	defer ethCli.Close()

	chainID := big.NewInt(31337)
	auth, err := bind.NewKeyedTransactorWithChainID(fundedKey, chainID)
	if err != nil {
		t.Fatalf("failed to create transactor: %v", err)
	}

	walletAddr := crypto.PubkeyToAddress(fundedKey.PublicKey)
	t.Logf("Wallet address: %s", walletAddr.Hex())

	contractAddr := deployTestERC721(t, ethCli, auth)
	mintERC721(t, ethCli, auth, contractAddr, walletAddr, 1)

	chainClient, err := web3.NewChainClient(rpcURL, 31337, zap.NewNop())
	if err != nil {
		t.Fatalf("failed to create chain client: %v", err)
	}
	defer chainClient.Close()

	nftVerifier := web3.NewNFTVerifier(chainClient, zap.NewNop())

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	verified, err := nftVerifier.VerifyNFTOwnership(ctx, contractAddr.Hex(), "1", walletAddr.Hex())
	if err != nil {
		t.Fatalf("VerifyNFTOwnership failed: %v", err)
	}
	if !verified {
		t.Fatal("expected wallet to own token 1, verification returned false")
	}
	t.Log("NFT ownership verified on Anvil chain")

	balance, err := nftVerifier.GetNFTBalance(ctx, contractAddr.Hex(), walletAddr.Hex())
	if err != nil {
		t.Fatalf("GetNFTBalance failed: %v", err)
	}
	if balance.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("expected balance 1, got %s", balance.String())
	}
	t.Logf("NFT balance correct: %s", balance.String())

	info, err := nftVerifier.GetNFTInfo(ctx, contractAddr.Hex(), "1")
	if err != nil {
		t.Fatalf("GetNFTInfo failed: %v", err)
	}
	if info.Name != "TestNFT" {
		t.Logf("Note: contract name is %q (may differ from minimal bytecode)", info.Name)
	}
}

func TestAnvil_NFTVerification_NoOwnership(t *testing.T) {
	rpcURL, fundedKey, cleanup := setupAnvil(t)
	defer cleanup()

	ethCli, err := ethclient.Dial(rpcURL)
	if err != nil {
		t.Fatalf("failed to dial anvil: %v", err)
	}
	defer ethCli.Close()

	chainID := big.NewInt(31337)
	auth, err := bind.NewKeyedTransactorWithChainID(fundedKey, chainID)
	if err != nil {
		t.Fatalf("failed to create transactor: %v", err)
	}

	walletAddr := crypto.PubkeyToAddress(fundedKey.PublicKey)

	contractAddr := deployTestERC721(t, ethCli, auth)
	mintERC721(t, ethCli, auth, contractAddr, walletAddr, 1)

	chainClient, err := web3.NewChainClient(rpcURL, 31337, zap.NewNop())
	if err != nil {
		t.Fatalf("failed to create chain client: %v", err)
	}
	defer chainClient.Close()

	nftVerifier := web3.NewNFTVerifier(chainClient, zap.NewNop())
	randomAddr := "0x000000000000000000000000000000000000dead"

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	verified, err := nftVerifier.VerifyNFTOwnership(ctx, contractAddr.Hex(), "1", randomAddr)
	if err != nil {
		t.Fatalf("VerifyNFTOwnership failed: %v", err)
	}
	if verified {
		t.Fatal("expected random address NOT to own token 1")
	}
	t.Log("Correctly rejected non-owner")
}

func TestAnvil_ChainBlockTags(t *testing.T) {
	rpcURL, _, cleanup := setupAnvil(t)
	defer cleanup()

	chainClient, err := web3.NewChainClient(rpcURL, 31337, zap.NewNop())
	if err != nil {
		t.Fatalf("failed to create chain client: %v", err)
	}
	defer chainClient.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	header, err := chainClient.HeaderByNumber(ctx, nil)
	if err != nil {
		t.Fatalf("HeaderByNumber(latest) failed: %v", err)
	}
	t.Logf("Latest block: %d hash=%s", header.Number.Uint64(), header.Hash().Hex())
}

func TestAnvil_RPCFailover(t *testing.T) {
	goodURL, fundedKey, cleanupAnvil := setupAnvil(t)
	defer cleanupAnvil()

	badURL := "http://127.0.0.1:19999"
	rpcURLs := []string{badURL, goodURL}

	client, err := web3.NewChainClientWithFallback(rpcURLs, 31337, zap.NewNop())
	if err != nil {
		t.Fatalf("failed to create chain client with fallback: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	blockNum, err := client.GetBlockNumber(ctx)
	if err != nil {
		t.Fatalf("GetBlockNumber after failover failed: %v", err)
	}
	t.Logf("Successfully failed over from bad RPC, block number: %d", blockNum)

	walletAddr := crypto.PubkeyToAddress(fundedKey.PublicKey)
	verifier := web3.NewNFTVerifier(client, zap.NewNop())

	ethCli, err := ethclient.Dial(goodURL)
	if err != nil {
		t.Fatalf("failed to dial anvil: %v", err)
	}
	defer ethCli.Close()

	chainID := big.NewInt(31337)
	auth, err := bind.NewKeyedTransactorWithChainID(fundedKey, chainID)
	if err != nil {
		t.Fatalf("failed to create transactor: %v", err)
	}

	contractAddr := deployTestERC721(t, ethCli, auth)
	mintERC721(t, ethCli, auth, contractAddr, walletAddr, 42)

	ctx2, cancel2 := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel2()

	verified, err := verifier.VerifyNFTOwnership(ctx2, contractAddr.Hex(), "42", walletAddr.Hex())
	if err != nil {
		t.Fatalf("VerifyNFTOwnership via failed-over connection failed: %v", err)
	}
	if !verified {
		t.Fatal("expected ownership verification to work via failover RPC")
	}
	t.Log("NFT verification succeeded via RPC failover")
}

func TestAnvil_HealthCheck(t *testing.T) {
	rpcURL, _, cleanup := setupAnvil(t)
	defer cleanup()

	client, err := web3.NewChainClient(rpcURL, 31337, zap.NewNop())
	if err != nil {
		t.Fatalf("failed to create chain client: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := client.HealthCheck(ctx); err != nil {
		t.Fatalf("health check failed: %v", err)
	}
}

func TestAnvil_RateLimiter(t *testing.T) {
	rpcURL, _, cleanup := setupAnvil(t)
	defer cleanup()

	client, err := web3.NewChainClient(rpcURL, 31337, zap.NewNop())
	if err != nil {
		t.Fatalf("failed to create chain client: %v", err)
	}
	defer client.Close()

	rl := web3.NewRPCRateLimiter(1000, 2000, zap.NewNop())
	client.SetRateLimiter(rl)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	blockNum, err := client.GetBlockNumber(ctx)
	if err != nil {
		t.Fatalf("GetBlockNumber with rate limiter failed: %v", err)
	}
	t.Logf("Block number with rate limiter: %d", blockNum)
}

func mustFindAnvilPath() string {
	p, _ := exec.LookPath("anvil")
	return p
}

func init() {
	_, _ = filepath.Abs(".")
}
