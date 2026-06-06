package service

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"go.uber.org/zap"
)

const demoNFTSimpleABI = `[{"constant":false,"inputs":[{"name":"to","type":"address"}],"name":"mint","outputs":[{"name":"","type":"uint256"}],"stateMutability":"nonpayable","type":"function"},{"constant":true,"inputs":[{"name":"owner","type":"address"}],"name":"balanceOf","outputs":[{"name":"","type":"uint256"}],"stateMutability":"view","type":"function"}]`

type DemoNFTMinter struct {
	client       *ethclient.Client
	parsedABI    abi.ABI
	contractAddr common.Address
	privateKey   *ecdsa.PrivateKey
	fromAddr     common.Address
	rpcURL       string
	logger       *zap.Logger
}

type DemoNFTMintResult struct {
	TxHashes    []string
	Balance     *big.Int
	TotalMinted int
}

func NewDemoNFTMinter(rpcURL, contractHex, privateKeyHex string, expectedChainID int64, logger *zap.Logger) (*DemoNFTMinter, error) {
	if rpcURL == "" || contractHex == "" || privateKeyHex == "" {
		return nil, fmt.Errorf("rpc url, contract address, and private key are all required")
	}
	if !common.IsHexAddress(contractHex) {
		return nil, fmt.Errorf("invalid contract address: %s", contractHex)
	}
	if !strings.HasPrefix(privateKeyHex, "0x") {
		privateKeyHex = "0x" + privateKeyHex
	}
	pk, err := crypto.HexToECDSA(strings.TrimPrefix(privateKeyHex, "0x"))
	if err != nil {
		return nil, fmt.Errorf("invalid private key: %w", err)
	}

	parsedABI, err := abi.JSON(strings.NewReader(demoNFTSimpleABI))
	if err != nil {
		return nil, fmt.Errorf("parse DemoNFT ABI: %w", err)
	}

	ethClient, err := ethclient.Dial(rpcURL)
	if err != nil {
		return nil, fmt.Errorf("dial rpc %s: %w", rpcURL, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	chainID, err := ethClient.ChainID(ctx)
	if err != nil {
		ethClient.Close()
		return nil, fmt.Errorf("resolve chain id from rpc: %w", err)
	}
	if expectedChainID > 0 && chainID.Cmp(big.NewInt(expectedChainID)) != 0 {
		ethClient.Close()
		return nil, fmt.Errorf("rpc chain id %s does not match configured %d", chainID.String(), expectedChainID)
	}

	return &DemoNFTMinter{
		client:       ethClient,
		parsedABI:    parsedABI,
		contractAddr: common.HexToAddress(contractHex),
		privateKey:   pk,
		fromAddr:     crypto.PubkeyToAddress(pk.PublicKey),
		rpcURL:       rpcURL,
		logger:       logger,
	}, nil
}

func (m *DemoNFTMinter) Close() {
	if m.client != nil {
		m.client.Close()
	}
}

func (m *DemoNFTMinter) FromAddress() common.Address {
	return m.fromAddr
}

func (m *DemoNFTMinter) BalanceOf(ctx context.Context, wallet common.Address) (*big.Int, error) {
	data, err := m.parsedABI.Pack("balanceOf", wallet)
	if err != nil {
		return nil, fmt.Errorf("pack balanceOf: %w", err)
	}
	out, err := m.client.CallContract(ctx, ethereum.CallMsg{To: &m.contractAddr, Data: data}, nil)
	if err != nil {
		return nil, fmt.Errorf("balanceOf call: %w", err)
	}
	bal := new(big.Int)
	if len(out) == 0 {
		return bal, nil
	}
	if err := m.parsedABI.UnpackIntoInterface(&bal, "balanceOf", out); err != nil {
		return nil, fmt.Errorf("unpack balanceOf: %w", err)
	}
	return bal, nil
}

func (m *DemoNFTMinter) Mint(ctx context.Context, wallet common.Address, count int) (*DemoNFTMintResult, error) {
	if count <= 0 {
		return nil, fmt.Errorf("count must be > 0")
	}
	if wallet == (common.Address{}) {
		return nil, fmt.Errorf("invalid wallet address")
	}

	nonce, err := m.client.PendingNonceAt(ctx, m.fromAddr)
	if err != nil {
		return nil, fmt.Errorf("pending nonce: %w", err)
	}
	gasPrice, err := m.client.SuggestGasPrice(ctx)
	if err != nil {
		return nil, fmt.Errorf("suggest gas price: %w", err)
	}
	chainID, err := m.client.ChainID(ctx)
	if err != nil {
		return nil, fmt.Errorf("chain id: %w", err)
	}

	result := &DemoNFTMintResult{TxHashes: make([]string, 0, count)}
	for i := 0; i < count; i++ {
		data, err := m.parsedABI.Pack("mint", wallet)
		if err != nil {
			return result, fmt.Errorf("pack mint call: %w", err)
		}
		tx := types.NewTransaction(nonce+uint64(i), m.contractAddr, big.NewInt(0), 200000, gasPrice, data)
		signed, err := types.SignTx(tx, types.NewEIP155Signer(chainID), m.privateKey)
		if err != nil {
			return result, fmt.Errorf("sign tx: %w", err)
		}
		if err := m.client.SendTransaction(ctx, signed); err != nil {
			return result, fmt.Errorf("send tx: %w", err)
		}
		result.TxHashes = append(result.TxHashes, signed.Hash().Hex())
		result.TotalMinted++
	}

	bal, err := m.BalanceOf(ctx, wallet)
	if err != nil {
		return result, fmt.Errorf("balance after mint: %w", err)
	}
	result.Balance = bal
	return result, nil
}
