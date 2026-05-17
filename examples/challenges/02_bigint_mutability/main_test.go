package main

import (
	"math/big"
	"testing"
)

func TestBalances_ShouldBeIndependent(t *testing.T) {
	addresses := []string{"0xUser1", "0xUser2"}
	balances, err := getBalances(addresses)
	if err != nil {
		t.Fatalf("getBalances failed: %v", err)
	}

	// 验证每个地址的余额值
	if balances["0xUser1"].Cmp(big.NewInt(100)) != 0 {
		t.Errorf("User1 余额应为 100, 实际 %s", balances["0xUser1"].String())
	}
	if balances["0xUser2"].Cmp(big.NewInt(200)) != 0 {
		t.Errorf("User2 余额应为 200, 实际 %s", balances["0xUser2"].String())
	}

	// 关键验证: 修改一个余额不应影响另一个
	// 如果 tokenID = balance 只是指针赋值, 他们指向同一个对象
	balances["0xUser1"].Add(balances["0xUser1"], big.NewInt(50))

	if balances["0xUser2"].Cmp(big.NewInt(200)) != 0 {
		t.Errorf("BUG: User2 余额被意外修改为 %s (应为 200)\n\n提示: getBalances 中 tokenID = balance 是指针赋值, 所有 map entry 指向同一个对象",
			balances["0xUser2"].String())
	}
}

// 如果 getBalances 正确使用 new(big.Int).Set(), 调用方修改返回的 map 不应影响原始数据
func TestOriginalDataNotCorrupted(t *testing.T) {
	addresses := []string{"0xUser1", "0xUser2"}
	_, _ = getBalances(addresses)

	expected1 := big.NewInt(100)
	actual1 := balanceDB["0xUser1"]
	if actual1.Cmp(expected1) != 0 {
		t.Errorf("原始数据库被修改! User1 期望 %s, 实际 %s\n\n提示: getBalances 中的指针赋值导致 map 和 balanceDB 共享底层 big.Int 对象",
			expected1.String(), actual1.String())
	}
}
