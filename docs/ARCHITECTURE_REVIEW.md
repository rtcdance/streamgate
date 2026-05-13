# Architecture Review — Key Technical Decisions

> 定位：面试官问"讲讲你的技术决策"时，你能拿出来的文档级别的故事。
> 每个 Decision 都包含：Context → Options → Decision → Tradeoffs → How to talk about it。

---

## Decision 1: NFT Cache 绑定 Block Hash

### Context

NFT-gated streaming 的核心路径：每次 manifest 请求都查一次 `ownerOf`/`balanceOf`。如果每次都走 RPC，10K 用户的场景下 QPS 直接打满。所以必须缓存。但缓存带来了一个 Web3 特有的问题：**重组发生后，缓存的验证结果可能是假的**。

### Options

1. **无缓存，每次查链** — 安全但不可扩展
2. **纯 TTL 缓存** — 简单但存在 reorg 窗口期（用户已失去 NFT 但仍可访问）
3. **缓存 + Block Hash 绑定 + Lightweight Reorg Check** — 我们选的方案

### Decision

每次 NFT 验证后，把 `blockNumber` 和 `blockHash` 和验证结果一起写入缓存。下次缓存命中时，调用 `eth_getBlockByNumber(entry.BlockNumber)` 比较 hash。如果不一致 → 缓存失效，重新验证。

### Tradeoffs

- 每次缓存命中多一次 RPC 调用。但 `eth_getBlockByNumber` 是由节点缓存的轻量调用（~1ms），远低于 `ownerOf`（~100ms+）
- 三级缓存：Memory L1 → Redis L2 → RPC L3，L1 命中不做 reorg 检查，只对跨进程命中做

### 面试怎么讲

> "我们面临的标准 Web3 矛盾：性能要求缓存，但缓存的结果可能被重组打脸。我的方案是把验证结果和 `(blockNumber, blockHash)` 绑定，命中后做一次轻量级的 header hash 校验。成本是一个缓存节点的 `eth_getBlockByNumber`——比重新跑一次 `ownerOf` 快两个数量级。"

---

## Decision 2: JWT 签名算法从 HS256 到 RS256

### Context

原有实现全部走 HS256，auth-service 和 gateway 共享同一个 `jwtSecret`。这意味着任何持有 secret 的服务都可以签发任意令牌。

### Options

1. **保持 HS256** — 简单但安全风险（单点被攻破 = 令牌信任链全毁）
2. **RS256（RSA）** — auth-service 专签不验，其他服务专验不签
3. **ES256（ECDSA）** — 签名更小但 ecsda 密钥管理更复杂

### Decision

RS256 作为主方案，保留 HS256 作为向后兼容的 fallback。核心改动：

- `AuthService` 新增 `signToken()` 统一签发入口，根据配置调度 HMAC 或 RSA
- `JWTVerifier` 专供下游服务做验签，不暴露签发能力
- `JWTAuthMiddleware` 同时支持 HMAC 和 RSA 公钥两种验证

### Tradeoffs

- RS256 签名比 HS256 慢 ~2x（RSA 2048 vs HMAC-SHA256），但对登录流程（每秒几十次）可忽略
- 需要管理 RSA 密钥对，增加了运维成本。但可以用环境变量注入公钥，密钥轮换只需重启 gateway

### 面试怎么讲

> "HS256 的问题不是算法本身，是信任模型。所有服务共享同一个 secret，任何一个节点被攻破就能伪造身份。我们把签名权收敛到 auth-service，gateway 和 streaming 只拿公钥。这是零信任架构的分层原则——每个服务只拥有它需要的最小权限。"

---

## Decision 3: 链感知 Finality 策略

### Context

EventIndexer 硬编码了 12 个 block 的确认深度。这对 Ethereum L1 是合理的，但对 Polygon（~128 blocks）、Arbitrum（L1 finality）、Solana（32 slots）来说要么过度保守要么不够安全。

### Options

1. **统一 12 确认** — 简单但 Polygon 上有重组风险
2. **每条链单独配置** — 灵活但在代码层面没有约束力
3. **FinalityStrategy 接口 + 预置策略** — 我们选的方案

### Decision

定义 `FinalityStrategy` 接口，每条链有自己的实现：

```
Ethereum L1: 12 blocks + BlockTagSafe
Polygon:     128 blocks + BlockTagSafe
BSC:         15 blocks + BlockTagSafe
L2:          64 blocks + BlockTagFinalized
Solana:      32 slots
```

### Tradeoffs

- L2 的"64 blocks"是保守近似值，真正的 L2 finality 来自 L1 上的 state root 验证。严谨方案需要查 L1 contract，工程复杂度提升 5x。64 blocks + BlockTagFinalized 是 80/20 折中。

### 面试怎么讲

> "这不是一个纯工程问题，而是一个链知识问题。Ethereum 的 reorg 深度 ~6 blocks，但 Polygon 因为有更强的激励机制和更快的出块速度可以达到 ~128。如果你对所有链用同一个确认深度，要么 Polygon 数据错了，要么 Ethereum 延迟大了。一个公司级系统必须区分对待。"

---

## Decision 4: RPC 加权评分 Failover

### Context

原有的 failover 是轮询机制——按 RPC 列表顺序尝试，失败后跳到下一个。冷却 30s 后可重试。

### Problem

轮询没考虑两个关键因素：
1. **慢 RPC 比挂掉的 RPC 更糟糕** — 挂掉的能秒级 failover，慢的 RPC 让整个调用链都变慢
2. **RPC 质量会随时间变化** — 下午变慢的节点可能晚上恢复，轮询不会记住历史

### Decision

用加权评分代替轮询：

- 每次 RPC 调用后更新 `Score`（指数移动平均 + 延迟归一化）
  - `latencyScore = max(0, 1 - latency.Seconds() / 5.0)` — <1s = 1.0, 5s+ = 0.0
  - 成功: `newScore = oldScore * 0.9 + latencyScore * 0.1`
  - 失败: `score *= 0.5`
- `connectAny()` 和 `failover()` 按评分降序选择端点
- `GetRPCScores()` 可观测

### Tradeoffs

- EMA 需要调参（decay=0.9, threshold=5s）。这些值基于公共 RPC 节点延迟分布的中位数经验
- 极端情况：如果两个节点交替成功/失败，评分会震荡。但实际中公共 RPC 节点的行为是稳定的（要么快，要么慢，要么挂）

### 面试怎么讲

> "RPC failover 不是简单的'这个节点挂了换下一个'——最差的节点不是挂掉的，是响应慢但没报错的。我用 EMA 评分把最近 N 次调用的延迟和成败编码成一个 0-1 之间的值，每次选分最高的节点。这个评分本身就是一种健康度量，可以直接暴露给普罗米修斯。"

---

## Decision 5: NFT 三级缓存（Memory → Redis → RPC）

### Context

每个 HLS manifest 请求都需要 NFT 验证。如果 10K 用户同时请求，qps = 10K NFT 验证/s，全部走 RPC 不可能。

### Options

1. **单层 Memory LRU** — 简单但不能跨进程共享
2. **单层 Redis** — 共享但增加了网络延迟
3. **Memory L1 + Redis L2** — 我们选的方案

### Decision

```
Request → Memory LRU (TTL: 30s, 10k entries)
              ↓ miss
          Redis (TTL: 60s)
              ↓ miss
          RPC Call (L3)
```

- L1 命中：无网络开销，~µs 级
- L2 命中：一次 Redis GET，~ms 级，结果写入 L1
- L3 miss：RPC 调用，~100ms+，结果写入 L1+L2

### Tradeoffs

- 两级 TTL（30s / 60s）：L1 先失效，把读取压力回退到 L2。Redis 上的冷数据自然过期。
- Cache 承载的数据必须和 block hash 绑定（见 Decision 1）。

### 面试怎么讲

> "这是经典的缓存分层的 Web3 版本。L1 扛热点，L2 做分布式共享，L3 是最终权威来源。但这里的特殊点是：缓存结果和链上状态有关联，不是纯 TTL 就能解决的——所以每层都存了 blockNumber + blockHash。"

---

## 总结：你的面试故事线

| 问题 | 你的回答入口 |
|---|---|
| "做过 Web3 什么项目？" | "NFT-gated 流媒体分发——钱包登录→链上验 NFT→签播放令牌→HLS 分发" |
| "遇到的最大技术挑战？" | "Reorg 导致缓存失效。我引入了 block-hash 绑定的缓存失效策略" |
| "怎么处理多链？" | "每条链有自己的 FinalityStrategy，Ethereum 12 块、Polygon 128 块" |
| "RPC 挂了怎么搞？" | "加权评分 failover，不是轮询。快的节点优先，慢的自动降权" |
| "JWT 用什么算法？" | "RS256。auth-service 专签，gateway 专验，签名权和验签权分离" |
| "性能怎么样？" | "三级缓存，L1 扛本地热点，L2 做分布式共享，RPC 是最终来源" |
