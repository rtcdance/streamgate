package main

import (
	"testing"
)

// 正常场景: RPC 支持 safe tag，且 safe 和 latest 一致
func TestVerifyOwner_HappyPath(t *testing.T) {
	state := &chainState{
		latestOwners:    map[uint64]string{42: "0xAlice"},
		safeOwners:      map[uint64]string{42: "0xAlice"},
		supportsSafeTag: true,
	}
	client := &MockEthClient{state: state}

	owner, err := VerifyOwner(client, 42)
	if err != nil {
		t.Fatalf("VerifyOwner failed: %v", err)
	}
	if owner != "0xAlice" {
		t.Errorf("expected 0xAlice, got %s", owner)
	}
}

// Reorg 场景: safe 和 latest 不同（区块被重组）
// latest 已被重组影响，safe 是最终确认的正确状态
func TestVerifyOwner_ReorgProtection(t *testing.T) {
	state := &chainState{
		// latest 显示 NFT 已被转移到 0xBob（重组后）
		latestOwners: map[uint64]string{42: "0xBob"},
		// 但 safe 状态显示 0xAlice 才是 final 持有者
		safeOwners:      map[uint64]string{42: "0xAlice"},
		supportsSafeTag: true,
	}
	client := &MockEthClient{state: state}

	owner, err := VerifyOwner(client, 42)
	if err != nil {
		t.Fatalf("VerifyOwner failed: %v", err)
	}

	if owner != "0xAlice" {
		t.Errorf("应返回 safe 状态的持有者 0xAlice\n  got: %s\n  want: 0xAlice\n\n提示: VerifyOwner 需要先用 BlockTagSafe 查询，latest 可能已被重组",
			owner)
	}
}

// 回退场景: RPC 不支持 safe tag
func TestVerifyOwner_FallbackOnUnsupported(t *testing.T) {
	state := &chainState{
		latestOwners:    map[uint64]string{42: "0xAlice"},
		safeOwners:      map[uint64]string{42: "0xAlice"},
		supportsSafeTag: false,
	}
	client := &MockEthClient{state: state}

	// 虽然不支持 safe，但应回退到 latest
	owner, err := VerifyOwner(client, 42)
	if err != nil {
		t.Fatalf("VerifyOwner 回退失败: %v\n\n提示: 需要处理 safe tag 不支持的错误，回退到 latest", err)
	}
	if owner != "0xAlice" {
		t.Errorf("expected 0xAlice, got %s", owner)
	}
}
