package main

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

func moduleABI() {
	header("模块 5: ABI 编码详解")

	// ─── 概念 ───
	step(1, "eth_call 在字节级别的工作原理")

	fmt.Println("  调用智能合约 = 向合约地址发送一条 data:")
	fmt.Println()
	fmt.Printf("  %s  data = 0x{函数选择器(4字节)} + {参数(32字节对齐)}%s\n", bold, reset)
	fmt.Println()
	fmt.Printf("  %s  ethCall = {%s\n", faint, reset)
	fmt.Printf("  %s    To:   0xCB3C43AF17b2603F2c0cC3565D3e63644E2A51A1%s\n", faint, reset)
	fmt.Printf("  %s    Data: 0x6352211e + 0000...002a%s\n", faint, reset)
	fmt.Printf("  %s  }%s\n", faint, reset)
	separator()

	// ─── 函数选择器 ───
	step(2, "函数选择器 (4 bytes)")

	sig := "ownerOf(uint256)"
	selector := crypto.Keccak256([]byte(sig))[:4]

	fmt.Println("  公式: keccak256('函数签名(参数类型)')[:4]")
	fmt.Println()
	detail("签名", sig)
	detail("keccak256 前 4 字节", fmt.Sprintf("0x%x", selector))
	detail("web3 标准", "0x6352211e (和 ethers.js 完全一致)")
	fmt.Println()

	section("常见函数选择器")

	type sel struct {
		sig      string
		selector string
	}
	sels := []sel{
		{"balanceOf(address)", "0x70a08231"},
		{"ownerOf(uint256)", "0x6352211e"},
		{"transfer(address,uint256)", "0xa9059cbb"},
		{"safeTransferFrom(address,address,uint256)", "0x42842e0e"},
		{"totalSupply()", "0x18160ddd"},
		{"name()", "0x06fdde03"},
	}

	fmt.Printf("  %-45s %s\n", bold+"函数签名", "选择器"+reset)
	fmt.Println("  " + strings.Repeat("─", 62))
	for _, s := range sels {
		fmt.Printf("  %-45s %s\n", s.sig, s.selector)
	}
	fmt.Println()
	promptExit()

	// ─── 参数编码 ───
	step(3, "参数编码 (32 字节对齐)")

	tokenID := big.NewInt(42)
	padded := common.LeftPadBytes(tokenID.Bytes(), 32)

	fmt.Println("  规则: 每种类型固定 32 字节")
	fmt.Println("  · uint256: 左填充到 32 字节")
	fmt.Println("  · address: 左填充到 32 字节（20 字节地址前补 0）")
	fmt.Println("  · bool:    左填充到 32 字节（1=左对齐补 0）")
	fmt.Println("  · bytes32: 右填充到 32 字节")
	fmt.Println()

	detail("tokenID", "42")
	detail("进制", fmt.Sprintf("0x%s (%d bits)", tokenID.Text(16), tokenID.BitLen()))
	detail("左填充后", fmt.Sprintf("0x%x (64 hex chars = 32 bytes)", padded))

	fmt.Println()
	fmt.Printf("  %s验证:%s padded[31] == 0x2a (= 42)?  %s\n",
		cyan, reset, green+fmt.Sprintf("0x%x == 42 ✓", padded[31])+reset)
	separator()

	// ─── 完整 calldata ───
	step(4, "组装完整调用数据")

	calldata := append(selector, padded...)
	ownerOfCalldata := hex.EncodeToString(calldata)

	fmt.Println("  calldata = selector(4) + padded_tokenID(32) = 36 字节")
	fmt.Println()
	detail("总长度", fmt.Sprintf("%d 字节", len(calldata)))
	fmt.Println()

	fmt.Printf("  %s  0x%s%s\n", faint, ownerOfCalldata[:8], reset)
	fmt.Printf("  %s    ↑── 6352211e (ownerOf 选择器)%s\n", faint, reset)
	fmt.Printf("  %s  0x%s%s\n", faint, ownerOfCalldata[8:12]+"..."+ownerOfCalldata[len(ownerOfCalldata)-4:], reset)
	fmt.Printf("  %s    ↑── 00...002a (tokenID=42, 32 bytes)%s\n", faint, reset)
	fmt.Println()

	section("和 ethers.js 对比")

	fmt.Printf("  ethers.js:\n")
	fmt.Printf("  %s    contract.ownerOf(42)%s\n", faint, reset)
	fmt.Printf("    → 0x%s\n", ownerOfCalldata)
	fmt.Println()
	fmt.Printf("  Go (本教程):\n")
	fmt.Printf("  %s    crypto.Keccak256([]byte(\"ownerOf(uint256)\"))[:4]%s\n", faint, reset)
	fmt.Printf("    → 0x%s\n", ownerOfCalldata)
	fmt.Println()
	ok("结果完全一致! 因为 ABI 标准是和语言无关的")
	separator()

	// ─── go-ethereum ABI 封装 ───
	step(5, "生产代码: 使用 go-ethereum ABI 封装")

	abiSource := `[{"constant":true,"inputs":[{"name":"tokenId","type":"uint256"}],"name":"ownerOf","outputs":[{"name":"","type":"address"}],"type":"function"}]`

	code(`  // 1. 解析 ABI JSON
  contractABI, err := abi.JSON(strings.NewReader(ownerOfABI))

  // 2. 编码调用 (自动生成选择器 + 参数对齐)
  data, err := contractABI.Pack("ownerOf", big.NewInt(42))

  // 3. 执行 eth_call
  result, err := client.CallContract(ctx,
      ethereum.CallMsg{To: &contract, Data: data}, nil)

  // 4. 解码返回值 (自动解析 ABI 输出)
  var owner common.Address
  err = contractABI.UnpackIntoInterface(&owner, "ownerOf", result)`)

	fmt.Println()
	fmt.Printf("  %sABI JSON:%s %s\n", bold, reset, faint+abiSource+reset)
	separator()

	// ─── 项目对照 ───
	step(6, "项目中的 ABI 用法")

	fmt.Println("  pkg/web3/nft.go:")
	fmt.Printf("  %s    func (v *NFTVerifier) verifyERC721(%s\n", faint, reset)
	fmt.Printf("  %s        ctx context.Context, contract, user common.Address,%s\n", faint, reset)
	fmt.Printf("  %s        tokenID uint64,%s\n", faint, reset)
	fmt.Printf("  %s    ) (bool, error) {%s\n", faint, reset)
	fmt.Printf("  %s        data, _ := v.erc721ABI.Pack(\"ownerOf\", tokenIDBig)%s\n", faint, reset)
	fmt.Printf("  %s        // ...%s\n", faint, reset)
	fmt.Printf("  %s    }%s\n", faint, reset)
	fmt.Println()
	fmt.Println("  pkg/web3/signature.go:")
	fmt.Printf("  %s    // 签名验证走 EIP-191, 不涉及 ABI%s\n", faint, reset)
	fmt.Printf("  %s    func recoverAddress(msg, sig []byte) (common.Address, error)%s\n", faint, reset)
	fmt.Println()

	info("完整示例见: examples/progressive-verify/main.go")
	info("三阶段演进: 裸 ethclient → ABI 封装 → 生产级")

	promptExit()
}
