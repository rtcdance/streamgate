package main

import (
	"testing"
)

func TestEIP191Prefix_Fix(t *testing.T) {
	// 这个测试验证修复后的 signMessage 能否被钱包签名兼容
	privateKeyHex := "4c0883a69102937d6231471b5dbb6204fe5129617082792ae468d01a3f362318"
	message := "Sign in to StreamGate"

	sig, err := signMessage(privateKeyHex, message)
	if err != nil {
		t.Fatalf("signMessage failed: %v", err)
	}

	recovered, err := recoverAddress(message, sig)
	if err != nil {
		t.Fatalf("recoverAddress failed: %v", err)
	}

	// 已知这个私钥对应的地址
	expected := "0x2c7536E3605D9C16a7a3D7b1898e529396a65c23"
	if recovered != expected {
		t.Errorf("地址不匹配\n  got:  %s\n  want: %s\n\n提示: 检查 EIP-191 前缀是否正确添加", recovered, expected)
	}
}

func TestEIP191Prefix_SelfConsistent(t *testing.T) {
	privateKeyHex := "4c0883a69102937d6231471b5dbb6204fe5129617082792ae468d01a3f362318"
	message := "I am the owner of this wallet"

	sig, err := signMessage(privateKeyHex, message)
	if err != nil {
		t.Fatalf("signMessage failed: %v", err)
	}

	recovered, err := recoverAddress(message, sig)
	if err != nil {
		t.Fatalf("recoverAddress failed: %v", err)
	}

	expected := "0x2c7536E3605D9C16a7a3D7b1898e529396a65c23"
	if recovered != expected {
		t.Errorf("自洽验签失败\n  got:  %s\n  want: %s\n\n提示: signMessage 和 recoverAddress 都要加前缀", recovered, expected)
	}
}
