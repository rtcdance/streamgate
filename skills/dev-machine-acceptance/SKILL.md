---
name: "dev-machine-acceptance"
description: "Verify a machine can run StreamGate's Docker-first acceptance (make verify-docker stages), check CPU/RAM/disk/toolchain/ports, stop SRS ABR when sharing the 118 dev box, and produce raw evidence. Use before acceptance, when onboarding a machine, or when asked 开发机够不够用/机器配置/验收环境."
---

# Skill: dev-machine-acceptance

## Trigger

Use this skill when:

- 新开发机、换机、远程开发机，准备开发或验收 StreamGate。
- 有人问“这台机器够不够跑本服务”“机器配置够不够”。
- 准备执行 `make verify-docker`、`make test-anvil`、`make contracts-test`、`make h5-demo-acceptance`、`make fullchain-deploy`。
- 开发机与 SRS ABR 共用（尤其是 `prod-srs`，SSH alias；IP 见本机 `~/.ssh/config`）。

## 0. 硬规则：Docker 是验收环境

StreamGate 的验收门禁是 **`make verify-docker` 的 stage** 或 **CI**：

- **禁止**把 host 上的 `make test` / `go test` / `go build` 当作验收证据；它们只是快速预检。
- **禁止**用 Mac 本机作为验收环境。
- `make verify-docker VERIFY_STAGE=all` 要求 **clean candidate**（无未提交/未跟踪改动），并且 `VERIFY_MODE=candidate`。
- Docker runner 只需要 host 的 **Shell + Git + Docker**；Go/Foundry/Node 是 host 快速编辑用的，不是验收前置。
- 所有 stage 日志落在 `.verify-artifacts/logs/<stage>.log`。

## 1. 容量门槛（本项目口径）

| 用途 | 最低 | 推荐 | 依据 |
| --- | --- | --- | --- |
| host 快速预检（`go build` / `make test` / `make lint`） | 4C / 8G | 8C / 16G | 全量 race + coverage 较重 |
| Docker `unit` stage | 4C / 8G | 8C / 16G | race + coverage + forge build |
| 单 stage（chain/durability/ops/faults/media/recovery） | 4C / 16G | 8C / 32G | 同时起 PG/Redis/MinIO/NATS/Anvil + 多服务 |
| `perf` / `release` | 8C / 16G | 8C / 32G | 压测 + 多镜像构建 |
| `make verify-docker VERIFY_STAGE=all` | 8C / 32G | 16C / 64G | 17 个 compose 服务 + 9 stage 串行 |
| E14 类性能/soak 参考 | 8C / 16G 参考机 | 独立 Linux | 共享机 ffmpeg 负载会污染数字 |

磁盘：

- 至少 **100GB 空闲 SSD**；Docker data-root、Go cache、`.verify-artifacts` 都放大盘（`/home`）。
- Playwright 浏览器约 290MB；MinIO/NATS/PG 数据卷、镜像和 9 个服务构建产物另计。
- 不要用只剩 20–30G 的根盘跑 `all`。

系统：

- Linux 优先；验收数字只认 Linux。
- macOS 可开发，但 Docker VM 至少 8GB；性能数字不能与参考机对比。
- 共享 118 上有 SRS/ffmpeg 负载时，先停 ABR，且 **不要** 在负载期跑 perf/release。

### 实测样例：`prod-srs`（2026-10-20）

```text
12 vCPU / 31GiB / AlmaLinux
/     70G，剩 23G
/home 379G，剩 371G
无 Go、无 Docker/Podman
停 ABR 前 load 4.97（ffmpeg 267% CPU）；停后 load 1.25
```

结论：CPU/内存够；补 Go/Docker 并把 data-root 放 `/home` 后适合开发与单 stage；`all`/perf 建议独立机器。

## 2. 预检（只读）

```bash
bash skills/dev-machine-acceptance/scripts/preflight.sh
```

或手工核对：

```bash
nproc; free -h; df -h / /home
docker version --format '{{.Server.Version}}'; docker compose version
git --version; go version; forge --version; anvil --version; ffmpeg -version | head -1
node --version; python3 --version
git submodule status contracts/lib/forge-std contracts/lib/openzeppelin-contracts
ss -tlnp | grep -E ':(8080|18000|5432|6379|9000|4222|8545|1926|1927|1937|8088|8089|9090)\b'
```

端口纪律：

| 端口 | 归属 | 处理 |
| --- | --- | --- |
| 1926 / 1927 / 1937 / 8088 / 8089 / 9090 | SRS origin/edge/ingest/nginx | 跑全栈前先停 ABR（§3），不要停 nginx |
| 8080 | StreamGate monolith | 冲突时改 config/端口 |
| 18000 | h5-demo | 冲突时改 compose 端口 |
| 5432 / 6379 / 9000 / 4222 / 8545 | PG / Redis / MinIO / NATS / Anvil | 确认 Docker 未占用或被别的栈占用 |

## 3. 与 SRS ABR 共用 118 开发机

### 3.1 识别

```bash
ps -eo pid,ppid,pcpu,args | grep -E 'ffmpeg|objs/srs|abr_|rsrc|s0a' | grep -v grep
cd /home/srs-abr/srs/trunk && bash prod_manage.sh status
```

### 3.2 停止（顺序）

1. 备份 crontab：`crontab -l > /root/crontab.backup.$(date +%Y%m%d-%H%M%S)`
2. 注释 ABR cron（用 awk 写临时文件再 `crontab file`；不要用 `crontab -l | sed ... | crontab -`）。
3. 停 `abr_dev_stream.sh`（先父后子）。
4. 停 `rsrc_supervisor.sh`，再停 `src_resilience_proxy.mjs`。
5. `cd /home/srs-abr/srs/trunk && bash prod_manage.sh stop all`。
6. 清残留：按 `/proc/<pid>/cmdline` 精确匹配 `b001` / `abrh` / `filter_complex` / `plubb` 的 ffmpeg，以及 `s0a_session_probe`。
7. 验证：`prod_manage.sh status` 四项 STOPPED；1926/1927/8088/8089 释放；`uptime` 负载下降。

一键脚本（默认 dry-run，`--yes` 执行）：

```bash
bash skills/dev-machine-acceptance/scripts/stop-srs-abr.sh
bash skills/dev-machine-acceptance/scripts/stop-srs-abr.sh --yes
```

**不要停**：nginx、`srs.ingest.obs.conf`（1937）、产品 `srsmedia`。

### 3.3 恢复

```bash
cd /home/srs-abr/srs/trunk
bash prod_manage.sh start all
bash full_chain_restart.sh
crontab /root/crontab.backup.<ts>
```

## 4. 验收分层

| 层 | 目的 | 命令 | 不能证明什么 |
| --- | --- | --- | --- |
| L0 机器预检 | 容量/工具链/端口 | `preflight.sh` | 不能证明功能 |
| L1 host 快速预检 | 编译/格式/lint/单测 | `go build ./cmd/monolith/streamgate`、`make fmt`、`make lint`、`make test` | 不是验收 |
| L2 Docker unit | 编译/race/coverage/forge/vet/ABI | `make verify-docker VERIFY_STAGE=unit` | 不覆盖真实链路 |
| L3 Docker 功能 stage | chain/durability/ops/faults/media/recovery | 单 stage 命令 | 不覆盖全部组合 |
| L4 full candidate | 九 stage 串行 | `make verify-docker VERIFY_STAGE=all`（clean candidate） | 不替代 CI 同 SHA |
| L5 CI | 同 SHA 的 CI/build | GitHub Actions run | — |

**判定规则：功能只有在 L2+ 有 stage 日志/工件时才算 verified；host 结果不算。**

## 5. 核心功能验收矩阵

| ID | 核心能力 | 主要 stage / 命令 | 通过证据 |
| --- | --- | --- | --- |
| F01 | 钱包登录：EIP-191/712/SIWE、Solana ed25519 | `unit` + `chain` + `make test-anvil` | `unit.log`、`chain.log` |
| F02 | NFT 验证：ERC-721/1155、ERC-165、safe/finalized | `chain` + `make test-anvil` | `chain.log` |
| F03 | 门禁 fail-closed：无规则 403、resolver 错误 500、disabled 503 | `unit` + `media` | `unit.log`、`media.log` |
| F04 | HLS manifest/segment 鉴权 + playback token | `media` | `media.log` |
| F05 | FFmpeg 转码 / HLS 输出 | `media` + `durability` | `media.log` |
| F06 | 上传 MinIO/S3 | `media` + `durability` | `media.log` |
| F07 | 重组 / pinned block（safe/finalized） | `chain` | `chain.log` |
| F08 | 多 RPC failover / 端点归因 | `chain` + `faults` | `chain.log`、`faults.log` |
| F09 | Redis challenge 原子消费 / 防重放 | `unit` + `durability` | `unit.log` |
| F10 | monolith vs 9 microservices | `release` + `fullchain-test` | `release.log` |
| F11 | h5-demo 端到端 | `make h5-demo-acceptance` | `scripts/h5-demo-acceptance/reports/` |
| F12 | 迁移 fresh/up/down（数量以 `ls migrations` 为准） | `durability` + `ops` | `durability.log`、`ops.log` |
| F13 | 性能 / soak | `perf` | `perf.log` |
| F14 | 镜像构建 / release | `release` | `release.log` |
| F15 | 安全 / Trivy / 供应链 | `release` + CI | CI run / `release.log` |

补充命令：

```bash
make contracts-test                 # Foundry 合约测试
make test-anvil                     # Anvil 端到端
make h5-demo-acceptance             # Playwright 五页冒烟
make fullchain-test                 # 全链 acceptance
make test-ci                        # 60% coverage floor
```

## 6. 证据

- `.verify-artifacts/logs/<stage>.log`（每个 stage 的原始日志）
- `.verify-artifacts/candidate.json`（clean candidate 的 SHA/镜像）
- `.verify-artifacts/coverage.out`
- `scripts/h5-demo-acceptance/reports/<timestamp>/`
- `TRANSITION_PLAN.md` §8（最终 `all` 记录）
- CI run URL / run id（同 SHA）

报告只写三种：`PASS`、`PASS WITH BOUNDARIES`（列边界）、`BLOCKED`（列缺失）。不要手改 `.verify-artifacts`。

## 7. 判定矩阵

| 配置 | host 预检 | 单 stage | `all` | 判定 |
| --- | --- | --- | --- | --- |
| 4C / 8G | 可以 | unit 可以，其他紧张 | 否 | 开发够，验收不够 |
| 4C / 16G | 可以 | 可以 | 紧张 | 最低单 stage |
| 8C / 16G | 推荐 | 推荐 | 可跑但慢 | 推荐 |
| 8C / 32G | 推荐 | 推荐 | 推荐 | 完整验收 |
| 12C / 31G 共享 118 | 可以 | 停 ABR 后可以 | 不建议 | 开发/单 stage，性能另找机器 |

## Must Not

- 不把 host `make test` / `go test` 当验收；不把 Mac 当验收环境。
- 不在 dirty tree 上跑 `VERIFY_STAGE=all`（脚本会拒绝，别绕过）。
- 不在 SRS/ffmpeg 负载期跑 perf/release。
- 不用 `pkill -f srs` / `pkill -f ffmpeg` 宽匹配；用 §3 的脚本。
- 不停 nginx、`srs.ingest.obs.conf`、产品 `srsmedia`。
- 不手改 `.verify-artifacts` 或把 host 结果混进证据。

## Exit Criteria

- 预检无 FAIL（WARN 列出）。
- 目标 stage 全部 exit 0；日志在 `.verify-artifacts/logs/`。
- 声称完整验收时：`VERIFY_STAGE=all` 在 clean candidate 上通过，且记录 SHA/CI run。
- 若共用 118：ABR 已停止并验证端口/负载；验收后按约定恢复。
- 报告写 `PASS` / `PASS WITH BOUNDARIES` / `BLOCKED`。
