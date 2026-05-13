# Web3+Go 代码模式手册

> 定位：你在阅读 `pkg/web3/` 代码时会遇到的核心 Go+Web3 模式。
> 每节都有"这是啥"、"为什么这么写"、"项目里哪里用了"。

---

## 模式 1: ABI 编码/解码

### 这是啥

智能合约的函数调用本质上是向合约地址发一笔 `data` 字段包含编码参数的交易。ABI (Application Binary Interface) 定义了如何将函数名和参数编码成这些字节。

### 代码模板

```go
// 1. 定义 ABI JSON（函数的签名）
const erc721ABIJSON = `[{"constant":true,"inputs":[{"name":"tokenId","type":"uint256"}],"name":"ownerOf","outputs":[{"name":"","type":"address"}],"type":"function"}]`

// 2. 解析 ABI（通常在 init 或 New 函数里做一次）
abi, err := abi.JSON(strings.NewReader(erc721ABIJSON))
// abi 对象是线程安全的，可以复用

// 3. 编码调用数据（Pack）
// 把函数名 "ownerOf" 和参数 tokenID 编码成 []byte
// 前 4 字节 = 函数选择器 (keccak256("ownerOf(uint256)") 的前 4 字节)
// 后 32 字节 = tokenID 的 ABI 编码（左填充到 32 字节）
data, err := abi.Pack("ownerOf", tokenIDInt)

// 4. 发起 eth_call（只读调用，不需要 gas）
// CallMsg.To = 合约地址
// CallMsg.Data = 上一步的编码结果
result, err := client.CallContract(ctx, ethereum.CallMsg{To: &contract, Data: data}, nil)

// 5. 解码返回值（Unpack）
// ownerOf 返回 address（20 字节，在返回值里是左填充到 32 字节的）
var owner common.Address
err = abi.UnpackIntoInterface(&owner, "ownerOf", result)
```

### 核心原则

- `abi.Pack("函数名", 参数...)` — 生成调用数据
- `client.CallContract(ctx, msg, blockNumber)` — 发送 eth_call
- `abi.UnpackIntoInterface(&目标变量, "函数名", 返回值)` — 解码结果
- **函数选择器冲突**：两个不同函数可能前 4 字节相同（概率极低但发生过）。所以 ABI JSON 必须精确匹配合约。

### 项目里哪里用了

| 文件 | 用法 |
|---|---|
| `pkg/web3/nft.go:92-96` | NFTVerifier 编码 ownerOf 调用 |
| `pkg/web3/nft.go:160-164` | NFTVerifier 编码 balanceOf 调用 |
| `pkg/web3/chain.go:562-571` | ChainClient 内部编码 balanceOf |

---

## 模式 2: EthCaller 接口抽象

### 这是啥

`go-ethereum` 的 `ethclient.Client` 是具体类型，没法 mock。所以我们定义一个接口，只提我们需要的方法。

### 代码模板

```go
type EthCaller interface {
    CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error)
    CodeAt(ctx context.Context, contract common.Address, blockNumber *big.Int) ([]byte, error)
}

// *ethclient.Client 自动满足这个接口（鸭子类型）
// 测试时用 mockEthCaller 代替
```

### 为什么这么写

- `*ethclient.Client` 有几十个方法。如果你依赖具体类型，测试时就必须启动真实 RPC。
- 定义一个只有你需要的方法的接口，测试时就能用 mock。
- Go 的隐式接口满足意味着 `*ethclient.Client` 不需要显式声明 `implements`。

### 进阶：可选接口

```go
// 某些 Client 额外支持 BlockTag，但不是所有
type BlockTagCaller interface {
    CallContractAtBlock(ctx context.Context, msg ethereum.CallMsg, blockTag BlockTag) ([]byte, error)
}

// 在调用时用类型断言检查
if btc, ok := client.(BlockTagCaller); ok {
    result, err = btc.CallContractAtBlock(ctx, msg, blockTag)
} else {
    // fallback 到普通 CallContract
}
```

### 项目里哪里用了

| 文件 | 接口 | 实现者 |
|---|---|---|
| `pkg/web3/nft.go:18` | `EthCaller` | `*ethclient.Client`, `ChainClient`, mock |
| `pkg/web3/reorg.go:48` | `HeaderReader` | `*ethclient.Client`, mock |
| `pkg/middleware/nft_gate.go:51` | `BlockProver` | `ChainClient` adapter |

---

## 模式 3: big.Int 操作

### 这是啥

以太坊的数值（余额、tokenID、gas 价格）都是 256 位整数，Go 的 `uint64` 装不下。所以 go-ethereum 全部用 `*big.Int`。

### 核心原则（最容易犯错的地方）

```go
// ❌ 错误：big.Int 是可变对象！
a := big.NewInt(5)
b := a
b.Add(b, big.NewInt(1))  // a 也变成了 6！
fmt.Println(a) // 6 — 不是你期望的

// ✅ 正确：每次运算创建新对象
a := big.NewInt(5)
b := new(big.Int).Add(a, big.NewInt(1))  // a 仍然是 5

// ✅ 用 SetString 从字符串解析（tokenID 可能是很长的数字）
id := new(big.Int)
if _, ok := id.SetString("12345678901234567890", 10); !ok {
    return fmt.Errorf("invalid token ID")
}

// ✅ 比较
if balance.Cmp(big.NewInt(0)) > 0 {
    // balance > 0
}

// ✅ 转 uint64（只有确定值足够小的时候）
if id.IsUint64() {
    n := id.Uint64()
}
```

### 项目里哪里用了

`pkg/web3/nft.go:86-89` — 每次调用 `ownerOf` 都重新创建 `*big.Int`，而不是复用全局变量：

```go
tokenIDInt := new(big.Int)
if _, ok := tokenIDInt.SetString(tokenID, 10); !ok {
    return false, fmt.Errorf("invalid token ID: %s", tokenID)
}
```

---

## 模式 4: Context 与 RPC 超时

### 这是啥

每一次 RPC 调用都是网络请求。不加超时的话，RPC 节点挂了你的 goroutine 就永远等下去了。

### 代码模板

```go
// ✅ 正确：每条 RPC 调用链路都带超时
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

balance, err := client.BalanceAt(ctx, addr, nil)

// ❌ 错误：context.Background() 没有超时
balance, err := client.BalanceAt(context.Background(), addr, nil)

// ❌ 错误：context.TODO() 同上，只是语义不同
```

### 进阶：生命周期 Context

对于后台轮询 goroutine（如 EventIndexer），需要用可取消的 context：

```go
type EventIndexer struct {
    lifecycleCtx    context.Context
    lifecycleCancel context.CancelFunc
}

func (ei *EventIndexer) Start(parent context.Context) error {
    ei.lifecycleCtx, ei.lifecycleCancel = context.WithCancel(parent)
    go ei.indexingLoop(ei.lifecycleCtx) // ← 所有子调用都用这个 ctx
}

func (ei *EventIndexer) Stop() {
    ei.lifecycleCancel() // ← 取消后所有 goroutine 通过 ctx.Done() 感知
}
```

### 项目里哪里用了

| 文件 | 模式 |
|---|---|
| `pkg/web3/chain.go:848-849` | HealthCheck 用 5s timeout context |
| `pkg/web3/chain.go:936-937` | connectAt 用 5s timeout 做 Dial |
| `pkg/web3/event_indexer.go:148-206` | Start/Stop 用生命周期 ctx |
| `pkg/middleware/nft_gate.go` | resolveOwnership 用 request context (来自 gin) |

---

## 模式 5: 泛型 + 函数式 RPC 重试

### 这是啥

每次 RPC 调用的模式是一样的：检查连接 → 限流 → 调用 → 如果失败就 failover。Go 1.18 泛型让这个模式可以被抽象成通用函数。

### 代码模板

```go
func withChainClient[T any](
    ctx context.Context, cc *ChainClient, op string,
    fn func(*ethclient.Client) (T, error),
) (T, error) {
    // 1. 限流
    limiter.Wait(ctx)
    // 2. 获取当前 client
    client := cc.client
    // 3. 调用
    result, err := fn(client)
    // 4. 记录延迟和评分
    cc.updateRPCScores(cc.activeRPC, latency, err == nil)
    // 5. 失败时自动 failover
    if err != nil {
        cc.failover()
        result, err = fn(cc.client)
    }
    return result, err
}
```

调用方不需要关心 failover 逻辑：

```go
// 调用方只需写"我要做什么"
balance, err := withChainClient(ctx, cc, "BalanceAt", func(client *ethclient.Client) (*big.Int, error) {
    return client.BalanceAt(ctx, addr, nil)
})
```

### 泛型的 T 有什么用

- `T = *big.Int` → `BalanceAt` 返回 `(*big.Int, error)`
- `T = uint64` → `BlockNumber` 返回 `(uint64, error)`
- `T = *types.Header` → `HeaderByNumber` 返回 `(*types.Header, error)`

如果没有泛型，要么写 3 个重复函数，要么用 `interface{}` 丢类型安全。

---

## 模式 6: 事件索引 (FilterLogs)

### 这是啥

智能合约的 Event 被存储在交易收据的 Logs 里。要监听某个合约的特定事件，需要调用 `eth_getLogs` (在 go-ethereum 里是 `FilterLogs`)。

### 代码模板

```go
// 1. 构造过滤条件
query := ethereum.FilterQuery{
    FromBlock: big.NewInt(1000000),        // 起始块
    ToBlock:   big.NewInt(1000100),        // 结束块
    Addresses: []common.Address{contract},  // 合约地址（可多个）
    Topics:    [][]common.Hash{{eventSig}}, // 事件签名（topic[0]）
}

// 2. 获取 logs
logs, err := client.FilterLogs(ctx, query)

// 3. 解析每条 log
for _, log := range logs {
    // log.Topics[0] = 事件签名 (keccak256("Transfer(address,address,uint256)"))
    // log.Topics[1] = 第一个 indexed 参数 (from)
    // log.Topics[2] = 第二个 indexed 参数 (to)
    // log.Data     = 非 indexed 参数 (ABI 编码)
    event := IndexedEvent{
        TransactionHash: log.TxHash.Hex(),
        BlockNumber:     log.BlockNumber,
        BlockHash:       log.BlockHash.Hex(),
        Topics:          topicsToStrings(log.Topics),
        Data:            fmt.Sprintf("0x%x", log.Data),
    }
}
```

### 安全性：Confirmation Blocks

不能索引最新块，因为最新块可能被重组：

```go
// 只索引 latestBlock - N, N = 最终性确认深度
safeBlock := latestBlock - confirmationBlocks
if safeBlock <= currentBlock {
    return // 还没到新的安全块
}
ei.indexRange(ctx, currentBlock+1, safeBlock)
ei.currentBlock = safeBlock
```

### 项目里哪里用了

`pkg/web3/event_indexer.go:397-424` — `indexRange` 函数实现了完整的 FilterLogs + 分页。

---

## 模式 7: 签名验证 (EC Recover)

### 这是啥

钱包签名 = 用私钥对消息签名，服务端用公钥恢复出地址，和声称的地址比对。

### 代码模板

```go
// 1. 验证签名长度
sig := common.FromHex(signature)  // "0x..." → []byte
if len(sig) != 65 {
    return false, fmt.Errorf("invalid signature length: %d", len(sig))
}

// 2. 调整 recovery ID (v)
// MetaMask 返回的 v 是 27 或 28，go-ethereum 需要 0 或 1
if sig[64] >= 27 {
    sig[64] -= 27
}

// 3. 构造以太坊签名消息
// 以太坊要求消息带上 "\x19Ethereum Signed Message:\n{len}" 前缀
// 这叫 EIP-191
prefixed := fmt.Sprintf("\x19Ethereum Signed Message:\n%d%s", len(message), message)
hash := crypto.Keccak256([]byte(prefixed))

// 4. 恢复公钥并推导地址
pubKey, err := crypto.SigToPub(hash, sig)
recoveredAddr := crypto.PubkeyToAddress(*pubKey)

// 5. 比对
expectedAddr := common.HexToAddress(address)
if recoveredAddr != expectedAddr {
    return false, nil
}
```

### 关键区别：EIP-191 vs EIP-712

| 特征 | EIP-191 (personal_sign) | EIP-712 (typed data) |
|---|---|---|
| 用户看到什么 | 一段文字 | 结构化表单 |
| 签名内容 | 任意字符串 | JSON 格式的 typed data |
| Go 实现 | `crypto.SigToPub` | `eip712.TypedDataAndHash` |
| 项目文件 | `pkg/web3/signature.go` | `pkg/web3/eip712.go` |

### 项目里哪里用了

`pkg/web3/signature.go:37-116` — 完整实现，包括 EIP-1271 回退逻辑。

---

## 模式 8: RPC Failover 的 error 分类

### 这是啥

RPC 调用失败原因分两类：**可重试的**（网络超时、限流）和**不可重试的**（合约 revert、参数错误）。混在一起会导致无限重试或过早放弃。

### 代码模板

```go
func isPermanentRPCError(err error) bool {
    permanentPatterns := []string{
        "execution reverted",  // 合约主动 revert
        "invalid opcode",      // 合约代码有问题
        "out of gas",          // gas 不够
        "nonce too low",       // 交易 nonce 问题
        "insufficient funds",  // 余额不足
    }
    for _, pattern := range permanentPatterns {
        if strings.Contains(err.Error(), pattern) {
            return true
        }
    }
    return false  // 不确定时默认可重试
}
```

### 进阶：Error Tree

```go
type RetryableError struct { Message string; Cause error }
func (e *RetryableError) IsRetryable() bool { return true }

type PermanentError struct { Message string; Cause error }
func (e *PermanentError) IsRetryable() bool { return false }

// 调用方：
if IsRetryable(err) {
    // 加入重试队列
} else {
    // 记录并放弃
}
```

### 项目里哪里用了

`pkg/web3/errors.go` — `RetryableError` 和 `PermanentError` 类型定义。
`pkg/web3/chain.go:1012-1035` — `isPermanentRPCError` 模式匹配。

---

## 模式 9: 地址处理

### 这是啥

以太坊地址有校验和（checksum）格式（EIP-55）。`common.HexToAddress` 会做标准化。

### 规则

```go
// 输入可以没有 0x 前缀
addr := common.HexToAddress("742d35Cc6634C0532925a3b844Bc9e7595f42bE1")
addr := common.HexToAddress("0x742d35Cc6634C0532925a3b844Bc9e7595f42bE1")
// 两者结果相同

// 校验地址格式（不校验是否存在，只校验格式）
valid := common.IsHexAddress("0x742d35Cc6634C0532925a3b844Bc9e7595f42bE1")

// 输出统一格式（EIP-55 校验和）
hex := addr.Hex()  // 0x742d35Cc6634C0532925a3b844Bc9e7595f42bE1

// 比较地址：直接比较值（不是字符串）
addr1 := common.HexToAddress("0xA")
addr2 := common.HexToAddress("0xa")
if addr1 == addr2 {  // true — 不区分大小写
}
```

### 常见错误

```go
// ❌ 用字符串比较
if strings.EqualFold(addr1.Hex(), addr2.Hex()) { // 不必要
}

// ✅ 直接用 struct 比较
if addr1 == addr2 { // 正确
}
```

---

## 模式 10: 配置-Driven 多链

### 这是啥

每条链的 RPC、explorer、currency 不同。用配置表而不是硬编码。

### 项目里的模式

```go
// pkg/web3/multichain.go
var SupportedChains = map[int64]*ChainConfig{
    1: {
        Name: "Ethereum",
        RPCs: []string{"https://eth.llamarpc.com", "https://ethereum-rpc.publicnode.com"},
        Finality: EthereumL1Finality(reader, logger),
    },
    137: {
        Name: "Polygon",
        RPCs: []string{"https://polygon-rpc.com"},
        Finality: PolygonFinality(reader, logger),
    },
}

// 调用方按 chainID 路由
client, err := mcm.GetClient(chainID)
verifier := NewNFTVerifier(client, logger)
verified, err := verifier.VerifyNFTOwnership(ctx, contract, tokenID, owner)
```

---

## 如何学：建议的阅读顺序

```
1. examples/nft-verify-demo/main.go          ← 最简单，纯 ethclient
2. pkg/web3/nft.go                           ← 封装后的 NFTVerifier
3. pkg/web3/signature.go                     ← 签名验证流程
4. pkg/web3/chain.go 的 withChainClient       ← RPC failover 模板
5. pkg/web3/reorg.go                         ← 重组检测
6. pkg/web3/event_indexer.go                 ← 事件索引
7. pkg/middleware/nft_gate.go                ← HTTP + Web3 集成
8. examples/progressive-verify/main.go       ← 看演进：raw → 封装 → 生产
```
