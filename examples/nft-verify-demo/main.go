package main

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"os"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

// 这是一个最简单的 NFT 验证示例
// 帮助你理解 Web3 开发的核心概念

func main() {
	// 步骤 1: 连接到以太坊测试网
	// 从环境变量读取 RPC URL（安全做法）
	rpcURL := os.Getenv("ETH_RPC_URL")
	if rpcURL == "" {
		rpcURL = "https://sepolia.infura.io/v3/YOUR_API_KEY" // 替换为你的 API Key
	}

	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		log.Fatal("连接 RPC 失败:", err)
	}
	defer client.Close()

	fmt.Println("✅ 成功连接到以太坊测试网")

	// 步骤 2: 查询区块高度（验证连接）
	blockNumber, err := client.BlockNumber(context.Background())
	if err != nil {
		log.Fatal("查询区块高度失败:", err)
	}
	fmt.Printf("📦 当前区块高度: %d\n", blockNumber)

	// 步骤 3: 查询钱包 ETH 余额
	walletAddress := common.HexToAddress("0x你的钱包地址") // 替换为你的地址
	balance, err := client.BalanceAt(context.Background(), walletAddress, nil)
	if err != nil {
		log.Fatal("查询余额失败:", err)
	}
	fmt.Printf("💰 钱包余额: %s ETH\n", weiToEther(balance))

	// 步骤 4: 查询 NFT 余额（ERC-721）
	nftContract := common.HexToAddress("0xNFT合约地址") // 替换为你的 NFT 合约
	nftBalance, err := checkERC721Balance(client, walletAddress, nftContract)
	if err != nil {
		log.Fatal("查询 NFT 余额失败:", err)
	}
	fmt.Printf("🎨 NFT 余额: %d\n", nftBalance)

	// 步骤 5: 验证是否持有 NFT
	if nftBalance > 0 {
		fmt.Println("✅ 用户持有 NFT，允许访问内容")
	} else {
		fmt.Println("❌ 用户未持有 NFT，拒绝访问")
	}
}

// 查询 ERC-721 NFT 余额
func checkERC721Balance(client *ethclient.Client, wallet, contract common.Address) (int64, error) {
	// ERC-721 balanceOf 函数签名
	// function balanceOf(address owner) external view returns (uint256)

	// 创建合约调用
	caller := bind.NewBoundContract(contract, erc721ABI(), client, nil, nil)

	// 准备调用参数
	var result []interface{}
	err := caller.Call(&bind.CallOpts{}, &result, "balanceOf", wallet)
	if err != nil {
		return 0, err
	}

	// 解析结果
	balance := result[0].(*big.Int)
	return balance.Int64(), nil
}

// ERC-721 ABI（只包含 balanceOf 函数）
func erc721ABI() string {
	return `[{
		"constant": true,
		"inputs": [{"name": "owner", "type": "address"}],
		"name": "balanceOf",
		"outputs": [{"name": "", "type": "uint256"}],
		"type": "function"
	}]`
}

// Wei 转 Ether（1 ETH = 10^18 Wei）
func weiToEther(wei *big.Int) string {
	ether := new(big.Float).Quo(
		new(big.Float).SetInt(wei),
		big.NewFloat(1e18),
	)
	return ether.Text('f', 6)
}

/*
运行步骤：

1. 安装依赖
   go mod init nft-verify-demo
   go get github.com/ethereum/go-ethereum

2. 设置环境变量
   export ETH_RPC_URL="https://sepolia.infura.io/v3/YOUR_API_KEY"

3. 修改代码中的地址
   - 替换钱包地址
   - 替换 NFT 合约地址

4. 运行
   go run main.go

预期输出：
✅ 成功连接到以太坊测试网
📦 当前区块高度: 5234567
💰 钱包余额: 0.123456 ETH
🎨 NFT 余额: 1
✅ 用户持有 NFT，允许访问内容

常见错误：
1. "connection refused" -> 检查 RPC URL
2. "invalid API key" -> 检查 Infura/Alchemy API Key
3. "execution reverted" -> 检查合约地址是否正确
*/
