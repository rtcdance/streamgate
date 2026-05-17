package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"fmt"

	"github.com/ethereum/go-ethereum/crypto"
)

func moduleSignature() {
	header("模块 1: 传统签名 vs EIP-191 链上签名")

	// ─── 生成密钥 ───
	step(1, "生成密钥对")
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		fail("密钥生成失败: " + err.Error())
		promptExit()
		return
	}
	address := crypto.PubkeyToAddress(privateKey.PublicKey)
	detail("以太坊地址", address.Hex())
	separator()

	// ─── 传统 HMAC ───
	step(2, "传统方式: HMAC-SHA256")
	message := []byte("user=alice&action=transfer&amount=100")
	secret := []byte("shared-server-secret")

	mac := hmac.New(sha256.New, secret)
	mac.Write(message)
	hmacSig := mac.Sum(nil)

	detail("消息", string(message))
	detail("密钥", "shared-server-secret (共享)")
	detail("HMAC", fmt.Sprintf("%x...%x", hmacSig[:4], hmacSig[len(hmacSig)-4:]))
	fmt.Println()

	info("  痛点: 签发方和验证方共享同一个密钥")
	info("  服务端知道密钥 → 可以伪造任何签名")
	info("  用户不知道密钥 → 不能自己生成签名")
	fmt.Println()
	promptExit()

	// ─── EIP-191 ───
	step(3, "Web3 方式: EIP-191 以太坊签名")
	msg := "I am signing in to StreamGate"
	prefixed := fmt.Sprintf("\x19Ethereum Signed Message:\n%d%s", len(msg), msg)
	hash := crypto.Keccak256([]byte(prefixed))

	eipSig, err := crypto.Sign(hash, privateKey)
	if err != nil {
		fail("签名失败: " + err.Error())
		promptExit()
		return
	}

	detail("原始消息", msg)
	detail("EIP-191 前缀", fmt.Sprintf("\\x19Ethereum Signed Message:\\n%d", len(msg)))
	detail("待签哈希", fmt.Sprintf("0x%x...%x", hash[:4], hash[len(hash)-4:]))
	detail("签名长度", fmt.Sprintf("%d 字节 (r=32, s=32, v=1)", len(eipSig)))
	fmt.Println()

	// ─── 验证 ───
	step(4, "服务端验签")
	recoveredPub, err := crypto.SigToPub(hash, eipSig)
	if err != nil {
		fail("验签失败: " + err.Error())
		promptExit()
		return
	}
	recoveredAddr := crypto.PubkeyToAddress(*recoveredPub)
	match := recoveredAddr == address

	detail("签名者地址", address.Hex())
	detail("恢复的地址", recoveredAddr.Hex())
	if match {
		ok("签名验证通过! 恢复出的地址和签名者一致")
	} else {
		fail("签名验证失败")
	}
	separator()

	// ─── 关键差异 ───
	step(5, "两种方式的核心差异")

	fmt.Printf("  %s传统 HMAC%s\n", bold, reset)
	fmt.Println("    · 对称密钥: 签发 = 验证")
	fmt.Println("    · 服务端全权控制: 可以冒充用户")
	fmt.Println("    · 用户无法自证: 没有私钥参与")
	fmt.Println("    · 适用: 服务端内部通信 (如 K8s ServiceAccount)")
	fmt.Println()

	fmt.Printf("  %sEIP-191 签名%s\n", bold, reset)
	fmt.Println("    · 非对称: 用户私钥签, 服务端公钥验")
	fmt.Println("    · 服务端无法冒充: 没有私钥")
	fmt.Println("    · 用户可自证: 用 MetaMask 亲自签名")
	fmt.Println("    · 适用: 用户身份认证 (Web3 钱包登录)")
	fmt.Println()

	info("项目中使用位置:")
	detail("签名验证", "pkg/web3/signature.go — recoverAddress 函数")
	detail("钱包登录", "pkg/service/auth_wallet.go — VerifySignature")
	detail("中间件", "pkg/middleware/nft_gate.go — NFT 门控")
	fmt.Println()

	promptExit()
}

// 展示不正确的 EIP-191 前缀会导致验签失败
func demoWrongPrefix() {
	fmt.Println()
	section("错误示例: 不加 EIP-191 前缀会怎样？")

	privateKey, _ := crypto.GenerateKey()
	address := crypto.PubkeyToAddress(privateKey.PublicKey)
	message := []byte("Sign this to authenticate")

	// 错误: 直接哈希
	wrongHash := crypto.Keccak256(message)
	wrongSig, _ := crypto.Sign(wrongHash, privateKey)

	// MetaMask 会用正确的前缀签名
	prefixed := fmt.Sprintf("\x19Ethereum Signed Message:\n%d%s", len(message), message)
	rightHash := crypto.Keccak256([]byte(prefixed))

	// 服务端用错误的方式验签 → 恢复出不同地址
	recoveredWrong, _ := crypto.SigToPub(wrongHash, wrongSig)
	recoveredRight, _ := crypto.SigToPub(rightHash, wrongSig)

	detail("原地址", address.Hex())
	detail("错误验签结果", crypto.PubkeyToAddress(*recoveredWrong).Hex())
	detail("正确验签结果", crypto.PubkeyToAddress(*recoveredRight).Hex())
	detail("是否匹配正确", fmt.Sprintf("%v", crypto.PubkeyToAddress(*recoveredRight) == address))

	fmt.Println()
	info("关键: 即使不加前缀也能验签成功（自洽），但和钱包签名不匹配")
	fmt.Println()
}
