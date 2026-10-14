# StreamGate 项目讲述稿 + 演示 + 追问（SG-09 / M6 的 AI 交付部分）

> 纪律：每个数字都能指到 SHA / 测试名 / 实测断言；file 以 2026-10-13 的仓库为准——行号会漂移，函数名不会。
> 诚实边界：不说 DRM、不说完全去中心化 / 无需信任、不说生产高可用已验证、不说版权确权；多 RPC 回退 ≠ 轻客户端证明；NFT 持有 ≠ 媒体版权。
> AI 归属：项目由 AI 辅助编码。**AI 的代码生成不等于"我独立设计实现"**——个人贡献主张逐条标【待本人确认】（第六节）。
> 状态图例：✅ 已完成且有证据 · ⏳ 待补 / 待实测 · 【待本人确认】。ChainPulse 是独立项目，证据另行整理，不用 StreamGate 的验收为它背书。

---

## 零、状态速览（被问进度就照这个说）

- ✅ SG-01～06、SG-10、SG-11、SG-12（九步全部）、SG-13 A+B、SG-02、SG-14（含备份恢复演练 17/17）、SG-07 性能实测（1/10/50 观众各 100% 成功）；证据登记 `TRANSITION_PLAN.md` §8。
- ⏳ **远端 CI（提交未推送，run 待运行）**、SG-08 的 GitHub 侧同 digest CD（本地 release stage 已实跑空卷部署 + 失败/恢复双断言，远端 workflow 仍待运行）。
- 实测口径：双部署模式 **ALL 66 PASSED**（59 常规 + 7 独立 worker 归因）；70s 撤销上界**实测 67s**；正证据 age ≤5s；能力 TTL 60s；容差 5s；依赖故障入口 **ALL 16 PASSED**；50 观众负载 **1400/1400 成功、ttfb_p95 61.7ms**（每观众独立钱包）。

---

## 一、3 分钟自我介绍（口播稿，约 700 字）

"我是 [姓名]，10 年以上 C++，其中 6 年多在音视频流媒体——iOS / Android 原生播放器和流媒体后端，现在转 Web3 后端方向。展示项目是 StreamGate：NFT 门控的流媒体后端，业务问题是'内容会员制'——链上资产决定观看权限，媒体交付仍是真实的流媒体系统。

主线可以现场复现：真实钱包签名登录 → 一次性挑战防重放 → 服务端 NFT 规则 → 同链同区块持仓证据 → 60 秒播放能力 → 真实 HLS 解码播放 → NFT 转移后权限失效 → RPC / Redis 故障下的拒绝与恢复。

三个最硬的证据：

第一，签名与授权。challenge 是 128 位 nonce、Redis Lua 一次性原子消费，重放必拒；gating 规则 owner 校验在业务层、与写入同一条 SQL 语句，A/B 双钱包越权负例在真实 Postgres 上 7 例全跑 0 skip；服务端代签只有 development + 31337 + 显式开关三重门才存在。

第二，链状态与撤销。授权读取固定到解析出的区块号与哈希，读前后各核对一次；正证据总年龄 ≤5 秒。能力 TTL 60 秒 + 证据 5 秒 + 容差 5 秒，三项**推导**出 70 秒转移撤销上界——验收实测 67 秒失效。

第三，媒体交付。原件由服务端算 SHA-256 封存进不可覆盖的 key；ffprobe 白名单门拒绝超限而不是截断；转码跑在独立 worker 容器（非 root、无 JWT、无链私钥），验收用数据库 lease_owner 加容器日志做执行归因；播放列表用 FFmpeg 真实 EXTINF，只重写 URI。双部署模式 66 项断言全过。

关于 AI：项目是 AI 辅助编码的，我的工作是设计决策、验收判据、代码评审和故障定位——三次'真跑一次'才暴露的问题：分片忽略 Range、规则缓存 30 秒不生效、三个 nonce 分配器在真实栈上互相顶掉交易。AI 让写代码变便宜，让判断代码对不对变贵，这个项目给我的都是判断侧的训练。

边界也说清楚：这是'链上权限 + 平台媒体服务'，RPC provider 是信任前提不是密码学证明；NFT 持有不等于版权；它不是 DRM。"

---

## 二、10 分钟演示脚本（登录 → 播放 → 转移 → 拒绝 → 故障恢复）

### 前置（不计入 10 分钟）

```bash
make fullchain-deploy                          # PG+Redis+MinIO+NATS+Anvil+monolith+worker+h5
curl -s http://localhost:18080/health          # {"status":"healthy"}
bash scripts/fullchain-acceptance.sh http://localhost:18080 | tee /tmp/acc.log   # 可选，66 项留档
```

变量（演示前 export；是运行时数据，背"从哪里取"不背值）：

```bash
BASE=http://localhost:18080
ANVIL=http://localhost:18545                   # chainId 31337
KEY_A=0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80   # 持有人（Anvil 确定性账户）
KEY_B=0x59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8412f4603b6b78690d   # 无 NFT 钱包
WALLET_A=$(cast wallet address --private-key $KEY_A); WALLET_B=$(cast wallet address --private-key $KEY_B)
CONTENT_ID=$(docker exec sg-fc-postgres psql -U postgres -d streamgate -tAc "SELECT id FROM contents ORDER BY created_at DESC LIMIT 1")
RULE_ID=$(docker exec sg-fc-postgres psql -U postgres -d streamgate -tAc "SELECT id FROM content_gating_rules ORDER BY created_at DESC LIMIT 1")
DEMO_NFT=$(docker exec sg-fc-postgres psql -U postgres -d streamgate -tAc "SELECT contract_address FROM content_gating_rules ORDER BY created_at DESC LIMIT 1")
TOKEN_ID=<验收输出 / 链上事件里查到的持有人 token id>
```

### 阶段 1：真实登录（2 分钟）

H5 页 `http://localhost:18000/demo/` 连接 demo 钱包签名登录，或走与验收相同的 CLI 路径：

```bash
CH=$(curl -s -X POST $BASE/api/v1/auth/challenge -H 'Content-Type: application/json' -d "{\"wallet\":\"$WALLET_A\",\"chain_id\":31337}")
MSG=$(printf '%s' "$CH" | python3 -c 'import sys,json;print(json.load(sys.stdin)["message"])')
CID=$(printf '%s' "$CH" | python3 -c 'import sys,json;print(json.load(sys.stdin)["challenge_id"])')
SIG=$(cast wallet sign --private-key $KEY_A "$MSG")
TOKEN_A=$(curl -s -X POST $BASE/api/v1/auth/login -H 'Content-Type: application/json' \
  -d "{\"wallet\":\"$WALLET_A\",\"chain_id\":31337,\"challenge_id\":\"$CID\",\"signature\":\"$SIG\"}" \
  | python3 -c 'import sys,json;print(json.load(sys.stdin)["token"])')
```

预期断言：`Wallet A challenge -> personal_sign -> login issued a token`、`Forged HS256 token rejected (401)`。
讲一句：nonce 128 位、TTL ≤300s、同一 challenge 第二次消费必失败（`MarkChallengeUsed` 是 Redis Lua 单命令原子操作）。

### 阶段 2：真实播放（2 分钟）

```bash
curl -s -o /dev/null -w '%{http_code}\n' -H "Authorization: Bearer $TOKEN_A" \
  $BASE/api/v1/streaming/$CONTENT_ID/manifest.m3u8      # → 200
# 浏览器 http://localhost:18000/demo/ 真播
```

预期断言：`Wallet A (holder) receives the gated manifest (200)`、`Downloaded N segment(s)`、`All segments fully decode with FFmpeg`、`Range request returns 206` + `Content-Range`、`Playlist actual timing: … summing to 10.0s`、`Browser played the stream and time advanced`（hls.js 真解码，`currentTime` ≥3s，实测 3.51–3.75s）。
讲一句：播放列表是 FFmpeg 真实产物只重写 URI；Range 是实测发现修出来的缺陷（修复前恒 200 + 整段）。

### 阶段 3：NFT 转移 → 权限失效（2 分钟）

```bash
cast send $DEMO_NFT "transferFrom(address,address,uint256)" $WALLET_A $WALLET_B $TOKEN_ID --rpc-url $ANVIL --private-key $KEY_A
cast rpc anvil_mine 48 --rpc-url $ANVIL     # 推进 safe 视图（Anvil 实测滞后约 32 块；无流量不前进）
curl -s -o /dev/null -w '%{http_code}\n' -H "Authorization: Bearer $TOKEN_A" \
  $BASE/api/v1/streaming/$CONTENT_ID/manifest.m3u8      # → 403
# 转回必须用 B 的私钥（调用方不是持有人会 revert）
cast send $DEMO_NFT "transferFrom(address,address,uint256)" $WALLET_B $WALLET_A $TOKEN_ID --rpc-url $ANVIL --private-key $KEY_B
cast rpc anvil_mine 48 --rpc-url $ANVIL     # 复验 → 200
```

预期断言：`Transferred the holder's N token(s) away (balance now 0)`、`The safe view now includes the transfer`、`Transferring the holding away denies the next manifest (403)`、`Transferring the holding back restores access (200)`。
讲一句：gate 读 safe 视图——safe 没越过转移块时 200 是**正确**行为，所以验收先断言余额归零、显式挖块、断言 safe 越块，最后才判 403。

### 阶段 4：拒绝矩阵（2 分钟）

```bash
TOKEN_B=<按阶段 1 同法用 KEY_B 登录>
curl -s -o /tmp/b.json -w '%{http_code}\n' -H "Authorization: Bearer $TOKEN_B" \
  $BASE/api/v1/streaming/$CONTENT_ID/manifest.m3u8      # → 403，/tmp/b.json 无 #EXTM3U
# 无 token → 401；capability 当 Bearer 打管理 API → 401；跨内容 capability → 401
```

预期断言：`Wallet B (no NFT) refused the manifest (403)`、`Wallet B's refusal carried no media`、`Unauthenticated request rejected (401)`、`A capability replayed as a Bearer token on a management API is refused (401)`、`Playback token refused for another content`、`Path traversal in the segment name refused`。
讲一句：fail-closed——零规则 403、依赖不可核验 503、未注册代签路由 404（不是 401/403，避免暴露"存在一个受保护的签名器"）。

### 阶段 5：故障与恢复（2 分钟）

```bash
bash scripts/dependency-fault-acceptance.sh    # 本机实测 ALL 16 PASSED（脚本自停/起依赖并恢复）
```

预期：RPC 全断 → 新 manifest `503 NFT_VERIFY_ERROR`，**已发能力的分片仍 200** 且仍按原有效期到期（链不可达不能延长它），恢复后新 manifest 200；Redis 断 → 受保护资源 401、`/health` 仍 200，恢复后 200。
可选：`docker kill sg-fc-transcoder && docker start sg-fc-transcoder` 模拟 worker 崩溃——证据是测试 `TestAcceptance_TranscodeRedeliveryAndCrash`（lease 过期回 pending、重投递不重复副作用）；"≤60s 重新认领"是 SG-14 SLO，⏳ **未实测，不报数**。

---

## 三、30 分钟深入讲述（1 + 4 + 1）

**0. 开场（2min）**：用第一节压缩版——业务问题 → 主线 → AI 一句话 → 边界一句话。

**1. 签名与资源授权（7min）**

- 流程：challenge 下发一次性消息 → `personal_sign` → login 验签并**原子消费** → JWT。challenge：16 字节 nonce、TTL ≤300s、绑定地址与链；消费是 Redis Lua 单命令"读 used + 写 used"——分两步有 TOCTOU，多实例下进程锁无效。
- 验签三路径：EIP-191 / EIP-712 / SIWE(EIP-4361)——SIWE 逐字段校验 domain/URI/chain/nonce/address（`pkg/web3/signature/siwe.go:ValidateSIWEMessage`），地址比对 `subtle.ConstantTimeCompare`，合约钱包 EIP-1271，未知 signType 显式拒绝（旧实现静默落到默认 personal_sign）。
- 凭据边界：capability 有独立 `token_type`/audience，JWT 中间件白名单（`""`/`access`）其余 401；算法精确固定 HS256/RS256（家族校验会放过 HS384/HS512）；iss/aud 配置即强制。**capability ≠ access token，反向也不行**（`ParsePlaybackToken` 要求自己的 audience）。
- owner 隔离：gating-rule create/update/delete 在业务层条件写，ruleID→contentID 的定位放进**同一条 UPDATE/DELETE 的 EXISTS**，读写间无窗口；404/403 稳定区分；匿名 401。
- 代签边界：通用 `/web3/tx/send` 已删除；存活的 `/nft/dev/mint` 固定合约 + 固定 selector + value 恒 0 + 铸给已认证钱包；三重门（debug + 31337 + `features.demo_tx_signer`）任一缺失即不注册（404）。
- 链写 nonce：`(chainID, signer)` 在 `pg_advisory_xact_lock` 下取 `max(DB+1, 链 pending)`，`UNIQUE(chain,signer,nonce)`；**先持久化 signed raw tx/nonce/hash 再广播**，RPC 超时记 `broadcast_unknown`，恢复时查 receipt 或重播**同一 raw 字节**，绝不重签。

**2. 链状态、缓存与撤销（8min）**

- 同区块证据：`resolveAnchor` 按配置 tag（默认 safe）一次解析 `(number,hash)`；verifier 的 block 参数**必填**（接口上无法悄悄 latest）；ERC-165 探测与余额同块；读后复核 header，期间换块即拒且不写缓存；无 prover / 解析失败 fail closed（用例断言**零链上读**）。
- 缓存语义：存**余额证据**不存裁决——键 `chain:wallet:contract:token:standard` **不含 min_balance**，每请求按当前规则重新比较；正证据总 age ≤5s 从原始 `observedAt` 起算，**写缓存不刷新时间**；负缓存 ≤2s；过期直接拒绝，无授权结果的过期容错。
- reorg：检测回调**原先在生产里从未注册**，现接到 `InvalidateNFTEvidence`——只清该链证据；真实重组用 `evm_snapshot/revert` 构造并由生产检测器判定（非 sleep）。
- 排障故事：记录型 RPC 代理抓到固定块实现残留的一次**未固定 latest `eth_call`**（`detectTokenStandardCached` 无条件先做未固定探测）——mock 单测看不见，真节点+代理才看得见。另一发现：Anvil 默认把 safe/finalized 钉在创世块（需 `--slots-in-an-epoch 1`），所以测试只断言"已进入 safe 视图的状态"。
- 能力与撤销：60s TTL 是**策略值**（`WithPlaybackTokenTTL` 在 `MaxPlaybackTokenTTL` 钳制，零 TTL 回落策略值而非 30 分钟）；能力携带 `sessionID + authEpoch + ruleVersion`；登出推高 epoch → **已发能力** 5s 内失效；规则版本与写入同一条 CTE 提交（迁移 036），版本变了旧能力被拒且**规则还原后也不复活**（还原是新版本）；嵌套 playlist 是续期点（≤5s 证据重查持仓），segment 只验能力不查链。
- 70s：`MaxTransferRevocationBound = 70s = 60+5+5` 之和，`ValidateProduction` 校验**总和**（三项各自合规加起来仍可能超界）。历史缺陷：容差硬编码 30s ⇒ 真实窗口 90s，修复后实测**过期落 67s**。
- 依赖故障：RPC 全断拒"新申请/续期"（503），已发能力在会话/规则/epoch 可验时可在**原有效期**内读 segment，不因链故障延长；Redis 断则受保护资源全拒而 `/health` 正常——拒绝受保护资源 ≠ 进程已死。

**3. 真实媒体交付与实测排障（7min）**

- 验收先修对：旧脚本自签 HS256 JWT（从未走认证）、1KB 全零字节冒充视频。现在 token 全来自 challenge→sign→login；fixture 真媒体（640x360、h264+aac、10s、sha256 记录）并强制整段解码；缺工具/缺媒体 = FAIL 不 skip。
- 原件可信：完成上传时服务端流式算 SHA-256，封存到 `originals/<contentID>/<sha256><ext>`（digest 在 key 里 ⇒ 不同字节不同 key）；再封存不同 digest → 409；**从不采信客户端 hash**；线上实证 DB `file_hash=bf582157…` 与封存对象 S3 GET 一致；verified digest 才进链上登记意图。
- 输入安全：SSRF 精确白名单、**解析主机后逐地址判定**（公网+内网记录整体拒、解析失败拒）；断言是目标服务**真实收到 0 请求**（只判 4xx 不够——"先校验后照样 fetch"仍会过）；metadata IP / localhost / 外网 / `file://` 线上全 400。
- 媒体门：ffprobe 白名单（mp4+H.264+AAC）、600s/1080p 上界、独立 60s 超时 + 1MiB 输出上限；超限**拒绝而非截断**——原 `LimitReader(2GiB)` 到限后 `io.Copy` 返回 nil，会"成功"转码一个被截断的原件。
- 任务权威：Postgres 是 task status 唯一来源；任务行与 outbox 意图**同事务**、PubAck 后标送达；跨实例 claim = 条件 UPDATE + lease（DB `NOW()` 计时）+ generation，失去 lease 无法提交；重试 = 同消息延迟 Nak、业务上限 3 次（含首次）；第 3 次失败 → failed + DLQ 意图**同事务**，发布成功才 Ack；`GetStatus` 不读进程 map。
- generation 发布：输出进私有 `g<N>/`，全部校验通过后在 lease 守卫的事务里把 `published_generation` **单调向前**切换——这次 UPDATE 就是发布本身；飞行中的 generation 拿着合法 capability 也读不到；失败只清自己的 `g<N>/`。
- 信任域隔离：不可信媒体解析在 `sg-fc-transcoder`——运行态实测非 root、`CapDrop=ALL`、只读根 + tmpfs `/tmp`、1GiB/2cpu、env 无 JWT/私钥/RPC；执行归因 = DB `lease_owner` 非空 **且** `docker logs sg-fc-transcoder` 命中 taskID（内嵌 worker 已禁用，无别的执行者）；容器缺失 = FAIL。
- 真实 playlist：只重写 URI；`EXT-X-KEY`（加密）与未知带 URI 标签**拒绝**而非泄露对象路径；EXTINF 总和 ≈ 真实时长（10s fixture 实测 10.0s）、TARGETDURATION ≥ 最大 EXTINF（RFC 8216）；手写"每段 6 秒"只剩遗留回退。

**4. 证据边界：压测与发布（4min）**

- 压测（✅ 已实测，口径先讲）：真实 HTTP client 把响应体**读完并校验大小/hash**，统计的是已授权 playlist + segment + init/key 的**全部响应字节**；固定 EXTINF 节奏、固定 rendition、每观众独立真实登录持 NFT；预热 10s / 采样 30s。**实测（commit `f4d65bc`，fixture hash `086df28b…`）**：1 观众 28/28、10 观众 280/280、50 观众 **1400/1400 成功、0 错误、0 个 4xx**，ttfb_p95 = 58.4 / 85.5 / 61.7ms —— 50 观众 ≥99% 门槛达标（实测 100%）。跑分还**抓出一个真实合桶缺陷**：IP 键控的全局限流器在 auth 之前把所有观众并进一个 manifest 桶（10 观众 120×429），媒体路径豁免后归零（`f4d65bc`，用例 `TestRateLimitMiddleware_MediaRoutesAreNotIPMerged`）。未做的如实说：冷缓存 / RPC 故障三场景、reference vs candidate 的 p95 回归比较（需两轮同 runner 数据）；CPU/RSS 未采。点名：`test/performance/performance_test.go:TestConcurrentRequests` 是 sleep 调度模拟，**其数字不可引用**。
- 发布（本地已实跑，远端待运行）：设计是一 SHA 一候选、扫描/部署/发布全程同 digest（`image@sha256:`）、临时栈空卷 + 真 migration + 真播放 smoke、恢复演练必须"B 确实失败 + A 恢复"双断言（rollback 成功不能洗绿 deploy 失败）；schema 已落地 additive 迁移 + 齐全 `.down.sql`（036–040）。**已跑**：verify-docker release stage 实跑空卷部署 smoke **ALL 66 PASSED** + 坏变体被拒 + 健康镜像恢复；部署命令失败的反向证明 `EXIT=1`（0 残留容器）。**未跑**：GitHub 侧同 digest CD、回滚 smoke、远端 CI——本轮提交未推送。
- 已落地的 CI 门一句话带过：覆盖率缺失/空文件即失败（自证 15/15，含 59.9% 必须失败）、迁移 `ON_ERROR_STOP`、Anvil 进候选门禁、smoke 改为"依赖就绪 → healthy → `/health` 200"（`febdf7f`）。

**5. 收尾（2min）**："这个项目证明的是链上权限如何影响真实媒体访问——每条授权语义都有一个会失败的测试守着，每个数字都指得到 SHA。边界我不回避：平台仍是信任中心，RPC 是信任前提，NFT 持有不是版权，不是 DRM；本地七阶段 Docker 验收与 50 观众负载已实测，远端 CI 与 cold/RPC 故障跑分还没跑。下一步最有价值的是 HLS AES-128 密钥门控（条件任务），那是传输加密层，我不会把它说成 DRM。"

### 30 分钟讲述的证据映射表（代码入口 → 测试/断言 → SHA）

| 主题 | 代码入口 | 测试 / 断言（含失败形态） | SHA |
|---|---|---|---|
| challenge 重放 / nonce 128-bit | `pkg/storage/redis_challenge.go:MarkChallengeUsed` | `TestWalletAuth_ChallengeReuse_Rejected`、`TestAuthenticateWithWallet_ChallengeAlreadyUsed`、`TestGenerateWalletChallenge_NonceIs128BitAndUnique`、`_TTLWithinBound` | — |
| capability ≠ access token | `pkg/middleware`（token_type 白名单）+ `ParsePlaybackToken` audience | `TestJWTAuth_RejectsPlaybackCapabilityAsAccessToken`、`_RejectsNonHS256Algorithm`、`TestJWTAuth_IssuerEnforced`、`TestJWTAuth_AudienceEnforced` | `c9b5bcc` |
| 规则 owner 隔离 | `pkg/service/gating/rule.go:CreateRuleForOwner` 等 | `test/integration/gating/rule_owner_test.go` 7 例真实 Postgres 0 skip（B 改/删 A 的规则全拒且断言 A 数据未动） | `8fdcc18` |
| 代签三重门 | `pkg/gateway/routes.go:DemoSignerAllowed` | `TestDemoSignerAllowed`、`…_NotAllowedIsNotFound`；真实二进制双启日志对照 | `0f56ce1` |
| 共享 nonce 权威 | `pkg/web3/tx/nonce_ledger.go:Reserve` | `TestAcceptance_SharedNonceAuthority`、`TestAcceptance_NonceAcrossInstances`（各 20× 并发零重复）——由验收 `FAIL: Mint receipt status=missing` 倒逼 | `0756ea4` |
| 登记幂等 / 广播未知 / receipt reorg | `pkg/service/registry/intent.go`、`publisher.go:recoverUnknown` | `TestAcceptance_RegistryIdempotency`（20 并发 1 行、冲突 409）、`TestAcceptance_BroadcastUnknownRecovery`、`TestAcceptance_TxReceiptReorg`、`TestAnvil_RegistryDurableIntentLifecycle` 5/5 | `e859d13` |
| 授权绕过历史故事 | `pkg/gateway/authz_regression_test.go` | 7 个常驻回归，修复前 4 个红（token 直通、gate 跳链校验、分片路由在 JWT 前、fail-open + 调用方自选合约） | — |
| 链作用域 / 固定块 | `pkg/middleware/nft_gate.go:resolveAnchor/resolveQuantity`、`verifier.go:DetectTokenStandardAtBlock` | `TestBlockHashCache_IsScopedToChain`、`TestResolveQuantity_PinsTheAnchorBlock`、`…_NeverAnchorsAtLatest`、`…_WithoutProverFailsClosed`、`…_BlockChangedDuringReadIsRejected` | `e5440bd`、`73611d2` |
| 双 Anvil + 记录代理 | `test/integration/gating_anvil_test.go` | `TestAnvil_BlockEvidenceIsPinnedAndChainScoped`（每 eth_call block==锚点；错链 endpoint 只收 eth_chainId）、`TestAnvil_EndpointFailoverStaysOnTheSameChain`（仅 endpoint 级） | `796e096` |
| 证据 5s 上界 | `NFTGateConfig.MaxEvidenceAge` | `TestCachedQuantity_EvidenceOlderThanBoundIsRejected`、`_ZeroObservedAtIsRejected` | `a9f0f8a` |
| 重组失效 | `InvalidateNFTEvidence` + `ReorgDetector` | `TestAnvil_SnapshotRevertProducesARealReorg`、`TestInvalidateNFTEvidence_DropsOnlyThatChainsEvidence` | `7e04648` |
| 数量阈值 / 缓存顺序 | `evaluateGatingRules` | `TestNFTGate_MinGreaterThanBalance_Denies`、`_CacheOrderLowThenHigh_Denies`、`_ConcurrentDifferentThresholds`（20 轮 min=1 恒 200 / min=2 恒 403）、`TestNFTGate_ERC1155Quantity_OneRejected_TwoAllowed`、`TestAnvil_ERC1155_Web3Service_NFTGate` | `2055677` |
| 60s 能力 / audience / 会话 epoch | `pkg/service/auth_wallet.go`、`pkg/core/config` | `TestCapabilityPolicy_TransferBoundIsTheSumOfItsParts`、`_MaximaSumToTheBound`、`TestValidateProduction_RejectsASumOverTheBound` | `bf2f4ca`、`727394a`、`08e5584`、`09bcff7` |
| 70s 实测 / 容差修复 | acceptance + `ParseToken` | `The capability expired inside the 70s revocation bound (67s)`；3s 过期接受 / 10s 拒绝的容差用例 | `396e833`、`685a919` |
| 转移 / 登出 / 规则版本撤销 | acceptance | `Transferring the holding away denies the next manifest (403)`、`Logout revokes an ALREADY-ISSUED capability within the 5s bound`、`A capability issued BEFORE the rule change is refused within the bound` | `b93b94f`、`99d4324`、`9f867f2`、`f62ade9` |
| 依赖故障矩阵 | `scripts/dependency-fault-acceptance.sh` | ALL 16 PASSED（RPC 矩阵 + Redis 矩阵） | `767d506` |
| 真签名 / 真媒体 / 真播 / Range | `scripts/fullchain-acceptance.sh` | 双模式 ALL 66 PASSED；`Browser played the stream and time advanced`；`Range request returns 206`+`Content-Range`（修复前恒 200） | `41f68a5`、`0146f06`、`9113dff`、`b13249f` |
| 原件 digest / 409 | `pkg/service/upload/upload.go:FinalizePresignedUpload` | `TestAcceptance_OriginalMediaDigest`、`TestAcceptance_ImmutableOriginalConflict`、`TestAcceptance_PresignedOriginalSealed` | `f891091` |
| SSRF 0 请求 | `pkg/service/transcoding`（下载守卫） | 真实 listener 被访问 0 次（开关两配置都测） | `0b95244` |
| 探测门 / 上界 / 进程组 | `ValidateMediaFile`、`proc_unix.go:kill(-pgid)` | `TestAcceptance_MediaProbeLimits`（14 子例）、`TestAcceptance_WorkerAndQueueBounds`（429/503 + Retry-After） | `313d414`、`e7ad960` |
| outbox / lease / DLQ | `pkg/service/transcoding`（claim/lease）、迁移 038 | `TestAcceptance_TranscodeRedeliveryAndCrash`、`_RetryAndDLQRecovery`、`_DeliveryHandleIsolation`、`_HeartbeatRenewal` | `a8784ad` |
| generation 原子发布 | `pkg/gateway/streaming_handlers.go:PublishedGenerationResolver`、迁移 039 | `TestAcceptance_TaskGenerationPublish` 6 子例（含飞行中 generation 不可读） | `f7f0dd0` |
| 独立 worker 归因 | `cmd/microservices/transcoder/main.go` | 验收 7 项 worker 隔离检查（user/caps/env/mem + lease_owner + docker logs） | `c67b222` |
| 真实 EXTINF | `pkg/service/streamingsvc/streaming.go:RewriteMediaPlaylistURIs` | `Playlist actual timing: … summing to 10.0s` | `de4b83c` |

---

## 四、追问答案（一句话 → 关键点 → 边界）

**Q1 nonce 原子性怎么保证？**
一句话：登录 challenge 与链上交易 nonce 是两套原子性——前者 Redis Lua 单命令消费，后者 Postgres advisory lock + 唯一约束。
关键点：链上 nonce 按 `(chainID,signer)` 取 `max(DB+1, 链 pending)`、`UNIQUE(chain,signer,nonce)` 兜底；**先持久化再广播**，超时是 unknown，恢复查 receipt 或重播同一 raw 字节绝不重签；最硬的故事是三个进程内分配器在真实栈互顶交易（验收 `FAIL: Mint receipt status=missing`）才收敛成 `NonceLedger`（`0756ea4`）。
测试：`TestGenerateWalletChallenge_NonceIs128BitAndUnique`、`TestWalletAuth_ChallengeReuse_Rejected`、`TestAcceptance_NonceAcrossInstances`、`TestAcceptance_SharedNonceAuthority`、`TestAcceptance_BroadcastUnknownRecovery`。
边界：这是平台代签侧的 nonce；用户自己钱包的 nonce 不归我管。

**Q2 EIP-191 / SIWE 怎么验？**
一句话：EIP-191 是"对服务端下发的原始消息恢复地址再比对"；SIWE(EIP-4361) 把消息结构化，逐字段校验 domain/URI/chain/nonce/address。
关键点：地址比对用 `subtle.ConstantTimeCompare`；challenge 绑定地址+链+过期+已用，验签通过后**原子消费**才发 JWT；未知 signType 显式 `ErrNotSupported`（旧实现静默落到 personal_sign）；EIP-712、EIP-1271 另有回归。
测试：`TestGenerateWalletChallenge_RejectsUnknownSigningType`、`TestAuthenticateWithWallet_ChallengeAlreadyUsed`、`TestEIP1271_IsValidSignature_Valid/Invalid`、`TestWalletAuth_ExpiredChallenge_Rejected`。
边界：验证的是"这个地址签过这条消息"，不等于现实身份；nonce TTL 300s 就是登录窗口。

**Q3 ERC-721/1155 数量语义？**
一句话：唯一授权信号 `GetNFTOwnershipQuantity`——集合 `balanceOf`、指定 ERC-721 是 0/1、指定 ERC-1155 读链上 `uint256`，按 `quantity >= min_balance` 比较。
关键点：ERC-165 是标准识别唯一真源（删掉 balanceOf 探测，它会把 ERC-20 误判成 NFT）；ERC-1155 collection 规则**必须有 tokenID**，"全 collection 总数"是虚构语义；缓存存证据不存裁决、min_balance 不进缓存键，低→高门槛不交叉放行；不支持固定块的客户端返回 Unknown 而非 latest。
测试：`TestNFTGate_ERC1155Quantity_OneRejected_TwoAllowed`、`TestAnvil_ERC1155_Web3Service_NFTGate`（mint 1→min2 拒、mint 2→放行）、`TestNFTGate_CacheOrder*`、修复前红后绿的 `verifier_extended_test` 旧用例。
边界：ERC-165 自报 true 只说明接口存在，不证明合约可信——规则发布者承担所选合约的信任边界。

**Q4 为什么 safe/finalized？重组怎么办？**
一句话：`latest` 重组时会说谎；授权固定到 tag 解析出的一个 `(number,hash)`，读前后核对，重组即失效证据。
关键点：verifier block 参数必填（接口上无法回落）；ERC-165 与余额同块；证据 age ≤5s 起于原始 observedAt；重组事件只清该链；真实重组用 snapshot/revert 由生产检测器判定；**代理抓到残留 latest `eth_call`** 是排障亮点；Anvil safe 默认钉创世块（`--slots-in-an-epoch 1` 才推进）。
测试：`TestResolveQuantity_NeverAnchorsAtLatest`、`…_BlockChangedDuringReadIsRejected`、`TestAnvil_BlockEvidenceIsPinnedAndChainScoped`、`TestAnvil_SnapshotRevertProducesARealReorg`。
边界：safe 换来确认延迟（实测滞后约 32 块）；**多 RPC 回退是 endpoint 级可用性，不是轻客户端密码学证明**——provider 是信任前提。

**Q5 播放 capability 为什么可信？**
一句话：NFT 校验通过后签发的短时 bearer 能力，携带钱包、内容、用途、audience、会话、规则版本，TTL 60s。
关键点：HLS.js 无法在分片上带 `Authorization` 故走 query——但**存在从不直接授予**，中间件密码学校验；playlist 是续期点（≤5s 重查），segment/init/key 只验能力+撤销版本（不逐段查链）；与 access token 同密钥同 issuer，**靠 audience 区分**互不冒用；playlist `private, no-store`、`Referrer-Policy: no-referrer`、错误不回显 token；与 JWT 同行时钱包必须一致（401）。
测试：`TestJWTAuth_RejectsPlaybackCapabilityAsAccessToken`、验收 `A capability replayed as a Bearer token on a management API is refused (401)`、`…paired with another wallet's JWT is refused (401)`、`Manifest is Cache-Control: private, no-store`。
边界：bearer 被复制后撤销/到期前可能被别人用；钱包绑定 ≠ 持有私钥证明；已下载内容不可收回。

**Q6 70 秒撤销边界怎么来的？**
一句话：不是拍的常数——60s 能力 + 5s 正证据 + 5s 容差之和，配置校验连**总和**一起查。
关键点：起点是"健康可信 RPC 的 safe 视图**已包含转移**"（safe 没越块时 200 是正确行为）；历史缺陷容差硬编码 30s ⇒ 真实窗口 90s，修复后实测**67s**；规则/会话撤销是另一条 ≤5s 的路（ruleVersion 不匹配即拒、epoch 推高即拒），与 70s 是两个独立机制。
测试：`TestCapabilityPolicy_TransferBoundIsTheSumOfItsParts`、`TestValidateProduction_RejectsASumOverTheBound`、验收 `…expired inside the 70s revocation bound (67s)`、`Transferring the holding away denies the next manifest (403)`。
边界：70s 是**服务器授权窗口**，不保证链 70s 内确认；RPC 观测延迟单列；对已下载内容无效。

**Q7 播放器分片 / key 请求怎么走？**
一句话：链 `[PlaybackTokenAuth, JWT, NFTGate]`——playlist 重查链，分片只验 capability + 撤销版本，Range 用 206。
关键点：capability 在 query（HLS.js 限制）；`EXT-X-MAP` init 段重写到受保护路由；`EXT-X-KEY` 带 URI **拒绝**不改写（不泄露对象路径）——AES-128 密钥门控是条件任务 SG-16 **未实现**；Range 单区间 206 + `Content-Range`，不可满足 416，多区间拒绝；播放器把 401/403 判为"授权结束"（清 src/销毁 hls/清 capability）而非网络重试，浏览器实测 `401=true 403=true 500=false`。
测试：`byte_range_test`、验收 `Range request returns 206`、`Path traversal in the segment name refused`、`Playback token refused for another content`。
边界：分片是明文 HLS——token-gated 访问控制，**不是 DRM**。

**Q8 真实压测测哪些响应字节？（✅ 已实测——口径 + 数字）**
一句话：已授权 playlist、segment、init/key 的**全部响应字节**，每个响应体读完并校验大小/hash，失败不计入成功吞吐。
关键点：固定 EXTINF 节奏 + 固定 rendition + 每观众独立真实登录持 NFT（每观众一个真实钱包，节流登录 + dev-mint）；预热 10s / 采样 30s；TTFB、p50/95/99、错误分布、HTTP 状态分布；未授权放行 0 硬门。**实测**：1/10/50 观众 = 28/28、280/280、1400/1400 全成功 0 错误，字节 42070/420700/2103500，ttfb_p95 58.4/85.5/61.7ms（commit `f4d65bc`）。50 观众 ≥99% 达标（100%）。
必须提的排障故事：多钱包化之后 10 观众仍 120×429，定位到 IP 键控全局限流器在 auth 之前合桶（Prometheus `http_requests_total` 无 429 而 `rate_limit_total{redis,denied}=290`、容器日志 0 条 429 三重证据），媒体路径豁免后归零。
未做如实说：cold_cache / rpc_primary_down / rpc_all_down 三场景、reference/candidate p95 回归（需两轮数据）、CPU/RSS 未采。
边界：`test/performance/performance_test.go:TestConcurrentRequests` 是 sleep 模拟，**引用它数字的回答是错的**。

**Q9 发布 digest 与 schema 回滚？（本地已实跑，远端待运行）**
一句话：设计是"一 SHA 一候选、全程同 digest、additive 迁移可回滚"；**本地 release stage 已实跑**（空卷部署 smoke ALL 66 PASSED + 坏变体被拒 + 健康镜像恢复），GitHub 侧同 digest CD 与远端 CI 未推送待运行。
关键点：候选清单 `{candidateSHA, images:{service:digest}}`，扫描/部署/发布只认 `image@sha256:`，提升不换 digest；临时栈空卷 + 真 migration + 真播放 smoke；恢复演练必须"B 确实失败 + A 恢复"双断言，rollback 成功不能掩盖 deploy 失败；schema 全 additive 且 `.down.sql` 齐全（036–040），迁移失败会红（`ON_ERROR_STOP`，删掉了 `|| true`）。
测试：`scripts/check-coverage_test.sh` 15/15（含 59.9% 必须失败）、`make test-ci COVERAGE_MIN=99` 必须非零、smoke"就绪→healthy→`/health` 200"已在真实依赖栈实测；反向证明"部署命令失败 harness 必须非零退出"实测 `EXIT=1`。
边界：本机绿灯是预检，权威门是 CI/Docker——本轮提交未推送，M5 远端部分是"待运行"。

**Q10 用了 AI 吗？你本人做了什么？**
一句话：用了，大部分代码 AI 生成；我的工作是设计决策、验收判据、评审与排障——**AI 的代码生成不能自动转成"我独立设计实现"**。
关键点：拿"实测才暴露"的缺陷当证据——Range 被忽略、规则缓存 30s、容差 30s 致 90s 窗口、固定块残留 latest eth_call、三 nonce 分配器互顶：这些都是**判据先立住、实现被自己的判据打红**；`TRANSITION_PLAN.md` §8 逐条记录缺陷与修复 SHA；分工清单见第六节【待本人确认】。
边界：核心链路（验签→门控→投递→转码）我能讲到第三层；外围模块指出哪些没深入；欢迎现场看代码。

**Q11 哪里去中心化了？**
一句话：所有权判定在链上（任何人可自行复核），基础设施有意自托管——不把它说成完全去中心化。
关键点：这是"链上 NFT 权限 + 平台媒体服务"；RPC provider 是信任前提；规则表、存储、转码、管理是中心化服务（内容运营本来就是中心化行为）；链上 registry 是平台登记不是版权证明。
边界：不说"无需信任""生产高可用已验证""版权确权"；NFT 持有 ≠ 版权；bearer 与已下载内容的限制照实说。

---

## 五、两道现场小练习（本人操作 + 讲解，Docker 验法）

> 由**本人**现场定位、修改、验证、讲解；AI 不生成录屏、不代答。变量沿用第二节。

### 题一：`min_balance` 改为 2（阈值越过持有量，持有人余额 = 1）

```bash
# 0) 定位比较逻辑（讲解点：证据与裁决分离）
grep -n "MinBalance\|min" pkg/middleware/nft_gate.go            # evaluateGatingRules: quantity >= min
# 1) 基线 200
curl -s -o /dev/null -w '%{http_code}\n' -H "Authorization: Bearer $TOKEN_A" \
  $BASE/api/v1/streaming/$CONTENT_ID/manifest.m3u8              # → 200
# 2) 走 owner API 改（必须走 API：版本 bump 与规则写入是同一条语句/CTE）
curl -s -X PUT $BASE/api/v1/gating-rules/$RULE_ID \
  -H "Authorization: Bearer $TOKEN_A" -H 'Content-Type: application/json' -d '{"min_balance":2}'   # → 200
# 3) DB 核对：min_balance=2 且 content_rule_versions.version +1
docker exec sg-fc-postgres psql -U postgres -d streamgate -c \
  "SELECT min_balance FROM content_gating_rules WHERE id='$RULE_ID'; SELECT * FROM content_rule_versions;"
# 4) ≤5s 界内轮询复验 → 403（ruleVersion 变化强制缓存重读）
until [ "$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $TOKEN_A" \
      $BASE/api/v1/streaming/$CONTENT_ID/manifest.m3u8)" = 403 ]; do sleep 1; done
# 5) 改回 1 → 恢复 200
```

**判分**：单测 `TestNFTGate_MinGreaterThanBalance_Denies`（改后拒）/ `TestNFTGate_MinEqualsBalance_Allows`（改回放行）/ `TestNFTGate_CacheOrderLowThenHigh_Denies`（低→高不继承放行）；验收断言 `Raising the rule threshold denies the holder on re-request (403)`、`Holder is served again once the threshold is restored`；DB 里 `min_balance=2` 且版本 +1。
**必须讲的三点**：① 为什么走 API 不走裸 SQL——版本与规则同一条语句提交，裸 SQL 不 bump 版本，gate 读到版本没变就复用缓存，只能等本地 TTL 兜底（权威在服务端写路径）；② 缓存键故意不含 min_balance——存的是余额证据不是裁决，阈值改动对同一份证据立即生效（`TestNFTGate_WarmAndColdAgree` 断言缓存里是真实余额）；③ 归因纪律——必须得到 403，**503 是"依赖不可核验"不能算阈值生效**（我在验收里踩过：规则指向会 revert 的合约时，无关的 503 曾让断言假 PASS，现在 503 对该断言是显式 FAIL）。

### 题二：转移 NFT 后"续期失败"（重取 manifest 必须被拒）

```bash
# 0) 基线 200；token id 从链上枚举持有人实际持有的（不假设 id 范围）
curl -s -o /dev/null -w '%{http_code}\n' -H "Authorization: Bearer $TOKEN_A" \
  $BASE/api/v1/streaming/$CONTENT_ID/manifest.m3u8              # → 200
# 1) 转走并确认余额归零
cast send $DEMO_NFT "transferFrom(address,address,uint256)" $WALLET_A $WALLET_B $TOKEN_ID \
  --rpc-url $ANVIL --private-key $KEY_A
cast call $DEMO_NFT "balanceOf(address)(uint256)" $WALLET_A --rpc-url $ANVIL     # → 0
# 2) 推进 safe 并确认越过转移块（Anvil 实测滞后约 32 块；无流量 safe 不前进）
cast rpc anvil_mine 48 --rpc-url $ANVIL
cast rpc eth_getBlockByNumber safe false | python3 -c 'import sys,json;print(int(json.load(sys.stdin)["number"],16))'
# 3) 续期（重取 manifest 即续期点，≤5s 证据重查）→ 403
curl -s -o /dev/null -w '%{http_code}\n' -H "Authorization: Bearer $TOKEN_A" \
  $BASE/api/v1/streaming/$CONTENT_ID/manifest.m3u8              # → 403
# 4) 转回必须用 B（当前持有人）私钥；用 A 的私钥会 revert；再挖块
cast send $DEMO_NFT "transferFrom(address,address,uint256)" $WALLET_B $WALLET_A $TOKEN_ID \
  --rpc-url $ANVIL --private-key $KEY_B
cast rpc anvil_mine 48 --rpc-url $ANVIL                          # 复验 → 200
```

**判分**：验收断言 `Transferred the holder's N token(s) away (balance now 0)`、`The safe view now includes the transfer`、`Transferring the holding away denies the next manifest (403)`、`Transferring the holding back restores access (200)`；单测 `TestCachedQuantity_ReorgStillDetectedWithinChain`、`TestAnvil_SnapshotRevertProducesARealReorg`；"旧能力还能播多久"答 Q6（推导 70s / 实测 67s）。
**必须讲的三点**：① 顺序——先断言余额归零、再断言 safe 越块、最后判 403，顺序错了会把一次失败的转移误读成"gate 没生效"（safe 没越块时 200 是正确行为）；② 转移后续期失败 ≠ 每个分片查链——manifest/playlist 是续期点，segment 只验能力与撤销版本；③ 转回必须用当前持有人私钥签名，否则 revert 静默失败，会把"非持有人被拒"变成假 PASS。

---

## 六、个人贡献与 AI 归属映射（M6 人工确认清单）

诚实原则（计划原文）：**AI 的代码生成不能自动转成"我独立设计实现"**。投递前由本人逐条确认。

| 主张 | 性质 | 证据 / 说明 | 状态 |
|---|---|---|---|
| 架构与验收判据设计（monolith-first、fail-closed、70s 推导、66 断言口径、四类 profile） | 个人决策 | `TRANSITION_PLAN.md` §1 / §1.1 / §6 | 【待本人确认】 |
| 缺陷定级与修复取舍（§2 P0/P1 表、每次红→绿的判据） | 个人评审 | `TRANSITION_PLAN.md` §2、§8 | 【待本人确认】 |
| 故障定位与修复决策（Range、规则缓存 30s、90s 窗口、latest 泄漏、nonce 三分配器冲突） | 个人排障 | 修复 SHA 见第三节映射表 | 【待本人确认】 |
| 两道现场小练习的定位与讲解 | 本人现场完成 | 第五节 | 【待本人确认】（需实际练习过） |
| 代码实现 | **AI 辅助生成** | 仓库提交记录 | 如实说明，不 claim 独立实现 |
| 实测结果（ALL 66 PASSED、67s、SSRF 0 请求、worker 归因） | 客观可复跑 | `scripts/fullchain-acceptance.sh`、`dependency-fault-acceptance.sh`、`TRANSITION_PLAN.md` §8 | ✅（远端 CI ⏳ 待运行） |
| 录制演示、回答追问、投递与面试 | 本人进行 | — | AI 不代录屏、不代答、不代发消息 |
| ChainPulse 条目 | 独立项目 | 证据另行整理 | 不引用 StreamGate 验收为其背书 |

---

## 七、信任边界话术（被问到就照说）

1. "这是**链上 NFT 权限 + 平台媒体服务**：平台控制规则、播放能力、存储、转码与管理；RPC provider 提供链状态——**多 RPC 回退不等于轻客户端的密码学证明**。"
2. "NFT 持有是观看资格，**不等于媒体版权**；链上 registry 是平台登记（attestation），不是版权确权。"
3. "不说完全去中心化、不说无需信任、不说生产高可用已验证——这三样我都没有证据。"
4. "capability 是 bearer：被复制后撤销/到期前可能被别人用；钱包绑定不等于持有私钥的证明；跨钱包负例覆盖 JWT 主体冲突与签名篡改，不是防分享。"
5. "已下载的媒体、播放器缓冲、已拿到的密钥**不可远程收回**——所以它是 token-gated 访问控制，**不是 DRM**，我也不这么包装它。"
6. "远端 CI **待运行**（未推送）——没跑完的我不写成数字；本地七阶段与 50 观众负载已实测，数字都指得到 commit。"
