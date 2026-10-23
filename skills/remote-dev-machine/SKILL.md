---
name: "remote-dev-machine"
description: "Develop StreamGate on the remote Linux dev machine (prod-srs) while the Mac is only an editor and SSH control plane. Use when the user wants builds, tests or Docker acceptance to run on the dev machine instead of the Mac, or needs code sync, remote execution, port tunnels and .verify-artifacts retrieval. Never use the Mac as the build/test/acceptance environment."
---

# Skill: remote-dev-machine（已停用，2026-10-23）

> **不再使用：** 验收环境已改为本机 Docker Desktop（见 `dev-machine-acceptance`）。
> `prod-srs` 上的 Docker 资源（容器、镜像、卷、网络、构建缓存）已全部清空。
> 本文件仅保留为历史记录，避免旧流程被再次执行；不要在其中新增操作步骤。

## Trigger

Use this skill when:

- 用户要求“用开发机开发/构建/测试”，或明确“不要用 Mac 本机跑构建/测试”。
- 需要在 `prod-srs`（SSH alias；IP 见本机 `~/.ssh/config`）上跑 `make verify-docker`、`make test`、`make test-anvil`、`make fullchain-deploy`。
- 需要同步代码、远程执行、端口转发、回传 `.verify-artifacts`。

## 0. 硬规则：Mac 不是构建/测试环境

Mac 只允许：

- 编辑文件（本地干净 worktree）。
- `git`、`ssh`、`rsync`/`scp`、`curl` 到 SSH tunnel，以及 `jq`/`shasum` 等只读校验工具。

**禁止**在 Mac 上执行并作为开发/验收证据：

- `go build`、`go test`、`make test`、`make verify-docker`、`make fullchain-deploy`
- `docker` / `docker compose`、`anvil` / `forge`、`ffmpeg`、Playwright

StreamGate 的验收门禁是 **Docker `make verify-docker` 或 CI**；Mac 上偶然跑出的结果只能当快速反馈，不能写进验收。

## 1. 拓扑与目录

```text
Mac（编辑/控制面） --ssh--> prod-srs（构建/测试/运行时）
                              /home/streamgate          # StreamGate 专用
                              /home/streamgate/.verify-artifacts
```

- `REMOTE_HOST` 默认 `prod-srs`。
- `REMOTE_DIR` 默认 `/home/streamgate`。
- 远程仓库是**专用构建 clone**；不要与 `/home/srs-abr` 混用。
- 本地源必须是干净 worktree；不要用旧的 dirty 分支。

## 2. 源码同步

### 2.1 前置

```bash
git status --porcelain          # git 模式必须为空
git rev-parse --abbrev-ref HEAD
git rev-parse HEAD
```

### 2.2 git 模式（默认，推荐）

```bash
git push origin HEAD:refs/heads/<branch>
ssh prod-srs 'cd /home/streamgate && git fetch origin && git checkout <branch> && git pull --ff-only'
```

远程有本地改动时停下报告，不 reset/clean。

### 2.3 rsync 模式（仅未提交 WIP）

```bash
bash skills/remote-dev-machine/scripts/remote-sync.sh --mode rsync --dry-run
bash skills/remote-dev-machine/scripts/remote-sync.sh --mode rsync --yes
```

排除：`.git`、`build`、`.verify-artifacts`、`node_modules`、`h5-demo/node_modules`、`scripts/h5-demo-acceptance/node_modules`、`contracts/lib`、`.env*`、`*.log`、`.DS_Store`、`.codegraph`、`.omo`、`.workbuddy`。

> rsync 会覆盖远程工作树，使 git clone 出现 modified 文件；建议用 `REMOTE_DIR=/home/streamgate-wip` 做 WIP 同步。

### 2.4 首次引导

```bash
ssh prod-srs 'mkdir -p /home/streamgate'
# 优先远程 git clone；无外网时用干净 bundle：
git bundle create /tmp/streamgate.bundle HEAD
scp /tmp/streamgate.bundle prod-srs:/tmp/
ssh prod-srs 'git clone /tmp/streamgate.bundle /home/streamgate'
ssh prod-srs 'cd /home/streamgate && git submodule update --init --recursive'
```

## 3. 远程执行

```bash
bash skills/remote-dev-machine/scripts/remote-run.sh -- go build ./cmd/monolith/streamgate
bash skills/remote-dev-machine/scripts/remote-run.sh -- make fmt
bash skills/remote-dev-machine/scripts/remote-run.sh -- make verify-docker VERIFY_STAGE=unit
bash skills/remote-dev-machine/scripts/remote-run.sh -- make verify-docker VERIFY_STAGE=media
bash skills/remote-dev-machine/scripts/remote-run.sh -- make h5-demo-acceptance
```

- 脚本：`ssh -o BatchMode=yes -o ConnectTimeout=8`，`cd $REMOTE_DIR`，默认 `timeout 3600`，日志写本地 `build/remote/`。
- **`VERIFY_STAGE=all` 需要 clean candidate**：远程 `git status --porcelain` 必须为空；先 commit/push，再在远程 checkout 同一 SHA。
- 长任务用 `tmux new-session -d -s sg-verify "make verify-docker VERIFY_STAGE=all 2>&1 | tee /tmp/sg-all.log"`；不要只靠 `nohup`。
- 每条命令看 exit code，不只看输出。

## 4. 端口与 tunnel

| 端口 | 归属 | 用法 |
| --- | --- | --- |
| 1926 / 1927 / 1937 / 8088 / 8089 / 9090 | SRS | 不要占用；需要时先停 ABR |
| 8080 | StreamGate monolith | SSH tunnel |
| 18000 | h5-demo | SSH tunnel |
| 5432 / 6379 / 9000 / 4222 / 8545 | PG / Redis / MinIO / NATS / Anvil | 由 Docker compose 内部使用 |

```bash
bash skills/remote-dev-machine/scripts/remote-tunnel.sh --start 8080 18000
bash skills/remote-dev-machine/scripts/remote-tunnel.sh --status
bash skills/remote-dev-machine/scripts/remote-tunnel.sh --stop
```

只绑 `127.0.0.1`；不要把端口暴露公网。AI 自检优先在 118 上 `curl 127.0.0.1`。

## 5. 资源与 SRS 共存

重活前：

```bash
bash skills/remote-dev-machine/scripts/remote-run.sh -- 'uptime; free -h; docker info --format "{{.ServerVersion}}"; pgrep -af "ffmpeg|objs/srs" | head'
```

- 跑 `verify-docker` 前按 `dev-machine-acceptance` §3 停 SRS ABR。
- 不要停 nginx、`srs.ingest.obs.conf`、产品 `srsmedia`。
- **perf/release 不在共享 118 跑**；用独立 8C/16G+ Linux 参考机或 CI。
- Docker data-root 放 `/home/docker`；`/home` 需 100GB+ 空闲。
- 不要并行跑多个 `verify-docker` stage；它们共享 Docker/compose 资源。

## 6. 证据回传

```bash
bash skills/remote-dev-machine/scripts/remote-fetch.sh .verify-artifacts
```

- 检查 `.verify-artifacts/logs/<stage>.log`、`candidate.json` 的 SHA、`coverage.out`。
- 证据必须来自远程 Docker runner；不要混入 Mac 本机结果。
- 不要手改 `.verify-artifacts`。

## 7. 安全红线

- 不在远程或 Mac 上 `git reset --hard` / `git clean -fd`；只允许 fast-forward。
- 不 `sudo` 安装/改系统，除非作者批准；Go/Docker 安装按 `dev-machine-acceptance` §1/§2。
- 不把密钥、Token、JWT、RPC 凭据写进命令行或日志；SSH key 权限 600。
- 不用 `pkill -f srs` / `pkill -f ffmpeg` 宽匹配。
- 不修改 `/home/srs-abr` 下的 SRS 代码/脚本。
- 不把 118 端口直接暴露公网。

## 8. 验收交接

- 功能验收（F01–F15、`verify-docker` stage、证据）按 `dev-machine-acceptance` §5 执行。
- 本 skill 只负责“在开发机上跑”和“把证据拿回来”，不重新定义验收标准。

## 9. 实测经验（prod-srs，2026-10）

同步与冻结：

- 无推送授权时用 **bundle** 同步：`git bundle create /tmp/sg-sync.bundle HEAD && scp … && ssh prod-srs 'cd /home/streamgate && git fetch /tmp/sg-sync.bundle HEAD && git checkout --detach FETCH_HEAD'`，然后确认 `git status --porcelain` 为空。
- 候选验收运行期间**冻结 `scripts/` 与 `test/`**：它们以只读 bind-mount 实时挂载，任何编辑都会让候选身份漂移（controller 会报 `source HEAD changed`），整轮作废。
- 远端工作树被临时 scp 弄脏时，先比 `git hash-object` 与 `git rev-parse <sha>:<path>`，字节相同才 `git checkout --`，不要盲目 reset。

磁盘与镜像（最容易卡住整轮）：

- 根分区（containerd 快照）常见 `/` 70G、`/home` 379G：**每轮之前**删掉上一轮镜像（`docker rmi <old-run>-*`、`image prune -f`），否则会在 `link: no space left on device` 处失败。
- **不要 `docker builder prune`**：预热过的 Alpine apk 层能避免 `apk add (no such package)` 这类 CDN 超时把阶段卡死。
- Playwright 与 npm 缓存固定在 `.verify-artifacts/`，并用 `VERIFY_PLAYWRIGHT_DOWNLOAD_HOST` 指向 npmmirror。
- 大输出日志做判据时注意：`set -o pipefail` 下 `docker logs | grep -q` 会 SIGPIPE(141) 假阴性，判据要进程内匹配。

高效验证（避免每修一处就烧 2–3 小时）：

- 迭代期只跑受影响门：`VERIFY_STAGE=release`，或多门一次跑 `VERIFY_STAGE=media,recovery,release`（comma list 原生支持）。
- 同一候选上把可疑门连跑 3–5 次做特性化，区分产品缺陷 / harness 缺陷 / 环境 flake，再决定是否动代码。
- harness 自测（`python3 scripts/cd/test_verify_shell.py`、`test_perf_protocol.py`）分钟级，先跑它们。
- `VERIFY_MODE=pr` 只跑 perf 短 smoke；完整矩阵 + 同 runner reference 比较留给最终 `all`。
- 终验仍需在**冻结的最终 HEAD** 上跑一次完整 `VERIFY_STAGE=all`，并回传证据；迭代期的单门结果不能替代它。
- 远端长任务放 `tmux`（短命令也要 `timeout`）；本地后台轮询任务可能静默死掉，直接 ssh 轮询才是事实源。

## 10. Exit Criteria

- 所有构建/测试/验收命令在 118 上执行，exit code 0（或明确记录失败）。
- 本地日志在 `build/remote/`；远程 SHA 与本地提交一致。
- 没有把 Mac 本机结果当作验收证据。
- `.verify-artifacts` 已回传，stage 日志/candidate SHA 已核对。
- 若停过 SRS：按约定恢复或明确记录保持停止。
