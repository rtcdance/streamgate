// Challenge 3: RPC 调用必须带超时
//
// BUG: 下面的函数使用 context.Background() 做 RPC 调用，
// 没有设置超时。如果 RPC 节点无响应，调用会永久阻塞。
//
// 任务: 给 queryBalance 和 queryBlockNumber 加上合理的超时。
//
// 期待结果:
//
//	go test -v ./examples/challenges/03_rpc_timeout/
//	✓ TestRPCWithTimeout 通过
package main

import (
	"context"
	"fmt"
	"time"
)

// rpcResponse 模拟 RPC 返回
type rpcResponse = string

// MockRPCClient 模拟一个可能无响应的 RPC 节点
type MockRPCClient struct {
	// 如果 > 0, BalanceAt 和 BlockNumber 会在这个延迟后返回
	simulateLatency time.Duration
	// 如果 true, 调用会永久阻塞（模拟 RPC 挂死）
	shouldHang bool
}

func (m *MockRPCClient) BalanceAt(ctx context.Context, address string) (*string, error) {
	if m.shouldHang {
		// 模拟 RPC 挂起
		select {}
	}

	select {
	case <-time.After(m.simulateLatency):
		result := "100"
		return &result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (m *MockRPCClient) BlockNumber(ctx context.Context) (uint64, error) {
	if m.shouldHang {
		select {}
	}

	select {
	case <-time.After(m.simulateLatency):
		return 42, nil
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

// BUG: queryBalance 没有设置超时 context，如果 RPC 挂死会永久阻塞
func queryBalance(client *MockRPCClient, address string) (*string, error) {
	// 用 context.Background() — 没有超时!
	ctx := context.Background()
	return client.BalanceAt(ctx, address)
}

// BUG: queryBlockNumber 同样没有超时
func queryBlockNumber(client *MockRPCClient) (uint64, error) {
	ctx := context.Background()
	return client.BlockNumber(ctx)
}

func main() {
	fmt.Println("Challenge 3: 修复 RPC 超时")
}
