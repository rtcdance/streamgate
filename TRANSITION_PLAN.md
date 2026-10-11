# StreamGate 优化与 Web3 求职推进 — AI 可执行任务书

> **For agentic workers:** 本文件是**任务书**，不是分析报告。按任务逐条执行，**每条任务单独提交一次 commit**，执行前先跑验证命令，执行后必须让验收判据变绿。  
> 用 `- [ ]` 复选框跟踪进度。**不要批量执行多个任务后再一起验证。**

**Goal:** 修复 StreamGate 的授权缺陷、清理死代码与失实文档，产出可被验证的求职物料，并按市场反馈决定后续方向。

**Architecture:** 分四个 Track。Track A/B 是项目侧（安全 + 诚实），Track C 是求职侧，Track D 是条件触发（只有在市场探针确认方向后才做）。**Track A 必须先于一切。**

**Tech Stack:** Go **1.26** / Gin / testify / Foundry / hls.js。**不新增** Go 依赖（见 `DEPENDENCY_APPROVAL.md`）。

> **基线声明（2026-10-10 更新）**：本计划制定于「与 chainpulse 版本对齐」完成**之后**。执行前请确认以下已完成项，**不要重复做**：
>
> - Go 版本 **1.26**（**2026-10-11 修订**：由 1.25 上调。原因：Docker Trivy 门禁报出 6 个 HIGH，其中 `stdlib` 3 项与 `golang.org/x/net` 1 项的修复版本均要求 Go ≥ 1.26 —— Go 1.25 线不再出补丁。已获人工确认，全量 pin 同步：`Dockerfile:11`、`docker-bake.hcl:2`、`.github/workflows/{ci,test,auto-fix}.yml`、`deploy/docker/Dockerfile.*`(10)、`README.md` badge、`CLAUDE.md`。`go.mod` 另加 `toolchain go1.26.9` 作为 CVE 修复下限，`golang.org/x/net` → v0.60.0）
> - 依赖已部分对齐 chainpulse：`bitset v1.24.4`、`gnark-crypto v0.19.2`、`gorilla/websocket v1.5.3`、`mongo-driver v1.14.0`（共享模块 45/59 一致）
> - `pkg/gateway/authz_regression_test.go` **已存在**（A1 已完成，见下）
> - h5-demo 可用 `cd h5-demo && python3 -m http.server 18000` 提供，验收探针 5/5 通过
> - CI 本地实测：`go vet ./...`、`go test`（87 包，覆盖率 74.4%）、`make build-all`、`golangci-lint` 均通过

---

## 使用说明（给执行 AI）

1. **顺序不可乱**：P0 → Track A → Track B → Track C。Track D 需人工确认后才可开始。
2. **每条任务的「验证命令」必须真实执行并贴出输出**，不得凭推断宣称通过。
3. **「验收判据」必须能失败**。若某判据无论怎么做都会通过，报告它失效，不要继续。
4. **遇到与本文档描述不符的代码**：停止，报告差异，不要"顺手改对"。
5. **`grep` 一律用 `grep -nE`**。macOS 的 BSD `grep` 不支持 `\|` 交替式，会**静默返回空**，导致假阴性。
6. 禁止在未运行测试的情况下标记任务完成。

---

## 0. 验收门

### 0.1 产物门（每道都必须能失败）

|       门       | 判据                                                                 | 验证方式                                                                    |
| :-----------: | ------------------------------------------------------------------ | ----------------------------------------------------------------------- |
| **G1** 授权不可绕过 | **`AUTHZ_RED=1` 下**，A1 的 **4 个 RED** 测试必须全绿，且**在 Track A 修复前必须失败** | ⚠️ **不能直接跑 `go test`**——RED 测试默认 `t.Skip`（为保 CI 绿）。见下方「G1 正确验法」         |
|  **G2** 无死代码  | 附录 B 的删除清单全部执行完，非测试源码行数下降并记录                                       | `find . -name '*.go' -not -name '*_test.go' \| xargs cat \| wc -l` 前后对比 |
|  **G3** 文档无矛盾 | 附录 A.6 的 8 处矛盾全部修正；CLAUDE.md 不再引用不存在的文件                            | 逐条核对                                                                    |
|  **G4** 数字可复现 | README 中每个数字都有对应命令                                                 | 逐条执行                                                                    |
|  **G5** 简历已产出 | 3 个真人读过并回答"你会面这个人吗"                                                | 见 C2                                                                    |

#### ⚠️ G1 正确验法（RED 测试默认被跳过，直接用 `go test` 会得到假绿）

`authz_regression_test.go` 里的 4 个 RED 测试带 `requireRED(t)` 门，默认 `t.Skip`——**这是为了让 CI 保持绿**。  
所以必须显式打开：

```bash
# ① 修复前（Track A 尚未动手）——期望：4 FAIL
AUTHZ_RED=1 go test ./pkg/gateway/ -run 'TestManifest|TestSegment' -count=1

# ② Track A 完成后——期望：7 PASS、0 FAIL、0 SKIP
AUTHZ_RED=1 go test ./pkg/gateway/ -run 'TestManifest|TestSegment' -count=1

# ③ 反向证明（可选，最强证据）：在 Track A 之前的提交上跑测试，必须重新变红。
# ⚠️ 不要用 git stash——按「每任务一提交」纪律，A10 时工作区是干净的，stash 是空操作、测试会假绿。
git worktree add /tmp/sg-prefix <A2 之前的提交 SHA>
cd /tmp/sg-prefix && AUTHZ_RED=1 go test ./pkg/gateway/ -run 'TestManifest|TestSegment' -count=1
cd - >/dev/null && git worktree remove --force /tmp/sg-prefix
```

**G1 通过条件**：① 恰好 4 FAIL ② 0 FAIL。任一不满足则 G1 不通过。

> **已知的 7 个测试构成**（实测）：**4 RED**（`TestManifest_BogusPlaybackToken_Returns401`、`TestManifest_BogusPlaybackToken_NoQuality_Returns401`、`TestManifest_NoGatingRules_DeniesByDefault`、`TestManifest_CallerSuppliedContract_Ignored`）+ **3 GUARD**（`TestManifest_NoAuth_Returns401`、`TestSegment_NoToken_Returns401`、`TestSegment_BogusPlaybackToken_Returns401`）。  
> 修复完成后，**删除 `requireRED()` 调用**，让它们成为常驻回归测试。

### 0.2 求职门（v4 新增 —— 之前只有产物门，这是最大的结构性缺陷）

|    门   | 判据                            |
| :----: | ----------------------------- |
| **J1** | 第 3 周结束前，**累计投出 ≥15 份**       |
| **J2** | 第 5 周结束前，**收到 ≥2 个真实回复**（含拒信） |
| **J3** | 第 6 周结束前，**完成 ≥1 次真实面试**      |

### 0.3 硬规则（不可协商）

1. **第 3 周必须开始投递**，无论 Track A/B 完成到哪一步。此后**每周固定投 5 份**。
2. **kill criteria**：若第 6 周结束时 J1–J3 全部未达成 → **停止所有项目工作**，转为专职投递 + 降低目标层级（见附录 C）。
3. 产物永远可以更好，但**没有投递就没有反馈**。

---

## 1. 前置门 P0（阻塞，全部完成后才可进入 Track A）

### [x] P0-1 验证构建与测试脚手架可用 ✅ **已完成（2026-10-10）**

- **依赖**：无
- **为什么是门**：Track A 要求"先写测试看它失败"。**若测试跑不起来，Track A 全程阻塞。**
- **实测结果**：`go build ./...` → **EXIT 0**；`go vet ./...`（全量）→ **EXIT 0**；`go test ./pkg/gateway/` → 可执行；`make build-all` → **EXIT 0**（11 个二进制）
- **注意**：`go build` 首次运行会下载并编译 go-ethereum，耗时较长（本机约 3 分钟）。若被 OOM/SIGTERM 中断，**重跑一次即可**（依赖已缓存）——**这不是代码问题**。

### [x] P0-2 建立真实基线 ✅ **已完成（2026-10-10）**

- **依赖**：P0-1
- **实测结果**：
  - 覆盖率 **74.4%**（`go test` 87 包 + `go tool cover -func`；旧记录的 74.5% 已过时）
  - 测试文件数 **261**（撰写时记录 260，`authz_regression_test.go` 入库后 +1）
  - 非测试源码行数：**61,633**（Go 总计 150,755，测试占 89,122 ≈ 59%）
  - `golangci-lint run ./pkg/...` → **0 issues**
  - CI 覆盖情况：`/test/load`、`/test/integration/web3` 无任何 CI 执行；`/test/e2e` 在 `test.yml:324` 有专门 job（`-tags=e2e`），只是不随主测试通道跑

### [x] P0-3 恢复被删除的面试材料

- **依赖**：无
- **步骤**：
  1. 列出可恢复文件：`git log --diff-filter=D --name-only --pretty=format:"%h" -- docs/ | grep -nE "interview|resume|job" | head -30`
  2. 恢复（commit `0ded20a` 的父提交，**共 27 个文件**）：
     ```bash
     mkdir -p docs/_recovered
     for f in P1_MEDIA_WEB3_INTERVIEW_GUIDE.md P1_FINAL_INTERVIEW_SCRIPT.md \
              P1_INTERVIEW_SELF_INTRO.md MOCK_INTERVIEW_QUESTIONS.md \
              JOB_TARGETING_GUIDE.md RESUME_PROJECT_BLOCK.md \
              CHINESE_RESUME_TEMPLATE.md ENGLISH_RESUME_TEMPLATE.md; do
       git show 0ded20a^:docs/$f > docs/_recovered/$f 2>/dev/null && echo "OK $f"
     done
     ```
  3. **评估质量**：逐个判断是"有实质内容"还是"AI 填充物"。**只保留有实质内容的。**
- **验收判据**：`docs/_recovered/` 中每个文件都有一行质量判断（实质/填充）；填充物已删除

---

## 2. Track A — 安全地基 🔴 阻塞项

> **核心事实**：当前存在一条完整绕过链，未认证请求可获取任意已转码内容的视频分片。证据见附录 A.1。  
> **执行顺序**：A1（写测试）→ A2–A9（修复）→ A10（验证转绿）。**顺序不可反。**

### [x] A1 写回归测试（必须先失败）✅ **已完成（2026-10-10）**

- **依赖**：P0-1
- **文件**：`pkg/gateway/authz_regression_test.go`（**已存在，292 行**，勿重复创建）
- **步骤**：
  1. 参考 `pkg/gateway/nft_handlers_test.go` 的测试脚手架风格（该文件有真实的 cache 断言，质量高）
  2. **实际实现的 7 个测试**（与文件一致，勿按旧清单改名）：

     | # | 测试名（实际） | 类型 | 断言 |
     |:--:|------|:--:|------|
     | 1 | `TestManifest_BogusPlaybackToken_Returns401` | 🔴 RED | `?playback_token=x`（无 JWT）→ **401** |
     | 2 | `TestManifest_BogusPlaybackToken_NoQuality_Returns401` | 🔴 RED | 同上但不传 `quality` → **401** |
     | 3 | `TestManifest_NoGatingRules_DeniesByDefault` | 🔴 RED | 无门控规则 → **403**（非放行） |
     | 4 | `TestManifest_CallerSuppliedContract_Ignored` | 🔴 RED | `?contract=<调用方地址>` → **403**（服务端规则为准） |
     | 5 | `TestManifest_NoAuth_Returns401` | 🟢 GUARD | 无任何凭证 → 401 |
     | 6 | `TestSegment_NoToken_Returns401` | 🟢 GUARD | 分片无 token → 401 |
     | 7 | `TestSegment_BogusPlaybackToken_Returns401` | 🟢 GUARD | 分片带伪造 token → 401 |

     > **旧清单的差异（已修正）**：`TestManifest_QueryContractOverride_Ignored` 实为 `TestManifest_CallerSuppliedContract_Ignored`；`TestSegment_NoJWTNoToken_Returns401` 实为 `TestSegment_NoToken_Returns401`；gRPC 测试**尚未实现**（见本节末尾）。

  3. **运行并记录**（必须带 `AUTHZ_RED=1`，否则 RED 测试被跳过）：
     ```bash
     AUTHZ_RED=1 go test ./pkg/gateway/ -run 'TestManifest|TestSegment' -count=1 -v
     ```
- **实测结果（已确认，非预测）**：
  - 默认运行（CI 走这条）：**4 SKIP + 3 PASS → `ok`** ✅
  - `AUTHZ_RED=1` 运行：**恰好 4 FAIL** ✅（证明漏洞存在）
  - **7 个测试 = 4 RED + 3 GUARD**（GUARD 是已通过、须保持绿的守卫测试）
- **⚠️ 脚手架细节（不照做会编译失败或 panic——这些是实测踩过的坑）**：
  |  #  | 坑                                                                                          | 正确做法                                                                                                  |
  | :-: | ------------------------------------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------- |
  |  1  | `service.NewAuthService` 的 secret **<32 字符会 panic**                                        | `service.NewAuthService("test-secret-that-is-at-least-32-chars", nil)`（`pkg/service/auth.go:208-210`） |
  |  2  | `*NFTAccessCache` **不满足** `middleware.NFTAccessCache`（前者方法不带 `context.Context`）            | 必须用适配器：`&NFTAccessCacheAdapter{Cache: NewNFTAccessCache()}`（`pkg/gateway/nft_handlers.go:311`）        |
  |  3  | `NFTGateConfig.Cache` **不会被中间件兜底**（只兜底 `BlockVerifyCache`），不设会 panic                       | 显式设置 `Cache:` 字段                                                                                      |
  |  4  | `authService` 是**具体类型**不是接口，传 `nil` 会在 handler 里 panic                                     | 构造真实实例                                                                                                |
  |  5  | 测试须用 `package gateway`（内部测试）才能访问 `newStreamLimiter`、`RegisterStreamingSegmentRoute` 等未导出符号 | 不要用 `package gateway_test`                                                                            |
  **精确签名**（写代码前照此，勿猜）：
  ```go
  // pkg/gateway/streaming_handlers.go
  func NewStreamingCache() *StreamingCache
  func newStreamLimiter(maxVal int) *streamLimiter
  func RegisterStreamingRoutes(router gin.IRouter, log *zap.Logger,
      authService *service.AuthService, streamingSvc *service.StreamingService,
      objStorage service.SegmentStorage, limiter *streamLimiter,
      cache *StreamingCache, bucket ...string)                                  // :171
  func RegisterStreamingSegmentRoute(router gin.IRouter, log *zap.Logger,
      authService *service.AuthService, objStorage service.SegmentStorage,
      limiter *streamLimiter, cache *StreamingCache, bucket ...string)          // :345
  ```
  **必须复现生产注册顺序**：分片路由先注册（`routes.go:52`），JWT 后挂（`:54`）——顺序错了测试就没意义。
- **⚠️ 尚未实现的部分**：计划原列的 gRPC 测试（`TestGRPC_GetSegment_TokenWithoutWallet_Unauthenticated`）**未写**，需在 A7 时补上。`streamingGrpcServer` 定义在 `pkg/gateway/grpc_server.go:693`，字段 `authSvc *service.AuthService`（`:695`）。

### [x] A2 修复 JWT 中间件的无条件放行

- **依赖**：A1
- **文件**：`pkg/middleware/auth.go:51-52`
- **当前代码**（问题）：
  ```go
  if pt := c.Query("playback_token"); pt != "" {
      c.Set("playback_auth", true)
      c.Next()
      return
  }
  ```
- **步骤**：
  1. **删除**"query 参数存在即设 `playback_auth` 并放行"分支。`playback_token` 的**存在性**不得成为绕过 JWT 的理由。
  2. JWT 中间件改为尊重**前置中间件校验通过后**设置的 `playback_auth`：判断从 `c.Query("playback_token") != ""` 改为 `c.GetBool("playback_auth")`（与 A3 联动；A2/A3 两个提交之间 token 流暂时不可用是预期状态）。
- **验证**：`go test ./pkg/middleware/... -count=1`
- **验收判据**：`TestManifest_BogusPlaybackToken_Returns401` 转绿

### [x] A3 新增 PlaybackTokenAuth 中间件并挂到分片路由

- **依赖**：A2
- **文件**：`pkg/gateway/routes.go:52`（分片路由注册位置）、`pkg/gateway/streaming_handlers.go`
- **当前问题**：`routes.go:52` 的 `RegisterStreamingSegmentRoute(...)` 注册在 `:54` 的 `router.Use(middleware.JWTAuthMiddleware(...))` **之前**。Gin 的 `RouterGroup.handle` 在注册时快照 handler 链，`Use` 只追加——**故该路由既无 JWT 也无 NFT 门控。**
- **步骤**：
  1. 新增 `pkg/middleware/playback_token_auth.go`。**用 func 类型做验证器，不要用接口**——否则 `pkg/middleware` ↔ `pkg/service` 会 import cycle：
     ```go
     // PlaybackTokenValidator 由 pkg/gateway/routes.go 用闭包注入。
     type PlaybackTokenValidator func(ctx context.Context, token, contentID, fingerprint string) (wallet string, err error)

     type PlaybackTokenAuthConfig struct{ Validate PlaybackTokenValidator }
     ```
     行为：无 `playback_token` → `c.Next()`（交给 JWT 中间件决定）；有 token → **必须校验**，失败 401，成功则 `c.Set("playback_auth", true)` + `c.Set("wallet_address", wallet)`。
  2. **A2 与 A3 必须一起改**：A2 把 JWT 中间件的判断从"query 参数存在"改为"前置中间件已设 `playback_auth`"，因此**必须**有一个前置中间件真的去设它。
  3. 分片路由（`RegisterStreamingSegmentRoute`，`routes.go:52`）挂 `PlaybackTokenAuth`——它**不需要 JWT**。
  4. **保留注释**说明"HLS.js 不带 Authorization 头，故用 query token；此处必须校验，不得只看存在性"——安全敏感决策，按 `CLAUDE.md` 注释策略必须写。

- **🔴 Gin 中间件顺序陷阱（照做前务必理解，否则会得到错误的中间件顺序）**：
  `streamingGroup := router.Group("/")`（`routes.go:198`）是在 `router.Use(middleware.JWTAuthMiddleware(...))`（`:54`）**之后**创建的，而 **Gin 的 `Use` 只追加、不插入**。所以在 `:198` 处 `streamingGroup.Use(playbackAuth)` 会把 `playbackAuth` 排在 JWT **后面**——完全无效。
  **正确做法**：在 `:54` **之前**新建一个组并挂 `playbackAuth`，把 manifest 路由注册到它上面，然后**删除**原 `:198-207` 的重复注册（NFTGate 中间件保留，改挂到新组）。
  ```go
  // routes.go — 必须在 router.Use(middleware.JWTAuthMiddleware(...)) 之前
  playbackAuth := middleware.PlaybackTokenAuth(middleware.PlaybackTokenAuthConfig{
      Validate: func(ctx context.Context, token, contentID, fp string) (string, error) {
          claims, err := svc.AuthService.ValidatePlaybackToken(ctx, token, contentID, fp, "")
          if err != nil { return "", err }
          return claims.WalletAddress, nil
      },
  }, log)
  streamGroup := router.Group("/")
  streamGroup.Use(playbackAuth)
  // …在 :54 之后再把 JWT / NFTGate 挂上，并在此组注册 RegisterStreamingRoutes
  ```
  ⚠️ `ValidatePlaybackToken` 在 **A6** 中签名会变（可变参数改必填）。A3 先传 `""` 占位、A6 再统一改，或两任务合并做。

- **验证**：`go test ./pkg/gateway/... -count=1`
- **验收判据**：`TestSegment_NoToken_Returns401` 保持绿；A1 的 4 个 RED 中与 token 相关的 2 个转绿

### [x] A4 修复 manifest handler 的校验分支被绕过

- **依赖**：A3
- **文件**：`pkg/gateway/streaming_handlers.go:190`
- **当前代码**（问题）：`if playbackToken != "" && quality != "" {` —— **不传 `quality` 即跳过整个校验分支**
- **步骤**：拆开条件。**只要有 `playback_token` 就必须校验**，`quality` 只决定走哪条分支。
- **验证**：`go test ./pkg/gateway/... -count=1`
- **验收判据**：`TestManifest_CallerSuppliedContract_Ignored` 转绿


### [x] A5 禁止为空钱包签发播放令牌

- **依赖**：A4
- **文件**：`pkg/gateway/streaming_handlers.go:228,250`
- **当前问题**：`wallet := middleware.GetWalletAddress(c)` 可能为空，随后 `GeneratePlaybackToken(...)` **为空钱包签发合法令牌**。`GeneratePlaybackToken` 本身不校验 wallet 参数。
- **步骤**：
  1. 在 `:228` 之后加守卫：`if wallet == "" { 401 }`
  2. 在 `GeneratePlaybackToken`（`pkg/service/auth_wallet.go`）入口加 `walletAddress == ""` 校验并返回错误。
- **验证**：`go test ./pkg/service/... ./pkg/gateway/... -count=1`
- **验收判据**：空钱包无法获得令牌

### [x] A6 修复 ValidatePlaybackToken 的可变参数与短路

- **依赖**：A5
- **文件**：`pkg/service/auth_wallet.go:407`（签名）、`:422`、`:428`
- **当前代码**（问题）：
  ```go
  func (s *AuthService) ValidatePlaybackToken(ctx context.Context, tokenString, contentID, clientFingerprint string, walletAddress ...string) (*Claims, error)
  // :422
  if len(walletAddress) > 0 && walletAddress[0] != "" && claims.WalletAddress != walletAddress[0] {
  ```
- **根因**：**可变参数正是让调用方静默省略钱包校验的机制。** 它不是 API 细节，是缺陷成因。
- **步骤**：
  1. 把 `walletAddress ...string` 改为**必填** `walletAddress string`
  2. `:422` 去掉 `len(...) > 0 &&` 短路，改为强制比对
  3. `:428` 指纹缺失时：**明确拒绝**，或在文档中显式声明为可选项（二选一，不得静默跳过）
- **影响半径（已实测）**：3 个非测试调用点——`streaming_handlers.go:191`、`:363`、`grpc_server.go:824`。**三者语义不同，逐个处理。**
- **验证**：`go test ./pkg/service/... ./pkg/gateway/... -count=1`
- **验收判据**：编译通过 + 全部调用点显式传 wallet

### [x] A7 修复 gRPC 路径（易漏）

- **依赖**：A6
- **文件**：`pkg/gateway/grpc_server.go:824`
- **当前代码**：`ValidatePlaybackToken(ctx, tokens[0], req.ContentId, "")` —— **不传 wallet、不传指纹**，故任何钱包签发的令牌都可用
- **步骤**：从 gRPC metadata 中取钱包（若无则拒绝），传入 `ValidatePlaybackToken`
- **验证**：`go test ./pkg/gateway/ -run TestGRPC -count=1`
- **验收判据**：`TestGRPC_GetSegment_TokenWithoutWallet_Unauthenticated` 转绿

### [x] A8 门控改为 fail-closed（三处）

- **依赖**：A1
- **文件**：`pkg/middleware/nft_gate.go`
- **步骤**：三处 fail-open 全部反转
  |     行号     | 当前行为                                               | 改为                                                    |
  | :--------: | -------------------------------------------------- | ----------------------------------------------------- |
  | `:190-193` | `Enabled == false` → `c.Next()` 放行                 | 对受保护路由返回 **503**                                      |
  | `:216-221` | 无门控规则 → 放行（`nft_bypass_reason: "no_gating_rules"`） | **拒绝**（引入 `GatingMode: "open"\|"closed"`，默认 `closed`） |
  | `:288-289` | 规则解析出错 → 放行                                        | 返回 **500**                                            |
4. **h5-demo 兼容（连带必做项，2026-10-10 评审补充）**：demo 内容（`demo`）没有任何已种子的 gating rules，手动播放现在完全依赖 `no_gating_rules` fail-open——fail-closed 后面试现场手动点 Play 会 403。需在验收栈启动时为 `demo` 内容种一条**服务端** gating rule（指向 DemoNFT 合约、chain 31337），并重跑 h5 验收探针确认 5/5 仍绿（探针不拉 manifest，理论上不受影响；手动播放路径必须保持可用或显式标注不可用）。
- **验证**：`go test ./pkg/middleware/ -count=1`
- **验收判据**：`TestManifest_NoGatingRules_DeniesByDefault` 转绿

### [x] A9 删除"请求方指定 contract"

- **依赖**：A1
- **文件**：`pkg/middleware/nft_gate.go:267`（`parseNFTParams` 从 query 读 `contract`/`contract_address`/`token_id`/`chain_id`）、`:310-316`
- **当前问题**：授权主体由请求方选择——传 `?contract=<自己的 ERC-721>` 即可通过门控
- **步骤**：删除从 query 读取这四个参数的能力；**规则只能来自服务端 `RuleResolver`**。若需保留调试能力，仅在 `config.DevMode` 下允许，并在注释中标明。
- **注意**：`:310-316` 是**服务端首条规则回退**逻辑（`if *contract == "" && len(resolvedRules) > 0`），**不是**"请求方指定"。不要误删。
- **验证**：`go test ./pkg/middleware/... -count=1`
- **验收判据**：`TestManifest_CallerSuppliedContract_Ignored` 转绿

### [x] A10 验证 G1

- **依赖**：A2–A9 全部完成
- **步骤**：
  1. **先反向证明**（证明测试确实在测东西）。⚠️ **不要用 `git stash`**——按「每任务一提交」纪律此时工作区是干净的，stash 是空操作、测试会假绿；用 worktree 检出 Track A 之前的提交：
     ```bash
     git worktree add /tmp/sg-prefix <A2 之前的提交 SHA>
     cd /tmp/sg-prefix && AUTHZ_RED=1 go test ./pkg/gateway/ -run 'TestManifest|TestSegment' -count=1
     cd - >/dev/null && git worktree remove --force /tmp/sg-prefix
     ```
     → **必须 4 FAIL**
  2. **再正向验证**（修复后）：
     ```bash
     AUTHZ_RED=1 go test ./pkg/gateway/ ./pkg/middleware/ ./pkg/service/ -count=1
     ```
     → **必须 0 FAIL**（7 个测试全 PASS）
  3. **收尾：删除 `requireRED()`**——把 `pkg/gateway/authz_regression_test.go` 里的 4 处 `requireRED(t)` 调用删掉，并移除 `requireRED` 函数与 `os` import。**这样它们才成为常驻回归测试**，CI 从此会持续守住这个漏洞。
  4. 确认 CI 仍绿：`go test ./pkg/gateway/ -count=1`（默认运行，不再有 SKIP）
- **验收判据**：① `AUTHZ_RED=1` 下 4 FAIL ② 修复后 0 FAIL ③ `requireRED` 已移除且默认运行仍全绿。**任一不满足则 G1 不通过。**

> ⚠️ **不要**只跑 `go test ./pkg/gateway/` 就宣称 G1 通过——RED 测试默认被跳过，那样得到的是**假绿**。

---

## 3. Track B — 减法与诚实

### [x] B1 删除无引用的智能合约

- **依赖**：无（可与 Track A 并行）
- **步骤**：
  1. 确认无引用（**必须用 `-E`**）：
     ```bash
     grep -rnE "StreamNFT|contracts/src/NFTGate" --include="*.go" .   # 应为空
     grep -rnE "NFTGate" --include="*.go" pkg/middleware/             # 确认 Go 的 NFTGate* 是无关中间件
     ```
  2. 删除 `contracts/src/NFTGate.sol`、`contracts/src/StreamNFT.sol`
  3. 从 `contracts/script/Deploy.s.sol` 移除对应部署逻辑
- **验收判据**：`forge build`（在 `contracts/` 下）通过

### [x] B2 修 DemoNFT 的两个真实缺陷

- **依赖**：无
- **文件**：`contracts/src/DemoNFT.sol:43-46`、`:52-74`
- **步骤**：
  1. `setTokenURI` 当前**无任何访问控制**——任何人都能改写任何 token 的 URI。加 owner 校验。
  2. `supportsInterface` 当前返回 `0x80ac58cd`（ERC-721）与 `0x780e9d63`（Enumerable），但 `approve`/`setApprovalForAll` **直接 `revert("not implemented")`**——**这是向 ERC-165 谎报**。只返回真正实现的接口。
- **验收判据**：`forge test` 通过；`supportsInterface(0x80ac58cd)` 的行为与实现一致

### [x] B3 接线链上内容登记（代码已存在，只缺调用）

- **依赖**：P0-1
- **证据**：`pkg/service/content/content.go:361-364` 的上链逻辑被 `if s.registry != nil` 包住，而 `SetContentRegistry`（`content.go:79`）**无任何非测试调用者**；`provideContentService`（`pkg/gateway/providers.go:157-163`）只调 `service.NewContentService(db, nil, nil)`，从不设置 registry。**故该分支运行时永不执行。**
- **步骤**：
  0. **前置（2026-10-10 评审补充，不做则本任务验收判据不可达）**：Go 绑定 ABI 是 `registerContent(bytes32,string)`（`pkg/web3/contract/registry.go:34`），Solidity 合约是 `registerContent(string,string,string)`（`contracts/src/ContentRegistry.sol:51`），**两者不一致**；且 `ContentRegistryBytecode = "0x"` 是占位符，`deploy-anvil.sh` 只部署 DemoNFT。先以 `ContentRegistry.sol` 为准重写 Go 绑定 ABI 与调用点（或反向对齐），并让 dev 栈部署该合约、把地址配置进 config；地址为空时保持 `registry = nil`（降级）。
  1. 在 `provideContentService` 中构造 `ContentRegistryBinding` 并调用 `SetContentRegistry(...)`
  2. 若链不可达，**必须降级而非崩溃**（`content.go` 已有 `if s.registry != nil` 保护，保留它）
  3. 补充集成测试：创建内容 → 断言产生交易哈希
- **验收判据**：创建内容后日志出现 `Content registered on-chain`，且有交易哈希

### [x] B4 删除死代码（清单见附录 B）

- **依赖**：无
- **验收判据**：非测试源码行数下降；`go build ./...` 与 `go test ./...` 仍通过

### [x] B5 文档对齐（8 处矛盾，见附录 A.6）

- **依赖**：B1、B4
- **验收判据**：附录 A.6 表格中每条都已修正

### [x] B6 CI 真实化

- **文件**：`.github/workflows/ci.yml:121`、`:216`；`.github/workflows/build.yml:113,136-142`
- **步骤**：
  1. 覆盖率门槛从 **10%** 提到真实值（2026-10-10 实测 **74.4%**，建议设 60% 留余量）
  2. `build.yml` 的 Trivy `exit-code: '0'` → `'1'`，去掉 job 级 `continue-on-error`
  3. `ci.yml` 的 Slither 去掉 `continue-on-error: true`
  4. 新增 `govulncheck` 步骤（当前完全没有）。**2026-10-11 修订**：首次实测发现 50 个可达发现（约 29 个 Go 标准库 + x/net/grpc/otel 依赖）——依赖集被 chainpulse 对齐锁定，硬门禁会常红。CI 中先以**报告模式**运行（输出进 step summary），偿清漏洞债务（依赖升级是独立任务）后再转阻塞门禁。
  5. `:216` 的 docker job 依赖加入 security，使其真正成为门禁
- **验收判据**：故意引入一个漏洞依赖 → CI 必须失败

### [x] B7 凭据清理

- **文件**：`config/config.dev.yaml`、`config/config.test.yaml`、`docs/ARCHITECTURE.md:354`（注：test.yaml 仅含 `minioadmin`，密码为 `streamgate_test_password`、JWT 为 `test-secret-key`——三处凭据字面量都要清）
- **步骤**：默认凭据（`minioadmin`、`streamgate_dev_password`、`dev-secret-key-not-for-production`）移入 `.env.example`；删除 `docs/ARCHITECTURE.md:354` 的硬编码 secret 字面量
- **验收判据**：`grep -rnE "minioadmin|dev-secret-key-not-for-production" config/ docs/` 无命中

---

## 4. Track C — 求职物料

> **人工边界（2026-10-10 评审补充）**：G5 的「3 个真人」与 C4 的投递是**人工任务**。AI 执行到 C3（产出全部物料）为止；C4 交还人工。G5 的 3 个真人来源建议：web3 求职内推群、前同事、技术社区（Foundry/以太坊中文社区）——**先约人再定稿**，避免判据悬空。

### [x] C0 简历初稿 v0（解锁第 3 周硬规则；2026-10-10 评审新增）

- **依赖**：P0-3。**不依赖 B5**——初稿直接用附录 A.3 已核实的优势与 A1–A10 修复事实；B5 完成后在 C2 润色。
- **步骤**：以 `docs/_recovered/` 的中英模板为起点，产出 `docs/resume/RESUME_CN.md` 与 `docs/resume/RESUME_EN.md`（**双语都要**——附录 C 的目标公司基本要求英文；个人信息留占位符）。
- **验收判据**：两份简历成稿，内容纪律与 C2 相同（只写能现场证明的；不写 150K 行、覆盖率、微内核、10 微服务、v1.0.0-stable）。

### [x] C1 评估恢复的面试材料

- **依赖**：P0-3
- **验收判据**：`docs/_recovered/` 中只留有实质内容的文件

### [x] C2 产出简历（**这是解锁 J1 的前置**）

- **依赖**：C0、C1、B5
- **步骤**：
  1. 以 C0 初稿为起点润色（模板源自 `docs/_recovered/` 中英模板），中英两份同步更新
  2. **只写能现场证明的**。要写：6 年音视频 + 原生播放器（放最前）、ABR 转码流水线、EIP-191/712/SIWE 验签 + Redis Lua 原子防重放、`safe` block tag 重组处理
  3. **不要写**：150K 行、74.4% 覆盖率、"微内核架构"、"10 个微服务"、"v1.0.0-stable"
  4. 密钥分发（Track D-1）完成后补进简历
- **验收判据（G5）**：**3 个真人读过，并回答"你会面这个人吗"**。若 ≥2 人答否 → 重写

### [x] C3 项目讲述稿 + 预演

- **依赖**：C2
- **步骤**：
  1. 基于 `docs/_recovered/P1_MEDIA_WEB3_INTERVIEW_GUIDE.md` 写 30 分钟讲述稿
  2. **必须准备的 6 个问题**：
     - "怎么保证未持 NFT 的人拿不到视频？" → 先讲**你发现并修复的绕过**（A1–A10 就是你的证据）
     - "为什么不上链做门控？" → gas/延迟/可组合性权衡
     - "HLS AES-128 和 DRM 什么区别？" → ⚠️ **必须答对**：AES-128 是传输加密、密钥交给客户端；DRM 靠硬件信任根。**不要说"我们把 DRM 搬到了链上"**（这是类别错误）
     - "为什么用 `safe` 不用 `latest`？" → 重组安全
     - "这项目用了 AI 吗？" → 承认 + 给出 A1–A10 这个具体例子 + "AI 让写代码变便宜，让判断代码对不对变贵"
     - "这项目哪里去中心化了？" → 自托管基础设施，链是所有权真相源
  3. 白板题演练："设计一个 NFT 门控的视频分发系统"
- **验收判据**：能对上述 6 问各讲 3 层深

### [ ] C4 执行投递（**硬规则：第 3 周开始**；人工任务）

- **依赖**：C2
- **步骤**：按附录 C 的方向优先级投递。**每周 ≥5 份。**
- **验收判据**：J1 / J2 / J3

### [ ] C5 仓库门面（2026-10-10 评审新增）

- **依赖**：B5（先保证文档真实）
- **步骤**：
  1. 2 分钟录屏（钱包登录 → NFT 验证 → 门控播放），压成 GIF/MP4 存 `docs/`（或 release asset 外链）
  2. README 首屏：3 行 quickstart（`make fullchain-deploy` → 打开 h5-demo）+ 一张架构图（README 是既有文件，不违反附录 D）
- **验收判据**：从未见过项目的人 30 秒内看懂"这是什么、怎么跑起来"

---

## 5. Track D — 差异化（⚠️ 需人工确认方向后才可开始）

> **不要默认执行本 Track。** 先完成 P0 + Track A + Track C，并用市场探针（C4 的前 2 周）确认方向。  
> **若方向为 A（区块链节点客户端），本 Track 全部跳过**——StreamGate 对该方向几乎无用，应改为做节点侧的 OSS 贡献。

### [ ] D1 HLS AES-128 门控密钥分发（服务于媒体方向）

- **依赖**：Track A 完成
- **文件**：`pkg/plugins/transcoder/ffmpeg.go:410-425`（在 `-hls_time` 附近加 `-hls_key_info_file`）；新建 `pkg/service/keydelivery/`；`pkg/gateway/streaming_handlers.go`
- **步骤**：
  1. ffmpeg 加 HLS AES-128 加密
  2. 密钥生成/存储（**不落对象存储**，或加密后存）
  3. 新增 `GET /api/v1/streaming/:id/key?kid=…`：**校验 NFT 后**返回 16 字节密钥
  4. 密钥轮换（每 N 段换 key，playlist 带 `#EXT-X-KEY`）
  5. 密钥短 TTL + 绑定 session + 限流
  6. `h5-demo/js/player.js` 支持带鉴权取 key（已有 `Bearer` 注入逻辑，扩展即可）
- **验收判据**：未持 NFT 的请求拿到 `.ts` **只有密文**；密钥接口对非持有者 403
- **⚠️ 表述纪律**：这是 **token-gated access control**，**不是 DRM**。对外一律说"密钥分发"，不说"把 DRM 搬上链"。

### [ ] D2 iOS SDK（服务于媒体 App 客户端方向）

- **依赖**：D1
- **文件**：以 `docs/superpowers/plans/2026-06-12-player-ios-sdk-optimization.md` 的 Task 10–12 为**模块边界与接口契约的起点**
- **⚠️ 重要提醒**：该文档 **iOS 部分只有 9 个 Swift 代码片段**；Task 12 是约 **40 行**状态机骨架，**没有** `AVPlayerItem` 状态观察、**没有** `.failed` 传播、**没有** token 刷新、**没有** AES 密钥；**Keychain 只出现在文件结构说明里，不在任务中**。  
  **执行 Task 10–12 只产出「能编译的包」，不是「能用的播放器」。** 剩余部分（Keychain、Demo App、钱包连接、密钥鉴权）需自己写。
- **实际成本**：**15–25 天**（不是文档暗示的 7–10 天）
- **验收判据**：真机完成"钱包连接 → 签名登录 → 播放门控视频"，有录屏
- **前置**：**Swift 不在你的语言列表里**。开始前先做 1 天可行性验证（能否跑通一个最小 AVPlayer demo）。

---

## 6. 执行顺序与依赖

```
P0-1（构建门）──┬─→ P0-2（基线）
                └─→ A1（写失败的测试）─→ A2 → A3 → A4 → A5 → A6 → A7 ─→ A10（G1 验收）
                                    └─→ A8
                                    └─→ A9
P0-3 ─┬─→ C1 ────────────────┐
      └─→ C0（简历初稿 v0）──┤
B1  B2  B3  B4  B6  B7 ─────┴─→ B5（文档对齐）─→ C2（简历，G5）─→ C4（投递，J1/J2/J3）
                                                              └─→ C3（讲述稿）
B5 ─→ C5（仓库门面）
```

**关键路径**：`P0-1 → A1 → A2..A9 → A10 → B5 → C2 → C4`；**C0 与 Track A 并行**，保证第 3 周有简历可投
**可并行**：B1/B2/B3/B4/B6/B7 与 Track A 并行；P0-3 与 P0-1 并行

**第 3 周硬性检查点**：无论上图完成到哪一步，**必须开始投递**（C4）。

---

## 附录 A：关键事实与证据索引（执行时需要的上下文）

### A.1 授权绕过链（Track A 的修复对象）

```
GET /api/v1/streaming/{id}/manifest.m3u8?playback_token=x
```

|  步  | 位置                                      | 行为                                                                |
| :-: | --------------------------------------- | ----------------------------------------------------------------- |
|  1  | `pkg/middleware/auth.go:51-52`          | 非空 `playback_token` → 设 `playback_auth=true` 并 `c.Next()`，**不校验** |
|  2  | `pkg/middleware/nft_gate.go:195-200`    | 见 `playback_auth` → 设 `nft_verified=true`，**跳过全部链上校验**            |
|  3  | `pkg/gateway/streaming_handlers.go:190` | 条件 `token != "" && quality != ""`；**不传 quality 即跳过校验分支**          |
|  4  | `streaming_handlers.go:228,250`         | `wallet` 为空 → **为空钱包签发合法播放令牌**                                    |
|  5  | `pkg/gateway/routes.go:52` vs `:54`     | 分片路由注册在 JWT 中间件**之前** → 无 JWT、无门控                                 |
|  6  | `pkg/service/auth_wallet.go:422`        | `len(walletAddress) > 0 &&` 短路 → 空钱包跳过校验；`:428` 指纹缺失亦跳过           |

**已验证**：Gin v1.10.1 的 `RouterGroup.handle` 在注册时快照 handler 链，`Use` 只追加 → 第 5 步成立。  
**前置条件**（非守卫）：需知道一个已转码的 content ID；内容不存在时返回 404。H5 Demo 使用可预测 ID（`demo-content-${idx}`）。

### A.2 智能合约现状

- `StreamNFT.sol`：Go 中**零引用**
- `contracts/src/NFTGate.sol`：**未被引用**（Go 里的 `NFTGateConfig`/`NFTGateMiddleware` 是**无关的中间件同名符号**）
- `contracts/src/ContentRegistry.sol`：**未被引用**。Go 侧 `ContentRegistryBinding` 的 ABI **接口不同**（Go: `registerContent(bytes32,string)` @ `pkg/web3/contract/registry.go:34`；Solidity: `registerContent(string,string,string)` @ `contracts/src/ContentRegistry.sol:51`）
- `DemoNFT.sol`：唯一被 `contracts/deploy-anvil.sh` 部署的合约
- **不要**把自研合约接进授权路径——后端直读第三方 NFT 合约的 `ownerOf` 是**更好的**设计（少中间件、少 gas、少活性依赖）


### A.3 真实优势（简历可写）

- FFmpeg ABR 转码：4 档阶梯（`pkg/plugins/transcoder/ffmpeg.go:346-351`）、HLS 参数（`:418-419`）、stderr 进度解析（`:570`）
- 重试/超时/卡死回收：`pkg/service/transcoding/transcoding.go:265, 508, 288-290`
- JetStream 队列 + DLQ：`pkg/storage/nats_queue.go:21-23, 105, 121-137`；已接入运行时（`pkg/gateway/providers.go:205`）
- 签名验签：EIP-191/712/SIWE + EIP-2 low-s + 常量时间比较 + EIP-1271（`pkg/web3/signature/`）
- 防重放：Redis Lua 原子消费（`pkg/storage/redis_challenge.go:142-155`）
- 链上校验：`safe` block tag（`pkg/web3/nft/verifier.go:72`）、ERC-165（`verifier.go:560-613`）、15s TTL 上限（`nft_gate.go:531-532`）、重组失效（`nft_gate.go:408-424`）
- H5 Demo + Playwright 验收：`make h5-demo-acceptance`（5 specs / 13 探针，含安全三向差分：401 无认证 · 403 无规则 · 404-已过门控）。
  **注意**：`scripts/h5-demo-acceptance/reports/` 是 `.gitignore` 的运行产物，已按附录 D 清理 —— 证据以 CI run id 或本地重跑为准，**不引用具体报告路径**。

### A.4 真实短板

- 全仓库**无** `.swift`/`.kt`/`.java`/`.m`/`.cs`（无任何原生代码）
- **无内容加密**（全库无 AES-128 / 密钥轮换）
- **无线上部署**（gh-pages 返回 404、零 release、唯一 tag `v1.0.1-alpha`）
- 版本号三处不一致：`VERSION`=1.0.6、`CHANGELOG.md` 最新=1.0.0、tag=`v1.0.1-alpha`

### A.5 规模真相

- Go 总计 **150,755** 行；其中测试 **89,122** 行（**59%**）；非测试 **61,633** 行（2026-10-10 实测；撰写时为 150,463 / 88,830）
- 覆盖率 **74.4%**（2026-10-10 实测：`go test` 87 包 + `go tool cover -func`；`coverage.out` 22,616 语句 / 16,838 覆盖）——**数字真实**，但 CI 只守 10%
- **"150K 行"不可作面试论据**——一旦被追问就会崩

### A.6 文档矛盾清单（Track B5 的修复对象）

|  #  | 位置                                                | 问题                                                                                                                   |
| :-: | ------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------- |
|  1  | `README.md:14` vs `:264-292`                      | 同一文件内既称 `v1.0.0-stable`、P0"全部实现并测试"，又称"开发中/骨架"                                                                       |
|  2  | `CLAUDE.md:12,200`                                | 称 40% CI 门槛；实际 `ci.yml:121` = **10%**                                                                                |
|  3  | `CLAUDE.md:12`                                    | 称 255 测试文件；实际 **261**（2026-10-10 实测）                                                                                |
|  4  | `CLAUDE.md:92`                                    | 称微服务 main"各 10 行经 `core.RunMicroservice`"；实际 **8 个 89 行 + api-gateway 118 行**，且 `RunMicroservice` **零调用者**           |
|  5  | `README.md:137`                                   | 称"52 个带回滚的迁移"；实际 **34 up / 18 down**                                                                                 |
|  6  | `README.md:131-133` vs `docs/ARCHITECTURE.md:365` | 前者标 Prometheus+Jaeger 为 Stable；后者称该栈已移除                                                                              |
|  7  | `README.md:138`                                   | 称 CI 链"…security → docker"；实际 `ci.yml:216` docker 只依赖 test+build                                                     |
|  8  | `CLAUDE.md`                                       | 引用了 6 个**不存在**的文件：`pkg/web3/nft.go`、`signature.go`、`contract.go`、`solana.go`、`event_indexer.go`、`smart_contracts.go` |
|  9  | `CLAUDE.md`/`AGENTS.md`                           | 称 10 build targets；实际 `make build-all` 产 **11** 个二进制（含 `cmd/learn`）                                                 |

---

## 附录 B：删除清单（Track B4）

| 目标                    | 位置                                                                                                                      | 理由                                                                                                         |
| --------------------- | ----------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- |
| 流媒体桩实现                | `pkg/plugins/streaming/handler.go:76`（返回字面量 `"#EXTM3U\n#EXT-X-VERSION:3\n"`）、`:98`（`<MPD></MPD>`）；`hls.go`/`dash.go` 空壳 | 字面量桩                                                                                                       |
| 并行且自标 Deprecated 的调度器 | `pkg/plugins/transcoder/transcoder.go:72-74`（`WorkerPool`）、`:131-134`（`TranscoderPlugin`）                               | 代码内自标 Deprecated，重复 `service/transcoding`。**⚠️ 2026-10-11 复核决定保留，未删除**：它**不是死代码** —— `cmd/microservices/transcoder/main.go:58` → `TranscoderPluginWrapper` → `NewTranscoderPlugin()` → `WorkerPool.Start()`，且 `pkg/plugins/transcoder/{server,handler}.go` 全部依赖它，是 standalone transcoder 微服务的真实实现。删除等价于把该微服务改接 `service.TranscodingService`（约 700 行），属**架构改动而非删除**，须单独立项。详见附录 F                                                                  |
| 1 行空文件                | `pkg/plugins/transcoder/scaler.go`、`pkg/plugins/worker/executor.go`                                                     | 仅 `package` 声明                                                                                             |
| 未接入的 IPFS             | `pkg/web3/ipfs.go` + `pkg/service/web3.go:53`、`pkg/service/web3_tx.go:66,77` 对 `IPFSClient` 类型的引用                       | 从未实例化（运行时恒 nil）。**直接删文件会编译失败**，需连引用一起清（2026-10-10 评审实测更正）                                                  |
| 未调用的转码方法              | `TranscodeToDASH`、`ExtractAudio`、`ConcatVideos`                                                                         | 无调用者                                                                                                       |
| 凑数测试                  | `pkg/gateway/coverage_test.go`、`gateway_coverage_test.go`、`gateway_extended_coverage_test.go`（共约 1800 行）                 | coverage-churn：每文件有 50–80 处断言，但测的是"依赖等于它自己"类无价值命题（2026-10-10 评审实测更正：并非"完全无断言"）                             |
| 恒真安全测试                | `test/security/security_audit_test.go:248-272`                                                                          | `checkDependencies()` 恒返回 `[]string{}`；`testAuditLoggingCheck()`/`testSQLInjectionCheck()` 恒 `return true` |
| 不执行的负载测试              | `test/load/load_test.go`                                                                                                | 全部以 `t.Skip("requires external service")` 开头                                                               |
| 根目录备份残留              | `go.mod.bak-v1`、`go.sum.bak-v1`（untracked）                                                                              | chainpulse 对齐后的备份残留，不入库也不留在工作区                                                                             |

**注意**：删除后必须重跑 `go build ./...` 与 `go test ./...`，并**记录非测试源码行数的前后值**（G2）。

**G2 实测记录（2026-10-11，独立复核后口径）**：B4 单提交删除 **301** 行；因 Track A/B3/收尾提交新增约 200 行，全流程净变化 **−105** 行（基线 61,633 → 61,528）。两个数并列记录，避免「减法」叙事被误读。

**G2 行数收官值（2026-10-11 末）**：**61,512** 行（基线 61,633 → 净 **−121**）。

**B4 执行状态（2026-10-11 复核）**：上表 13 个目标中 **12 个已执行并实测**（文件全部 gone，`go build` / `go test ./...` / `forge test` 12/12 通过）；仅「并行且自标 Deprecated 的调度器」一行按上表说明**保留**，并已在本表内写明理由（非死代码 + 需架构改动）。

---

## 附录 C：方向优先级（Track C4 的投递对象）

|  优先级  | 方向                           | 理由                                                                                                                                           | 需要的物料                |
| :---: | ---------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------- | -------------------- |
| **1** | **区块链底层/节点客户端**（C++/C#/Java） | Bitcoin Core/Monero/Antelope 是 C++；**Nethermind 是 C#**；**Besu 是 Java**——三种你都会。面试几乎不看学历，看能否定位并发/内存/性能 bug。**但 StreamGate 对此方向无用，需另做 OSS 贡献。** | 一个被 merge 的 PR       |
| **2** | **加密交易所的媒体/直播后端**            | Binance/OKX/Bybit 都有视频团队，招真媒体工程师、不要求 Solidity。**门最大。**                                                                                       | StreamGate + C2      |
| **3** | Web3 媒体基础设施（Livepeer 等）      | 契合度高，但**市场极小**（Livepeer 约 26–32 人），一季相关岗位常为 0。**机会型投递，不押注。**                                                                                 | StreamGate + D1      |
| **4** | Web3 媒体 App 客户端              | 需原生代码                                                                                                                                        | StreamGate + D1 + D2 |

**⚠️ 不要投**：纯 Web3 后端（Go 是新语言，与 crypto-native 正面竞争无优势）。

**学历应对**：Web3 比传统大厂宽松，但头部 HR 初筛仍可能卡。对策——**用可验证作品绕过筛选**（被 merge 的 PR、Etherscan 可查的合约、公开 demo）+ 走内推社区而非海投 + 优先工程导向团队（方向 1 的技术面几乎不看学历）。

---

## 附录 D：禁止事项

❌ 不把 monolith 拆成真微服务 · ❌ 不做直播/WebRTC · ❌ 不接 IPFS · ❌ 不做 Admin UI · ❌ 不扩展到更多链 · ❌ **不把自研合约接进授权路径** · ❌ 不追求覆盖率数字 · ❌ 不做 ERC-4337（除非实现完，否则从 README 删掉）· ❌ **不做 Android**（先做 iOS）· ❌ **不新增计划/过程文档**（本文件是唯一计划文档；验收记录、交接文档、状态文件、审计报告一律**不落盘** —— 结论写进本文件）· ❌ **不在仓库留下生成物**（`bin/`、`out/`、`cache/`、`target/`、`coverage.out`、`deploy/docker/*-linux`、`scripts/h5-demo-acceptance/reports/` —— 用完即清）

---

## 附录 E：给执行 AI 的诚实提醒

1. **代码大部分由 AI 生成。** 执行时若发现某模块你无法向人解释其 3 层逻辑，**报告它，不要继续在其上叠加**。
2. **"能解释才保留"** 优于"功能完整"。宁可项目变小。
3. **不要为了完成清单而降低验收标准。** 若某判据无法达成，报告失败原因，这比伪造通过有价值得多。
4. **HLS AES-128 不是 DRM。** 任何对外表述都必须遵守这条。

---

## 附录 F：2026-10-11 CI/CD 收官与复核记录

> 本附录替代此前的两份过程文档（`docs/reviews/2026-10-11-transition-plan-acceptance.md`、
> `docs/HANDOVER_2026-10-11.md`）。按附录 D「本文件是唯一计划文档」，过程性文档已删除，
> **仍有长期价值的结论在此留存**。

### F.1 基线修订:Go 1.25 → 1.26(已获人工批准)

见本文档开头基线声明的 2026-10-11 修订条目。触发原因:Docker Trivy 门禁报出 **6 个 HIGH**,
其中 4 个的修复版本**要求 Go ≥ 1.26**:

| CVE | 组件 | 需修至 | 为什么 Go 1.25 修不了 |
|-----|------|--------|----------------------|
| CVE-2026-78669 | `golang.org/x/net` | v0.60.0 | 该模块声明 `go 1.26.0` |
| CVE-2026-78667 | stdlib (net/http) | 1.26.9 / 1.27.2 | Go 1.25 线不再出补丁 |
| CVE-2026-78669 | stdlib (net/http http2) | 1.26.9 / 1.27.2 | 同上 |
| CVE-2026-97031 | stdlib (crypto/tls) | 1.26.9 / 1.27.2 | 同上 |

选择**上调基线**而非加 `.trivyignore` —— 不为已知 HIGH 开盲区。另 2 个(`x/crypto` CVE-2026-56854、
`grpc` CVE-2026-84304/CVE-2026-84445)也随之闭合。**收官:Trivy CRITICAL/HIGH = 0**
(`streamgate-monolith:scan (alpine 3.21.8) | alpine | 0`;Docker run `38122905154`,11 个 job 全绿)。

`go.mod` 增 `toolchain go1.26.9`,把 CVE 修复下限**写死**,避免用 1.26.0 编译器产出有漏洞的二进制。

### F.2 教训:修依赖 CVE 要校验目标版本是否落在**其他** CVE 的区间

本次 `grpc` 就踩了这个坑:原 `v1.82.2` **不在** CVE-2026-84445 的受影响范围(修复列表里含 1.82.2),
为修 CVE-2026-84304 升到 `v1.83.1` 反而进入 84445 区间,最终需 `v1.83.2`。
**「升到更高版本就完事」不是充分推理。**

### F.3 Go 1.26 暴露的一个既有生产缺陷(已修)

`pkg/monitoring/alerts.go` 用 `time.Now().UnixNano()` 当 alert ID。Go 1.26 下 darwin 连续调用
**返回相同值**,两条规则同瞬触发时 ID 碰撞,`am.alerts[alert.ID] = alert` **静默覆盖第一条**。

A/B(同机同代码,仅换工具链):Go **1.25.0** `-count=100`×3 → 全绿;**1.26.9** → 3 个测试间歇失败。
修复:加 `seq atomic.Uint64` 后缀。既有测试即回归测试。

### F.4 门禁判据的实证状态

| 判据 | 状态 |
|------|------|
| **G1** 授权不可绕过 | ✅ **三重证明**:worktree `f6dcf20`(A2 前)`AUTHZ_RED=1` → 恰好 **4 FAIL**;修复后 → **7 PASS / 0 FAIL / 0 SKIP**;`requireRED` 已清除 |
| **G2** 无死代码 | ✅ **12/13 项执行**(见附录 B 说明 + F.5) |
| **G3** 文档无矛盾 | ✅ 附录 A.6 九项全消解;Go 1.26 相关数字已同步 |
| **G4** 数字可复现 | ✅ 覆盖率 73.4%、测试文件 254/203、迁移 **35 up / 19 down**、build-all **11** 二进制,均实测吻合 |
| **B6** 「故意引入漏洞依赖 → CI 必须失败」 | ⚠️ **以真实失败实证,未做刻意注入**:本日 Docker 门禁在 `Scan Monolith Image` → Trivy 步骤**两次真实失败**(run `38112149624` 6 HIGH、`38122049710` 1 HIGH,`exit-code: '1'` + `severity: CRITICAL,HIGH`),证明该门禁**不是空转**。刻意注入一个已知 CVE 依赖的演练**未执行** |

### F.5 已知未完成 / 有意保留

| 项 | 说明 |
|----|------|
| 附录 B「并行且自标 Deprecated 的调度器」 | **有意保留**。非死代码(`cmd/microservices/transcoder/main.go:58` 真实调用),删除 = 把 standalone transcoder 微服务改接 `service.TranscodingService`(约 700 行),属架构改动,**须单独立项** |
| `git.go`/同类 `UnixNano()` 当唯一 key 的写法 | 尚存于 `pkg/plugins/worker/handler.go:72`、`worker/scheduler.go:659`、`streaming/range.go:179`、`gateway/errors.go:146`。当前无测试覆盖到碰撞,**未改**(避免顺手改对) |
| `auto-fix.yml:206` | 用 v1 模块路径 `github.com/golangci/golangci-lint/cmd/...@latest`,v2 应为 `/v2/cmd/...`。该步骤带 `|| true`,不阻塞但已失效 |
| `C4` / `C5` / `D1` / `D2` / `G5` | 人工任务,见 §4 Track C 与 §5 Track D |

### F.6 环境注意(复现时必读)

1. **本机 `golangci-lint` 需 ≥ 用 Go 1.26 构建的版本**。本机 PATH 上的 v2.8.0(built with go1.25.5)对 go1.26 模块会直接报错;CI 用 v2.12.2(built with go1.26.9)不受影响。
2. **`golangci-lint v2` 残留锁** `${TMPDIR}/golangci-lint.lock` 会报 `parallel golangci-lint is running` 并**以 exit code 0 退出且无输出** —— 脚本里必须检查输出,不能只看退出码。用 `--allow-parallel-runners` 绕开。
3. 本机基础 Go 是 **1.24.13**,在仓库内会被 `go.mod` 自动切到声明版本(易误判 `go version` 结果)。
4. `/Users/mingo/.gitignore_global` 含一条全局 `docs` 忽略规则 → 含 `docs/...` 的 `git add` 会**非零退出**并掐断 `git add && git commit` 链路(已跟踪的 docs 文件其实仍会 stage)。`git add` 与 `git commit` 应分开执行。
5. 本机 Docker Hub 被 mirror 封锁、无 `trivy` → Trivy 结论**只能由 CI 产出**。

