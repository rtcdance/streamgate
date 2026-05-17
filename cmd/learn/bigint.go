package main

import (
	"fmt"
	"math/big"
)

func moduleBigInt() {
	header("模块 2: big.Int 可变性陷阱")

	// ─── 问题演示 ───
	step(1, "问题: 两个变量指向同一个 *big.Int")
	a := big.NewInt(5)
	b := a // 同一个指针

	detail("a 初始值", a.String())
	detail("b = a", "指向同一对象")
	fmt.Println()

	fmt.Printf("  %sb.Add(b, 1) 之后...%s\n\n", bold, reset)
	b.Add(b, big.NewInt(1))

	detail("a 期望值", "5")
	detail("a 实际值", a.String())
	detail("b 实际值", b.String())
	fmt.Println()

	fail("a 被意外修改了! b.Add 修改了接收者 b，但 a 指向同一块内存")
	separator()

	// ─── 实际场景 ───
	step(2, "生产代码中的真实场景")

	fmt.Println("  在 Web3 开发中, big.Int 随处可见:")
	fmt.Println()
	fmt.Printf("  %s  // tokenID = 42, 给另一个函数用%s\n", faint, reset)
	fmt.Printf("  %s  tokenID := big.NewInt(42)%s\n", faint, reset)
	fmt.Printf("  %s  result := callContract(tokenID)  // tokenID 被修改!%s\n", faint, reset)
	fmt.Printf("  %s  tokenID.Add(tokenID, big.NewInt(1))  // 原始值已变%s\n", faint, reset)
	fmt.Println()

	info("go-ethereum 中几乎所有参数都是 *big.Int")
	info("ChainID, BlockNumber, Value, GasPrice, TokenID...")
	separator()

	// ─── 正确做法 ───
	step(3, "解决方案: 显式拷贝")

	c := big.NewInt(5)
	d := new(big.Int).Set(c)
	fmt.Printf("  d = new(big.Int).Set(c) → d 是独立拷贝\n\n")

	d.Add(d, big.NewInt(1))

	detail("c 值", c.String())
	detail("d 值", d.String())

	if c.Int64() == 5 && d.Int64() == 6 {
		ok("显式拷贝正确! c 未被修改, d 是独立值")
	}
	separator()

	// ─── 安全模式 ───
	step(4, "更安全的模式: 用方法返回值而非修改")

	e := big.NewInt(10)
	f := new(big.Int).Add(e, big.NewInt(5))
	// 不调用 e.Add，而是用 new(big.Int).Add 创建新值

	detail("e 不变", e.String())
	detail("f = e + 5", f.String())

	fmt.Println()
	fmt.Printf("  %s模式对比:%s\n", bold, reset)
	fmt.Println()
	fmt.Printf("  %s  危险  e.Add(e, x)  // e 被修改%s\n", red, reset)
	fmt.Printf("  %s  安全  new(big.Int).Add(e, x)  // e 不变%s\n", green, reset)
	separator()

	// ─── 项目对照 ───
	step(5, "项目中的正确用法")

	fmt.Println("  pkg/web3/nft.go 中:")
	fmt.Printf("  %s    tokenIDBig := new(big.Int).SetUint64(tokenID)%s\n", faint, reset)
	fmt.Printf("  %s    result, err := client.CallContract(ctx, msg, big.NewInt(BlockTagSafe))%s\n", faint, reset)
	fmt.Println()
	fmt.Println("  每次都创建新的 *big.Int，绝不复用接收者")

	fmt.Println()
	fmt.Println("  pkg/web3/contract.go 中:")
	fmt.Printf("  %s    // 用 SetBytes 从 [32]byte 创建独立 big.Int%s\n", faint, reset)
	fmt.Printf("  %s    val := new(big.Int).SetBytes(b[:])%s\n", faint, reset)
	fmt.Println()

	promptExit()
}
