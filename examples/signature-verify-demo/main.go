package main

import (
	"crypto/ecdsa"
	"fmt"
	"log"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
)

// 这个示例展示如何验证以太坊签名
// 这是 Web3 登录的核心机制

func main() {
	fmt.Println("=== 以太坊签名验证示例 ===\n")

	// 场景：用户想要登录你的系统
	// 1. 后端生成一个随机消息
	message := "Sign this message to login:\nNonce: abc123\nTimestamp: 1234567890"
	fmt.Printf("📝 原始消息:\n%s\n\n", message)

	// 2. 用户在前端用 MetaMask 签名（这里我们模拟）
	// 实际开发中，这一步在前端完成
	privateKey, _ := crypto.GenerateKey() // 模拟用户私钥
	signature, err := signMessage(message, privateKey)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("✍️  签名结果:\n%s\n\n", signature)

	// 3. 后端验证签名
	// 从签名中恢复出签名者的地址
	recoveredAddress, err := recoverAddress(message, signature)
	if err != nil {
		log.Fatal(err)
	}

	// 4. 比较地址
	expectedAddress := crypto.PubkeyToAddress(privateKey.PublicKey)
	fmt.Printf("🔑 预期地址: %s\n", expectedAddress.Hex())
	fmt.Printf("🔓 恢复地址: %s\n", recoveredAddress.Hex())

	if recoveredAddress == expectedAddress {
		fmt.Println("\n✅ 签名验证成功！用户身份确认")
		fmt.Println("   可以发放 JWT token 了")
	} else {
		fmt.Println("\n❌ 签名验证失败！")
	}

	// 演示：篡改消息会导致验证失败
	fmt.Println("\n=== 演示：篡改消息 ===")
	tamperedMessage := "Sign this message to login:\nNonce: HACKED\nTimestamp: 1234567890"
	recoveredAddress2, _ := recoverAddress(tamperedMessage, signature)
	fmt.Printf("🔓 恢复地址: %s\n", recoveredAddress2.Hex())
	if recoveredAddress2 != expectedAddress {
		fmt.Println("✅ 检测到篡改，验证失败（符合预期）")
	}
}

// 签名消息（模拟 MetaMask 的 personal_sign）
func signMessage(message string, privateKey *ecdsa.PrivateKey) (string, error) {
	// 1. 添加以太坊签名前缀
	// 这是 EIP-191 标准
	prefixedMessage := fmt.Sprintf("\x19Ethereum Signed Message:\n%d%s", len(message), message)

	// 2. 计算消息哈希
	hash := crypto.Keccak256Hash([]byte(prefixedMessage))

	// 3. 签名
	signature, err := crypto.Sign(hash.Bytes(), privateKey)
	if err != nil {
		return "", err
	}

	// 4. 调整 v 值（以太坊特有）
	signature[64] += 27

	return hexutil.Encode(signature), nil
}

// 从签名恢复地址
func recoverAddress(message, signatureHex string) (common.Address, error) {
	// 1. 解码签名
	signature, err := hexutil.Decode(signatureHex)
	if err != nil {
		return common.Address{}, err
	}

	// 2. 调整 v 值
	if signature[64] >= 27 {
		signature[64] -= 27
	}

	// 3. 添加以太坊签名前缀
	prefixedMessage := fmt.Sprintf("\x19Ethereum Signed Message:\n%d%s", len(message), message)

	// 4. 计算消息哈希
	hash := crypto.Keccak256Hash([]byte(prefixedMessage))

	// 5. 从签名恢复公钥
	pubKey, err := crypto.SigToPub(hash.Bytes(), signature)
	if err != nil {
		return common.Address{}, err
	}

	// 6. 从公钥计算地址
	address := crypto.PubkeyToAddress(*pubKey)

	return address, nil
}

/*
前端代码示例（JavaScript）：

// 1. 请求 nonce
const response = await fetch('/api/v1/auth/nonce', {
    method: 'POST',
    body: JSON.stringify({ address: userAddress })
});
const { message } = await response.json();

// 2. 用 MetaMask 签名
const signature = await ethereum.request({
    method: 'personal_sign',
    params: [message, userAddress]
});

// 3. 发送签名到后端验证
const loginResponse = await fetch('/api/v1/auth/verify', {
    method: 'POST',
    body: JSON.stringify({
        address: userAddress,
        signature: signature,
        chain_type: 'evm'
    })
});
const { token } = await loginResponse.json();

// 4. 保存 token，用于后续 API 调用
localStorage.setItem('auth_token', token);

关键点：
1. 消息必须包含 nonce（防重放攻击）
2. 消息必须包含时间戳（防过期）
3. nonce 只能使用一次
4. 签名验证不需要私钥（这是非对称加密的魔法）
5. 前端签名，后端验证，安全可靠
*/
