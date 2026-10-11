# TRANSITION_PLAN.md 验收与 Review

> 本文是**验收记录**（verification record），不是设计文档 —— 不违反附录 D「不新增设计文档」。
> 验收人：独立复核（未参与执行）。时间：2026-10-11。基线：`master` @ `fe98833`。
> 原则：所有结论可被命令复现；不接受「提交信息说通过了」。

---

## 0. 结论摘要

| 门 | 结论 | 关键证据 |
|:--:|:--:|------|
| **G1** 授权不可绕过 | ✅ **通过（最强证据）** | 反向 `4 FAIL` @ `f6dcf20`；正向 `AUTHZ_RED=1` 7 PASS / 0 FAIL / 0 SKIP；`requireRED` 已移除 |
| **G2** 无死代码 | ⚠️ **部分通过** | 清单大部分已删；`handler.go`、`security_audit_test.go` 仅部分处置 |
| **G3** 文档无矛盾 | ❌ **未通过** | A.6 #1 正文矛盾仍在；AGENTS.md 仍称 40%；CLAUDE.md 数字过期 |
| **G4** 数字可复现 | ⚠️ **基本通过** | 覆盖率/迁移/构建目标已核实；个别数字仍互相矛盾 |
| **G5** 简历 3 人读过 | ⛔ **无法由本次验收判定（人工）** | 属人工任务 |

**总体判定：安全修复（Track A）质量高、证据链完整，可认定完成。但 Track B5（文档对齐）未达 G3 标准，且最后一个 commit（`fe98833`）越出计划范围改动了已发布的 migration，并弱化了验收探针——这两项是本次 review 的主要风险。**

补充事实：**计划并未 100% 执行完毕**。`C4`（投递）、`C5`（仓库门面/录屏）、`D1`、`D2` 仍未开始（TRANSITION_PLAN.md 复选框已如实保留为未勾选）。已完成的是：P0 三项、Track A 全部、Track B 全部、C0–C3。

---

## 1. G1 授权不可绕过 —— ✅ 通过

### 1.1 反向证明（证明测试确实在测东西）

在 A2 之前的提交 `f6dcf20` 上建 worktree 复跑：

```
git worktree add /tmp/sg-prefix f6dcf20
AUTHZ_RED=1 go test ./pkg/gateway/ -run 'TestManifest|TestSegment' -count=1
```

结果 —— **恰好 4 FAIL**，与计划预测一致：

```
--- FAIL: TestManifest_BogusPlaybackToken_Returns401
--- FAIL: TestManifest_BogusPlaybackToken_NoQuality_Returns401
--- FAIL: TestManifest_NoGatingRules_DeniesByDefault
--- FAIL: TestManifest_CallerSuppliedContract_Ignored
--- PASS: TestManifest_NoAuth_Returns401
--- PASS: TestSegment_NoToken_Returns401
--- PASS: TestSegment_BogusPlaybackToken_Returns401
```

### 1.2 正向证明（修复后）

```
AUTHZ_RED=1 go test ./pkg/gateway/ -run 'TestManifest|TestSegment' -count=1 -v
→ 7 PASS / 0 FAIL / 0 SKIP
go test ./pkg/gateway/ -run 'TestManifest|TestSegment' -count=1
→ ok  (默认运行不再有 SKIP)
```

`requireRED` 已删除：全仓仅剩 `authz_regression_test.go:208` 的一行说明性注释，无函数、无 `os` 依赖残留。

**G1 通过条件全部满足。**

---

## 2. Track A 代码复核（逐条读源码，非读测试）

| 任务 | 文件:行 | 复核结论 |
|:--:|------|------|
| A2 | `pkg/middleware/auth.go:57` | ✅ 判断已从 `c.Query("playback_token") != ""` 改为 `c.GetBool("playback_auth")`。token **存在性**不再是凭证。 |
| A3 | `pkg/middleware/playback_token_auth.go` + `routes.go:59-72` | ✅ 新中间件用 func 类型验证器（避免 import cycle）；**Gin 顺序正确**：`streamGroup` 在 `router.Use(JWT)`(:71) **之前**创建(:68)并挂 `playbackAuth`(:69)，随后 `streamGroup.Use(JWT)`(:72)、`Use(NFTGate)`(routes.go:220)。模板法链 = `[PlaybackTokenAuth, JWT, NFTGate]`，与计划要求一致。仅校验 query token，nil validator 时 fail-closed 401。 |
| A4 | `streaming_handlers.go:194-209` | ✅ 条件已拆开：**只要有 `playback_token` 就必须校验**；`quality` 只决定走 `:211` 分支。 |
| A5 | `streaming_handlers.go:196,244` + `auth_wallet.go:369-372` | ✅ 双侧守卫：handler 内空钱包 401；`GeneratePlaybackToken` 入口拒绝空钱包。 |
| A6 | `auth_wallet.go:455` | ✅ `walletAddress ...string` → 必填 `string`；`ParsePlaybackToken` 拆出；3 个非测试调用点（`streaming_handlers.go:200`、`:388`、`grpc_server.go:842`）全部显式传 wallet。 |
| A7 | `grpc_server.go:834-843` | ✅ 从 context 取 wallet，空则 `Unauthenticated`，再 `ValidatePlaybackToken(..., wallet)`。 |
| A8 | `nft_gate.go:200-207,233-247,309-317` | ✅ 三处 fail-open 全部反转：`Enabled=false → 503`；无规则 → 403（新增 `GatingMode`，默认 `closed`）；解析出错 → 500。 |
| A9 | `nft_gate.go:292-302` | ✅ `parseNFTParams` 不再读 query 的 `contract`/`token_id`/`chain_id`；授权主体只来自 `RuleResolver`。`:336-342` 的服务端首条规则回退**按计划保留**（未误删）。 |

**A9 说明**：计划允许「调试能力仅在 `config.DevMode` 下保留」，实现选择了完全删除，比计划更严格，可接受。

### 2.1 附加发现（非计划项，但影响可解释性）

- **ℹ️（P2）`nft_gate.go:197` 的 `config.Enabled.Store(true)` 会无条件覆盖调用方配置**。`routes.go:216` 先 `Enabled.Store(cfg.Features.NFTGating)`，紧接着 `:220` 构造中间件时被强制改写为 `true`。后果：`features.nft_gating: false` 被静默忽略，且 A8 新增的 `503 GATING_DISABLED` 分支经配置**不可达**。该行是既有代码（非 A8 引入），但 A8 让它的语义变重要了。建议：要么尊重配置，要么删掉该开关。
- **ℹ️（P2）播放令牌缺少类型/受众判别位**。`ParsePlaybackToken` 只靠 `claims.Subject == contentID` 区分令牌种类。登录 JWT 的 `Subject` 是钱包地址，会因不匹配而被拒（当前不可利用），但这是一个隐式契约。建议加显式 `token_type`/`aud` 声明。

---

## 3. G2 死代码 —— ⚠️ 部分通过

### 3.1 已核实删除

| 目标 | 状态 |
|------|:--:|
| `contracts/src/NFTGate.sol`、`StreamNFT.sol` | ✅ 已删（`forge test` 12/12 通过） |
| `pkg/web3/ipfs.go` + 引用 | ✅ 已删 |
| `pkg/gateway/{coverage,gateway_coverage,gateway_extended_coverage}_test.go` | ✅ 已删 |
| `test/load/load_test.go` | ✅ 已删 |
| `pkg/plugins/streaming/{hls,dash}.go`、`transcoder/scaler.go`、`worker/executor.go` | ✅ 已删 |
| `go.mod.bak-v1`、`go.sum.bak-v1` | ✅ 已删 |
| 字面量桩 `"#EXTM3U…#EXT-X-VERSION:3"` / `"<MPD></MPD>"` | ✅ 已从 `pkg/plugins/streaming/server.go` 移除（已注释说明） |

### 3.2 仅部分处置（⚠️）

1. **`pkg/plugins/streaming/handler.go` 仍在**（附录 B 明确点名 `:76`/`:98`）。现状：`:76` 改写空 body `[]byte{}`，`:98` 返回硬编码 `bitrates []int{1000,2500,5000}`——**仍是桩**，只是不再返回字面量 manifest。该 handler 有真实调用者（`server.go:86`）和 24KB 测试，故「删文件」不可行；但**计划要求的是删桩实现，实际只删了字面量**。建议显式记录「保留理由」，否则这条会在下次 review 被重新提起。
2. **`test/security/security_audit_test.go` 仅部分修复**。计划目标 `:248-272` 的恒真函数中：`checkDependencies()` 已删（改为指向 CI govulncheck）✅；但
   - `testRateLimitingCheck()` 仍 `return limiter.Allow("test")`，而同文件 `TestRateLimitingEnforcement` 自述 *"Rate limiter is a stub implementation - always returns true"* → **该检查恒真**；
   - `testAuthorizationCheck()` 用**局部字面量 map** 判 `hasUser && hasAdmin` → **恒真**；
   - `TestSecurityAudit` 仍以「≥90% 通过率」为断言，而 5 项检查中有 2 项恒真。
   建议：把这两项从 audit 中删掉或改成真实断言，否则「security audit」依然是凑数测试。

### 3.3 行数记录（G2 要求）

```
B4 单提交前后（非测试 Go 源码行）: 61,824 → 61,523   (−301)
全流程净变化 : 61,528  (基线 61,633,  net −105)
```
下降成立 ✓，但净额偏小——因为 Track A + B3 + 收尾提交新增了约 200 行。建议在计划中同时记录「删除 301 行」与「净 −105 行」两个数，避免「减法」叙事被误读。

覆盖率实测 **73.4%**（`go tool cover -func`，与 `CLAUDE.md:12` 的 ~73.4% 一致）。较基线 74.4% 略降，原因是删掉的 cover-churn 测试本身在虚增覆盖率——**这是净化，不是退步**。

---

## 4. G3 文档无矛盾 —— ❌ 未通过

A.6 表格中 9 项，核实结果：

| # | 项 | 状态 |
|:-:|------|:--:|
| 2 | `CLAUDE.md` 不再称 40% 门槛 | ✅ 已改 60%（:110） |
| 3 | 测试文件数 | ❌ **CLAUDE.md:12 仍写 261，实测 254**；「206 in-package tests under pkg/」实测 **203** |
| 4 | 微服务 main「各 10 行」 | ✅ 已改为「8 个 89 行 + api-gateway 118 行」口径 |
| 5 | 迁移数 52 | ✅ 已改 34 up / 18 down（实测 `.down.sql` = 18） |
| 6 | README 与 ARCHITECTURE 对监控栈矛盾 | ✅ 已改，Prometheus/OTel 标 `[~]` 并注明「scrape stack removed during simplification」 |
| 7 | CI 链「…security → docker」 | ✅ 已改，注明「security and docker are enforcing gates; coverage floor 60%」 |
| 8 | `CLAUDE.md` 引用 6 个不存在文件 | ✅ 已全部移除引用（现均为「not-mentioned」） |
| 9 | 10 build targets | ✅ 已改「11 build targets: 1 monolith + 9 microservices + cmd/learn」 |
| **1** | **README 内「stable 全实现」vs「开发中/骨架」** | ❌ **未修完**：badge 已改 `development-in-progress`（:14）✓，但正文 `:107` 仍写 `#### P0 — Core Flow (Stable)` + *"All items are implemented and tested"*，与 `:267` *"architecture skeletons … still being tightened into a real end-to-end path"* 直接冲突 |

### 4.1 额外矛盾（A.6 未列，本次发现）

- **`AGENTS.md:65` / `:81` 仍称 `40% coverage`**，与 README:138 / CLAUDE.md:110 的 60% 冲突。该文件头部自称 *"canonical source is kept in sync"*，实际未同步。相同问题也在 `.atomcode/skills/project-conventions/SKILL.md:28`。
- **README 内部数字不一致**：`:103` 称 `make deploy-microservices (16 services)`，`:128` 称 `+ 9 microservices`。二者至少有一个需要加限定语（容器数 vs 服务数）。

**判定：G3 未通过。** 单点修复质量高（#2/#5/#6/#7/#8/#9 干净），但 #1 的正文矛盾、以及 AGENTS.md 整份文档的滞后，使「文档无矛盾」不成立。

---

## 5. G4 数字可复现 —— ⚠️ 基本通过

| README/CLAUDE 声明 | 复现命令 | 实测 | 结论 |
|------|------|:--:|:--:|
| CI 覆盖率门槛 60% | `grep -A2 'Check coverage' .github/workflows/ci.yml` | `below 60% threshold` | ✅ |
| 34 migrations / 18 down | `ls migrations/*.down.sql \| wc -l` | 18 | ✅ |
| 11 build targets | `make build-all` | 11 个二进制（含 `bin/learn`） | ✅ |
| 9 microservices | `ls cmd/microservices` | 9 | ✅（但 README:103 的「16 services」需限定） |
| 覆盖率 ~73.4% | `go test ./... -coverprofile` + `go tool cover -func` | **73.4%** | ✅ |
| 测试文件数 261 | `find . -name '*_test.go' \| wc -l` | **254** | ❌ |

---

## 6. B6 / B7 —— ✅ 通过

- CI：`ci.yml:122` 门槛 = 60%；`build.yml:141` Trivy `exit-code: '1'`；Slither（`ci.yml:224`）**已无** `continue-on-error`；govulncheck 以报告模式运行（`:211-220`，符合 2026-10-11 修订）；`ci.yml:233` docker `needs: [test-go, build, security]` ✓。
- 残余 `continue-on-error: true` 仅出现在 4 处合理位置（upload coverage、install forge deps、gas report、govulncheck-report）。
- B7：`grep -rnE "minioadmin|dev-secret-key-not-for-production|streamgate_dev_password|streamgate_test_password|test-secret-key" config/ docs/` → **无命中**；`.env.example` 已建 ✓。

---

## 7. B3 链上内容登记 —— ✅ 通过（且比计划更完整）

- ABI 已与 Solidity 对齐：`registry.go:42` = `registerContent(string,string,string)`，与 `ContentRegistry.sol:51` 一致（原 `bytes32` 分歧已修）。
- **接线真实存在**：`gateway.go:68` → `if registry := ProvideContentRegistry(cfg, log); registry != nil { contentSvc.SetContentRegistry(registry) }`。`ProvideContentRegistry`（`providers.go:181`）对「地址/私钥/RPC 任一缺失」逐项降级返回 `nil`，符合「链不可达必须降级而非崩溃」。
- 集成测试：`test/integration/content_anvil_test.go` ✓。

> 注：`ContentRegistryBytecode = "0x"` 仍是占位符（`registry.go:89`），但已改为在 anvil entrypoint 里部署合约并注入地址，绑定不再依赖该常量——可接受。

---

## 8. 合约与构建 —— ✅ 通过

```
forge test  → 2 suites, 12 passed, 0 failed, 0 skipped
              DemoNFTTest 7/7 (setTokenURI 权限、ERC-165 诚实声明、approve 家族)
              ContentRegistryTest 5/5
go build ./...   → exit 0
go vet ./...     → exit 0
golangci-lint run ./pkg/... → 0 issues
make build-all   → exit 0 (11 binaries)
go test ./...    → 全部 ok，0 FAIL（约 2 分钟）
```

B1/B2 对应合约改动已被 forge 测试覆盖：`testSupportsInterface_EnumerableNotClaimed`、`testSetTokenURI_ByNonOwnerReverts`、`testUnapprovedTransferReverts` 均为真断言。

---

## 9. Track C —— ✅ 产物齐备，内容纪律合格

- `docs/resume/RESUME_CN.md`、`RESUME_EN.md`、`PROJECT_TALK_SCRIPT.md` 均已产出。
- **内容纪律抽查**：在简历正文中检索 `150K / 74.4% / 微内核 / 10 microservices / v1.0.0-stable` —— 仅命中第 3 行的**自我约束说明**（"不写 150K 行…"），正文无违禁声明 ✅。
- `docs/_recovered/README.md` 给出逐文件质量判断表：**8/8 判定为「实质」**，并主动标注了 3 类风险（过时文件路径、与附录 C 方向优先级不一致、「10+ 年 C++」与「6 年音视频」口径冲突）。这份 triage 是本次交付里质量最高的一份文档——它披露了自己的不确定性。
- C3 的 6 问 3 层结构完整，Q3（AES-128 ≠ DRM）表述正确，明确写了「密钥交给客户端 / DRM 靠硬件信任根」，未犯类别错误 ✅。

---

## 10. ⚠️ 主要风险：收尾提交 `fe98833` 越出计划范围

该提交（13 文件 / +175 −34）中有两处**计划未要求、且不可逆**的改动。

### 10.1 🔴 P1：就地修改**已发布**的 migration

```diff
 migrations/021_create_gating_rules_table.sql | 4 +-
-    content_id UUID NOT NULL,
+    content_id VARCHAR(255) NOT NULL,

 migrations/033_add_fk_and_indexes.sql        | 5 +-
-ALTER TABLE content_gating_rules
-    ADD CONSTRAINT fk_gating_rules_content
-    FOREIGN KEY (content_id) REFERENCES contents(id) ON DELETE CASCADE;
+-- FK to contents(id) removed (2026-10-11): …
```

**为什么这是 P1：**

1. 迁移器**按版本跳过已应用项**（`pkg/storage/migrate.go:86`: `if applied[f.Version] { continue }`，表 `schema_migrations`）。
2. 全链栈用的是**持久命名卷**（`docker-compose.fullchain.yml:17`: `fc-postgres-data:/var/lib/postgresql/data`），只有 `down -v` / `fullchain-teardown --volumes` 才清空。
3. 因此：**任何已跑过 021/033 的环境（开发者本机的旧卷、任何已部署实例）都不会生效**——列仍是 `UUID`、FK 仍在。此时种 `content_id='demo'` 的规则会失败（`invalid input syntax for type uuid: "demo"`，或 FK 违约 → 500）。而**全新库**则是 `VARCHAR` 无 FK。
4. 结果：**同一份代码在不同环境产生不同 schema**，且「demo 门控种规则」只在全新卷上可用。

**符合计划纪律的做法**是新增 `035_alter_gating_rules_content_id.sql`：`ALTER TABLE … ALTER COLUMN content_id TYPE VARCHAR(255)` + `DROP CONSTRAINT IF EXISTS fk_gating_rules_content`。这样对全新库与既有库都收敛到同一状态。

> 这也直接违反计划使用说明第 4 条：「遇到与本文档描述不符的代码：停止，报告差异，不要『顺手改对』」。

### 10.2 🔴 P1：A8 第 4 项（demo 手动播放保活）未满足

计划 A8 明确要求：*「需在验收栈启动时为 `demo` 内容种一条服务端 gating rule…手动播放路径必须保持可用或显式标注不可用」*。

实测：**种子逻辑只存在于验收探针内部**（`scripts/h5-demo-acceptance/specs/01-index.mjs:59-85`，在 `page.evaluate` 里 POST `/api/v1/content/demo/gating-rules`）。**栈启动路径（compose entrypoint / Makefile target）没有种任何规则**；`make h5-demo-acceptance`（`Makefile:539`）只是对已运行的栈跑 Playwright。

后果：fail-closed 之后，人在 h5-demo 上点 Play → `403 GATING_REQUIRED`。而 `docs/resume/PROJECT_TALK_SCRIPT.md:127` 的演示脚本第 4 步仍写着 *「持有者拉 manifest → **200**，内嵌 playback_token」*，且**没有种子步骤**。这条演示脚本在面试现场会当场失败（或需要即兴补救）。

### 10.3 🔴 P1：同一提交里**放宽**了验收探针

```diff
 scripts/h5-demo-acceptance/lib/common.mjs | 7 +++
+            // 403 on streaming manifest is the EXPECTED fail-closed denial …
+            if (text.includes('403') && url.includes('/streaming/') && url.includes('/manifest.m3u8')) return;
+            // 401 on streaming manifest is the EXPECTED result of the 01-index …
+            if (text.includes('401') && url.includes('/streaming/') && url.includes('/manifest.m3u8')) return;
```

把 401/403 判为「预期错误」本身合理（fail-closed 是目标行为）。**但问题在于覆盖面**：检索全部 5 个 spec，与 manifest 相关的断言**只有一条**——`manifest-unauth-401`（未认证 → 401）。

也就是说，**没有任何一条验收断言证明「合法持有者能拿到 200 + 可用 token + 拉到 segment」**。因此「5/5 通过」在 A8 之后**不再覆盖受保护播放这条主链路**；恰好在这条链路被改动的同一个提交里，它的失败信号又被拉黑了。这正是「零假通过」要防的情形。

**建议补一条正向探针**：持 NFT 的 demo 钱包 → manifest 必须 200 且含 `playback_token` → 带 token 拉 segment 必须 200（并在种规则后执行）。

> 附：最后一次绿色报告 `reports/2026-10-11T00-38-18`（`stack_passed: true`，5/5）时间戳为 00:38 UTC，而收尾提交时间为 00:58 UTC —— **该绿灯早于最终提交约 19 分钟**，且此前 00:14 / 00:19 / 00:29 三轮均为 `01-index` 失败。故这次绿灯**不能**作为 `fe98833` 最终状态的证据。

---

## 11. 无法由本次验收判定的项（如实标注）

| 项 | 原因 |
|------|------|
| **G5**（3 个真人回答「你会面这个人吗」） | 纯人工，无产物可查 |
| **C4**（投递 ≥15 份 / J1–J3） | 人工任务，计划已勾为未完成 |
| **C5**（2 分钟录屏 + README 首屏） | 未开始 |
| **D1/D2** | 计划要求人工确认方向后才可开始，未开始 |

---

## 12. 整改建议（按优先级）

| 优先级 | 事项 | 具体动作 |
|:--:|------|------|
| **P0** | migration 收敛 | 新增 `035`：`ALTER COLUMN content_id TYPE VARCHAR(255)` + `DROP CONSTRAINT IF EXISTS fk_gating_rules_content`；**恢复 021/033 原样**，避免新老环境 schema 分叉 |
| **P1** | demo 播放保活 | 在 fullchain 栈启动时（entrypoint 或专门 target）种子 `demo` 门控规则；或改 `PROJECT_TALK_SCRIPT.md:127` 显式标注「需先种规则」 |
| **P1** | 补正向探针 | 新增 spec 断言「持有者 manifest 200 + token + segment 200」；或回滚 `common.mjs` 对 403 的拉黑，改由断言显式覆盖 |
| **P2** | 文档对齐收尾 | ① `CLAUDE.md:12` 改 254 / 203；② `AGENTS.md:65,81` 40%→60%（或把 AGENTS.md 降级为纯指针）；③ 消解 `README.md:107` 与 `:267` 的矛盾；④ README `:103` 的「16 services」加限定语 |
| **P2** | 门控开关 | `nft_gate.go:197` 尊重 `cfg.Features.NFTGating`，或删除该配置项 + 删除不可达的 503 分支 |
| **P2** | 测试净化 | `test/security/security_audit_test.go` 移除 2 个恒真检查（`testRateLimitingCheck` / `testAuthorizationCheck`） |
| **ℹ️** | 记录保留理由 | 为 `pkg/plugins/streaming/handler.go` 的保留写一行理由（有真实调用者 + 桩体已清），避免下次 review 重复提出 |

---

## 13. 复核命令清单（可逐条重跑）

```bash
# G1 反向（应 4 FAIL）
git worktree add /tmp/sg-prefix f6dcf20 && cd /tmp/sg-prefix
AUTHZ_RED=1 go test ./pkg/gateway/ -run 'TestManifest|TestSegment' -count=1
cd - && git worktree remove --force /tmp/sg-prefix

# G1 正向（应 0 FAIL / 0 SKIP）
AUTHZ_RED=1 go test ./pkg/gateway/ -run 'TestManifest|TestSegment' -count=1 -v

# 构建 / 测试 / lint
go build ./... && go vet ./... && golangci-lint run ./pkg/... && make build-all
go test ./... -coverprofile=/tmp/cov.out && go tool cover -func=/tmp/cov.out | tail -1

# 合约
cd contracts && forge test

# G2 行数
find . -name '*.go' -not -name '*_test.go' -not -path './contracts/*' | xargs cat | wc -l

# B7 凭据
grep -rnE "minioadmin|dev-secret-key-not-for-production|streamgate_dev_password|test-secret-key" config/ docs/

# G3 数字
grep -nE "261 test files" CLAUDE.md ; find . -name '*_test.go' | wc -l
grep -nE "40% coverage" AGENTS.md

# 迁移版本跳过语义
grep -nE "applied\[f.Version\]" -A 3 pkg/storage/migrate.go
```

---

*本记录所有结论均可由上表命令复现。若与执行方叙述不符，以命令输出为准。*

---

## 14. 整改记录（2026-10-11，执行方复核 @ `685ebf6`）

按 §12 清单逐项整改；每项附可复现证据。

### P0 — migration 收敛 ✅

- `migrations/021` / `033` **已恢复原样**（`021:3` = `content_id UUID NOT NULL`；`033` FK 约束已恢复，grep `fk_gating_rules_content` = 1 命中）。
- 新增 `migrations/035_alter_gating_rules_content_id.sql`（`DROP CONSTRAINT IF EXISTS` + `ALTER COLUMN TYPE VARCHAR(255) USING content_id::text`）与配套 `.down.sql`。
- **收敛实测**（全新库，迁移链 001→035 全跑）：`information_schema.columns` 显示 `content_id = character varying` —— 新老环境收敛到同一 schema。

### P1-a — demo 播放保活 ✅

- `scripts/docker-deploy.sh` 新增 **[4b/5]** 步：服务健康后经 `psql` 以 `WHERE NOT EXISTS` 幂等种子 `content_id='demo' → DemoNFT @31337` 规则；种子命令已在实测栈上执行成功（`INSERT 0 1`）。
- `docs/resume/PROJECT_TALK_SCRIPT.md` 演示第 3/4 步已改为诚实口径（未认证 401 / 无 NFT 403 / 持有者过门控——已转码 200+token，未转码 404 CONTENT_NOT_FOUND）。

### P1-b — 正向探针 ✅

- `common.mjs` 的全局 401/403 拉黑已撤销，改为 **transient 标记**（报告中可见、注记 expected fail-closed denial，不改变 spec 失败判定）。
- 新增 **`manifest-holder-passes-gate`** 正向差分断言：持 minted DemoNFT 的钱包 → manifest 必须越过门控（无分片时 404 CONTENT_NOT_FOUND；有分片时 200 且含 `playback_token=`），否则 fail。
- **提交后新鲜报告**：`reports/2026-10-11T01-38-13`（晚于 `685ebf6`），**6/6 specs、0 硬错误**；01-index 12/12 check 全过，含 `manifest-unauth-401` + `manifest-holder-passes-gate` 差分（401 未经认证 / 404 已过门控）。
- 200 + segment 全链路需要真实转码分片，属 D1/演示准备（上传+转码样例视频），已在讲述稿中标注为演示前置——非本验收范围。

### P2 — 文档与代码 ✅

- `CLAUDE.md:12`：**254 test files / 203 in-package**（2026-10-11 实测 `find` 口径）。
- `AGENTS.md:65,81` 与 `.atomcode/skills/project-conventions/SKILL.md:28`：40% → **60%**（grep `40% coverage` = 0 命中；atomcode 该文件维持原 untracked 状态，磁盘已修正）。
- `README.md` Roadmap:`P0 — Core Flow (Implemented, hardening)` / `P1 — (Mostly complete)`，与 Project Status 一节不再冲突；`:103` 改为 `(10 service binaries, 16 containers)`。
- `nft_gate.go` 的 `Enabled.Store(true)` 保留（构造即启用的默认语义，测试依赖），但 **`routes.go` 现在在构造之后应用 `cfg.Features.NFTGating`**——flag=false 时门真实关闭，A8 的 `503 GATING_DISABLED` 分支可达。
- `security_audit_test.go`：`testRateLimitingCheck` / `testAuthorizationCheck` 两个恒真检查及其表项已删（余 3 项均为真实断言，TestSecurityAudit 通过）。
- `pkg/plugins/streaming/handler.go` 的残余桩（空 body segment 处理器、硬编码 info 处理器）连同路由与测试**全部删除**——附录 B 该项不再「部分处置」。
- TRANSITION_PLAN 附录 B 补记 G2 两个口径：B4 单提交 **−301** 行、全流程净 **−105** 行。

### 复核命令复跑结果（2026-10-11，@ `685ebf6`）

```
AUTHZ_RED=1（正向）      → ok，7 PASS / 0 FAIL / 0 SKIP
golangci-lint ./pkg/...  → 0 issues
CLAUDE.md "261"          → 0 命中；find *_test.go → 254
AGENTS.md "40% coverage" → 0 命中
migrations 021 UUID      → 原样；033 FK → 原样；035 存在（up+down）
B7 凭据 grep             → 仅本验收记录自引的命令文本（非泄漏）
h5 acceptance            → 6/6 specs, 0 硬错误（报告 2026-10-11T01-38-13，晚于最终提交）
```

仍未完成（人工/计划内未开始，维持 §11 标注）：G5、C4、C5、D1、D2。
