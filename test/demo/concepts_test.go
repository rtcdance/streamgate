//go:build demo

package demo_test

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/golang-jwt/jwt/v4"
)

// ============================================================
// Web3+Go 概念演示测试
//
// 这些测试不是为了验证代码正确性，而是为了演示具体的
// Web3 概念在代码级别的工作原理。每个测试都有详细的
// log 输出，运行时可以看到每个步骤发生了什么。
//
// 运行: go test -tags=demo -v ./test/demo/
// ============================================================

// Demo1: 签名验证中的 v 值调整
// 展示为什么 MetaMask 的签名需要调整 v 值才能被 go-ethereum 验证。
func TestDemo1_SignatureVAdjustment(t *testing.T) {
	t.Log("=== Demo 1: 签名 v 值调整 ===")
	t.Log("")
	t.Log("MetaMask 返回的签名中 v = 27 或 28")
	t.Log("go-ethereum 的 crypto.SigToPub 需要 v = 0 或 1")
	t.Log("所以需要: if sig[64] >= 27 { sig[64] -= 27 }")
	t.Log("")

	// 生成一个测试密钥对
	privateKey, _ := crypto.GenerateKey()
	address := crypto.PubkeyToAddress(privateKey.PublicKey)
	message := []byte("hello")
	hash := crypto.Keccak256(message)

	// 用 go-ethereum 签名 — 内部使用 v = 0/1
	goSig, _ := crypto.Sign(hash, privateKey)
	t.Logf("go-ethereum 签名 v = %d", goSig[64])

	// 模拟 MetaMask: v + 27
	mmSig := make([]byte, 65)
	copy(mmSig, goSig)
	mmSig[64] += 27
	t.Logf("MetaMask 签名 v = %d", mmSig[64])

	// 不调整 v 直接恢复 — 会失败
	_, err := crypto.SigToPub(hash, mmSig)
	t.Logf("不调整 v 直接 SigToPub → %v", err)

	// 调整 v 后恢复 — 成功
	mmSig[64] -= 27
	recoveredPub, _ := crypto.SigToPub(hash, mmSig)
	recoveredAddr := crypto.PubkeyToAddress(*recoveredPub)
	t.Logf("调整 v 后 SigToPub → recovered = %s", recoveredAddr.Hex())
	t.Logf("原始地址 = %s", address.Hex())
	t.Logf("匹配: %v", recoveredAddr == address)
	t.Log("")
	t.Log("结论: MetaMask 的 v(27/28) 必须转为 v(0/1) 才能被 SigToPub 识别")
}

// Demo2: EIP-191 消息前缀
// 展示为什么以太坊签名必须加 \x19Ethereum Signed Message:\n 前缀。
func TestDemo2_EIP191Prefix(t *testing.T) {
	t.Log("=== Demo 2: EIP-191 消息前缀 ===")
	t.Log("")
	t.Log("以太坊要求签名的消息必须加前缀:")
	t.Log("  \\x19Ethereum Signed Message:\\n{len(message)}")
	t.Log("如果没有这个前缀，服务端验证和钱包签名会不匹配")
	t.Log("")

	privateKey, _ := crypto.GenerateKey()
	address := crypto.PubkeyToAddress(privateKey.PublicKey)
	message := "Sign this to authenticate"

	// 错误: 直接哈希消息
	wrongHash := crypto.Keccak256([]byte(message))
	wrongSig, _ := crypto.Sign(wrongHash, privateKey)

	// 正确: 加 EIP-191 前缀
	prefixed := fmt.Sprintf("\x19Ethereum Signed Message:\n%d%s", len(message), message)
	rightHash := crypto.Keccak256([]byte(prefixed))
	rightSig, _ := crypto.Sign(rightHash, privateKey)

	t.Logf("错误哈希: 0x%x...", wrongHash[:4])
	t.Logf("正确哈希: 0x%x...", rightHash[:4])
	t.Logf("两个签名不同: %v", !bytes.Equal(wrongSig, rightSig))

	// 验证正确路径
	pub, _ := crypto.SigToPub(rightHash, rightSig)
	recovered := crypto.PubkeyToAddress(*pub)
	t.Logf("正确路径 recovered = %s == %s: %v", recovered.Hex(), address.Hex(), recovered == address)

	// 验证错误路径
	pub2, _ := crypto.SigToPub(wrongHash, wrongSig)
	recovered2 := crypto.PubkeyToAddress(*pub2)
	t.Logf("错误路径 recovered = %s == %s: %v", recovered2.Hex(), address.Hex(), recovered2 == address)
	t.Log("")
	t.Log("结论: 不加 EIP-191 前缀的签名也能恢复出地址，但和钱包签的不一样")
}

// Demo3: JWT HS256 vs RS256
// 展示为什么生产环境应该用 RS256 而非 HS256。
func TestDemo3_JWTHS256vsRS256(t *testing.T) {
	t.Log("=== Demo 3: JWT HS256 vs RS256 ===")
	t.Log("")
	t.Log("HS256 的问题: 任何知道 secret 的服务都能签发令牌")
	t.Log("RS256 的优势: auth-service 专签不验, 其他服务专验不签")
	t.Log("")

	// HS256: 共享密钥
	secret := []byte("shared-secret")
	claims := jwt.MapClaims{
		"sub": "0xUser",
		"wallet_address": "0xUser",
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(1 * time.Hour).Unix(),
	}

	hsToken := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	hsStr, _ := hsToken.SignedString(secret)
	t.Logf("HS256 令牌: %s...", hsStr[:40])

	// 任何持有 secret 的人都能伪造
	fakeClaims := jwt.MapClaims{
		"sub": "0xAttacker",
		"wallet_address": "0xAttacker",
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(1 * time.Hour).Unix(),
	}
	fakeToken := jwt.NewWithClaims(jwt.SigningMethodHS256, fakeClaims)
	fakeStr, _ := fakeToken.SignedString(secret)
	t.Logf("攻击者用同一 secret 伪造: %s...", fakeStr[:40])

	// RS256: 非对称密钥
	privateKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	publicKey := &privateKey.PublicKey

	rsaToken := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	rsaStr, _ := rsaToken.SignedString(privateKey)
	t.Logf("RS256 令牌: %s...", rsaStr[:40])

	// 验证方只有公钥，不能签名
	parsed, _ := jwt.Parse(rsaStr, func(token *jwt.Token) (interface{}, error) {
		return publicKey, nil
	})
	t.Logf("RS256 公钥验证: valid=%v", parsed.Valid)

	t.Log("")
	t.Log("结论: RS256 分离了签名权和验签权")
	t.Log("auth-service: 持有私钥 → 只能签发")
	t.Log("gateway/streaming: 持有公钥 → 只能验证")
}

// Demo4: big.Int 可变性陷阱
// 展示 *big.Int 修改接收者的特性。
func TestDemo4_BigIntMutability(t *testing.T) {
	t.Log("=== Demo 4: big.Int 可变性陷阱 ===")
	t.Log("")
	t.Log("*big.Int 的方法会修改接收者并返回自身")
	t.Log("两个变量指向同一个 big.Int 时，改一个会影响另一个")
	t.Log("")

	a := big.NewInt(5)
	b := a // 指向同一对象
	t.Logf("a = %d, b = %d", a.Int64(), b.Int64())

	b.Add(b, big.NewInt(1))
	t.Logf("b.Add(1) 后: a = %d, b = %d (应该 a=5, b=6)", a.Int64(), b.Int64())
	t.Logf("但实际 a = %d ← 也被改了!", a.Int64())

	// 正确做法
	c := big.NewInt(5)
	d := new(big.Int).Set(c) // 显式拷贝
	d.Add(d, big.NewInt(1))
	t.Logf("正确做法: c = %d, d = %d", c.Int64(), d.Int64())
	t.Log("")
	t.Log("结论: 用 new(big.Int).Set(src) 或 new(big.Int).Add(x,y) 创建新对象")
}

// Demo5: 地址比较
// 展示 common.Address 的正确比较方式。
func TestDemo5_AddressComparison(t *testing.T) {
	t.Log("=== Demo 5: 地址比较 ===")
	t.Log("")
	t.Log("common.Address 是 [20]byte，直接 == 比较")
	t.Log("不需要转成字符串再用 EqualFold")
	t.Log("")

	addr1 := common.HexToAddress("0x742d35Cc6634C0532925a3b844Bc9e7595f42bE1")
	addr2 := common.HexToAddress("0x742d35cc6634c0532925a3b844bc9e7595f42be1")

	t.Logf("addr1 = %s", addr1.Hex())
	t.Logf("addr2 = %s", addr2.Hex())
	t.Logf("addr1 == addr2: %v", addr1 == addr2)
	t.Log("")
	t.Log("结论: common.Address 是 [20]byte，直接 == 比较即可")
}

// Demo6: RPC 调用必须带超时
// 展示没有超时的 RPC 调用会导致 goroutine 永久阻塞。
func TestDemo6_RPCTimeout(t *testing.T) {
	t.Log("=== Demo 6: RPC 超时 ===")
	t.Log("")
	t.Log("context.Background() 没有超时或取消机制")
	t.Log("如果 RPC 节点挂了但 TCP 连接没断开,")
	t.Log("BalanceAt 可能永久阻塞 → goroutine 泄漏")
	t.Log("")

	t.Log("错误做法:")
	t.Log("  balance, err := client.BalanceAt(context.Background(), addr, nil)")
	t.Log("")
	t.Log("正确做法:")
	t.Log("  ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)")
	t.Log("  defer cancel()")
	t.Log("  balance, err := client.BalanceAt(ctx, addr, nil)")
	t.Log("")
	t.Log("结论: 每条 RPC 调用都必须带超时 context")
}

// Demo7: NFT 验证中的 BlockTag
// 展示为什么用 latest 做 NFT 验证有风险。
func TestDemo7_BlockTagSafety(t *testing.T) {
	t.Log("=== Demo 7: BlockTag 安全读取 ===")
	t.Log("")
	t.Log("用 latest block 验证 NFT 所有权:")
	t.Log("  如果在 block 100 验证通过，但 block 100 在 6 个块后被重组")
	t.Log("  持有者 A 已经不再拥有该 NFT，但系统仍放行")
	t.Log("")
	t.Log("用 safe block(-4) 或 finalized block(-3):")
	t.Log("  读取信标链已确认的块，不会被重组")
	t.Log("  代价是延迟增加 ~12 秒")
	t.Log("")
	t.Log("go-ethereum 中的 block 参数约定:")
	t.Log("  nil = latest")
	t.Log("  -4  = safe (~4 个 epoch)")
	t.Log("  -3  = finalized (~2 个 epoch)")
	t.Log("")
	t.Log("项目中的实现: chain.CallContractAtBlock(ctx, msg, BlockTagSafe)")
	t.Log("如果 RPC 不支持，回退到 latest")
}

// Demo8: RPC Failover 加权评分演示
// 展示 withChainClient 的评分逻辑。
func TestDemo8_RPCFailoverScoring(t *testing.T) {
	t.Log("=== Demo 8: RPC 加权评分逻辑 ===")
	t.Log("")
	t.Log("withChainClient 每次 RPC 调用后更新评分:")
	t.Log("")
	t.Log("  latencyScore = 1.0 - (latency / 5.0)")
	t.Log("  success: newScore = oldScore * 0.9 + latencyScore * 0.1")
	t.Log("  failure: score *= 0.5")
	t.Log("")

	// 模拟一次成功调用（延迟 100ms）
	oldScore := 1.0
	latency := 0.1 // 100ms
	latencyScore := 1.0 - (latency / 5.0)
	if latencyScore < 0 {
		latencyScore = 0
	}
	newScore := oldScore*0.9 + latencyScore*0.1
	t.Logf("成功 (100ms):  评分 %.4f → %.4f", oldScore, newScore)

	// 模拟一次失败
	failScore := oldScore * 0.5
	t.Logf("失败:          评分 %.4f → %.4f", oldScore, failScore)

	// 模拟慢调用（延迟 4s）
	slowLatency := 4.0
	slowLatencyScore := 1.0 - (slowLatency / 5.0)
	if slowLatencyScore < 0 {
		slowLatencyScore = 0
	}
	slowNewScore := oldScore*0.9 + slowLatencyScore*0.1
	t.Logf("成功 (4000ms): 评分 %.4f → %.4f", oldScore, slowNewScore)
	t.Log("")
	t.Log("结论: 挂掉的 RPC 分数暴跌，慢 RPC 分数下降，快的 RPC 保持高分数")
	t.Log("connectAny() 和 failover() 始终从分数最高的节点开始尝试")
}

// Demo9: ERC-721 ownerOf ABI 编码
// 展示智能合约调用在字节级别是怎么工作的。
func TestDemo9_ABIEncoding(t *testing.T) {
	t.Log("=== Demo 9: ERC-721 ownerOf ABI 编码 ===")
	t.Log("")
	t.Log("智能合约调用 = 向合约地址发一笔 data 交易")
	t.Log("data = 函数选择器(4字节) + 参数(32字节对齐)")
	t.Log("")

	// 函数选择器 = keccak256("ownerOf(uint256)") 的前 4 字节
	selector := crypto.Keccak256([]byte("ownerOf(uint256)"))[:4]
	t.Logf("keccak256('ownerOf(uint256)')[:4] = 0x%x", selector)

	// 参数 = tokenID (左填充到 32 字节)
	tokenID := big.NewInt(42)
	padded := common.LeftPadBytes(tokenID.Bytes(), 32)
	t.Logf("tokenID=42 左填充 32 字节 = 0x%x", padded)

	// 完整调用数据
	calldata := append(selector, padded...)
	t.Logf("完整 calldata = 0x%x", calldata)
	t.Logf("  前 4 字节: 函数选择器 6352211e")
	t.Logf("  后 32 字节: tokenID=42")
	t.Log("")
	t.Log("这就是 eth_call 发送给合约的数据")
	t.Log("合约用前 4 字节找到函数，用后 32 字节作为参数")
}

// Demo10: SHA-256 哈希 + 存储键
// 展示上传文件的哈希计算和存储路径。
func TestDemo10_UploadHashingAndStorage(t *testing.T) {
	t.Log("=== Demo 10: 上传文件哈希 + 存储路径 ===")
	t.Log("")
	t.Log("文件上传时同时计算 SHA-256 哈希:")
	t.Log("  tee := io.TeeReader(reader, sha256.New())")
	t.Log("  数据同时写入 MinIO 和哈希计算器")
	t.Log("")

	content := []byte("test video content")
	h := sha256.New()
	tee := h  // io.TeeReader 的逻辑

	// 模拟写入
	tee.Write(content)
	hash := fmt.Sprintf("%x", h.Sum(nil))
	t.Logf("文件内容: %q", string(content))
	t.Logf("SHA-256:  %s", hash)
	t.Log("")

	ownerID := "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266"
	uploadID := "550e8400-e29b-41d4-a716-446655440000"
	storageKey := fmt.Sprintf("%s/%s.mp4", ownerID, uploadID)
	t.Log("存储路径格式: {ownerID}/{uploadID}.{ext}")
	t.Logf("实际路径: %s", storageKey)
	t.Log("")
	t.Log("这个路径存储在 MinIO 的 streamgate bucket 中")
	t.Log("上传记录存入 PostgreSQL uploads 表")
}

var _ = big.NewInt
