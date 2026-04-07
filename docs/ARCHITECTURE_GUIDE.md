# StreamGate 架构评审 + 开发指南

> 定位：面向音视频工程师转型 Web3 + Go 的项目评审报告 + 开发路线图 + 面试讲稿
> 用户：10+ 年 C++ / 音视频后端经验，转型 Go + Web3，目标 L4-L5
> 核心：音视频分发主线 + Web3 NFT 鉴权增量，单体优先（学习调试）+ 微服务（生产扩展）
> 学习路径：用本指南 + web3-faq + learning-roadmap 把项目收敛成可跑、可讲、可面试

---

## 一、硬性要求：禁止 Mock 数据

### 1.1 铁律

> **⚠️ 禁止任何 Mock/假数据。链上状态必须来自真实链上；metadata 等链下资源也必须通过真实链上入口（如 `tokenURI`）解析，而不是本地伪造。**

| 模块 | 禁止 | 必须 |
|------|------|------|
| **NFT 验证** | `return 0` 占位符 | 调用 `balanceOf`/`ownerOf` 真实合约 |
| **签名验证** | `return true` 假验证 | 恢复地址 + 比较 |
| **余额查询** | 返回固定值 | `eth_getBalance` 真实链上 |
| **交易状态** | `return "success"` | `eth_getTransactionReceipt` 真实状态 |
| **TokenURI** | `return "https://..."` 占位符 | 至少调用合约 `tokenURI` 方法；仅拿到 tokenURI 不等于完整元数据链路已完成 |
| **Gas 价格** | `return 30` 固定值 | `eth_gasPrice` 实时获取 |

### 1.2 真实测试网配置

```yaml
# config/config.dev.yaml
web3:
  chains:
    sepolia:
      chain_id: 11155111
      rpc_url: https://rpc.sepolia.org
      explorer: https://sepolia.etherscan.io
      nft_contracts:
        - address: "0x8667b7bdf8f27e76200fa450bf48aa78bbbcc61f"
          type: ERC721
          name: "StreamGate Test NFT"
    amoy:
      chain_id: 80002
      rpc_url: https://rpc-amoy.polygon.technology
      explorer: https://amoy.polygonscan.com
```

### 1.3 已确认的真实合约

| 链 | 合约地址 | 类型 | 验证方法 |
|----|----------|------|----------|
| **Sepolia** | `0x8667b7bdf8f27e76200fa450bf48aa78bbbcc61f` | ERC-721 | `balanceOf` |
| **Amoy** | 待部署 | ERC-721/1155 | `balanceOf` |

### 1.4 Mock 数据检测命令

```bash
# 检查 NFT 占位符
grep -n "return 0, nil" pkg/web3/chain.go          # 应无输出
grep -n "return common.Address{}" pkg/web3/chain.go  # 应无输出

# 检查时间戳占位符
grep -n "return 0$" pkg/web3/signature.go            # 应无输出

# 检查假 tokenURI
grep -rn "https://metadata.example.com" pkg/web3/    # 应无输出

# 验证真实合约调用存在
grep -n "CallContract" pkg/web3/chain.go             # 应有输出
```

---

## 二、架构评审核心结论

### 2.1 当前最大问题（Top 3）

| 优先级 | 问题 | 影响 | 修复难度 |
|--------|------|------|----------|
| 🔴 P0 | RPC 单节点，无高可用 | 外部依赖脆弱，无法贴近企业实践 | 中 |
| 🟡 P1 | 音视频主线优势还可以再讲得更突出 | 你的核心竞争力需要继续转成项目卖点 | 低 |
| 🟡 P1 | 部分监控与可观测性路径仍需要继续收口 | 架构看起来完整，验收口径还可进一步统一 | 中 |
| 🟡 P2 | 文档需要持续和代码状态保持同步 | 避免面试讲述时出现口径偏差 | 低 |

### 2.1.1 面试主线应该怎么讲

> 这个项目不应该讲成“我做了一个 Web3 demo”，而应该讲成：
>
> “我把自己多年音视频后端经验沉淀到一个内容分发系统里，再把 Web3 NFT 鉴权接到内容访问控制上。
> 这样既能体现我熟悉的高并发、缓存、转码、任务调度，也能证明我具备链上鉴权、钱包登录、RPC 高可用这些新能力。”

### 2.2 企业实践对照表

| 模块 | 当前实现 | 企业标杆 | 差距 |
|------|----------|----------|------|
| **RPC 连接** | `ethclient.Dial` 单节点 | Alchemy: 多节点 + 熔断器 + 限流 | ❌ 无 failover |
| **NFT 鉴权** | `getNFTBalance` 返回 0，API 未闭环 | OpenSea: ERC-721/1155 合约调用 + 访问控制 | ❌ 核心能力未跑通 |
| **缓存** | 内存 LRU + `pkg/storage/redis.go` | OpenSea: Redis + 事件失效 | ⚠️ 组件存在，主链路未打通 |
| **监控** | 存在多套 metrics 实现，当前需先统一到 `pkg/plugins/metrics/prometheus.go` | Prometheus + Grafana | ⚠️ 路径未统一，验收口径不稳定 |
| **转码 / Worker Pool** | 插件骨架存在，执行器仍是占位 | Livepeer: 动态调度 + 重试 + 资源隔离 | ❌ 没把音视频经验转成可讲亮点 |

### 2.3 模块清单与状态

| 模块 | 文件 | 状态 | 优先级 |
|------|------|------|--------|
| **Web3 核心** | | | |
| NFT 验证 | `pkg/web3/chain.go` | ❌ 占位符 | P0 必须修 |
| 签名验证 | `pkg/web3/signature.go` | ⚠️ EIP-191 + challenge/nonce 已接通，Redis 优先 / 内存回退 | P1 继续完善 |
| 多链支持 | `pkg/web3/multichain.go` | ⚠️ RPC 无高可用 | P0 必须修 |
| Gas 监控 | `pkg/web3/gas.go` | ✅ 完整实现 | - |
| 事件索引 | `pkg/web3/event_indexer.go` | ⚠️ 解码有 TODO | P2 可选 |
| **存储层** | | | |
| Redis 缓存 | `pkg/storage/redis.go` | ✅ 完整实现 | - |
| 内存缓存 | `pkg/storage/cache.go` | ⚠️ 有数据竞争 | P1 需修 |
| **监控层** | | | |
| Metrics | `pkg/plugins/metrics/prometheus.go` | ⚠️ 推荐收敛到这一套 Prometheus registry + `/metrics` 暴露路径 | P1 推荐 |
| **服务层** | | | |
| NFT Service | `pkg/service/nft.go` | ⚠️ 所有权校验真实，元数据链路仍是占位拼装，端到端未完成 | P1 需拆分说明 |
| Auth Service | `pkg/service/auth.go` | ⚠️ 钱包 challenge 登录已接通，仍需继续补 Redis 集成与生产级 hardening | P1 继续完善 |

### 2.4 适合你背景的最终项目定位

1. 主项目名义上是 `StreamGate`，但面试定位应收敛为“NFT-gated streaming gateway”。
2. 主能力排序应是：音视频分发与转码 > 缓存与并发调度 > Web3 鉴权与访问控制 > 微服务拆分。
3. 微服务不是卖点本身，真正的卖点是“为什么这个场景需要这些拆分”。
4. Web3 不是孤立模块，而是内容访问控制的一部分：用户持有 NFT 才能拉取受保护的 HLS/DASH 内容；NFT Service 当前只代表链上所有权校验，不代表完整元数据服务。
5. `tokenURI` 调用只证明链上元数据入口是真实的，不代表已经完成“拉取 metadata JSON -> 解析字段 -> 处理异常/超时/空值”这条端到端元数据链路。

---

## 二点五、4 个核心交付物

### G1. Wallet Sign-In

**目标**：把钱包 challenge 登录做成生产可用的身份入口。

**必须完成**：
- EIP-191 签名验签
- nonce 生成与校验
- 过期时间校验
- replay attack 防护
- 登录成功后签发 JWT

**当前状态**：`pkg/service/auth.go` 中 challenge 登录、签名校验、一次性消费和 playback token 已实现

**状态模型（定案）**：
- challenge 必须包含：`wallet_address`、`nonce`、`issued_at`、`expires_at`、`used_at`、`chain_id`
- challenge 存储统一放 Redis，key 建议为：`auth:challenge:<wallet>:<nonce>`
- challenge 必须一次性消费，验证成功后立即标记 `used` 或删除
- 默认过期时间：5 分钟
- 多实例部署时禁止使用进程内 map 存储 challenge

**验收标准**：
- 错误签名无法登录
- 过期 challenge 无法登录
- 同一 challenge 不能重复使用
- challenge 在 Redis 中可查询且验证成功后被消费

**面试价值**：从传统后端转向 Web3 的第一张可信名片

---

### G2. NFT Verify

**当前状态**：真实链上 `balanceOf` / `ownerOf` 已接通，`/api/v1/nft/verify` 已接入，并支持短 TTL 缓存与 `cache_hit`；这一节只代表“所有权校验链路”已跑通，不代表 NFT metadata 获取已经端到端完成。

**目标**：把 NFT 验证完善成可持续运行的链上授权模块。

**必须完成**：
- ERC-721 `balanceOf` 或 `ownerOf` 真实调用保持可用
- `pkg/web3/chain.go` 维持真实合约调用
- `/api/v1/nft/verify` 保持接通真实 handler

**建议响应**：
```json
{"has_nft": true, "balance": 1, "chain_id": 11155111, "cache_hit": false}
```

**验收标准**：用真实测试网 NFT 返回正确结果，不持有返回 `has_nft=false`，重复请求命中缓存时 `cache_hit=true`

**面试价值**：证明项目不是假数据 demo

---

### G3. Protected HLS Access

**目标**：把 NFT 鉴权接入流媒体访问控制。

**必须完成**：
- 请求 `manifest.m3u8` 前先做 NFT 鉴权
- 未通过鉴权时返回 401 / 403
- 通过鉴权时返回播放清单

**访问控制策略（定案）**：
- 第一阶段只保护 `manifest.m3u8`
- `segment` 请求默认不直接触发链上校验，而是依赖短时播放令牌
- 推荐链路：`wallet login -> JWT -> request manifest -> 服务端校验 NFT -> 生成短时播放令牌 -> 返回 manifest`
- NFT 鉴权结果缓存 30-120 秒
- 播放令牌有效期 60-300 秒
- CDN 只缓存媒体内容，不缓存用户鉴权结果
- 明确禁止每个 segment 请求都访问链上 RPC

**建议优先级**：第一阶段先保护 `manifest`，第二阶段再保护 `segment`

**验收标准**：未持有 NFT 无法获取 manifest，持有可正常播放

**面试价值**：这是你"音视频经验 + Web3 新能力"真正结合的地方

---

### G4. Transcoding Worker

**当前状态**：`pkg/plugins/transcoder` 已有任务队列、worker pool、健康检查、失败重试与测试覆盖，`pkg/plugins/worker` 已有调度、优先级、取消和重试。

**目标**：把转码/调度链路进一步收敛成可对外演示的媒体工作流。

**必须完成**：
- 提交转码任务、任务排队、Worker 执行 FFmpeg
- 状态更新、失败重试、取消和健康检查

**工程点**：并发数限制、优先级、超时、重试次数、日志与指标

**验收标准**：输入视频，输出 HLS；任务状态可观测：pending / running / completed / failed；任务队列与 worker 状态可在测试中稳定复现

**面试价值**：最能拉开你和普通 Web3 后端候选人差距的模块

---

## 三、双模式架构

### 3.1 为什么需要双模式

| 场景 | 模式 | 理由 |
|------|------|------|
| **学习调试** | 单体 | 一个进程串起“鉴权 -> 缓存 -> 流媒体”，最适合补 Web3 新能力 |
| **面试展示** | 单体 | 先讲清核心业务闭环，再讲为什么可以拆服务 |
| **生产部署** | 微服务 | 转码、鉴权、缓存、监控可按瓶颈独立扩展 |
| **压测优化** | 单体 / 微服务 | 单体定位瓶颈，微服务验证拆分收益 |

### 3.2 单体模式（开发 / 学习 / 面试）

```
┌──────────────────────────────────────────────────────────────┐
│                     StreamGate Monolith                       │
├──────────────────────────────────────────────────────────────┤
│  ┌──────────────────────────────────────────────────────┐   │
│  │                  API Gateway                          │   │
│  │         REST (8080) + Auth + Rate Limiting           │   │
│  └──────────────────────────────────────────────────────┘   │
│                            │                                 │
│  ┌─────────────┐  ┌─────────────┐  ┌────────────────────┐ │
│  │ Web3 Layer │  │ Media Layer │  │   Storage Layer   │ │
│  │ - NFT      │  │ - Transcode │  │ - Redis          │ │
│  │ - Signature│  │ - HLS/DASH  │  │ - MinIO          │ │
│  │ - Multi-chain│ │            │  │                   │ │
│  └─────────────┘  └─────────────┘  └────────────────────┘ │
│                            │                                 │
│  ┌──────────────────────────────────────────────────────┐   │
│  │                   Infrastructure                       │   │
│  │   Prometheus Metrics │ Zap Logging │ Graceful Shutdown│   │
│  └──────────────────────────────────────────────────────┘   │
└──────────────────────────────────────────────────────────────┘
```

**启动命令**:
```bash
# 开发模式
go run ./cmd/monolith/streamgate/main.go

# 运行已构建的单体二进制
# 当前配置系统默认读取 config 中的 app.mode
./bin/streamgate
```

### 3.3 微服务模式（生产 / 扩展）

```
                          ┌─────────────────┐
                          │  Load Balancer  │
                          │   (Nginx/LB)    │
                          └────────┬────────┘
                                   │
                    ┌───────────────┼───────────────┐
                    │               │               │
           ┌────────▼────────┐   │   ┌────────▼────────┐
           │   API Gateway    │   │   │   Auth Service  │
           │   (Port 9090)   │───┘   │   (Port 9007)  │
           └────────┬────────┘       └────────┬────────┘
                    │                          │
      ┌─────────────┼─────────────┐          │
      │             │             │          │
┌─────▼─────┐ ┌────▼─────┐ ┌────▼─────┐ ┌───▼─────┐
│  Upload    │ │ Streaming │ │ Worker   │ │  NATS   │
│  (9091)   │ │ (9093)    │ │ (9008)   │ │  (4222) │
└───────────┘ └───────────┘ └───────────┘ └─────────┘
```

**服务职责**:

| 服务 | Port | 职责 | 扩展性 |
|------|------|------|--------|
| **API Gateway** | 9090 | REST 入口、路由、鉴权 | 水平 |
| **Auth Service** | 9007 | NFT 验证、签名验证 | 水平 |
| **Streaming** | 9093 | 转码、HLS/DASH | 垂直（CPU） |
| **Worker** | 9008 | 异步任务、调度 | 水平 |

**启动命令**:
```bash
# 启动基础设施
docker-compose up -d nats redis minio

# 启动核心服务
./bin/api-gateway &
./bin/auth &
./bin/streaming &
./bin/worker &

# 可选能力服务（按需要启动）
# ./bin/cache &
# ./bin/metadata &
# ./bin/monitor &
```

---

## 四、核心模块重构方案

### 4.1 NFT 鉴权重构（最优先）

**文件**: `pkg/web3/chain.go`

**为什么它是最优先**:
- 这是你从音视频后端转向 Web3 的最关键增量能力
- 它直接决定“用户能不能播放受保护内容”
- 如果这里只是占位符，整个项目就退化成普通流媒体 demo

**Before** (占位符 ❌):
```go
func (cc *ChainClient) getNFTBalance(...) (int, error) {
    return 0, nil  // 占位符
}
```

**After** (链上调用核心逻辑示例):
```go
import (
    "github.com/ethereum/go-ethereum"
    "github.com/ethereum/go-ethereum/accounts/abi"
    "github.com/ethereum/go-ethereum/common"
)

// ERC-721 ABI
var erc721ABI = `[{"inputs":[{"internalType":"address","name":"owner","type":"address"}],"name":"balanceOf","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"}]`

func (cc *ChainClient) getNFTBalance(ctx context.Context, wallet, contract common.Address) (int, error) {
    parsedABI, err := abi.JSON(strings.NewReader(erc721ABI))
    if err != nil {
        return 0, fmt.Errorf("failed to parse ABI: %w", err)
    }
    
    data, err := parsedABI.Pack("balanceOf", wallet)
    if err != nil {
        return 0, fmt.Errorf("failed to pack call: %w", err)
    }
    
    result, err := cc.client.CallContract(ctx, ethereum.CallMsg{
        To:   &contract,
        Data: data,
    }, nil)
    if err != nil {
        return 0, fmt.Errorf("CallContract failed: %w", err)
    }
    
    balance := new(big.Int).SetBytes(result)
    cc.logger.Info("NFT balance retrieved",
        zap.String("wallet", wallet.Hex()),
        zap.String("balance", balance.String()))
    
    return int(balance.Int64()), nil
}
```

**面试怎么讲**:
> "这个项目本质上是内容访问控制系统。传统音视频平台一般靠账号、订阅或 DRM 控制访问，我这里增加了一层 Web3 gating：
> 只有持有指定 NFT 的钱包，才能播放受保护内容。
>
> NFT 鉴权上我实现了 ERC-721 标准：
> 1. 打包 ABI 调用数据（方法选择器 + 参数）
> 2. 通过 ethclient.CallContract(ctx, msg, nil) 调用链上合约
> 3. 解析返回的 big.Int 结果
> 
> 这相当于把链上资产所有权接到了内容分发入口。"

### 4.2 RPC 高可用重构

**文件**: `pkg/web3/rpc_client.go` (新建)

```go
package web3

// CircuitBreaker implements circuit breaker pattern
type CircuitBreaker struct {
    failures     int
    maxFailures int
    resetTimeout time.Duration
    state        CircuitBreakerState
    mu           sync.RWMutex
}

// RPCClient wraps ethclient with high availability
type RPCClient struct {
    client       *ethclient.Client
    urls         []string
    currentIndex int
    breaker      *CircuitBreaker
    logger       *zap.Logger
}

// NewRPCClient creates RPC client with multiple URLs
func NewRPCClient(ctx context.Context, urls []string, chainID int64, logger *zap.Logger) (*RPCClient, error) {
    rc := &RPCClient{
        urls:    urls,
        breaker: NewCircuitBreaker(3, 30*time.Second),
        logger:  logger,
    }
    
    // 找到第一个可用的 URL
    for i, url := range urls {
        client, err := ethclient.Dial(url)
        if err != nil {
            continue
        }
        if _, err := client.ChainID(ctx); err != nil {
            client.Close()
            continue
        }
        rc.client = client
        rc.currentIndex = i
        return rc, nil
    }
    return nil, fmt.Errorf("no available RPC URL")
}

// Call executes RPC call with retry and circuit breaker
func (rc *RPCClient) Call(ctx context.Context, method string, args ...interface{}) (interface{}, error) {
    if rc.breaker.IsOpen() {
        if err := rc.switchNode(ctx); err != nil {
            return nil, fmt.Errorf("circuit breaker open and no fallback: %w", err)
        }
    }
    
    result, err := rc.doCall(ctx, method, args...)
    if err != nil {
        rc.breaker.RecordFailure()
        if fallbackErr := rc.switchNode(ctx); fallbackErr == nil {
            result, err = rc.doCall(ctx, method, args...)
        }
    }
    return result, err
}

func (rc *RPCClient) switchNode(ctx context.Context) error {
    original := rc.currentIndex
    for i := 1; i < len(rc.urls); i++ {
        next := (rc.currentIndex + i) % len(rc.urls)
        if next == original {
            continue
        }
        client, err := ethclient.Dial(rc.urls[next])
        if err != nil {
            continue
        }
        if _, err := client.ChainID(ctx); err != nil {
            client.Close()
            continue
        }
        rc.client.Close()
        rc.client = client
        rc.currentIndex = next
        rc.breaker.Reset()
        return nil
    }
    return fmt.Errorf("no available fallback node")
}
```

**面试怎么讲**:
> "我原来做音视频就很关注外部依赖的稳定性，比如对象存储、CDN、转码资源池。Web3 里对应的关键外部依赖就是 RPC。
>
> 所以我参考 Alchemy 做了高可用：
> 1. 多 RPC URL 列表，配置多个节点
> 2. 熔断器模式：连续失败 3 次打开熔断，30 秒后尝试半开
> 3. 故障自动切换：找到下一个可用节点
> 4. 限流处理：429 Too Many Requests 时指数退避"

### 4.3 Prometheus 集成

**建议统一目标文件**: `pkg/plugins/metrics/prometheus.go`

**为什么选这一套**:
- 这一套已经提供标准 Prometheus registry 和 `/metrics` HTTP 暴露路径
- `pkg/plugins/monitor/server.go` 中的 metrics collector 仍有多个 TODO
- `pkg/monitoring/prometheus.go` 更像兼容层/自定义导出，不适合作为当前阶段唯一验收标准

**指标归属规则（定案）**:
- 鉴权模块负责 `auth_*`
- NFT 校验模块负责 `nft_*`
- RPC 客户端负责 `rpc_*`
- 转码 / Worker 负责 `transcode_*`、`worker_*`
- 播放访问控制负责 `streaming_*`
- 唯一暴露入口为 `/metrics`
- 命名规则固定为：`domain_metric_unit`
- 核心标签仅允许：`chain`、`status`、`service`、`job_type`

```go
collector := metrics.NewMetricsCollector(logger)
if err := collector.DefaultMetrics(); err != nil {
    return err
}

if err := collector.StartServer(ctx, ":9095"); err != nil {
    return err
}
```

---

## 五、4 周执行计划

> 注：以下是原始路线图，当前仓库的 P0 Web3 鉴权闭环、HLS 受保护访问、转码/worker 基础链路已经完成，现阶段更适合把这些内容当作“已实现模块”来讲，只保留未完成的高可用和生产硬化项继续推进。

### 第 1 周：跑通 Web3 鉴权闭环

**目标**: 让“持有 NFT 才能访问内容”这条主链路真实可跑

| Day | 任务 | 验收标准 |
|-----|------|----------|
| 1-2 | 修复 `pkg/web3/chain.go` + 接通 handler | `/api/v1/nft/verify` 不再返回占位响应 |
| 3-4 | 实现 ERC-721 NFT 验证 | 用测试网 NFT 验证成功 |
| 5 | 修复钱包登录验签主链路 | 签名、nonce、过期时间校验通过 |
| 6-7 | 实现 RPC 多节点 + 熔断器 | 一个节点挂了自动切换 |

**验收命令（P0 完成后的目标结果）**:
```bash
curl -X POST http://localhost:8080/api/v1/nft/verify \
  -H "Content-Type: application/json" \
  -d '{"wallet": "0x...", "contract": "0x8667b7bdf8f27e76200fa450bf48aa78bbbcc61f", "chain_id": 11155111}'
# 期望: {"has_nft": true, "balance": 1}
```

### 第 2 周：把音视频主线和 Web3 主线接起来

**目标**: 让鉴权、缓存、流媒体访问形成真实业务链路

| Day | 任务 | 验收标准 |
|-----|------|----------|
| 1-2 | Redis 缓存层接入 NFT 鉴权 | 热钱包 / 热内容访问命中缓存 |
| 3-4 | 鉴权结果接入流媒体访问控制 | 未持有 NFT 无法获取 manifest |
| 5-6 | Prometheus 集成 | 统一使用一套 `/metrics` 暴露路径，并能看到鉴权/缓存/播放相关指标 |
| 7 | 文档 + 代码审查 | 单元测试 > 60% |

### 第 3 周：把音视频工程能力做成亮点

**目标**: 体现你在转码、调度、并发和资源管理上的经验

| Day | 任务 | 验收标准 |
|-----|------|----------|
| 1-2 | Worker Pool 实现 | 任务调度、重试、优先级可观测 |
| 3-4 | FFmpeg 集成 | 视频转码成功，输出 HLS |
| 5-6 | 性能压测 | 100 并发稳定，热点请求走缓存 |
| 7 | 优化 + 文档 | 主链路稳定、关键指标可观测、给定并发下无明显错误 |

### 第 4 周：整理成适合面试的项目故事

**目标**: 项目可讲、亮点清晰、能贴近企业场景

| Day | 任务 | 验收标准 |
|-----|------|----------|
| 1-2 | 评估服务入口收敛策略 | 能讲清单体优先、微服务扩展的 trade-off |
| 3-4 | 准备面试 Q&A | 每个模块能讲 2 分钟，重点突出音视频 + Web3 |
| 5 | Mock interview | 模拟面试 1 轮 |
| 6-7 | 整理文档 | README + 设计文档 + 架构图 |

---

## 六、面试 Q&A 模板

### Q: 你的微内核架构是怎么设计的？

> "我原来做音视频系统时就很关注核心链路稳定、扩展点清晰，所以这里用微内核架构把能力拆成核心 + 插件。
>
> 核心只负责三件事：
> 1. 插件注册表（Plugin Registry）：管理插件生命周期
> 2. 事件总线（Event Bus）：插件间通信
> 3. 配置管理（Config Manager）：统一配置加载
> 
> 这样做的好处是：我可以先用单体把主链路跑通，再把转码、鉴权、缓存这些瓶颈能力独立拆出去。
> 对面试来说也更好讲，因为先有业务闭环，再有架构扩展。"

### Q: NFT 验证怎么实现的？

> "NFT 验证不是孤立功能，它决定用户能不能访问受保护内容。
>
> 我实现了 ERC-721 标准：
> 1. 打包 ABI 调用数据（方法选择器 + 参数）
> 2. 通过 ethclient.CallContract(ctx, msg, nil) 调用链上合约
> 3. 解析返回的 big.Int 结果
> 
> 在业务上，这一步会放在播放入口前面，只有验证通过才返回 manifest 或播放地址。"

### Q: RPC 高可用怎么做的？

> "从音视频后端视角看，RPC 跟 CDN、对象存储一样，都是关键外部依赖。
> 所以我参考 Alchemy 做了高可用设计：
> 1. 多 RPC URL 列表
> 2. 熔断器模式：连续失败 3 次打开熔断
> 3. 故障自动切换
> 4. 限流处理：429 时指数退避"

### Q: 这个项目为什么适合你的背景？

> "因为它不是纯 Web3 demo，而是把 Web3 放进了我熟悉的音视频场景。
>
> 我原本擅长的部分是：高并发服务、转码链路、缓存、任务调度、流媒体分发；
> 这次新增的部分是：钱包签名、NFT 鉴权、RPC 高可用。
>
> 这样项目既能证明我原来的工程能力还在，也能证明我已经补上 Web3 和 Go 的新能力。"

### Q: 什么是 EIP-191？

> "EIP-191 定义了签名的格式：
> `\x19Ethereum Signed Message:\n<length><message>`
> 
> 作用：防止签名被当作交易签名，区分不同类型的签名，避免 replay attack。"

### Q: 什么是 replay attack？怎么防护？

> "Replay attack 是把别人签名的消息重放。
> 
> 防护措施：
> 1. Nonce：每个签名包含唯一随机数
> 2. 过期时间：签名有有效期限
> 3. 域名绑定：EIP-712 的 domain separator"

---

## 七、代码质量检查清单

### 7.1 Go 代码质量问题（严重）

| 问题 | 位置 | 严重性 | 修复 |
|------|------|--------|------|
| **资源泄漏** | `pkg/web3/chain.go:61-74` | 🔴 高 | NewChainClient 失败时未关闭 client |
| **数据竞争** | `pkg/storage/cache.go:52` | 🔴 高 | Get() 在 RLock 下写入 lastAccess |
| **非确定性 Key** | `pkg/monitoring/metrics.go` | 🔴 高 | getMetricKey 遍历 map 无序 |
| **魔法数字** | `pkg/web3/chain.go:68` | 🟡 中 | `5*1000*1000*1000` 应为 `5*time.Second` |

### 7.2 问题详细分析

**问题 1: 资源泄漏**
```go
// 错误
client, err := ethclient.Dial(rpcURL)
if err != nil {
    return nil, err  // ❌ client 未关闭
}

// 正确
client, err := ethclient.Dial(rpcURL)
if err != nil {
    return nil, err
}
chainIDFromRPC, err := client.ChainID(ctx)
if err != nil {
    client.Close()
    return nil, err
}

return &ChainClient{client: client, chainID: chainIDFromRPC.Int64()}, nil
```

**问题 2: 数据竞争**
```go
// 错误
func (cs *CacheStorage) Get(key string) (interface{}, error) {
    cs.mu.RLock()
    defer cs.mu.RUnlock()
    item.lastAccess = time.Now()  // ❌ 写操作在读锁下
    return item.value, nil
}

// 正确
func (cs *CacheStorage) Get(key string) (interface{}, error) {
    cs.mu.Lock()
    defer cs.mu.Unlock()
    item.lastAccess = time.Now()
    return item.value, nil
}
```

**问题 3: 魔法数字**
```go
// 错误
ctx, cancel := context.WithTimeout(context.Background(), 5*1000*1000*1000)

// 正确
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
```

---

## 八、冗余清理

### 8.1 建议下沉而非立即删除的代码

```bash
# 第一步：停止继续扩展与面试目标无关的模块
# pkg/ml pkg/analytics pkg/dashboard

# 第二步：明确 plugin / internal module / microservice 的长期边界
# - plugin：有多实现替换需求
# - internal module：单体内稳定业务能力
# - microservice：只有部署/扩容/隔离收益明确时才拆
```

### 8.2 收敛理由

| 目录 | 文件数 | 删除理由 |
|------|--------|----------|
| `pkg/ml/` | 11 | 与当前 Web3 鉴权主线弱相关，暂停继续投入 |
| `pkg/analytics/` | 8 | 先用 Prometheus/Grafana 覆盖核心观测需求 |
| `pkg/dashboard/` | 4 | 对面试主线帮助有限，可延后 |

### 8.3 分层边界（定案）

| 层级 | 判定标准 | 当前项目结论 |
|------|----------|--------------|
| `plugin` | 存在多实现替换需求 | `storage`、`chain`、`transcoder`、`streaming` |
| `internal module` | 单体内稳定能力，不需要插件抽象 | `cache`、`metadata`、`worker scheduler`、`monitor glue` |
| `microservice` | 只有在部署、扩容、隔离上收益明确时拆分 | 首批保留 `api-gateway`、`auth`、`streaming`；其余按需要演进 |

### 8.4 真正的插件候选（多实现才需要抽象）

| 插件 | 为什么需要插件 | 多实现示例 |
|------|---------------|-----------|
| `StoragePlugin` | 存储后端可替换 | S3 / MinIO / IPFS / Local |
| `ChainPlugin` | 不同区块链 | Ethereum / Solana / Polygon |
| `TranscoderPlugin` | 编码器可替换 | FFmpeg / 硬件编码器 |
| `StreamingPlugin` | 流媒体协议 | HLS / DASH / 自定义 |

### 8.5 当前建议保留的代码

```
cmd/
├── monolith/streamgate/main.go    # 单体入口
└── microservices/
    ├── api-gateway/              # API 入口
    ├── auth/                     # 认证服务
    └── streaming/                # 流媒体

pkg/
├── web3/                        # Web3 核心（需修复）
│   ├── chain.go                 # NFT 验证
│   └── signature.go              # 签名验证
├── storage/                     # 存储层
├── cache/                       # 内部模块（Redis/LRU）
├── metadata/                    # 内部模块（索引/搜索）
├── worker/                      # 内部模块（任务调度）
├── monitor/                     # 内部模块（监控 glue）
└── plugins/                     # 真正的插件
    ├── storage/                 # StoragePlugin
    ├── chain/                   # ChainPlugin
    ├── transcoder/              # TranscoderPlugin
    └── streaming/               # StreamingPlugin
```

---

## 九、验收检查表

### Week 1 验收

- [ ] `pkg/web3/chain.go` NFT 验证真实可用
- [ ] 用 Sepolia 测试网 NFT 验证成功
- [ ] RPC 多节点配置
- [ ] 熔断器实现
- [ ] `curl` 测试 NFT 验证返回正确结果
- [ ] challenge 存储在 Redis，且验证成功后一次性消费

### Week 2 验收

- [ ] Redis 缓存集成
- [ ] 统一使用一套 `/metrics` 暴露路径
- [ ] 鉴权计数指标存在
- [ ] RPC failover 指标存在
- [ ] manifest 鉴权成功，segment 不直接触发链上校验

### Week 3 验收

- [ ] Worker Pool 实现
- [ ] FFmpeg 转码集成
- [ ] 主链路稳定、关键指标可观测、给定并发下无明显错误

### Week 4 验收

- [ ] plugin / internal module / microservice 边界写入文档并固定
- [ ] 面试 Q&A 准备完成
- [ ] Mock interview 完成

---

## 十、参考资源

### 企业实践

- **Alchemy**: https://docs.alchemy.com/docs/how-to-build-a-reliable-web3-app
- **OpenSea**: https://docs.opensea.io/reference/api-overview
- **Livepeer**: https://docs.livepeer.org/architecture

### 技术文档

- **go-ethereum**: https://pkg.go.dev/github.com/ethereum/go-ethereum
- **Prometheus Go Client**: https://prometheus.io/docs/instrumenting/go/
- **ERC-721**: https://eips.ethereum.org/EIPS/eip-721
- **ERC-1155**: https://eips.ethereum.org/EIPS/eip-1155

### 测试网资源

| 链 | Chain ID | RPC URL | NFT 合约 |
|----|----------|---------|----------|
| Sepolia | 11155111 | https://rpc.sepolia.org | 0x8667b7bdf8f27e76200fa450bf48aa78bbbcc61f |
| Amoy | 80002 | https://rpc-amoy.polygon.technology | 待部署 |

### 学习路径

| 文档 | 用途 |
|------|------|
| [web3-faq.md](web3-faq.md) | 常见问题：23 个 Web3 基础问题 |
| [learning-roadmap.md](learning-roadmap.md) | 学习路线图：2-3 周计划 |

> **注意**：以上文档位于 `docs/` 目录，与本指南同一目录。

---

## 十一、Web3 测试策略

### 11.1 当前测试现状

当前仓库已经有完整的 `test/` 目录分层，但 Web3 主链路测试资产仍处于“部分存在、部分待补齐”的状态。

| 层级 | 当前状态 | 真实入口 |
|------|----------|----------|
| **单元测试** | ✅ 已有基础测试，但 Web3 主链路覆盖不足 | `test/unit/` |
| **集成测试** | ⚠️ 已有 Web3 集成测试文件，但断言偏弱 | `test/integration/web3/web3_integration_test.go` |
| **E2E 测试** | ⚠️ 目录存在，但关键 NFT 验证用例仍是 placeholder | `test/e2e/nft_verification_test.go` |
| **Mocks / Helpers** | ✅ 目录存在，但 Web3 专用 mock 仍需补齐 | `test/mocks/`、`test/helpers/` |

**当前已确认的问题**：
- `test/unit/nft_test.go` 目前主要是地址 / TokenID 基础校验，不是 NFT 链路单测
- `test/integration/web3/web3_integration_test.go` 当前更偏 smoke test，而不是强状态断言
- `test/e2e/nft_verification_test.go` 仍是 placeholder

### 11.2 目标测试矩阵

下表是目标态，不代表当前仓库已经全部具备。

| 交付物 | 目标测试用例 |
|--------|--------------|
| **G1 Wallet Sign-In** | 正确签名登录、错误签名失败、过期 challenge 失败、nonce 重放失败、并发登录安全 |
| **G2 NFT Verify** | 持有 NFT 返回 `balance > 0`、不持有返回 `balance = 0`、RPC 超时处理、熔断器触发行为 |
| **G3 Protected HLS** | 有 NFT 能获取 manifest、无 NFT 返回 401/403、缓存失效后重新验证 |
| **G4 Transcoding** | 任务状态流转、FFmpeg 失败重试、超时任务标记 failed |

### 11.3 测试设计原则

Web3 测试必须分层：

| 层级 | 工具 | 特点 | 适用场景 |
|------|------|------|----------|
| **单元测试** | testify/mock | Mock 外部依赖，无需网络 | G1/G2 纯逻辑、错误处理 |
| **集成测试** | Anvil/Ganache | 本地模拟链，无主网 gas | 合约交互、状态流转 |
| **E2E 测试** | Sepolia Testnet | 真实链上数据 | 最终验收、回归测试 |

**边界说明**：
- 单元测试允许 Mock 外部依赖，例如 RPC、Redis、challenge store
- 业务闭环验收不得依赖 Mock 数据
- 集成测试和 E2E 测试必须使用真实合约交互或真实链数据

### 11.4 目标测试实现建议

以下是建议新增或增强的测试方向，不代表当前仓库已经具备这些测试文件：

- 为 `pkg/web3` 增加真实链路单测骨架，围绕 `VerifyNFTOwnership`、`GetNFTBalance`、RPC 错误处理
- 为钱包登录补测试，围绕 challenge 生成、一次性消费、过期校验、重放保护
- 将 `test/integration/web3/web3_integration_test.go` 从 smoke test 提升为状态断言型测试
- 将 `test/e2e/nft_verification_test.go` 从 placeholder 补成真实链路 E2E

### 11.5 覆盖率目标

覆盖率目标只针对 **G1-G4 主链路相关包**，不要求为了达标回头补 `ml/analytics/dashboard` 等已降级模块。

| 阶段 | 目标 | 统计范围 |
|------|------|----------|
| Week 1 | 60% | `pkg/web3`、`pkg/service` 中 G1/G2 相关代码 |
| Week 2 | 70% | + 鉴权缓存、访问控制链路 |
| Week 3 | 75% | + Worker、转码、任务调度 |
| Week 4 | 80% | G1-G4 全链路 + 关键 E2E |

### 11.6 可执行测试命令

**工具链前置条件**：
- 当前环境 `go version` 为 `go1.24.13`
- 项目目标 Go 版本为 1.24，执行 `go test ./...` 无需升级

```bash
# 查看 Go 版本
go version

# 当前阶段推荐：聚焦主链路包，而不是直接跑整仓
go test -v ./pkg/web3/...
go test -v ./pkg/service/...
go test -v ./test/unit/...
go test -v ./test/integration/web3/...

# 覆盖率（主链路）
go test -coverprofile=coverage.out ./pkg/web3/... ./pkg/service/...
go tool cover -html=coverage.out

# 集成测试（需要 Anvil，本地模拟链）
anvil --fork-url https://rpc.sepolia.org --chain-id 11155111
go test -v ./test/integration/web3/...

# E2E 测试（目标态，当前 nft_verification_test 仍需补齐）
SEPOLIA_RPC=https://rpc.sepolia.org go test -v ./test/e2e/...
```

### 11.7 已确认的测试数据

| 链 | 合约 | 用途 |
|----|------|------|
| Sepolia | `0x8667b7bdf8f27e76200fa450bf48aa78bbbcc61f` | E2E NFT 测试 |
| Anvil (Fork Sepolia) | 同上 | 集成测试 |

---

**最后**: 这个项目最大的卖点应该讲成：
1. **音视频内容分发 + Web3 NFT 鉴权**，场景真实且有业务闭环
2. **真实的链上数据 + RPC 高可用**，证明不是 Web3 demo
3. **单体跑通主链路，微服务承接扩展**，体现工程 trade-off

先跑通 P0，再优化其他。对你来说，最重要的不是堆功能，而是把“音视频老兵如何把 Web3 接进真实内容系统”讲清楚。
