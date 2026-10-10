# 简历(中文)

> 产出纪律(TRANSITION_PLAN C0/C2):只写能现场证明的;不写 150K 行、覆盖率数字、"微内核架构"、"10 个微服务"、版本号。
> `[...]` 为占位符,投递前必须填真实信息。

---

## 个人信息

- 姓名:[你的姓名]
- 手机:[你的手机号] · 邮箱:[你的邮箱]
- 城市:[你的城市] · GitHub:[github.com/你的用户名](https://github.com/你的用户名)(StreamGate 仓库置顶)

## 个人简介

6 年音视频后端开发经验,做过原生播放器与流媒体分发系统,熟悉 HLS、转码、访问控制与高并发后端。当前专注把 Web3 能力落到真实业务:以 Go 实现了钱包验签登录、链上 NFT 门控与受保护 HLS 投递的完整后端(StreamGate),并主导过一次完整的授权链路安全审计与修复。

## 核心技能

- **音视频**:HLS/ABR 转码流水线(FFmpeg)、manifest/segment 投递、播放器内核与原生播放器开发经验
- **后端**:Go、HTTP/gRPC、任务队列(NATS JetStream + DLQ)、重试/超时/背压设计
- **Web3**:EIP-191 / EIP-712 / SIWE 验签、EIP-2 low-s、EIP-1271 合约签名校验、NFT 持有权校验(ERC-721/1155、ERC-165 探测)、链重组处理(`safe` block tag + 缓存失效)
- **安全工程**:challenge-response 防重放(Redis Lua 原子消费)、fail-closed 授权设计、TDD 安全回归

## 项目经历

### StreamGate — NFT 门控流媒体后端(Go,个人项目)

钱包签名登录 + 链上 NFT 持有权校验 + 受保护 HLS 投递的完整后端;自己发现并修复了一条完整授权绕过链。

- **钱包身份与防重放**:实现 EIP-191 / EIP-712 / SIWE 三种验签路径,签名校验用常量时间比较并强制 EIP-2 low-s;challenge 一次性消费用 Redis Lua 脚本原子完成,杜绝 TOCTOU 重放。
- **NFT 门控授权**:manifest 访问时校验 NFT 持有权(按规则支持 ownerOf/balanceOf 与 ERC-165 标准自动探测);读链固定用 `safe` block tag,检测到重组自动失效缓存;正结果缓存 TTL 上限 15 秒,在防篡改与 RPC 压力之间取平衡。
- **受保护投递**:授权在 manifest 层完成,segment 用内容绑定、短 TTL 的 playback token 投递,链上 RPC 不进播放热路径;令牌绑定钱包与指纹,跨用户复用会被拒绝。
- **ABR 转码流水线**:FFmpeg 4 档分辨率阶梯 + HLS 切片,stderr 实时进度解析;转码任务经 NATS JetStream 分发,带重试、超时与卡死 worker 回收,失败任务进 DLQ。
- **安全审计(项目亮点)**:通过自写回归测试发现一条完整的未授权访问链(分片路由无鉴权、playback token 未校验即放行、门控 fail-open、调用方可自选验证合约等 6 处缺陷),按 TDD 流程逐项修复并全部固化为常驻 CI 回归测试;门控语义改为 fail-closed。
- **工程化**:golangci-lint + CI 门禁(覆盖率下限、govulncheck、镜像漏洞扫描)、docker compose 一键全链路、Playwright 端到端验收探针(含"未认证请求必须 401"的安全探针)。

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

### 投递前自检(来自 TRANSITION_PLAN,不要删)

1. 所有 `[...]` 占位符已替换为真实信息;工作经历里的每一条都能展开讲 10 分钟。
2. "6 年"口径已与真实年数核对。
3. StreamGate 每一条 bullet 都能在仓库里指到代码与测试:`pkg/web3/signature/`、`pkg/web3/nft/verifier.go`、`pkg/middleware/nft_gate.go`、`pkg/service/transcoding/`、`pkg/gateway/authz_regression_test.go`。
4. 不承诺 DRM / 完全去中心化 / 生产级高可用——被问到就用讲述稿里的边界话术(见 `PROJECT_TALK_SCRIPT.md`)。
