---
name: "dev-machine-acceptance"
description: "Set up and verify StreamGate's Docker dev/test stack (docker-compose.fullchain.yml) and Docker-first acceptance (make verify-docker stages, fullchain-test) on the local macOS Docker Desktop machine, check CPU/RAM/disk/toolchain/ports, and produce raw evidence. Use when onboarding a machine, before acceptance, or when asked 开发机够不够用/机器配置/验收环境/搭建 Docker 环境."
---

# Skill: dev-machine-acceptance

## Trigger

Use this skill when:

- 在**本机**（macOS + Docker Desktop）搭 Docker 开发/自测/部署验收环境：见 §0.1。这是默认环境。
- 有人问“这台机器够不够跑本服务”“机器配置够不够”。
- 准备执行 `make verify-docker`、`make test-anvil`、`make contracts-test`、`make h5-demo-acceptance`、`make fullchain-deploy`。
- 远程 `prod-srs` 的 Docker 环境已于 2026-10-23 清空，不再用于验收；历史流程仅保留在 `remote-dev-machine` 供查证。

## 0. 硬规则：Docker 是验收环境

StreamGate 的验收门禁是 **`make verify-docker` 的 stage** 或 **CI**。**本机 Docker Desktop 是默认的开发和功能验收环境**：

- **禁止**把 host 上的 `make test` / `go test` / `go build` 当作验收证据；它们只是快速预检。
- Mac 本机的 Go/Foundry 结果同样只是预检；但 **Docker Desktop 内的 stage 日志是有效的功能验收证据**（L2/L3）。
- **性能数字和最终候选的 `all`** 仍以 Linux/CI 为准：macOS 的 Docker VM 与宿主共享 CPU，不能外推为容量或回归基线；本机只跑单 stage / 受影响门（§9）。
- `make verify-docker VERIFY_STAGE=all`（以及 `perf`、`release`）在本机会被 **EFF-00 环境门直接拒绝**：需要原生 linux/amd64、≥8 CPU、≥32 GiB、Docker 文件系统 ≥100 GiB、同 daemon 无其他验收。拒绝发生在工具镜像构建之前，不是失败重试的对象；本机请改用定向 stage。
- 正式运行（`all`/`perf`/`release`）会先把已批准的 HEAD **冻结克隆**到运行工件再挂载，编辑目录的改动不会进入验收；同一 daemon 由固定名网络锁串行化，第二个验收在构建前被拒绝（`VERIFY_MUTEX_TAKEOVER=1` 只在属主无运行容器时接管）。
- 共享 Go 编译缓存默认关闭（`VERIFY_SHARED_GOCACHE=1` 才启用）：GOCACHE 也存测试结果，未完成冷/热与 `-count=1` 复跑验证前不得默认开启。
- `make verify-docker` 的 Docker runner 只需要 host 的 **Shell + Git + Docker**；Go/Foundry/Node 是 host 快速编辑用的，不是它的前置。host-run 的 `fullchain-test` 另需 `cast`/`ffmpeg`/`python3`/`curl`（§0.1）。
- 所有 stage 日志落在 `.verify-artifacts/logs/<stage>.log`。

## 0.1 本机 Docker 开发/测试环境（Docker Desktop）

适用于 macOS/Windows 工作站：host 只做编辑与客户端，服务全部跑在 `docker-compose.fullchain.yml`（17 个容器，全部带 healthcheck）。

前置：

- Docker Desktop VM ≥ 8GB / 6 vCPU；8GB VM 上镜像构建必须串行，否则会 OOM（见 §1）。
- host 客户端工具：`cast`、`ffmpeg`/`ffprobe`、`python3`、`curl`——`fullchain-test` 缺一即 FAIL。
- `deploy/docker/{streamgate,api-gateway}-linux` 必须先构建；两个 quick Dockerfile 直接 COPY，部署脚本不会代建。

```bash
GOOS=linux GOARCH=$(go env GOARCH) CGO_ENABLED=0 go build -o deploy/docker/streamgate-linux ./cmd/monolith/streamgate
GOOS=linux GOARCH=$(go env GOARCH) CGO_ENABLED=0 go build -o deploy/docker/api-gateway-linux ./cmd/microservices/api-gateway
make fullchain-deploy        # 或 docker compose -f docker-compose.fullchain.yml up -d
```

启动后先核对（全部 healthy 再谈验收）：

```bash
docker ps --filter name=sg-fc --format 'table {{.Names}}\t{{.Status}}'
curl -fsS localhost:18080/health; curl -fsS localhost:28080/health
curl -fsS -o /dev/null -w '%{http_code}\n' localhost:18000/demo/
```

端口：

| host 端口 | 服务 |
| --- | --- |
| 18080 / 19090 | monolith HTTP / gRPC |
| 28080 | api-gateway |
| 18000 / 18001 | h5-demo（monolith / gateway 视图） |
| 18545 | Anvil 31337（DemoNFT + ContentRegistry） |
| 15432 / 16379 / 19000-19001 / 14222 / 28500 | PG / Redis / MinIO / NATS / Consul |

验收命令：

```bash
VERIFY_STAGE=unit make verify-docker          # L2
GRPC_TARGET=localhost:19090 make fullchain-test   # 71 项全链功能
```

本机踩坑（均为实测）：

- 镜像构建时 6 个 `go build -a` 并发会在 8GB VM 上被 OOM killer 反复杀死，表现为长时间无输出；逐个服务 `docker compose -f docker-compose.fullchain.yml build <svc>` 即可（每个约 1 分钟）。串行构建完成后用 `./scripts/docker-deploy.sh`（**不带** `--build`）启动，避免 compose 再次并行构建。
- `make verify-docker` 构建镜像时默认 `GOPROXY=direct`，受限网络会 `gnutls_handshake() failed`；用 `VERIFY_GOPROXY=https://goproxy.cn,direct make verify-docker`。
- `make fullchain-deploy` 会留下 `deploy/docker/*-linux`；`verify-docker` 的 prebuild 以 `generated binary already exists` 拒绝启动。跑验收前先 `rm -f deploy/docker/*-linux`（生成物，已被 `.gitignore` 忽略）。
- 启动报 `runtime code hash mismatch`：solc 把 forge 的绝对路径 remapping 写进 metadata，host 与 `/contracts` 编出的字节码不同。`contracts/foundry.toml` 的 `bytecode_hash = "none"` 保证可复现；改合约后重新生成 `deployments/content-registry.<chain>.json`（`make contracts-manifest-anvil`，或用 `scripts/registry-manifest.sh --rpc/--address` 指向正在运行的栈）。
- `fullchain-test` 的 gRPC 四条在 host 上失败：默认 `GRPC_TARGET=monolith:9090` 只在 compose 网络内可解析；用 `localhost:19090`。

## 1. 容量门槛（本项目口径）

| 用途 | 最低 | 推荐 | 依据 |
| --- | --- | --- | --- |
| host 快速预检（`go build` / `make test` / `make lint`） | 4C / 8G | 8C / 16G | 全量 race + coverage 较重 |
| Docker `unit` stage | 4C / 8G | 8C / 16G | race + coverage + forge build |
| 单 stage（chain/durability/ops/faults/media/recovery） | 4C / 16G | 8C / 32G | 同时起 PG/Redis/MinIO/NATS/Anvil + 多服务 |
| `perf` / `release` | 8C / 16G | 8C / 32G | 压测 + 多镜像构建 |
| `make verify-docker VERIFY_STAGE=all` | 8C / 32G | 16C / 64G | 17 个 compose 服务 + 9 stage 串行 |
| E14 类性能/soak 参考 | 8C / 16G 参考机 | 独立 Linux | 共享机 ffmpeg 负载会污染数字 |
| 本机 Docker Desktop | 8GB VM / 6 vCPU（单 stage + 串行构建） | 16GB VM / 8 vCPU | 6 个并发 `go build -a` 会 OOM；`all`/perf 另找机器 |

磁盘：

- 至少 **100GB 空闲 SSD**；本机实测 485GB 可用，Docker VM 磁盘镜像按需增长。
- Playwright 浏览器约 290MB；MinIO/NATS/PG 数据卷、镜像和 9 个服务构建产物另计。
- 每轮开始前删除上一轮 `sg-*` 镜像（`docker rmi` + `docker image prune -f`），但**不要** prune builder cache（预热层是资产）。

系统：

- 本机 macOS + Docker Desktop 是默认开发/功能验收环境；**性能数字只认 Linux/CI**，不能拿本机 VM 数字做回归比较。
- 8GB VM 上镜像构建必须串行（§0.1）；不要在本机跑 `VERIFY_STAGE=all`、`perf`、`release` 的完整矩阵。
- 远程 `prod-srs` 的 Docker 已于 2026-10-23 清空，不再作为验收环境。

### 本机实测（2026-10-23，Docker Desktop）

```text
宿主：10 vCPU / 16GiB / macOS，磁盘可用 485GB
Docker VM：6 vCPU / 8GiB / overlayfs
```

结论：适合 `unit`、`chain`、`durability`、`ops`、`faults`、`media`、`recovery` 单 stage 与
`fullchain-deploy` + `fullchain-test`；构建按服务串行。`all`/perf 仍走 Linux/CI。

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

## 3. 与 SRS ABR 共用 118 开发机（历史，2026-10-23 起不再使用）

> **已停用：** `prod-srs` 上的 Docker 环境已清空，验收改在本机 Docker Desktop。
> 本节仅保留为历史操作记录，不要在当前流程中执行。

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
- `fullchain-test` 日志（建议 `tee .verify-artifacts/logs/fullchain-acceptance.log`）
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

## 8. 验收运行经验（Linux/Docker 实测，2026-10）

harness 自身的坑（每条都有真实失败现场，先查这里再怀疑产品）：

- **静态目录权限**：`mktemp -d` 是 `0700 root`；nginx worker 以非 root 运行，穿不过目录会**对每个请求返回 403**。以容器内非 root 服务提供宿主目录时，创建后立即 `chmod 755`。失败形态：`H5 proxy did not become ready`，而眼睛看 `index.html` 明明存在。
- **`set -o pipefail` + `docker logs … | grep -q …` 会假阴性**：日志超过管道缓冲（约 64KB）且匹配点在前面时，`grep -q` 命中即退出，`docker logs` 收到 SIGPIPE(141)，pipefail 把整条管道判为失败。判据要求"行存在"时改为先 `logs="$(docker logs …)"` 再 `[[ "$logs" == *"…"* ]]`；失败时把真实行打出来。属确定性缺陷，不是 flake。
- **可复现字节码**：`contracts/foundry.toml` 必须保留 `bytecode_hash = "none"`，否则 solc 把绝对路径 remapping 写进 metadata，不同构建路径产出不同 runtime code，镜像会对链上合约报 `runtime code hash mismatch`。**固定基线/回退目标必须选在可复现修复之后**：老基线的字节码不可重建，会在新栈里启动即 FATAL；基线随修复顺延并重测 reference，不要静默沿用。
- **Anvil 健康检查**：只探 `eth_chainId` 会在 `forge create` 冷编译窗口内放行，monolith 与合约部署共用 deployer key 时会抢 nonce，DemoNFT 落到非预期地址、mint 返回成功 receipt 但余额 0。健康检查应要求**配置的两个合约地址都已有 code**。
- **容器网络别名**：多个验证项目都会起 `verify-h5` 别名；若控制器仍挂在旧项目网络，浏览器请求会按 DNS 轮询打到已拆除的 nginx（上游已死 → 502，重试又成功）。失败诊断要同时抓 **h5 nginx 自己的日志**与控制器所联网的别名现场。teardown 必须确认控制器已断网。
- **共享宿主磁盘**：containerd 快照在根分区（如 `/` 70G），`/home` 可能几百 G 但没用上。每个候选的镜像+构建缓存会持续累积；跑下一轮前删旧 run 镜像（`docker rmi <run>-*` + `image prune -f`），**但不要 prune builder cache**——预热过的 apk 层是资产。
- **Alpine / Debian 源**：Alpine CDN 超时会表现为 `apk add … (no such package)` 并让阶段在构建期失败；保留预热层或预构建。Debian 包网络慢可用宿主侧流式反代（iptables REDIRECT :80 → 本地代理），代理必须保留 `Content-Length` 或显式 `Connection: close`，否则 HTTP/1.1 客户端会等连接关闭。
- **Playwright**：浏览器缓存放 `.verify-artifacts/playwright-cache` 并以 `VERIFY_PLAYWRIGHT_DOWNLOAD_HOST` 指向可用镜像站；不要每次都重下。
- **阶段串行**：所有 stage 共享 Docker/端口/控制器，绝不并行跑两个 `verify-docker`。

## 9. 高效验证（迭代期 vs 终验）

`VERIFY_STAGE=all` 是**终验**，不是每次修复后的默认动作。推荐循环：

1. **改动分级**：先判断这次改动影响哪些门（改 harness → 对应 stage；改 `pkg/web3` → chain/faults；改授权/播放 → media/recovery/release；改性能测量 → unit+perf）。
2. **只跑受影响门**：`VERIFY_STAGE=release`（约 5–10 分钟）或逗号列表 `VERIFY_STAGE=media,recovery,release`（脚本原生支持），确认修复生效。
3. **flake 特性化**：单点失败先在同候选上把该门连跑 3–5 次，统计复现率再定性（产品缺陷 / harness 缺陷 / 环境 flake），不要靠整链"再赌一次"。
4. **快速预检**：harness 自测（`python3 scripts/cd/test_verify_shell.py`、`test_perf_protocol.py`）与 tools 镜像内的 `go vet` 是分钟级，能拦掉大部分 harness 级失败。
5. **批量修复**：同一轮里把已知问题一起修完，**只在最终冻结候选上跑一次完整 `all`**；每修一处就跑整链会白烧 2–3 小时。
6. **复用边界**：证据只能来自同一候选 SHA 的同一次运行；跨 SHA 或跨运行复用必须被显式记录，不能拿旧 run 的数字冒充当前候选（provenance 门就是为此存在的）。
7. **perf 迭代模式**：`VERIFY_MODE=pr` 只跑短 smoke（1 观众、1 轮、不跑 reference）；完整矩阵与同 runner reference 比较留给候选模式的最终 `all`。

## Must Not

- 不把 host `make test` / `go test` 当验收；不把 Mac 本机的 Go/Foundry 结果当验收（Docker 客户端流程见 §0.1）。
- 不在 dirty tree 上跑 `VERIFY_STAGE=all`（脚本会拒绝，别绕过）。
- 不在 SRS/ffmpeg 负载期跑 perf/release。
- 不在 8GB Docker VM 上并发构建全部镜像（会 OOM）；按 §0.1 串行构建。
- 不用 `pkill -f srs` / `pkill -f ffmpeg` 宽匹配；用 §3 的脚本。
- 不停 nginx、`srs.ingest.obs.conf`、产品 `srsmedia`。
- 不手改 `.verify-artifacts` 或把 host 结果混进证据。

## Exit Criteria

- 预检无 FAIL（WARN 列出）。
- 目标 stage 全部 exit 0；日志在 `.verify-artifacts/logs/`。
- 声称完整验收时：`VERIFY_STAGE=all` 在 clean candidate 上通过，且记录 SHA/CI run。
- 若共用 118：ABR 已停止并验证端口/负载；验收后按约定恢复。
- 报告写 `PASS` / `PASS WITH BOUNDARIES` / `BLOCKED`。
