# Web3+Go 常见错误自查手册

> 这些错误是转型 Web3 时最容易踩的坑，每个都来自真实项目经验。
> 按"发现难度"排序——越靠前的越隐蔽。

---

## M1: 在 RPC 失败后继续使用零值

### 错误代码

```go
balance, err := client.BalanceAt(ctx, addr, nil)
// 没有检查 err！
if balance.Cmp(big.NewInt(0)) == 0 {
    // RPC 失败时 balance 是 nil，这里直接 panic
}
```

### 为什么错

`ethclient` 的方法在 RPC 失败时返回 `(zero value, error)`。对于 `*big.Int` 来说 zero value 是 `nil`。对 `nil` 调用 `.Cmp()` → **nil pointer dereference → panic**。

### 正确做法

```go
balance, err := client.BalanceAt(ctx, addr, nil)
if err != nil {
    return fmt.Errorf("余额查询失败: %w", err)
}
// 现在 balance 一定非 nil
if balance.Cmp(big.NewInt(0)) == 0 {
    // 余额确实为零
}
```

### 项目里怎么做的

`pkg/web3/nft.go:122-129` — `VerifyNFTOwnership` 在 `CallContract` 返回错误后立刻 return：

```go
if err != nil {
    return false, fmt.Errorf("failed to call ownerOf: %w", err)
}
```

---

## M2: 忽视 big.Int 的可变性

### 错误代码

```go
a := big.NewInt(5)
b := a
b.Add(b, big.NewInt(1)) // 修改了 b 也修改了 a！
// 现在 a == 6，b == 6
```

### 为什么错

`*big.Int` 的方法**直接修改接收者**并返回自身。`a.Add(b, c)` 会把结果写进 `a`。两个变量指向同一个 `*big.Int` 时，修改一个会影响到另一个。

### 正确做法

```go
a := big.NewInt(5)
b := new(big.Int).Set(a)  // 显式拷贝
b.Add(b, big.NewInt(1))   // a=5, b=6
```

### 在项目里查找

`pkg/web3/nonce.go:80-83` — NonceManager 中比较缓存值时用 `new(big.Int)` 创建临时对象：

```go
if netNonce > next {
    next = netNonce
}
// netNonce 来自 GetNonce，已经是拷贝
```

---

## M3: 用字符串比较地址

### 错误代码

```go
addr1 := common.HexToAddress("0xA")
addr2 := common.HexToAddress("0xa")
if strings.EqualFold(addr1.Hex(), addr2.Hex()) {
    // 不必要的字符串操作
}
```

### 为什么错

`common.Address` 是 `[20]byte`，Go 的 `==` 直接按字节比较。转换成字符串再比较浪费 CPU，且可能因 EIP-55 校验和格式不同而产生误判。

### 正确做法

```go
if addr1 == addr2 {
    // 正确，按字节比较，不区分大小写
}
```

---

## M4: Context.Background() 导致 goroutine 泄漏

### 错误代码

```go
// 在后台轮询里
for {
    balance, err := client.BalanceAt(context.Background(), addr, nil)
    // 如果 RPC 挂了这个 goroutine 永远不返回
    time.Sleep(1 * time.Second)
}
```

### 为什么错

`context.Background()` 永远不会超时或被取消。如果 RPC 节点 TCP 连接挂了但没报错（比如半开连接），`BalanceAt` 可能永远阻塞。这个 goroutine 就泄漏了。

### 正确做法

```go
// 用生命周期 context，Stop 时取消
type Poller struct {
    ctx    context.Context
    cancel context.CancelFunc
}

func (p *Poller) Start() {
    p.ctx, p.cancel = context.WithCancel(context.Background())
    go p.loop()
}

func (p *Poller) Stop() {
    p.cancel() // 所有 RPC 调用通过 ctx 感知取消
}

func (p *Poller) loop() {
    for {
        select {
        case <-p.ctx.Done():
            return
        default:
        }
        // 每条 RPC 调用带超时
        ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
        balance, err := client.BalanceAt(ctx, addr, nil)
        cancel() // 防止资源泄漏
        // ...
    }
}
```

### 项目里怎么做的

`pkg/web3/event_indexer.go:148-206` — `Start` 创建生命周期 context，`Stop` 调用 cancel。

---

## M5: 直接签名不经过 EIP-191 前缀

### 错误代码

```go
// 直接对消息做 Keccak256 然后签名
hash := crypto.Keccak256([]byte(message))
sig, err := crypto.Sign(hash, privateKey)
```

### 为什么错

以太坊钱包（MetaMask 等）在签名时会自动加上 `\x19Ethereum Signed Message:\n{len}` 前缀。如果你服务端不用这个前缀，恢复出来的地址就和钱包签的对不上。

### 正确做法

```go
// 必须加 EIP-191 前缀
prefixed := fmt.Sprintf("\x19Ethereum Signed Message:\n%d%s", len(message), message)
hash := crypto.Keccak256([]byte(prefixed))
sig, err := crypto.Sign(hash, privateKey)
```

### 项目里怎么做的

`pkg/web3/signature.go:119-127` — `hashMessage` 方法：

```go
func (sv *SignatureVerifier) hashMessage(message string) []byte {
    prefix := fmt.Sprintf("\x19Ethereum Signed Message:\n%d", len(message))
    hash := crypto.Keccak256([]byte(prefix + message))
    return hash
}
```

---

## M6: v 值不调整（27/28 vs 0/1）

### 错误代码

```go
sig := common.FromHex(signature) // 65 字节
pubKey, err := crypto.SigToPub(hash, sig)
// 如果 v 是 27 或 28，SigToPub 会报错 "invalid signature recovery id"
```

### 为什么错

MetaMask 返回的 `v` 是 27 或 28（以太坊传统格式）。go-ethereum 的 `SigToPub` 需要 `v` 是 0 或 1（ECDSA 标准）。两者差 27。

### 正确做法

```go
sig := common.FromHex(signature)
if sig[64] >= 27 {
    sig[64] -= 27 // 27→0, 28→1
}
pubKey, err := crypto.SigToPub(hash, sig)
```

### 项目里怎么做的

`pkg/web3/signature.go:59-62` — 调整 v：

```go
if sig[64] >= 27 {
    sig[64] -= 27
}
```

---

## M7: 用 latest block 做 NFT 验证（reorg 漏洞）

### 错误代码

```go
// 验证时用 latest block
result, err := client.CallContract(ctx, msg, nil)
// nil = latest block，可能还在重组窗口内
```

### 为什么错

Ethereum 的 latest block 可能在 6 个块后被重组。如果你基于 latest block 验证 NFT 所有权并立刻放行，被重组后真正持有者可能已经不是这个人了。

### 正确做法

```go
// 用 safe (-4) 或 finalized (-3) block tag
safeBlock := big.NewInt(-4) // go-ethereum 的 "safe" 约定
result, err := client.CallContract(ctx, msg, safeBlock)
if err != nil {
    // RPC 不支持 safe tag，回退到 latest
    result, err = client.CallContract(ctx, msg, nil)
}
```

### 项目里怎么做的

`pkg/web3/chain.go:127-153` — `CallContractAtBlock` 尝试 safe/finalized，失败回退 latest。

---

## M8: RPC URL 硬编码

### 错误代码

```go
client, err := ethclient.Dial("https://eth.llamarpc.com") // 硬编码
```

### 为什么错

1. 公共 RPC 有速率限制，开发环境换个 IP 就不能用了
2. 不同环境（dev/staging/prod）用不同 RPC
3. RPC 可能挂，硬编码无法切换

### 正确做法

```go
// 环境变量 + fallback 列表
rpcURLs := []string{
    os.Getenv("ETH_RPC_URL"),
    "https://eth.llamarpc.com",
    "https://ethereum-rpc.publicnode.com",
}
// 第一个失败就自动切换到下一个
```

### 项目里怎么做的

`pkg/web3/multichain.go:22-142` — 每条链配置了多个 RPC，`ChainClient` 在 `withChainClient` 里自动 failover。

---

## M9: 没考虑 Solana 地址格式差异

### 错误代码

```go
// 用 hex 解析所有地址
if !common.IsHexAddress(address) {
    return false, fmt.Errorf("invalid address")
}
```

### 为什么错

Ethereum 地址是 20 字节 hex（`0x...`），但 Solana 地址是 32 字节 base58（`7...`）。同一个系统如果要支持两条链，地址验证逻辑必须区分。

### 正确做法

```go
if isSolanaChain(chainID) {
    _, err := solana.PublicKeyFromBase58(address)
    return err == nil
}
return common.IsHexAddress(address)
```

### 项目里怎么做的

`pkg/service/auth_wallet.go:27-36` — `IsValidSolanaAddress` 函数。

---

## M10: FilterLogs 不设范围导致 OOM

### 错误代码

```go
// 没有 FromBlock 和 ToBlock — 拉取所有历史 event！
logs, err := client.FilterLogs(ctx, ethereum.FilterQuery{
    Addresses: []common.Address{contract},
})
```

### 为什么错

`FilterLogs` 没有范围限制时会返回从创世块到最新块的所有 event。一个热门合约可能有几百万个 event，全拉回来内存直接爆炸。

### 正确做法

```go
// 始终限制区块范围
query := ethereum.FilterQuery{
    FromBlock: big.NewInt(int64(fromBlock)),
    ToBlock:   big.NewInt(int64(toBlock)),
    Addresses: []common.Address{contract},
}
// 每批最多 10000 个块
if toBlock-fromBlock > 10000 {
    // 分批查询
}
logs, err := client.FilterLogs(ctx, query)
```

### 项目里怎么做的

`pkg/web3/event_indexer.go:397-424` — `indexRange` 每次查询分批区块范围，不跨度过大。

---

## 快速自查清单

```
上线前检查：

□ 每条 RPC 调用都有 error check（尤其 balance 类型返回值）
□ 没有 context.Background() 用在无限重试的循环里
□ 每条 RPC 调用有超时（context.WithTimeout）
□ big.Int 运算用了 new(big.Int) 或 Set 拷贝
□ 地址比较用 == 不是字符串 EqualFold
□ 签名验证做了 v 值调整（27/28 → 0/1）
□ 签名验证加了 EIP-191 前缀
□ NFT 验证用了 BlockTagSafe 或至少不是 latest
□ RPC URL 没有硬编码（走 env 或配置）
□ FilterLogs 有区块范围限制
```
