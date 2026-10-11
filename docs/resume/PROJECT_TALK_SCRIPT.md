# StreamGate 项目讲述稿 + 面试问答(C3)

> 纪律:每个数字都能指到代码;所有 file:line 以 2026-10-10 的仓库为准。
> 诚实边界:不说"DRM"、不说"完全去中心化"、不说"生产级"。

---

## 一、30 分钟讲述稿(结构:1 + 4 + 1)

### 0. 开场(2 分钟)— 一句话定位

"StreamGate 是一个 NFT 门控的流媒体后端:钱包签名登录,链上 NFT 持有权决定能不能播,HLS 分片投递受保护。它解决的业务问题是'内容会员制'——把 Web3 资产当授权层,而不是把区块链当产品本身。我的背景是音视频后端,所以我故意让媒体链路是真刀真枪的:FFmpeg ABR 转码、JetStream 任务队列、HLS 投递,Web3 只是替换了授权那一层。"

关键:先讲业务问题,再讲技术,最后才提 AI/个人项目属性。

### 1. 钱包身份(5 分钟)

- 流程:后端发一次性 challenge → 客户端签名 → 后端验签 → challenge 原子消费 → 发 JWT。
- 验签支持三条路径:EIP-191 personal_sign、EIP-712 typed data、SIWE(EIP-4361)。实现要点:常量时间比较(防时序侧信道)、EIP-2 low-s 校验(防签名延展性)、EIP-1271(合约钱包签名委托校验)。
- 防重放:challenge 一次性消费用 Redis Lua 脚本,把"读 used 标记 + 写 used 标记"做成单命令原子操作——两步分开写就有 TOCTOU 窗口,多实例部署时内存锁也挡不住。
- 代码:`pkg/service/auth_wallet.go`、`pkg/web3/signature/`(verifier.go / eip712.go / siwe.go / eip1271.go)、`pkg/storage/redis_challenge.go:142`。

### 2. NFT 门控(7 分钟)

- 规则来自服务端(content_gating_rules 表 + RuleResolver),**绝不**来自请求参数——这是本次安全修复的核心原则之一。
- 校验内容:按规则走 ownerOf(token 级)或 balanceOf(集合级),ERC-165 探测合约标准(`pkg/web3/nft/verifier.go:560`)。
- 链上读的安全细节:
  - 固定用 `safe` block tag(`verifier.go:72`)——`latest` 在重组时会说谎;
  - 正结果缓存 TTL 上限 15 秒(`pkg/middleware/nft_gate.go:531`),负结果 TTL 更长;
  - 检测到重组:用 block hash 二次验证缓存条目,不一致即失效并进入降级 TTL(`nft_gate.go:408-424`)。
- 门控语义 fail-closed:没有规则 → 403;规则解析失败 → 500;中间件被禁用 → 503。"宁可错杀,不可放行"——因为这是授权层。

### 3. 受保护投递(5 分钟)

- 授权在 manifest 时做一次;segment 用短 TTL(30 分钟)、内容绑定、钱包绑定的 playback token。
- 为什么不在每个 segment 都查链:延迟 + RPC 费用 + CDN 缓存都会崩;capability token 是流媒体行业的标准做法(签名 URL / DRM license 的轻量版)。
- token 的信任根:它只在 NFT 校验通过后签发;携带钱包 + contentID + 指纹;短 TTL 限制爆炸半径;跨用户复用会被钱包绑定校验拒绝。
- 代码:`pkg/gateway/streaming_handlers.go`(manifest 签发 :258,segment 校验 :363)、`pkg/middleware/playback_token_auth.go`。

### 4. 媒体后端(5 分钟)

- FFmpeg ABR:4 档分辨率阶梯(`pkg/plugins/transcoder/ffmpeg.go:346`),HLS 切片参数(:417),stderr 正则解析进度(:570)——FFmpeg 不给结构化进度,只能刮 stderr。
- 任务队列:NATS JetStream + 独立 DLQ stream(`pkg/storage/nats_queue.go`),重试上限、指数退避、任务超时 60 分钟、卡死 worker 回收(`pkg/service/transcoding/transcoding.go:255-290,508`)。
- 这一块是"我原来的领域":并发、状态机、故障恢复。

### 5. 安全审计故事(4 分钟)——**面试最值钱的一段**

"这个项目是 AI 辅助写的。写完之后我做了一次授权路径的审计,用 TDD 的方式:先写 7 个测试——4 个编码'必须有 but 现在没有'的行为,3 个守卫已有行为。跑起来,4 个全红,证明了一条完整的绕过链:

1. JWT 中间件看到 query 里有 `playback_token` 就直接放行,不验签;
2. NFT 门控看到 `playback_auth` 标记就跳过全部链上校验;
3. 分片路由注册在 JWT 中间件**之前**(Gin 的 handler 链在注册时快照,`Use` 只追加),所以分片请求既没有 JWT 也没有门控;
4. manifest handler 的校验分支条件是 `token != "" && quality != ""`——不传 quality 就跳过校验;
5. 空钱包也能签发合法令牌;
6. 验签函数的可变参数允许调用方静默跳过钱包绑定比对;
7. 门控 fail-open:没有规则就放行,调用方还能用 query 参数自选'用哪个合约来验证我'。

修了 9 个任务,现在这 7 个测试是常驻 CI 回归;门控语义全部反转为 fail-closed。这个经历让我对'AI 让写代码变便宜,让判断代码对不对变贵'有了具体的体感。"

证据:`pkg/gateway/authz_regression_test.go`(7 个测试)、TRANSITION_PLAN.md 的 A.1 附录(逐行证据)。

### 6. 收尾(2 分钟)

"下一步最有价值的是 HLS AES-128 密钥分发:内容加密 + NFT 校验后的密钥接口。注意这是 token-gated access control,不是 DRM——DRM 靠硬件信任根,我做的就是密钥分发这一层。"

---

## 二、必须准备的 6 个问题(每个 3 层:一句话 → 关键点 → 主动说边界)

### Q1 "怎么保证未持 NFT 的人拿不到视频?"

- 一句话:三层防御——manifest 时的链上校验、fail-closed 门控、segment 的绑定令牌。
- 关键点:先主动讲你自己发现并修复的绕过链(上面第 5 节),这是最强证据——"我曾经拿不到,因为我自己的代码有 6 个洞,现在每个洞都有一个 CI 测试守着。"
- 边界:传输层目前不加密(拿到 .ts 是明文,前提是你有合法令牌);AES-128 密钥分发是规划中的下一步。

### Q2 "为什么不上链做门控?"

- 一句话:链是所有权真相源,门控判定留在链下——gas、延迟、可组合性都不允许每个请求上链。
- 关键点:每次 segment 查链 → 播放延迟 + RPC 成本 + 链拥堵时整个产品不可用;签名 URL/capability token 是流媒体标准做法;服务端规则表决定"哪个内容要哪个 NFT",链只回答"这个地址有没有"。
- 边界:规则表本身是中心化的——这是一个有意权衡(内容运营本来就是中心化行为);如果要求抗审查,可以把规则 hash 上链做一致性校验,那是另一个产品形态。

### Q3 "HLS AES-128 和 DRM 什么区别?"(必须答对)

- 一句话:AES-128 是传输加密,密钥最终交给客户端;DRM(Widevine/FairPlay)靠硬件信任根,密钥不交给普通应用层。
- 关键点:AES-128 防的是"链路上被扒";对拿到密钥的终端用户没有约束力。DRM 防的是"拿到内容的人再分发"。
- 边界(主动说):StreamGate 做的是密钥分发这一层,**不是 DRM**——我绝不把这两者混着说。
- 类别错误警示:任何"我们把 DRM 搬到链上"的说法都是错的。

### Q4 "为什么用 `safe` 不用 `latest`?"

- 一句话:`latest` 在重组时会变,`safe` 是被超多数确认过、极难重组的块。
- 关键点:balanceOf 在 block N 是 true、在重组后可能变 false;用 `latest` 拿到的"有 NFT"可能几秒后被撤销,而门控缓存已经放行了。`safe` + 短 TTL + 重组检测三层兜底。
- 边界:`safe` 换来的是几秒到十几秒的确认延迟,对"开始播放"这个动作完全可接受;如果是高频小额场景(如按秒计费),要重新权衡。

### Q5 "这项目用了 AI 吗?"

- 一句话:用了,大部分代码是 AI 辅助生成的;我的工作是审计、判断和修复——最有力的证据就是那次授权绕过的发现与修复。
- 关键点:AI 让写代码变便宜,让判断代码对不对变贵;我做的审计(TDD 先写失败测试再修)恰恰是"判断"那一侧的工作;仓库里 TRANSITION_PLAN.md 记录了每一处缺陷的行号证据。
- 边界:核心链路(验签、门控、投递、转码)的每一层设计我都能讲到第三层;外围模块我会主动指出哪些是脚手架、哪些我没深入过。我不会假装全懂,但我清楚边界画在哪里,也欢迎现场看代码。

### Q6 "这项目哪里去中心化了?"

- 一句话:所有权判定去中心化了(链是真相源),基础设施是有意自托管的——我不假装它是一个完全去中心化网络。
- 关键点:NFT 在链上,任何人可以自己跑一个校验器验证同样的规则;但内容分发、规则表、转码是中心化服务,这是产品形态决定的(内容运营/合规本身就是中心化行为)。
- 边界:完全去中心化的视频分发(IPFS + 每个观众都是节点)在体验和成本上 today 不可行;我的取舍是把"信任最小化"放在授权层。

---

## 三、白板题:"设计一个 NFT 门控的视频分发系统"(15 分钟框架)

1. **澄清需求**:内容规模?观众规模?延迟敏感?防到哪一层(链路/终端)?是否要 DRM?
2. **画出三个平面**:
   - 控制面:登录(挑战-签名)、规则管理(哪个内容要哪个 NFT)
   - 授权面:manifest 时链上校验(safe tag + 缓存 + 重组失效)
   - 数据面:segment 投递(capability token,CDN 友好,无私钥)
3. **关键取舍**:为什么 token 而不是每片查链;为什么 fail-closed;缓存 TTL 怎么定。
4. **失效模式**:RPC 挂了怎么办(熔断 + 缓存降级 + fail-closed);重组怎么办;令牌泄露怎么办(短 TTL + 钱包绑定 + 可吊销 jti)。
5. **收尾**:提 AES-128 密钥分发作为加密层扩展,并明确"这不是 DRM"。

---

## 四、演示脚本(若可现场跑)

1. `make demo`(或 `make fullchain-deploy`)起栈——部署脚本会自动为 `demo` 内容种服务端 gating rule(`scripts/docker-deploy.sh` [4b/5] 步),fail-closed 门控因此对 demo 保持可用。
2. 打开 h5-demo → demo 钱包连接 → 签名登录 → 自动 mint → NFT verify(显示余额)。
3. `curl` 未认证拉 manifest → **401**;无 NFT 钱包 → **403 NFT_REQUIRED**(安全探针,`scripts/h5-demo-acceptance/specs/01-index.mjs` 里也有)。
4. 持有者拉 manifest → **通过门控**:内容已转码时返回 200 + playback_token;尚未转码时返回 404 CONTENT_NOT_FOUND(注意:这说明请求已经通过了 JWT + 链上 NFT 校验,404 只是没有分片)。要演示 200,先上传并转码一个样例视频。
5. 指出 `pkg/gateway/authz_regression_test.go`:"这 7 个测试以前 4 个是红的"。
