# 简历(中文)

> **当前交付状态（2026-10-30）：本机候选 PASS WITH BOUNDARIES，同 SHA CI attempt 10 已失败，修复待验。** 代码候选 `4eae43d6abe94f21c7730c1dc27fb2b12b9f095a` 的完整 Docker `all` / M0–M8 已通过，原始 `command.exit=0`；工件 `sg-final3-4eae43d6` 于本轮再次独立校验。CI run `40608718226` 的 attempt 10 已失败；后续修复须以新候选完整验收和 CI 验证。下方标注旧 SHA 的测试和性能数字仍只属于其历史运行；生产部署、本人讲述/录屏/投递未完成。本材料修订不继承代码候选的 CI 状态。

> 产出纪律(TRANSITION_PLAN SG-09):只写能现场证明的;每个数字都带提交 SHA / 测试出处(§8 证据);不写行数、覆盖率数字、"微内核架构"、微服务数量、版本号。
> 【待本人确认】= 需本人核实后才能投递;`待补` / `待实测` = 明确未完成项,面试中不得说成已完成。
> `[...]` 为占位符,投递前必须填真实信息。

---

## 个人信息

- 姓名:[你的姓名]
- 手机:[你的手机号] · 邮箱:[你的邮箱]
- 城市:[你的城市] · GitHub:[github.com/你的用户名](https://github.com/你的用户名)(StreamGate 仓库置顶)

## 个人简介

10+ 年 C++,6+ 年音视频流媒体(iOS / Android 原生播放器与流媒体分发),现专注 Go 与 Web3 后端。StreamGate 可复现闭环:钱包签名登录 → 服务端 NFT 规则 → 同区块持仓证据 → 60 秒播放能力 → 受保护 HLS 真实解码 → safe 视图确认 NFT 转移后 ≤70 秒拒绝 → 故障注入与恢复。项目 AI 辅助实现;需求冻结、设计取舍、评审与失败用例定位由本人负责【待本人确认】。

## 核心技能

- **音视频**:FFmpeg ABR / HLS、真实 EXTINF playlist、Range/206、浏览器 hls.js 实播断言;iOS / Android 原生播放器内核
- **后端**:Go、PostgreSQL(单语句事务 / advisory lock / outbox)、Redis(Lua 原子消费、有界证据缓存、会话 epoch)、NATS JetStream(重投递 / lease / 显式 DLQ)、MinIO / S3
- **Web3**:EIP-191 / EIP-712 / SIWE、EIP-2 low-s、EIP-1271、ERC-721/1155 数量语义与 ERC-165 识别、safe/finalized 同区块证据、重组失效、链写幂等与 durable nonce
- **安全工程**: fail-closed 授权、撤销上界推导(60+5+5=70s)、**HTTP/gRPC 双入口共用一套授权器与规则快照**(同 token_type 子句 / 同数量阈值 / 同区块锚)、A/B 双钱包负例验收、TDD 安全回归

## 项目经历

### StreamGate — NFT 门控流媒体后端(Go,个人项目,AI 辅助开发)

主线:真实钱包签名 → 一次性挑战 → 服务端 NFT 规则 → 同区块持仓证据 → 60 秒播放能力 → 真实 HLS 播放 → 转移后权限失效 → RPC / Redis 故障与恢复。**AI 边界**:实现代码大部分由 AI 生成;本人拍板设计与验收标准、评审改动、定位修复失败用例——AI 的代码生成不等于"我独立设计实现"【待本人确认】。

当前状态：候选 `4eae43d6` 的完整本机 Docker 门已通过，同 SHA CI 与最终交付审核仍待完成。当前媒体门两条部署腿各 77 项通过；HTTP/gRPC 的转移拒绝分别实测 62.989s/63.208s 与 60.949s/61.149s，起点为 safe 视图确认转移，不包含链最终性等待。证据：`sg-final3-4eae43d6/logs/media.log`；其余历史结果按各自 SHA 解读。**未做**：生产 rollout、本人讲述、录屏和投递。

**条目一:签名与资源授权**

- **问题**:初版授权链有系统性绕过面——nonce 并发可重复分配、gating-rule 写入不校验内容归属、JWT 用户可指定服务器代签任意交易、播放凭据可当管理 API 的 access token。
- **个人决策**【待本人确认】:授权主体一律来自服务端(规则表 + 已认证会话);代签默认关闭且三重门(dev + 链 31337 + 显式开关,未启用 404);交易 nonce 以 Postgres advisory lock + `UNIQUE(chain,signer,nonce)` 为唯一权威;challenge 一次性消费用 Redis Lua 单命令原子完成。
- **改动入口**:`pkg/storage/redis_challenge.go`、`pkg/service/gating/rule.go`(`CreateRuleForOwner` 单语句 owner 条件写)、`pkg/gateway/routes.go`(`DemoSignerAllowed`)、`pkg/middleware/`(token_type 白名单、iss/aud)、`pkg/web3/tx/nonce_ledger.go`。
- **测量条件**:真实 Postgres 的 A/B 双钱包越权负例(7 例 0 skip);两实例同库 20×20 并发分配;fullchain 双部署模式验收。
- **结果**:B 对 A 的规则写入 0 行(404/403 稳定区分);未开启时代签路由 404;`TestAcceptance_SharedNonceAuthority` 零重复——修复由真实验收 FAIL(mint receipt 缺失)驱动(`0756ea4`);`TestAcceptance_RegistryIdempotency` 20 并发仅 1 行、同 key 异 payload 409(`e859d13`)。

**条目一续:授权入口不做两套(HTTP / gRPC)**

- **问题**:gRPC `GetManifest` 用布尔持仓接口代替**数量阈值**判断,并把能力 TTL 硬编码成 2 分钟;HTTP 与 gRPC 各有一套授权逻辑,阈值、TTL、凭据规则都可能单边漂移。
- **个人决策**【待本人确认】:两个入口共用**同一授权器与同一规则快照**——一次读取得规则 + 规则版本 + 权威读取时间,能力只能由该带封印的授权结果签发;gRPC 按**真实链上数量**执行 ERC-721/1155、token_id、min_balance 与 OR 规则,能力 TTL 用共享 60 秒策略(删掉硬编码的 2 分钟);同一请求同时带 access 与 capability 时钱包、会话、epoch 必须一致。
- **改动入口**:`pkg/service/`(`ResolvePolicySnapshot` / `AuthorizePlayback`)、`pkg/gateway/grpc_server.go`、`test/grpcparity`、`scripts/fullchain-acceptance.sh` 第 9b 节。
- **测量条件**:同一候选镜像的**两条部署腿**(monolith 与独立 `api-gateway`)各跑一遍 gRPC 平价节。
- **结果**:真实持有者经 gRPC 取到 manifest,其中能力 `exp` 落在 60±5 秒,并以该段 URL **不带 `Authorization` 头**取到与受保护 HTTP 媒体**逐字节一致**的真实媒体;非持有者、真实数量不足(持有 1 要求 2 / 持有 2 要求 3)与双凭据冲突分别被 `PermissionDenied` 拒绝;两条腿各 71/71 PASS(`d0a9451`、`6178f11`)。该节还抓到一个真实缺陷:独立 `api-gateway` 入口漏把授权器交给 gRPC 服务,该模式下 gRPC 一律 `Internal` 失败关闭而 HTTP 正常(`d2f1d81`,此前只跑 monolith 看不出来)。

**条目二:链状态、缓存与撤销**

- **问题**:`latest` 在重组时会说谎;缓存会跨链、跨阈值串证据;转移后已发的播放凭据仍长期有效。
- **个人决策**【待本人确认】:持仓读取固定到单一同区块(number+hash)锚点,ERC-165 探测与余额同块;正证据总 age ≤5s(从原始 observedAt 起算);缓存只存余额证据、每请求按规则重比;撤销上界 = 能力 60s + 证据 5s + 时钟容差 5s = 70s,生产配置校验总和强制;规则版本(Postgres)与会话 epoch(Redis)在每条媒体请求收口。
- **改动入口**:`pkg/middleware/nft_gate.go`(`resolveAnchor` / `InvalidateNFTEvidence`)、`pkg/web3/nft/verifier.go`、`pkg/core/config/config.go`(`MaxTransferRevocationBound`)、迁移 036、`storage.SessionStore`。
- **测量条件**:两个真 Anvil + 记录型 RPC 代理(断言每个 `eth_call` 的 block 参数 == 锚点);snapshot/revert 真重组;转移→拒绝→转回→恢复;RPC / Redis 依赖故障入口。
- **结果**:历史能力自然过期观测为 67s，不能用作转移到拒绝的计时证据;5s 证据 age;转移后 manifest 403、转回 200;logout 5s 内撤销已发能力;RPC 全故障时新 manifest 503、已发能力按自身有效期工作且到期不延长;重组失效只清本链证据(`796e096`、`b93b94f` / `396e833`、`99d4324`、依赖故障 16/16 `767d506`)。

**条目三:真实媒体交付与实测排障**

- **问题**:旧验收自签 JWT、用 1KB 全零字节冒充视频;手工 playlist 固定每段 6 秒;下载超 2 GiB 被静默截断后仍报成功;取消转码留下孤儿 FFmpeg;任务状态只在进程 map。
- **个人决策**【待本人确认】:验收必须真签名 / 真媒体 / 真解码(FFmpeg 全解码 + 浏览器实播);原件服务端 SHA-256 封存到不可覆盖 key,重复封存不同字节 409;ffprobe 白名单门 + `maxBytes+1` 拒绝超限;子进程按进程组终止;Postgres 为任务唯一权威(lease / outbox / 显式 DLQ);产物按 generation 私有前缀、验证后原子发布;不可信媒体解析放进独立 worker 容器(非 root、drop caps、无 JWT / 链私钥)。
- **改动入口**:`scripts/fullchain-acceptance.sh`、`pkg/service/upload`(封存)、`pkg/plugins/transcoder/ffmpeg.go`(`ValidateMediaFile`)、`pkg/service/transcoding`(lease / outbox / DLQ)、`published_generation`、`cmd/microservices/transcoder/main.go`。
- **测量条件**:monolith 与 api-gateway 跑同一验收入口;真实 fixture(10s / H.264+AAC / 640×360);浏览器断言 `currentTime` ≥3s;DB `lease_owner` + worker 日志做执行归因。
- **结果**:双模式 **ALL 66 PASSED**(59 + 7);EXTINF 合计 10.0s(`de4b83c`);封存对象 sha256 与 DB digest 一致(`f891091`);探测门 14 子例(`313d414`);lease / DLQ 用例(`a8784ad`);generation 发布 6 子例(`f7f0dd0`);隔离 worker 归因(`c67b222`)。

**ChainPulse(独立项目,证据另行整理)**:与 StreamGate 分别写证明任务,不重复罗列 Go / Redis / 微服务栈;未复核数字不写入本简历【待本人确认:是否保留】。

**信任边界(面试必守)**:这是"链上 NFT 权限 + 平台媒体服务";多 RPC 回退不等于轻客户端证明;NFT 持有不等于媒体版权;平台登记只是平台声明;不说完全去中心化 / 无需信任 / 生产高可用已验证 / 版权确权 / DRM;已下载媒体无法远程收回。

## 工作经历

### [公司 A] — [职位]([起止时间])

- [负责音视频后端 / 流媒体系统:替换为真实业务与规模,例如"支撑日活 X 万的播放链路"]
- [访问控制、任务调度、转码链路或高并发优化的真实成果]
- [与技术栈]

### [公司 B] — [职位]([起止时间])

- [真实经历]

## 教育经历

- [学校] · [专业] · [学历] · [起止年份]

---

### 投递前自检(来自 TRANSITION_PLAN SG-09,不要删)

1. 所有 `[...]` 占位符已替换;工作经历里每一条能展开讲 10 分钟。
2. 所有【待本人确认】已逐条确认;三个"个人决策"确由本人拍板,否则改写为"参与评审"。
3. 每个数字与测试名能指到证据:提交 `0756ea4 / e859d13 / f891091 / 313d414 / a8784ad / f7f0dd0 / c67b222 / de4b83c` 与 TRANSITION_PLAN.md §8。
4. 以 `TRANSITION_PLAN.md` 顶部状态标记和 §8 最新证据为准；旧候选 `ab172c2` / run `39005594931` 及旧性能数据只作历史记录，不能称为当前完成。`4eae43d6` 已有完整本机验收与性能证据，后续修复候选的完整验收及同 SHA CI 尚待取得；生产 rollout 未执行，本人讲述/录屏/投递待本人确认。
5. 不承诺 DRM / 完全去中心化 / 生产级高可用——边界话术见 `PROJECT_TALK_SCRIPT.md`。
