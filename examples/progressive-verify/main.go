package main

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

// 这个示例展示 NFT 验证的三个演进阶段。
// 同一个功能（"这个地址是否拥有这个 NFT"），从基本到生产。

// 配置：换成你自己的 Sepolia RPC
const rpcURL = "https://ethereum-sepolia-rpc.publicnode.com"

// 一个已知的 Sepolia NFT 合约和持有者（CryptoKitties 示例）
const nftContract = "0xCB3C43AF17b2603F2c0cC3565D3e63644E2A51A1"

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		fmt.Printf("⚠️  无法连接 RPC (这在无网络环境正常): %v\n", err)
		fmt.Println("\n=== 代码编译通过，以下为输出示例 ===")
		// 即使 RPC 不可用，代码仍然展示了完整的模式
		dryRun()
		return
	}
	defer client.Close()

	owner := "0x3B0Bec0c182b9E7870A0CB824f1b25A988Dfa3De"

	fmt.Println("=== 阶段一：裸 ethclient ===")
	stage1(ctx, client, nftContract, "1", owner)
	fmt.Println()

	fmt.Println("=== 阶段二：ABI 封装 ===")
	stage2(ctx, client, nftContract, "1", owner)
	fmt.Println()

	fmt.Println("=== 阶段三：生产级（缓存 + BlockTag + reorg 检测） ===")
	stage3(ctx, client, nftContract, "1", owner)
}

// ——— 阶段一：裸 ethclient ———
// 最直接的方式。理解原理用，生产不推荐。
func stage1(ctx context.Context, client *ethclient.Client, contractAddr, tokenID, ownerAddr string) {
	// 直接手动编解码
	contract := common.HexToAddress(contractAddr)
	owner := common.HexToAddress(ownerAddr)

	// keccak256("ownerOf(uint256)") 的前 4 字节 = 0x6352211e
	selector := common.Hex2Bytes("6352211e")

	tokenIDInt := new(big.Int)
	tokenIDInt.SetString(tokenID, 10)

	tokenIDPadded := common.LeftPadBytes(tokenIDInt.Bytes(), 32)

	data := make([]byte, 0, 4+32)
	data = append(data, selector...)
	data = append(data, tokenIDPadded...)

	result, err := client.CallContract(ctx, ethereum.CallMsg{To: &contract, Data: data}, nil)
	if err != nil {
		fmt.Printf("  ❌ ownerOf 调用失败: %v\n", err)
		return
	}

	var tokenOwner common.Address
	copy(tokenOwner[:], result[len(result)-20:])

	isOwner := tokenOwner == owner
	fmt.Printf("  合约: %s\n", contractAddr)
	fmt.Printf("  Token: %s\n", tokenID)
	fmt.Printf("  持有者: %s\n", ownerAddr)
	fmt.Printf("  结果: %v (方法: 手动 ABI)\n", isOwner)
}

// ——— 阶段二：使用 go-ethereum 的 abi 包 ———
// 更安全，不易出错。适用于封装工具函数。
func stage2(ctx context.Context, client *ethclient.Client, contractAddr, tokenID, ownerAddr string) {
	contract := common.HexToAddress(contractAddr)
	owner := common.HexToAddress(ownerAddr)

	// 用 go-ethereum 的标准 ABI 解析
	contractABI, err := abi.JSON(strings.NewReader(ownerOfABI))
	if err != nil {
		fmt.Printf("  ❌ ABI 解析失败: %v\n", err)
		return
	}

	tokenIDInt := new(big.Int)
	tokenIDInt.SetString(tokenID, 10)

	data, err := contractABI.Pack("ownerOf", tokenIDInt)
	if err != nil {
		fmt.Printf("  ❌ ABI 编码失败: %v\n", err)
		return
	}

	result, err := client.CallContract(ctx, ethereum.CallMsg{To: &contract, Data: data}, nil)
	if err != nil {
		fmt.Printf("  ❌ ownerOf 调用失败: %v\n", err)
		return
	}

	var tokenOwner common.Address
	err = contractABI.UnpackIntoInterface(&tokenOwner, "ownerOf", result)
	if err != nil {
		fmt.Printf("  ❌ ABI 解码失败: %v\n", err)
		return
	}

	isOwner := tokenOwner == owner
	fmt.Printf("  结果: %v (方法: go-ethereum ABI)\n", isOwner)
}

// ——— 阶段三：生产级（和项目 pkg/web3/nft.go 一致） ———
// 特点:
//  1. BlockTagSafe — 读 finalized 数据，防 reorg
//  2. 泛型 withRetry — 自动 failover
//  3. 可选缓存 — NFTVerifier 的 NFTAccessEntry 绑定了 BlockNumber+BlockHash
func stage3(ctx context.Context, client *ethclient.Client, contractAddr, tokenID, ownerAddr string) {
	contract := common.HexToAddress(contractAddr)
	owner := common.HexToAddress(ownerAddr)

	contractABI, err := abi.JSON(strings.NewReader(ownerOfABI))
	if err != nil {
		fmt.Printf("  ❌ ABI 解析失败: %v\n", err)
		return
	}

	tokenIDInt := new(big.Int)
	tokenIDInt.SetString(tokenID, 10)

	data, err := contractABI.Pack("ownerOf", tokenIDInt)
	if err != nil {
		fmt.Printf("  ❌ ABI 编码失败: %v\n", err)
		return
	}

	// 1. 用 BlockTagSafe（或 -4 = "safe"）读 finalized 状态
	//    safe = 信标链确认后的最新块，不会被重组
	safeBlock := big.NewInt(-4)
	result, err := client.CallContract(ctx, ethereum.CallMsg{To: &contract, Data: data}, safeBlock)
	if err != nil {
		// 如果 RPC 不支持 safe tag，回退到 latest
		fmt.Printf("  ⚠️  safe tag 不支持，回退到 latest: %v\n", err)
		result, err = client.CallContract(ctx, ethereum.CallMsg{To: &contract, Data: data}, nil)
		if err != nil {
			fmt.Printf("  ❌ ownerOf 调用失败: %v\n", err)
			return
		}
	}

	var tokenOwner common.Address
	err = contractABI.UnpackIntoInterface(&tokenOwner, "ownerOf", result)
	if err != nil {
		fmt.Printf("  ❌ ABI 解码失败: %v\n", err)
		return
	}

	isOwner := tokenOwner == owner

	// 2. 记录当前 block 信息（用于缓存 + reorg 检测）
	currentHeader, err := client.HeaderByNumber(ctx, nil)
	if err == nil {
		blockHash := currentHeader.Hash().Hex()
		blockNum := currentHeader.Number.Uint64()
		fmt.Printf("  当前区块: %d (%s)\n", blockNum, blockHash[:10]+"...")
	}

	fmt.Printf("  结果: %v (方法: 生产级 — BlockTagSafe + block hash 跟踪)\n", isOwner)
	fmt.Println()
	fmt.Println("  如果这个结果要缓存，你会存:")
	fmt.Printf("    NFTAccessEntry{\n      HasNFT: %v,\n      BlockNumber: <上面的块号>,\n      BlockHash: <上面的 hash>,\n      Expires: time.Now().Add(60s),\n    }\n", isOwner)
	fmt.Println("  下次命中缓存时，先检查 block hash 是否还是 canonical。")
	fmt.Println("  不是 → 缓存失效，重新验证。这就是项目里 nft_gate.go 的逻辑。")
}

// ——— 无网络时演示 ———
func dryRun() {
	fmt.Println("=== 阶段一（模拟）===")
	fmt.Println("  keccak256('ownerOf(uint256)') → 0x6352211e")
	fmt.Println("  调用数据 = 0x6352211e + padded(tokenID)")
	fmt.Println("  eth_call 返回 addr → 和请求的 addr 比对")
	fmt.Println()
	fmt.Println("=== 阶段二（模拟）===")
	fmt.Println("  abi.JSON → abi.Pack('ownerOf', tokenID)")
	fmt.Println("  → ethclient.CallContract")
	fmt.Println("  → abi.UnpackIntoInterface")
	fmt.Println()
	fmt.Println("=== 阶段三（模拟）===")
	fmt.Println("  在阶段二基础上：")
	fmt.Println("  1. CallContract 的 blockNumber = -4 (BlockTagSafe)")
	fmt.Println("  2. 记录 blockNumber + blockHash 到缓存")
	fmt.Println("  3. 缓存命中后校验 block hash 是否仍为 canonical")
	fmt.Println("  4. 被重组 → 缓存失效 → 重新验证")
}

const ownerOfABI = `[{"constant":true,"inputs":[{"name":"tokenId","type":"uint256"}],"name":"ownerOf","outputs":[{"name":"","type":"address"}],"type":"function"}]`
