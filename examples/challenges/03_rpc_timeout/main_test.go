package main

import (
	"fmt"
	"testing"
	"time"
)

func TestRPCWithTimeout(t *testing.T) {
	// 创建一个延迟 3 秒的模拟 RPC 客户端
	client := &MockRPCClient{
		simulateLatency: 3 * time.Second,
		shouldHang:      false,
	}

	done := make(chan string, 1)
	go func() {
		balance, err := queryBalance(client, "0xTest")
		if err != nil {
			done <- fmt.Sprintf("queryBalance 错误: %v", err)
			return
		}
		done <- fmt.Sprintf("余额: %s", *balance)
	}()

	select {
	case result := <-done:
		t.Logf("结果: %s", result)
	case <-time.After(500 * time.Millisecond):
		t.Errorf("调用超时! —— queryBalance 用了 3 秒的 context.Background()，但测试只等了 0.5 秒\n\n提示: 给 queryBalance 加上 context.WithTimeout(ctx, 1*time.Second)")
	}
}

func TestRPCErrorOnTimeout(t *testing.T) {
	// 创建挂死的 RPC 客户端
	client := &MockRPCClient{shouldHang: true}

	done := make(chan error, 1)
	go func() {
		_, err := queryBlockNumber(client)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Errorf("应返回超时错误，但没有返回错误\n\n提示: queryBlockNumber 应该用 context.WithTimeout，超时后 ctx.Done() 会触发")
			return
		}
		t.Logf("正确返回错误: %v", err)
	case <-time.After(2 * time.Second):
		t.Errorf("queryBlockNumber 永久阻塞! —— context.Background() 不会超时\n\n提示: 加上 context.WithTimeout, 超时时间建议 500ms")
	}
}
