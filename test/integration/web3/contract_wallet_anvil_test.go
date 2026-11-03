//go:build anvil

package web3_test

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/rtcdance/streamgate/pkg/web3/signature"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func deployWalletFixture(t *testing.T, client *ethclient.Client, url string, parsed abi.ABI, bin []byte, args ...any) (deployedAddress common.Address, bound *bind.BoundContract) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	auth := transactor(t, anvilKey(t, 0))
	auth.Context = ctx
	address, tx, contract, err := bind.DeployContract(auth, parsed, bin, client, args...)
	require.NoError(t, err)
	receipt, err := bind.WaitMined(ctx, client, tx)
	require.NoError(t, err)
	require.Equal(t, uint64(types.ReceiptStatusSuccessful), receipt.Status)
	mineUntilSafe(t, url, receipt.BlockNumber.Uint64())
	return address, contract
}

// This is a real deployment/RPC proof test, not complete session or media
// acceptance. Both are required separately before closing W3-02/03/05.
func TestCWAccountProof_Anvil(t *testing.T) {
	url, cleanup := startAnvil(t)
	defer cleanup()
	client, err := ethclient.Dial(url)
	require.NoError(t, err)
	defer client.Close()
	ctx := context.Background()
	key := anvilKey(t, 0)
	owner := crypto.PubkeyToAddress(key.PublicKey)
	nextKey := anvilKey(t, 1)
	next := crypto.PubkeyToAddress(nextKey.PublicKey)
	walletABI, walletBin := loadArtifact(t, "contracts/out/Study1271Wallet.sol/Study1271Wallet.json")
	wallet, walletContract := deployWalletFixture(t, client, url, walletABI, walletBin, owner)
	nftABI, nftBin := loadArtifact(t, "contracts/out/DemoNFT.sol/DemoNFT.json")
	nftAddress, _ := deploy(t, client, transactor(t, key), nftABI, nftBin)
	nft := bind.NewBoundContract(nftAddress, nftABI, client, client, client)
	receipt := transact(t, client, transactor(t, key), nft, "mint", wallet)
	mineUntilSafe(t, url, receipt.BlockNumber.Uint64())
	signer := signature.NewSignatureVerifier(zap.NewNop())
	message := "localhost wants you to sign in with your Ethereum account:\n" + wallet.Hex() + "\n\ncontract wallet integration proof"
	encoded, err := signer.SignMessage(message, key)
	require.NoError(t, err)
	v := signature.NewAccountVerifier(client, zap.NewNop())
	proof, err := v.Verify(ctx, anvilChainID, "siwe", wallet.Hex(), message, encoded)
	require.NoError(t, err)
	require.Equal(t, signature.AccountContract, proof.Kind)
	require.Equal(t, wallet, proof.Address)
	require.NotEqual(t, owner, proof.Address)
	var values []any
	require.NoError(t, nft.Call(&bind.CallOpts{Context: ctx, BlockNumber: new(big.Int).SetUint64(proof.BlockNumber)}, &values, "balanceOf", owner))
	require.Zero(t, values[0].(*big.Int).Sign(), "controller has no gated NFT")
	values = nil
	require.NoError(t, nft.Call(&bind.CallOpts{Context: ctx, BlockNumber: new(big.Int).SetUint64(proof.BlockNumber)}, &values, "balanceOf", wallet))
	require.Equal(t, int64(1), values[0].(*big.Int).Int64())
	// ERC-2098 proves the account branch does not impose a 65-byte EOA decoder.
	sig := common.FromHex(encoded)
	compact := append([]byte(nil), sig[:64]...)
	compact[32] |= (sig[64] - 27) << 7
	_, err = v.Verify(ctx, anvilChainID, "siwe", wallet.Hex(), message, "0x"+common.Bytes2Hex(compact))
	require.NoError(t, err)
	_, err = v.Verify(ctx, 1, "siwe", wallet.Hex(), message, encoded)
	require.ErrorIs(t, err, signature.ErrWalletDependency)
	receipt = transact(t, client, transactor(t, key), walletContract, "setOwner", next)
	mineUntilSafe(t, url, receipt.BlockNumber.Uint64())
	_, err = v.Verify(ctx, anvilChainID, "siwe", wallet.Hex(), message, encoded)
	require.ErrorIs(t, err, signature.ErrWalletSignature, "old proof must be rejected independently of NFT transfer")
	encoded, err = signer.SignMessage(message, nextKey)
	require.NoError(t, err)
	_, err = v.Verify(ctx, anvilChainID, "siwe", wallet.Hex(), message, encoded)
	require.NoError(t, err)
	values = nil
	require.NoError(t, nft.Call(&bind.CallOpts{Context: ctx, BlockNumber: receipt.BlockNumber}, &values, "balanceOf", wallet))
	require.Equal(t, int64(1), values[0].(*big.Int).Int64(), "policy rotation must not masquerade as an NFT transfer")
	// A different deployed policy accepts seven opaque bytes and emits real
	// malformed ABI/revert responses through eth_call.
	fixtureABI, fixtureBin := loadArtifact(t, "contracts/out/Study1271Wallet.t.sol/Study1271ResponseFixture.json")
	opaque := []byte{1, 2, 3, 4, 5, 6, 7}
	fixture, fixtureContract := deployWalletFixture(t, client, url, fixtureABI, fixtureBin, [32]byte(proof.Digest), opaque)
	_, err = v.Verify(ctx, anvilChainID, "siwe", fixture.Hex(), message, "0x"+common.Bytes2Hex(opaque))
	require.NoError(t, err)
	for mode := uint8(1); mode <= 5; mode++ {
		receipt = transact(t, client, transactor(t, key), fixtureContract, "setMode", mode)
		mineUntilSafe(t, url, receipt.BlockNumber.Uint64())
		_, err = v.Verify(ctx, anvilChainID, "siwe", fixture.Hex(), message, "0x"+common.Bytes2Hex(opaque))
		require.ErrorIs(t, err, signature.ErrWalletSignature, "adversarial mode %d", mode)
	}
}
