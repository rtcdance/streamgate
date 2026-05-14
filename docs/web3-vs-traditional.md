# Web3 vs 传统后端：转型者对照指南

> 如果你有传统后端经验（CRUD、REST API、关系型数据库），
> 这份指南帮你把已有知识映射到 Web3 世界——相同的问题，不同的解法。

---

## 1. 数据库 vs 区块链

| 传统后端 | Web3 |
|---|---|
| 数据存在自己的 PostgreSQL/MySQL 里 | 数据存在全球数千个节点的公共账本上 |
| 你可以 INSERT/UPDATE/DELETE 任意数据 | 你只能 APPEND（写交易），不能删除或修改历史 |
| 查询是 SQL: `SELECT * FROM users WHERE id = ?` | 查询是 RPC: `eth_call` / `eth_getLogs` |
| 数据一致性由数据库事务保证 | 数据一致性由共识算法 + 重组(reorg)处理保证 |
| 你的数据你控制 | 你的数据全节点都能看到（公开透明） |

### 代码对比

```go
// 传统：查询用户余额
func getUserBalance(db *sql.DB, userID string) (int64, error) {
    var balance int64
    err := db.QueryRow("SELECT balance FROM users WHERE id = $1", userID).Scan(&balance)
    return balance, err
}

// Web3：查询 ERC-20 代币余额
func getTokenBalance(client *ethclient.Client, tokenAddr, walletAddr common.Address) (*big.Int, error) {
    // 构造 ABI 调用数据
    data, _ := erc20ABI.Pack("balanceOf", walletAddr)
    result, err := client.CallContract(context.Background(), ethereum.CallMsg{
        To:   &tokenAddr,
        Data: data,
    }, nil)
    // 解码返回值
    var balance *big.Int
    erc20ABI.UnpackIntoInterface(&balance, "balanceOf", result)
    return balance, err
}
```

**关键差异**: Web3 没有"查询"，只有"模拟调用合约方法"。你不能写 `WHERE`，只能调用合约暴露的只读方法。

---

## 2. 用户认证 vs 钱包签名

| 传统后端 | Web3 |
|---|---|
| 用户名 + 密码 → 比对 bcrypt hash | 钱包地址 + 签名 → ECDSA 公钥恢复 |
| 验证 "你知道什么" | 验证 "你拥有什么"（私钥） |
| session/token 由服务端签发 | JWT 由服务端签发（但签名的私钥在用户端） |
| 密码可以重置 | 私钥丢了 = 身份永久丢失 |
| 注册 = INSERT INTO users | "注册" = 第一次签名登录（没有注册流程） |

### 代码对比

```go
// 传统：密码验证
func verifyPassword(storedHash, inputPassword string) bool {
    return bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(inputPassword)) == nil
}

// Web3：签名验证
func verifySignature(address, message, signature string) bool {
    prefixed := fmt.Sprintf("\x19Ethereum Signed Message:\n%d%s", len(message), message)
    hash := crypto.Keccak256([]byte(prefixed))
    sig := common.FromHex(signature)
    if sig[64] >= 27 { sig[64] -= 27 }  // MetaMask → go-ethereum 格式转换
    pubKey, _ := crypto.SigToPub(hash, sig)
    recovered := crypto.PubkeyToAddress(*pubKey)
    return recovered == common.HexToAddress(address)
}
```

**核心认知转变**: Web3 没有密码哈希比对，用的是**公钥密码学**。用户"登录"= 用私钥签名一个 challenge，服务端用公钥恢复地址。

---

## 3. API 认证 vs JWT + 钱包

| 维度 | 传统 | Web3 |
|---|---|---|
| **令牌签发** | 登录时服务端签发，存 session | 钱包签名 challenge 后服务端签发 JWT |
| **签名算法** | HS256（对称）或 RS256 | 同样可以用 HS256/RS256，但用户级签名用 ECDSA |
| **身份标识** | username / email / user_id | wallet_address (0x...) |
| **权限模型** | RBAC (role → permission) | 用 NFT 持有量做门控（持有一个 NFT = 有权限） |
| **注销** | 删除 session / blacklist token | 把 JWT 的 jti 加入黑名单（链上无法注销） |

### 项目中的实现

```go
// 传统 CRUD handler
func handleCreatePost(c *gin.Context) {
    userID := c.GetInt("user_id")
    // userID = session 中取的
}

// Web3 handler（本项目）
func handleUpload(c *gin.Context) {
    wallet := middleware.GetWalletAddress(c) // ← 从 JWT claims 取
    contract := middleware.GetNFTContract(c) // ← 从 NFT gate middleware 取
    // wallet + NFT ownership 同时验证
}
```

---

## 4. 数据变更 vs 事件监听

| 传统后端 | Web3 |
|---|---|
| 用户操作 → HTTP POST → 服务端 UPDATE | 用户操作 → 发送交易 → 合约状态变更 → emit Event |
| 你用 Webhook 通知下游 | 你用 FilterLogs 监听链上事件 |
| 数据变更即时可见 | 数据变更需要等区块确认（~12秒） |
| 没有"重组"概念 | 最近几个块可能被重组，需要处理 |

### 代码对比

```go
// 传统：用户付款后更新数据库
func handlePayment(db *sql.DB, userID string, amount int64) {
    tx, _ := db.Begin()
    tx.Exec("UPDATE users SET balance = balance - $1 WHERE id = $2", amount, userID)
    tx.Exec("INSERT INTO transactions (user_id, amount) VALUES ($1, $2)", userID, amount)
    tx.Commit()
}

// Web3：监听链上 Transfer 事件
func listenTransfers(client *ethclient.Client, contract common.Address) {
    query := ethereum.FilterQuery{
        Addresses: []common.Address{contract},
        Topics:    [][]common.Hash{{transferEventSig}},
        FromBlock: big.NewInt(1000000),
        ToBlock:   big.NewInt(1000100),
    }
    logs, _ := client.FilterLogs(context.Background(), query)
    for _, log := range logs {
        // log.Topics[1] = from 地址
        // log.Topics[2] = to 地址
        // log.Data = 转账金额（ABI 编码）
    }
}
```

**核心认知转变**: Web3 应用不是通过 HTTP 请求感知数据变化的，而是**监听链上事件**。你的后端变成一个事件消费者。

---

## 5. 错误处理

| 场景 | 传统后端 | Web3 |
|---|---|---|
| 请求超时 | HTTP 504 | RPC 返回 error，需要重试 |
| 数据不存在 | 返回 404 | eth_call 返回空/合约 revert |
| 参数错误 | 返回 400 | 交易 revert（gas 照扣！） |
| 并发冲突 | 数据库行锁 | nonce 管理（顺序交易） |
| 服务降级 | 数据库只读副本 | RPC failover + BlockTagSafe 回退 |

### 需要记住的

```go
// 传统：失败可以无条件重试
// Web3: 有些失败不能重试！
if isPermanentRPCError(err) {
    // 合约 revert / invalid opcode → 重试也会失败
    return NewPermanentError(err)
} else {
    // 网络超时 / 限流 → 可以重试
    return NewRetryableError(err)
}
```

---

## 6. 性能考量

| 资源 | 传统 | Web3 |
|---|---|---|
| 数据库读延迟 | 1-10ms | RPC 100-500ms |
| 数据库写延迟 | 10-50ms | 交易确认 12s-几分钟 |
| 吞吐 | 万级 QPS | 单节点 ~100 QPS（有 rate limit） |
| 缓存 | Redis 随意用 | 必须缓存，但要注意 reorg 后的缓存失效 |
| 存储成本 | 磁盘便宜 | 链上存储极贵（~200 gas/字节） |

### 缓存策略差异

```go
// 传统缓存：只要 TTL 够短
cache.Set(key, value, 60*time.Second)

// Web3 缓存：必须绑 block number/hash
cache.Set(key, NFTAccessEntry{
    HasNFT:      true,
    BlockNumber: 1000000,
    BlockHash:   "0xabc...",
    Expires:     time.Now().Add(60*time.Second),
})
// 命中后检查：block hash 是否还是 canonical？
```

---

## 7. 测试策略

| 维度 | 传统 | Web3 |
|---|---|---|
| 单元测试 | mock DB / mock HTTP | mock EthCaller（拦截 CallContract） |
| 集成测试 | Testcontainers (PostgreSQL) | Anvil（本地以太坊节点） |
| E2E 测试 | 真实部署 | 全栈 Docker + Anvil + MinIO |
| 关键测试点 | SQL injection, XSS, CSRF | Reorg 处理, 签名验证, nonce 管理 |

### 项目中的测试金字塔

```
           /\
          /  \         E2E: test/e2e/ (需要 Docker 全链路)
         /    \
        /      \       Demo: test/demo/ (纯概念演示，零依赖)
       /        \
      /          \     Integ: test/integration/ (需要 DB/Anvil)
     /            \
    /______________\   Unit: pkg/*/*_test.go (纯 Go mock)
```

---

## 8. 常见认知转变清单

```
传统 → Web3 思维转变:

1. 数据模型:
   ❌ "我设计数据库表结构"
   ✅ "我设计合约状态变量和事件"

2. 用户:
   ❌ "用户名 + 密码注册"
   ✅ "钱包地址即身份，私钥即密码"

3. 权限:
   ❌ "角色 → 权限 → 菜单"
   ✅ "NFT 合约 → tokenID → 操作权限"

4. 事务:
   ❌ "ACID 数据库事务"
   ✅ "区块确认 + reorg 处理 + nonce 管理"

5. 调试:
   ❌ "看日志 + 查数据库"
   ✅ "看 RPC 响应 + 查交易 receipt + 事件 log"

6. 部署:
   ❌ "git push → CI/CD → 上线"
   ✅ "合约部署 → 验证 → 前端指向新合约地址"

7. 缓存:
   ❌ "Redis TTL 就够了"
   ✅ "TTL + block hash 验证 + reorg 检测"

8. 认证:
   ❌ "session / OAuth / JWT"
   ✅ "Challenge → EIP-191 签名 → 公钥恢复 → JWT"
```

---

## 9. 在项目中找到对应

| Web3 概念 | 在项目中的位置 |
|---|---|
| RPC 调用抽象 | `pkg/web3/chain.go` — `withChainClient` 泛型 |
| NFT 验证 | `pkg/web3/nft.go` — `NFTVerifier` |
| 签名验证 | `pkg/web3/signature.go` — `SignatureVerifier` |
| 多链 | `pkg/web3/multichain.go` — `MultiChainManager` |
| 事件索引 | `pkg/web3/event_indexer.go` — `EventIndexer` |
| EIP-1271 | `pkg/web3/eip1271.go` — 合约钱包验证 |
| Reorg 检测 | `pkg/web3/reorg.go` — `ReorgDetector` |
| 链感知 Finality | `pkg/web3/reorg.go` — `FinalityStrategy` 接口 |
| RPC 加权评分 | `pkg/web3/chain.go` — `updateRPCScores` |
| JWT 密钥分离 | `pkg/service/auth.go` — `JWTVerifier` + RS256 |
| NFT 门控中间件 | `pkg/middleware/nft_gate.go` |
| 上传格式校验 | `pkg/gateway/upload_handlers.go` — 魔数检测 |
