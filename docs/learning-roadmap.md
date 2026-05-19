# StreamGate Learning Roadmap

A 2-3 week plan for developers transitioning from traditional backend (Go) to Web3.

---

## 学习路径总览

```
Week 1: Web3 基础 + 核心代码阅读
Week 2: 流媒体架构 + 深度源码
Week 3: 实战任务 + 扩展
```

---

## Week 1: Web3 Core

### Day 1-2: 区块链基础与钱包登录

**目标：** 理解 EIP-191 签名和钱包认证流程

阅读顺序：
1. `examples/signature-verify-demo/main.go` — 最简单的 EIP-191 签名示例
2. `pkg/web3/signature.go` — 签名验证的核心实现（已加注释）
3. `pkg/service/auth_wallet.go` — 钱包登录的完整服务端实现
4. `pkg/web3/siwe.go` — EIP-4361 (Sign-In with Ethereum) 标准化登录

**核心概念：**
- EIP-191: `\x19Ethereum Signed Message:\n` 前缀
- 为什么 v 值要 ±27（MetaMask 27/28 vs go-ethereum 0/1）
- `ConstantTimeCompare` 防止时序攻击
- EIP-1271 智能合约钱包回退

**练习：** `go test -v ./examples/challenges/01_eip191_prefix/`

### Day 3-4: NFT 所有权验证

**目标：** 理解链上 NFT 验证原理

阅读顺序：
1. `examples/nft-verify-demo/main.go` — NFT 验证最小示例
2. `pkg/web3/nft.go` — ERC-721/1155 验证 + ERC-165 自动检测
3. `pkg/middleware/nft_gate.go` — NFT 门控中间件

**核心概念：**
- ERC-721 `ownerOf(tokenId)` vs ERC-1155 `balanceOf(address,id)`
- ERC-165 `supportsInterface` 自动检测 token 标准
- TOCTOU 防护：`CheckApproval` 防止验证后立即被转移
- Reorg 保护：`BlockTagSafe` / `BlockTagFinalized`

**练习：** `go test -v ./examples/challenges/02_bigint_mutability/`

### Day 5: 多链和支持

**目标：** 理解多链架构

阅读顺序：
1. `pkg/web3/multichain.go` — 多链管理器
2. `pkg/web3/chain.go` — EVM 链客户端 + RPC 故障转移
3. `pkg/web3/solana.go` + `pkg/web3/solana_client.go` — Solana 集成

**练习：** `go test -v ./examples/challenges/04_blocktag_safety/`

---

## Week 2: 流媒体架构

### Day 6-7: HLS 流媒体交付

**目标：** 理解 NFT 门控的 HLS 播放流程

阅读顺序：
1. `examples/streaming-demo/main.go` — HLS manifest 生成演示
2. `pkg/service/streaming.go` — 生产级流媒体服务
3. `pkg/gateway/streaming_handlers.go` — REST API handler

**核心概念：**
- HLS 格式：`.m3u8` manifest + `.ts` segment
- Playback token：短期令牌（2min TTL）绑定钱包 + 内容 + 合约
- 多码率自适应：同时查找多个码率的分段

### Day 8-9: 转码流水线

阅读顺序：
1. `pkg/service/transcoding.go` — FFmpeg 转码服务
2. `pkg/plugins/transcoder/ffmpeg.go` — FFmpeg 命令封装

**核心概念：**
- Optimistic DB lock：防止多 worker 竞争同任务
- 上下文超时：5min per-task，防止 FFmpeg 跑飞

### Day 10: 微内核架构

**目标：** 理解项目的核心架构模式

阅读顺序：
1. `pkg/core/microkernel.go` — 微内核 + 插件生命周期
2. `pkg/plugins/api/gateway.go` — 插件实现示例

**核心概念：**
- 拓扑排序（Kahn 算法）解决插件依赖
- 双模式部署：单体（内存总线）vs 微服务（NATS）
- 优雅回滚：Init 失败时逆转序 Stop 已初始化的插件

---

## Week 3: 实战

### Day 11-12: 挑战练习

完成所有 challenges（由易到难）：

```
01_eip191_prefix/   — EIP-191 签名前缀
02_bigint_mutability/ — Go big.Int 可变性陷阱
03_rpc_timeout/     — RPC 超时处理
04_blocktag_safety/ — 区块标签安全
```

### Day 13-14: 跟踪代码 + 扩展

- 阅读 `pkg/web3/reorg.go` — 重组成理
- 阅读 `pkg/web3/gas.go` — Gas 估算
- 阅读 `pkg/monitoring/prometheus.go` — 监控指标
- 尝试自己实现一个新的示例

### 延伸阅读

- [ARCHITECTURE_GUIDE.md](ARCHITECTURE_GUIDE.md) — 架构详解
- [Web3 Best Practices](web3-best-practices.md)
- OpenZeppelin Solidity 合约：`contracts/src/`