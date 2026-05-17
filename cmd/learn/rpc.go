package main

import "fmt"

func moduleRPCTimeout() {
	header("模块 3: RPC 调用必须带超时")

	step(1, "问题: context.Background() 无超时")

	fmt.Printf("  %s错误代码:%s\n", bold, reset)
	code("  balance, err := client.BalanceAt(\n    context.Background(),  // ← 永不超时!\n    addr, nil,\n  )")

	info("如果 RPC 节点挂了但 TCP 连接没断开:")
	fmt.Println("  . Go 的 http.Client 默认没有超时")
	fmt.Println("  . ethclient 的内部 goroutine 等待响应")
	fmt.Println("  . 调用者永久阻塞")
	fmt.Println("  . goroutine 泄漏")
	fmt.Println()

	detail("阻塞后果", "goroutine 泄漏 → 内存持续增长 → OOM")
	detail("无超时调用", "占住连接池 → 新请求排队 → 连锁故障")
	separator()

	step(2, "RPC 调用生命周期")

	fmt.Printf("  %s  [dial]───[request]───[response]%s\n", faint, reset)
	fmt.Println()

	fmt.Println("  如果 RPC 节点无响应:")
	fmt.Printf("  %s  [dial]───[... 永久等待 ...]%s\n", red, reset)
	fmt.Println()

	info("带超时的正确做法:")
	fmt.Printf("  ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)\n")
	fmt.Printf("  defer cancel()\n")
	fmt.Printf("  balance, err := client.BalanceAt(ctx, addr, nil)\n")
	fmt.Println()

	detail("5 秒超时后", "ctx.Done() → 调用返回错误")
	detail("defer cancel()", "资源释放，无泄漏")
	separator()

	step(3, "超时 vs 无超时对比")
	info("无超时:")
	fmt.Println("    . goroutine 数: 持续增长")
	fmt.Println("    . 连接池: 被阻塞调用占满")
	fmt.Println("    . 内存: 每 5 分钟泄漏 ~200KB")
	fmt.Println()

	info("有超时:")
	fmt.Println("    . goroutine 数: 稳定")
	fmt.Println("    . 连接池: 调用失败即释放")
	fmt.Println("    . 内存: 稳定")
	separator()

	step(4, "推荐模式")

	code(`  func queryBalance(ctx context.Context, client *ethclient.Client, addr common.Address) (*big.Int, error) {
      queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
      defer cancel()
      balance, err := client.BalanceAt(queryCtx, addr, nil)
      if err != nil {
          return nil, fmt.Errorf("balance query: %w", err)
      }
      return balance, nil
  }`)

	fmt.Println()
	fmt.Printf("  %s关键原则:%s\n", bold, reset)
	fmt.Println("  1. 每条 RPC 调用都必须有超时")
	fmt.Println("  2. 超时值: 短查询 5s, 长查询 30s, 交易回执 120s")
	fmt.Println("  3. 继承上游 context (HTTP 请求取消 → RPC 调用取消)")
	fmt.Println("  4. 始终 defer cancel()")
	separator()

	step(5, "项目中的实现")

	fmt.Println("  pkg/web3/contract.go:")
	fmt.Printf("  %s    ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)%s\n", faint, reset)
	fmt.Printf("  %s    defer cancel()%s\n", faint, reset)
	fmt.Println()
	fmt.Println("  withChainClient 泛型 (pkg/web3/client.go):")
	fmt.Printf("  %s    每次 RPC 调用都接受 context 参数%s\n", faint, reset)
	fmt.Printf("  %s    超时由调用者决定, 不隐式假设%s\n", faint, reset)

	promptExit()
}
