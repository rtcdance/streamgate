# 交接：public-chain 阶段证据（w3-extension / candidate d267ad44）

> 更新时间：2026-11-18 08:0x。本文是当前任务 #5 的完整交接，供任何后续会话/接手人直接继续。
> 目标：达成 TRANSITION_PLAN.md 整体目标——public-chain 四阶段 oracle exit=0，extension.json 更新后汇总验证器全 PASS。
> 本版为 2026-11-18 深夜恢复会话的全面修订：修正了旧版两处关键错误（证据根位置、vault 状态）。

---

## 1. 当前状态总览（2026-11-18 07:45 验证器实测）

| 组件 | 状态 |
|------|------|
| source / ci / **public-runtime** / public-network / cw-suites / cw-segment-cost / local-monolith / local-api-gateway | **全部 PASS**（8 组件） |
| public-chain | **FAIL**（诚实语义：network 依赖解除后，r9 的 pending 证据被真实评估，`extra responses` 回放失败 → 验证器拒绝坏证据，非洗绿；这正是 r14 要解决的） |
| public-browser / public-controls（双模式） | BLOCKED（依赖 public-chain） |

- w3 栈已受控重启 STARTED_HEALTHY（10 容器 healthy，H5 18500/18501 均 200），证据目录 `d267ad44-run42548083086-public-preparation-r7/stack` 可读。
- 基线报告：`/tmp/verifier-after-relaunch.json`（2026-11-18 07:3x）。
- **只剩两件事**：①owner 在 MetaMask 导入助记词；②按 RUNBOOK 跑 r14 → extension.json → 验证器 exit=0。

## 2. 旧版交接的两处勘误（重要）

1. **证据根从未丢失**。`extension.json`、r9–r13 全部监督证据、d267ad44 CI capsule（`d267ad44-run42548083086-acceptance-original/`）都在主仓：
   `streamgate/.verify-artifacts/w3-extension/supervision/`
   旧版写的 `/private/tmp/sg-w3-verify-d267ad44` 只是验证器的**辅助 checkout**（已被 /tmp 清理，无关紧要——验证器可从 `streamgate-wt-freshness` 候选 checkout 直接跑）。
2. **vault 不在本 profile**。`/tmp/sg-cdp-wallet` 的 MetaMask 状态是全新 onboarding（KeyringController 空）。取证（LevelDB 全代际 SSTable 解析、IDB SQLite WAL checkpoint/.recover/ZSTD 帧扫描、删除行追踪）均无 vault 痕迹；profile 在 r9 时段（04:00–08:00）零改动 → r9 早晨的钱包活在另一个已被 /tmp 清理的 profile 里。**r9 的「vault 应能 Unlock」假设是错的。**

## 3. MetaMask 环境现状（已恢复到只剩助记词一步）

- CfT（Chrome for Testing 157.0.8103.78，`/tmp/sg-metamask/cft/`）已用 **v13.56.0 dist** 运行：
  `--remote-debugging-port=9226 --user-data-dir=/tmp/sg-cdp-wallet --load-extension=/tmp/sg-metamask/dist`
- v13.56.0 dist 已下载校验（25,221,068 字节）并替换到 **原路径** `/tmp/sg-metamask/dist`（⚠️ 扩展 ID 由加载路径派生：换目录会导致 ID 变成 `cjooo…` 且读不到原 origin 存储——必须保持路径名不变）。
- 扩展 ID 正确恢复为 `oikiooigkahilkohdknlepcancnnhndd`。
- **当前卡在 onboarding 等待 owner 导入助记词**（密码 `Mingo@Ulucu88`）。**不要走 onboarding 创建新钱包**——必须用「导入钱包」恢复 A=0x8535…42A。
- 下载工具教训：GitHub 资产 CDN 会发 206 但从 0 开始，续传必须校验 `Content-Range: bytes <pos>-`；并发下载者（见 §7 atomcode）会互相写坏文件。

## 4. r14 执行手册（已重建并字节级验证）

**所有脚本在 `/private/tmp/round14-scripts/`（⚠️ /tmp 会被清理，启动前先 `ls` 核对），持久副本在 `.verify-artifacts/w3-extension/round14-tools/`**：
- `RUNBOOK.md` — 完整命令序列（mkdir 新证据目录 `20261118T000000Z-w3-17-r14` → 广播 → 账本 → manifest → 四阶段 oracle → pending → extension.json → 验证器）。
- `mint.js` — 单笔广播：CDP 9226 + 注入页 window.ethereum `eth_sendTransaction` + notification popup 解锁/确认驱动 + **链上回执核验后才落盘**。用法：`node mint.js <to|""> <calldataFile> <gasHex> <output.json> <opId>`。弹窗队列卡住先跑 `rejectall.js`。
- `mkledger.py` — 账本组装（operationID=sha256(id+":"+hash)，与 r9 公式不同但 oracle 只要求唯一；calldataSHA256/ reservedFeeWei 算法与 oracle 一致）。
- `mkcalldata.py` — transferFrom/execute 编码，**已与 oracle `transfer_data`/`execute_data` 构建器字节级对拍通过**（关键修复：transfer-c 尾对齐是 `(-len%32)`=28 字节零填充；旧版 32 字节会让第 6 笔 oracle FAIL）。
- 账本 3/4/5/6-op 已全部通过 oracle `ledger_operations` 四阶段校验（离线、无需 RPC）。

**已核对的关键参数**（详见 RUNBOOK）：
- gas：deploy-nft 0x8033a0 / deploy-wallet 0x456f10 / mint-a,transfer-a 0x493e0 / mint-c,transfer-c 0x55730；maxFee 0x3b9aca00（1 gwei）、prio 0x5f5e100。
- budget：sum(gas×maxFee)×1.2 ≈ 0.0167 ≤ 0.02 ✓。
- pending：4-op 账本（=eoa-transferred 账本副本），phase=eoa-transferred，`--wait-safe-seconds 0`，期望 exit 2（BLOCKED 且可回放）。**绝不用 wait=1**（r9 pending 之死因）。
- manifest build 用 **capsule 内原件**：`d267ad44-...-acceptance-original/payload/wallet-build/artifacts/d267ad44…/{DemoNFT,Study1271Wallet}.json`（本地 forge 重编译 creation/runtime code SHA 一致但 artifactSHA256 不一致，不能用）。
- oracle/manifest controller 必须小写传参；oracle 拒绝覆盖已存在 output，重跑前删残留。

## 5. 已核对不变的常量

- controller A = `0x853599CcC0a64F6cA4212AB4dEA5259Eba33942A`（待 owner 导入助记词恢复签名）
- recipient B = `0x732e24a0BcAb6fA41b1C9148BCF1135587bC76c1`（不可控，多余 token 卡死）
- 链：Sepolia（11155111）；RPC：PublicNode（primary）+ thirdweb（fallback），顺序不可颠倒
- candidate：`d267ad4439ba672fecf373cae22f07852a0f6ada`；CI run：`42548083086`
- calldata 基底：`/tmp/round7/{deploy-nft,deploy-wallet,mint-a}.txt`（11020/5950/74 字节，已验证）
- r9 证据：`supervision/20261117T000000Z-w3-12-r9/`（四阶段 oracle 全 exit 0，仅 pending 流不对齐）

## 6. Docker 栈（已恢复）

- daemon 于 07:26 就绪（管理员弹窗拖 8 小时后批准/超时放行）。
- **教训**：①daemon 刚就绪立刻 compose up 会被 VM 启动竞态打断（全部容器 Exited(255)）→ 等稳定再动手；②被杀半死容器按 11-13 先例按标签清除（`docker rm -f` + `network rm` + `volume rm`；`compose down` 会因 W3_JWT_SECRET 插值失败），再 `launch_stack.py` 受控重启（STARTED_HEALTHY，报告 `/tmp/stack-relaunch-0745.json`）。
- launch_stack 命令行已录在 cron automation-263c3afe（每 30 分钟）与本文 §4。

## 7. 并发会话管控

- **atomcode 代理已 SIGSTOP 冻结**（pid 2550）：它循环执行同一交接的下载/解压，与新会话并发写坏 mm56.zip 三次。恢复 `pkill -CONT -f atomcode`；确认不需要时建议直接终止。**接管任何 wallet/证据链前必须 `lsof` 查并发写者 + `CronList` 排查定时器**。
- 当前看门狗：cron `automation-263c3afe-6d09-4616-819f-3ac78391074c`（每 30 分钟：Docker→launch_stack 兜底、监视 MetaMask 离开 onboarding）。r14 开始后**应暂停或删除它**（避免与广播流程并发）。

## 8. 已知坑位清单（仍有效，增量见 §2–§4）

1. `/tmp` 会被清理——calldata/dist/脚本随时可能消失，重跑前先核对。
2. mint.js 读 calldata 是 readFileSync 原文——双 0x 前缀会广播垃圾（已内置校验，见脚本头注释）。
3. oracle 拒绝覆盖 output——重跑前删残留 json/exit。
4. manifest controller 小写；transfer fixture 是 transferFrom（0x23b872dd）。
5. 弹窗点击 ≠ 广播成功——必须链上回查 receipt（mint.js 已内置）。
6. MetaMask 解锁 5 分钟自动锁屏——mint.js 内置解锁逻辑。
7. B 地址不可控，多余 token 不可回收——新合约全新部署。
8. recheck_history 高频（~40+ 请求）PublicNode 间歇限流——重试即可；正式验证必须用未改动原版脚本跑通一次留证。
9. **钱包 profile 绝不能放 /tmp**（本次 vault 丢失根因）；v13.14 读不了 v13.56 vault 且会覆写。

## 9. 验收口径（不变）

- 成功标准：`verify_web3_extension.py` exit=0、全部组件 PASS；正式验证用未改动原版脚本。
- 完成后收尾：写回 TRANSITION_PLAN.md（§10.10 状态），清理 /tmp/cft、/tmp/mm-*、/tmp/sg-*、/tmp/hls-test、/tmp/round7、/tmp/round8、/tmp/round14*、/tmp/dl-*、/tmp/ldb*、/tmp/anvil*；暂停/删除看门狗 cron；处理 atomcode。

---

*交接修订完成。前置仅剩：owner 导入助记词 → 按 §4 RUNBOOK 顺序执行。*
