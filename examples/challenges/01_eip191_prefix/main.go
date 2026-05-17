// Challenge 1: EIP-191 签名前缀
//
// BUG: 下面的签名验证代码缺少 EIP-191 前缀。
// MetaMask 钱包签名的消息包含 \x19Ethereum Signed Message:\n 前缀，
// 但这段代码直接哈希原始消息，导致验签失败。
//
// 任务: 修复 signMessage 函数，加入正确的 EIP-191 前缀。
//
// 期待结果:
//
//	go test -v ./examples/challenges/01_eip191_prefix/
//	✓ VerifySignature_ShouldPass 通过
package main

import (
	"fmt"

	"github.com/ethereum/go-ethereum/crypto"
)

// signMessage 对消息进行以太坊签名（目前缺少 EIP-191 前缀）
func signMessage(privateKeyHex, message string) ([]byte, error) {
	privateKey, err := crypto.HexToECDSA(privateKeyHex)
	if err != nil {
		return nil, fmt.Errorf("私钥加载失败: %w", err)
	}

	prefixed := fmt.Sprintf("\x19Ethereum Signed Message:\n%d%s", len(message), message)
	hash := crypto.Keccak256([]byte(prefixed))

	return crypto.Sign(hash, privateKey)
}

// recoverAddress 从签名恢复出签名者地址
func recoverAddress(message string, sig []byte) (string, error) {
	prefixed := fmt.Sprintf("\x19Ethereum Signed Message:\n%d%s", len(message), message)
	hash := crypto.Keccak256([]byte(prefixed))

	if len(sig) == 65 && sig[64] >= 27 {
		sig[64] -= 27
	}

	pub, err := crypto.SigToPub(hash, sig)
	if err != nil {
		return "", fmt.Errorf("验签失败: %w", err)
	}

	addr := crypto.PubkeyToAddress(*pub)
	return addr.Hex(), nil
}

func main() {
	fmt.Println("Challenge 1: 修复 EIP-191 签名前缀")
}
