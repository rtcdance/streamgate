// Challenge 2: big.Int 可变性陷阱
//
// BUG: NFT 余额查询函数重用同一个 *big.Int 对象，
// 导致并发调用时互相覆盖。
//
// 任务: 修复 getBalances 函数，确保每次查询使用独立的 *big.Int。
//
// 期待结果:
//
//	go test -v ./examples/challenges/02_bigint_mutability/
//	✓ TestBalances_ShouldBeIndependent 通过
package main

import (
	"fmt"
	"math/big"
)

// balanceDB 模拟链上余额存储
var balanceDB = map[string]*big.Int{
	"0xUser1": big.NewInt(100),
	"0xUser2": big.NewInt(200),
}

// getBalances 查询多个地址的 NFT 余额
func getBalances(addresses []string) (map[string]*big.Int, error) {
	balances := make(map[string]*big.Int)

	// BUG: 重用同一个 *big.Int 变量
	var tokenID *big.Int
	for _, addr := range addresses {
		balance, ok := balanceDB[addr]
		if !ok {
			continue
		}

		// BUG: 这里应该 new(big.Int).Set(balance) 创建新对象
		tokenID = balance
		balances[addr] = tokenID
	}

	return balances, nil
}

// calculateTotal 计算总余额（用于验证 getBalances 的调用方不会受影响）
func calculateTotal(balances map[string]*big.Int) *big.Int {
	total := big.NewInt(0)
	for _, b := range balances {
		total.Add(total, b)
	}
	return total
}

// verifyBalance 验证地址是否拥有特定数量的代币（简化版）
func verifyBalance(address string, expected *big.Int) error {
	balance, ok := balanceDB[address]
	if !ok {
		return fmt.Errorf("地址 %s 不存在", address)
	}
	if balance.Cmp(expected) != 0 {
		return fmt.Errorf("余额不匹配: 期望 %s, 实际 %s", expected.String(), balance.String())
	}
	return nil
}

func main() {
	fmt.Println("Challenge 2: 修复 big.Int 可变性陷阱")
}
