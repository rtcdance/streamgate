# 简历(中文)

> 产出纪律(TRANSITION_PLAN SG-09):只写能现场证明的;每个数字都带提交 SHA / 测试出处(§8 证据);不写行数、覆盖率数字、"微内核架构"、微服务数量、版本号。
> 【待本人确认】= 需本人核实后才能投递;`待补` / `待实测` = 明确未完成项,面试中不得说成已完成。
> `[...]` 为占位符,投递前必须填真实信息。

---

## 个人信息

- 姓名:[你的姓名]
- 手机:[你的手机号] · 邮箱:[你的邮箱]
- 城市:[你的城市] · GitHub:[github.com/你的用户名](https://github.com/你的用户名)(StreamGate 仓库置顶)

## 个人简介

10+ 年 C++,6+ 年音视频流媒体(iOS / Android 原生播放器与流媒体分发),现专注 Go 与 Web3 后端。StreamGate 可复现闭环:钱包签名登录 → 服务端 NFT 规则 → 同区块持仓证据 → 60 秒播放能力 → 受保护 HLS 真实解码 → NFT 转移后 ≤70 秒撤销 → 故障注入与恢复。项目 AI 辅助实现;需求冻结、设计取舍、评审与失败用例定位由本人负责【待本人确认】。

## 核心技能

- **音视频**:FFmpeg ABR / HLS、真实 EXTINF playlist、Range/206、浏览器 hls.js 实播断言;iOS / Android 原生播放器内核
- **后端**:Go、PostgreSQL(单语句事务 / advisory lock / outbox)、Redis(Lua 原子消费、有界证据缓存、会话 epoch)、NATS JetStream(重投递 / lease / 显式 DLQ)、MinIO / S3
- **Web3**:EIP-191 / EIP-712 / SIWE、EIP-2 low-s、EIP-1271、ERC-721/1155 数量语义与 ERC-165 识别、safe/finalized 同区块证据、重组失效、链写幂等与 durable nonce
- **安全工程**:fail-closed 授权、撤销上界推导(60+5+5=70s)、A/B 双钱包负例验收、TDD 安全回归

## 项目经历

### StreamGate — NFT 门控流媒体后端(Go,个人项目,AI 辅助开发)

主线:真实钱包签名 → 一次性挑战 → 服务端 NFT 规则 → 同区块持仓证据 → 60 秒播放能力 → 真实 HLS 播放 → 转移后权限失效 → RPC / Redis 故障与恢复。**AI 边界**:实现代码大部分由 AI 生成;本人拍板设计与验收标准、评审改动、定位修复失败用例——AI 的代码生成不等于"我独立设计实现"【待本人确认】。

证据状态:已完成并有实测——SG-01~06、SG-10~12(架构 / 合约 / 媒体九步)、SG-13 A+B、SG-14(运维,备份恢复演练 17/17)、SG-07(性能,1/10/50/100 观众 × 3 重复各 100%、50 观众 1400/1400、100 观众容量档 2800/2800;故障矩阵 cold_cache/rpc_primary_down/rpc_all_down 实跑,全端点停时 0 放行 0 字节;同 runner 固定 reference 的 p95 回归门实测 ratio=1.029≤1.20,权威集合见计划 §8.16 H)。**待补**:远端 CI(未推送,待运行)、SG-08 的 GitHub 侧同 digest CD(本地 release stage 已实跑空卷部署 + 失败/恢复双断言)。

**条目一:签名与资源授权**

- **问题**:初版授权链有系统性绕过面——nonce 并发可重复分配、gating-rule 写入不校验内容归属、JWT 用户可指定服务器代签任意交易、播放凭据可当管理 API 的 access token。
- **个人决策**【待本人确认】:授权主体一律来自服务端(规则表 + 已认证会话);代签默认关闭且三重门(dev + 链 31337 + 显式开关,未启用 404);交易 nonce 以 Postgres advisory lock + `UNIQUE(chain,signer,nonce)` 为唯一权威;challenge 一次性消费用 Redis Lua 单命令原子完成。
- **改动入口**:`pkg/storage/redis_challenge.go`、`pkg/service/gating/rule.go`(`CreateRuleForOwner` 单语句 owner 条件写)、`pkg/gateway/routes.go`(`DemoSignerAllowed`)、`pkg/middleware/`(token_type 白名单、iss/aud)、`pkg/web3/tx/nonce_ledger.go`。
- **测量条件**:真实 Postgres 的 A/B 双钱包越权负例(7 例 0 skip);两实例同库 20×20 并发分配;fullchain 双部署模式验收。
- **结果**:B 对 A 的规则写入 0 行(404/403 稳定区分);未开启时代签路由 404;`TestAcceptance_SharedNonceAuthority` 零重复——修复由真实验收 FAIL(mint receipt 缺失)驱动(`0756ea4`);`TestAcceptance_RegistryIdempotency` 20 并发仅 1 行、同 key 异 payload 409(`e859d13`)。

**条目二:链状态、缓存与撤销**

- **问题**:`latest` 在重组时会说谎;缓存会跨链、跨阈值串证据;转移后已发的播放凭据仍长期有效。
- **个人决策**【待本人确认】:持仓读取固定到单一同区块(number+hash)锚点,ERC-165 探测与余额同块;正证据总 age ≤5s(从原始 observedAt 起算);缓存只存余额证据、每请求按规则重比;撤销上界 = 能力 60s + 证据 5s + 时钟容差 5s = 70s,生产配置校验总和强制;规则版本(Postgres)与会话 epoch(Redis)在每条媒体请求收口。
- **改动入口**:`pkg/middleware/nft_gate.go`(`resolveAnchor` / `InvalidateNFTEvidence`)、`pkg/web3/nft/verifier.go`、`pkg/core/config/config.go`(`MaxTransferRevocationBound`)、迁移 036、`storage.SessionStore`。
- **测量条件**:两个真 Anvil + 记录型 RPC 代理(断言每个 `eth_call` 的 block 参数 == 锚点);snapshot/revert 真重组;转移→拒绝→转回→恢复;RPC / Redis 依赖故障入口。
- **结果**:能力实测 67s 过期 ≤70s 上界;5s 证据 age;转移后 manifest 403、转回 200;logout 5s 内撤销已发能力;RPC 全故障时新 manifest 503、已发能力按自身有效期工作且到期不延长;重组失效只清本链证据(`796e096`、`b93b94f` / `396e833`、`99d4324`、依赖故障 16/16 `767d506`)。

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
4. 待补项(GitHub 侧同 digest CD、远端 CI、冷缓存 / RPC 故障跑分)按"未完成"口径讲,不引用预测数字。
5. 不承诺 DRM / 完全去中心化 / 生产级高可用——边界话术见 `PROJECT_TALK_SCRIPT.md`。
