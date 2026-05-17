// Challenge 4: BlockTag 安全读取
//
// BUG: NFT 验证函数 VerifyOwner 使用了 nil (latest) 作为区块标签。
// latest block 可能被重组：攻击者先验证通过，卖掉 NFT，然后利用重组
// 让验证结果失效。
//
// 注意看 MockEthClient 的实现: getOwner(nil) 返回"当前状态"（可能被 reorg），
// getOwner(BlockTagSafe) 返回"最终确认状态"（不会被 reorg）。
//
// 任务: 修改 VerifyOwner，先用 BlockTagSafe 验证，如果不支持再回退到 latest。
//
// 期待结果:
//
//	go test -v ./examples/challenges/04_blocktag_safety/
//	✓ TestVerifyOwner_ShouldUseSafeBlock 通过
//	✓ TestVerifyOwner_FallbackOnUnsupported 通过
package main

import (
	"errors"
	"fmt"
	"math/big"
)

// BlockTagSafe 对应信标链的 safe 标签（约 4 个 epoch 确认）
const BlockTagSafe = -4

// 一个链上可以有多个状态视图:
type chainState struct {
	// latestOwners: 最新区块的 owner 映射（可能被重组）
	latestOwners map[uint64]string
	// safeOwners: 已被信标链最终确认的 owner 映射（不可重组）
	safeOwners map[uint64]string
	// RPC 节点是否支持 safe/finalized block tag
	supportsSafeTag bool
}

// MockEthClient 模拟以太坊客户端
type MockEthClient struct {
	state *chainState
}

// getOwner 返回指定区块状态的 owner。
// - blockNum == nil → latest（可能被 reorg）
// - blockNum == BlockTagSafe → safe（最终确认）
func (c *MockEthClient) getOwner(tokenID uint64, blockNum *big.Int) (string, error) {
	if blockNum != nil && blockNum.Int64() == BlockTagSafe {
		if !c.state.supportsSafeTag {
			return "", errors.New("RPC 不支持 safe block tag")
		}
		owner, ok := c.state.safeOwners[tokenID]
		if !ok {
			return "", fmt.Errorf("token %d 不存在 (safe)", tokenID)
		}
		return owner, nil
	}

	// nil → latest（默认）
	owner, ok := c.state.latestOwners[tokenID]
	if !ok {
		return "", fmt.Errorf("token %d 不存在 (latest)", tokenID)
	}
	return owner, nil
}

func VerifyOwner(client *MockEthClient, tokenID uint64) (string, error) {
	owner, err := client.getOwner(tokenID, big.NewInt(BlockTagSafe))
	if err != nil {
		owner, err = client.getOwner(tokenID, nil)
		if err != nil {
			return "", fmt.Errorf("NFT 验证失败: %w", err)
		}
	}
	return owner, nil
}

func main() {
	fmt.Println("Challenge 4: 修复 BlockTag 安全")
}
