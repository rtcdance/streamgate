package main

import (
	"fmt"
	"strings"
)

func moduleBlockTag() {
	header("模块 4: BlockTag 安全读取")

	// ─── 概念 ───
	step(1, "为什么关心 BlockTag?")

	fmt.Println("  eth_call 的最后一个参数指定读取哪个区块的数据:")
	fmt.Println()

	fmt.Printf("  %s  BlockNumber = nil  → latest (默认)%s\n", faint, reset)
	fmt.Printf("  %s  BlockNumber = -4   → safe%s\n", faint, reset)
	fmt.Printf("  %s  BlockNumber = -3   → finalized%s\n", faint, reset)
	fmt.Println()

	info("问题: 用 latest 做 NFT 验证有被 reorg 绕过的风险")
	separator()

	// ─── Reorg 场景 ───
	step(2, "Reorg 攻击场景")

	fmt.Println("  1. 攻击者 A 拥有 NFT #42")
	fmt.Println("  2. A 调用 StreamGate API 验证 NFT -> 通过")
	fmt.Println("  3. A 卖出 NFT #42 给 B（交易上链在 block 100）")
	fmt.Println("  4. 同时，A 做了一笔大交易引发了叔块（uncle）")
	fmt.Println()

	fmt.Printf("  %s  时间线:%s\n", bold, reset)
	fmt.Printf("  %s    Block 100: A 把 NFT 转给 B%s\n", faint, reset)
	fmt.Printf("  %s    Block 101 (叔块): 包含 A 的转账%s\n", faint, reset)
	fmt.Printf("  %s    Block 101': 矿工选择了另一个叔块 → block 100 被重组%s\n", faint, reset)
	fmt.Printf("  %s    → NFT 回到 A 手中! A 可以再次验证通过%s\n", red, reset)
	fmt.Println()

	info("这是概率性攻击，但在高价值场景下不可忽视")
	separator()

	// ─── BlockTag 图解 ───
	step(3, "BlockTag 层级图解")

	blocks := []struct {
		num  int64
		tag  string
		note string
	}{
		{-1, "pending", "未打包的交易"},
		{0, "latest", "最新区块（可能被重组）"},
		{-4, "safe", "信标链确认，~4 epoch ≈ 25 分钟"},
		{-3, "finalized", "最终确认，不会被重组"},
	}

	for _, b := range blocks {
		note := b.note
		if b.tag == "safe" || b.tag == "finalized" {
			note = green + note + reset
		}
		if b.tag == "latest" {
			note = yellow + note + reset
		}
		fmt.Printf("  %sBlockTag(%d)%s  → %-12s %s\n",
			cyan, b.num, reset, b.tag, note)
	}
	fmt.Println()

	detail("本项目默认值", "safe (-4)")
	detail("回退策略", "RPC 不支持 safe → latest")
	detail("缓存时长", "60 秒 (NFTAccessEntry.Expires)")
	separator()

	// ─── 代码模式 ───
	step(4, "项目中的 BlockTagSafe 模式")

	code(`  // pkg/web3/nft.go
  const BlockTagSafe = -4

  func (v *NFTVerifier) VerifyAccess(
      ctx context.Context, chain string, contract common.Address,
      tokenID uint64, user common.Address,
  ) (bool, error) {
      // 用 safe block 读取，防 reorg
      blockNum := big.NewInt(BlockTagSafe)
      result, err := v.client.CallContract(ctx, msg, blockNum)
      if err != nil {
          // 如果 RPC 不支持 safe tag，回退到 latest
          result, err = v.client.CallContract(ctx, msg, nil)
          if err != nil {
              return false, err
          }
      }
      // ... 解码 result, 比对 owner
  }`)

	fmt.Println()
	fmt.Printf("  %s注意:%s 如果 RPC 不支持 safe/finalized, 会自动回退\n", yellow, reset)
	fmt.Println("  这是兼容性设计, 不是安全性降级")
	separator()

	// ─── 项目中的缓存策略 ───
	step(5, "配合缓存的 reorg 检测")

	fmt.Println("  pkg/web3/reorg.go 实现了 BlockHash 跟踪:")
	fmt.Println()

	fmt.Printf("  %s  NFTAccessEntry {%s\n", faint, reset)
	fmt.Printf("  %s    HasNFT:      bool,%s\n", faint, reset)
	fmt.Printf("  %s    BlockNumber: uint64,  // 验证时的区块号%s\n", faint, reset)
	fmt.Printf("  %s    BlockHash:   string,  // 验证时的区块哈希%s\n", faint, reset)
	fmt.Printf("  %s    Expires:     time.Time,%s\n", faint, reset)
	fmt.Printf("  %s  }%s\n", faint, reset)
	fmt.Println()
	fmt.Println("  缓存命中后, 对比当前 canonical 链的 block hash:")
	fmt.Println("  · 匹配 → 缓存有效（区块没被重组）")
	fmt.Println("  · 不匹配 → 缓存失效, 重新验证")
	fmt.Println()

	info("总体策略: BlockTagSafe 读取 + BlockHash 缓存验证")
	info("双层防护: 读取时防 reorg + 缓存命中后检测 reorg")
	fmt.Println()

	// ─── 关键差异 ───
	section("传统 DB vs Web3 读取差异")

	type row struct {
		traditional string
		web3        string
	}
	rows := []row{
		{"SELECT ... → 永远读到最新", "eth_call → 可指定任何历史区块"},
		{"事务隔离级别", "BlockTag 层级选择"},
		{"MVCC 快照", "state at block N"},
		{"没有\"已消失\"的数据", "reorg 让已确认的块消失"},
	}

	fmt.Printf("  %-35s %-35s%s\n", bold+"传统数据库", "Web3 区块链"+reset, reset)
	fmt.Println("  " + strings.Repeat("─", 70))
	for _, r := range rows {
		fmt.Printf("  %-35s %-35s\n", r.traditional, r.web3)
	}
	fmt.Println()

	promptExit()
}
