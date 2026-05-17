// Interactive CLI learning tool for Web3+Go concepts.
//
// Usage:
//
//	go run ./cmd/learn          # Interactive menu mode
//	go run ./cmd/learn --list   # List all modules
//	go run ./cmd/learn --run 1  # Run module 1 directly
//
// No external dependencies — pure Go stdlib + go-ethereum.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
)

func main() {
	listMode := flag.Bool("list", false, "List all modules")
	runModule := flag.Int("run", 0, "Run a specific module by number")
	flag.Parse()

	modules := []struct {
		num    int
		title  string
		desc   string
		runner func()
	}{
		{1, "传统签名 vs EIP-191 链上签名",
			"HMAC-SHA256 对比 EIP-191 以太坊签名：理解 Web3 身份认证的核心差异",
			moduleSignature},
		{2, "big.Int 可变性陷阱",
			"为什么两个 *big.Int 指向同一个对象会互相影响？如何安全使用",
			moduleBigInt},
		{3, "RPC 调用必须带超时",
			"context.Background() 的风险和 context.WithTimeout 的正确用法",
			moduleRPCTimeout},
		{4, "BlockTag 安全读取",
			"为什么 NFT 验证不能用 latest block，safe/finalized 标签如何防 reorg",
			moduleBlockTag},
		{5, "ABI 编码详解",
			"从字节级别理解智能合约调用：函数选择器 + 32字节参数编码",
			moduleABI},
	}

	if *listMode {
		fmt.Println()
		fmt.Println(bold + "Web3+Go 学习模块" + reset)
		fmt.Println(strings.Repeat("─", 50))
		for _, m := range modules {
			fmt.Printf("  %d. %s\n", m.num, m.title)
			fmt.Printf("     %s\n\n", m.desc)
		}
		return
	}

	if *runModule > 0 {
		for _, m := range modules {
			if m.num == *runModule {
				clearScreen()
				m.runner()
				return
			}
		}
		fmt.Fprintf(os.Stderr, "未知模块编号: %d\n", *runModule)
		os.Exit(1)
	}

	// Interactive menu mode
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	go func() {
		<-sigCh
		fmt.Println(bold + " 再见！" + reset)
		os.Exit(0)
	}()

	for {
		clearScreen()
		fmt.Println(bold + "╔══════════════════════════════════════════╗")
		fmt.Println(bold + "║      Web3+Go 转型学习工具               ║")
		fmt.Println(bold + "╚══════════════════════════════════════════╝" + reset)
		fmt.Println()
		fmt.Println(faint + "选择一个模块，按 Enter 进入:" + reset)
		fmt.Println()

		for _, m := range modules {
			fmt.Printf("  %s%d%s  %s%s%s\n", cyan, m.num, reset, bold, m.title, reset)
			fmt.Printf("       %s%s%s\n", faint, m.desc, reset)
			fmt.Println()
		}

		fmt.Printf("  %s  %s%s\n", faint, "q  退出", reset)
		fmt.Printf("  %s  %s%s\n", faint, "l  列出所有模块", reset)
		fmt.Println()
		fmt.Print("请输入编号: ")

		var input string
		fmt.Scanln(&input)
		input = strings.TrimSpace(input)

		switch input {
		case "q", "quit", "exit":
			fmt.Println(bold + " 再见！" + reset)
			return
		case "l", "list":
			fmt.Printf(bold + "模块列表:" + reset + "\n")
			for _, m := range modules {
				fmt.Printf("  %d. %s — %s\n", m.num, m.title, m.desc)
			}
			fmt.Print(faint + "按 Enter 继续..." + reset)
			fmt.Scanln()
			continue
		}

		var found bool
		for _, m := range modules {
			if fmt.Sprintf("%d", m.num) == input {
				clearScreen()
				m.runner()
				found = true
				break
			}
		}
		if !found {
			fmt.Printf("\n  %s无效输入: %s%s\n", yellow, input, reset)
			fmt.Print(faint + "按 Enter 继续..." + reset)
			fmt.Scanln()
		}
	}
}

func clearScreen() {
	fmt.Print("\033[H\033[2J")
}
