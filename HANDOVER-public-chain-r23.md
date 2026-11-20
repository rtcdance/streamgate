# HANDOVER — public-chain / public-browser（r23 终态）

> 生成于 2026-11-18。承接 `HANDOVER-public-chain.md`。
> 结论先行：**public-network 已稳定 PASS；public-chain 在 r19 曾 PASS；public-browser 仅 monolith 三阶段与 api-gateway 的 eoa/contract 在 r22 取得可用证据；controls 与 negative(api-gateway) 未完成。**
> 剩余四项（public-browser-* / public-controls-* 的最终验证）**必须**改 FROZEN 观察器（新候选 C2）或走 CUA 人工路径，并再开一条“浏览器/controls 全在 transfer 前”的新链；这不是环境问题，是脚本能力边界。

---

## 1. 状态矩阵（验证器实测）

| 组件 | 状态 | 依据 |
|---|---|---|
| source / ci / public-runtime | PASS | `verifier-r22-2.json` |
| **public-network** | **PASS** | tenderly + ethpandaops，多次验证器实测 |
| **public-chain** | **PASS（r19）** | `supervision/verifier-r19.json` |
| public-chain（r22/r23） | FAIL | pending 抓取时 ethpandaops 返回 1 次 **429** |
| cw-suites / cw-segment-cost | PASS | capsule |
| local-monolith / local-api-gateway | PASS | capsule |
| public-browser-monolith | **eoa/contract/negative 均 PASS** | r22 `phase-*`，`WALLET_PHASE_OBSERVED` |
| public-browser-api-gateway | eoa/contract PASS；negative 未完成 | 18501 权限仅含 Sepolia，切主网被拒 |
| public-controls-* | 未运行 | 需 C 在 transfer 前持有 token |

---

## 2. 证据目录（`supervision/`）

| 目录 | 内容 | 关键结果 |
|---|---|---|
| `20261118T163000Z-w3-17-r19` | 完整 6 笔链 | NFT `0x391f36a4c44029db179b2f999eaeb6aef1117efc`、WALLET `0x50152fd7367e7eb843bf3d063c249629362b0724`；四阶段 `oracle-*.exit=0`，`oracle-pending.exit=2` |
| `20261118T200000Z-w3-17-r22` | 完整 6 笔链 + 双模式浏览器 | NFT `0xadef395d3b053e164e4000d75bc41da6e9ebf33d`、WALLET `0x8ae4db2e8f18c3444b6b952b0e3d5682e7c7e3fb`；四阶段 `0`、pending `2`；`phase-eoa-monolith`、`phase-contract-monolith`、`phase-negative-monolith2`、`phase-eoa-api-gateway`、`phase-contract-api-gateway2` 均 `WALLET_PHASE_OBSERVED`（已补 `command.exit`） |
| `20261118T230000Z-w3-17-r23` | 新链（3 笔） | NFT `0xb9af0d05f282e415ffd3ac2c11e7ac5ed1599b80`、WALLET `0x9554ffcd16c687e3ee5d8e90613f7410555f16fe`；`MANIFEST_RECORDED`；`oracle-eoa-held.exit=0`；`phase-eoa-monolith` PASS；`phase-eoa-api-gateway2` FAIL（identity ended） |
| `verifier-r19.json` / `verifier-r22-2.json` / `verifier-r23` | 验证器输出 | 见上表 |
| `extension.json` | 当前选择器 | 指向 r23（chain/network/manifest）+ r22 的 browserModes |

---

## 3. 运行环境（现场）

- **CfT（Chrome for Testing）**：`--remote-debugging-port=9226 --user-data-dir=/tmp/sg-cdp-wallet --load-extension=/tmp/sg-metamask/dist`（MetaMask 13.56）；当前 pid `42981`。
- **MetaMask**：已解锁；Sepolia 自定义 RPC 为 `https://ethereum-sepolia-rpc.publicnode.com`（显示名 “TenderlyTenderly”）。
- **钱包**：A(controller)=`0x853599CcC0a64F6cA4212AB4dEA5259Eba33942A`；B(recipient)=`0x732e24a0BcAb6fA41b1C9148BCF1135587bC76c1`。
- **Docker w3 栈**：10 容器 healthy；H5：monolith `http://localhost:18500`、gateway `http://localhost:18501`。
- **gateway 修复**：容器内 `/app/config/config.w3sepolia.yaml` 的占位 `https://sepolia-endpoint.invalid` 已改为 `https://sepolia.gateway.tenderly.co`（否则 `/auth/login` 1271 验证 503）。
- **H5 内容**：`content2 = d3d1c1fe-b923-4b9a-9467-b808db80b99c`（90s，MinIO `streams/<id>/g1/640x360/`）；`content1 = bea64464-4efd-4ad0-9926-d778cf461c68`（6s）。
- **DB**：`content_gating_rules` 指向 r23 NFT；`streams` / `stream_qualities` 为手工插入（栈内无 metadata/worker 消费者，转码产物需手工补目录/记录）。
- **MinIO**：转码输出目录为 `<q>/`（如 `360p/`），需重命名为与 variant 同名的 `640x360/`，否则 quality manifest 404。

---

## 4. 必须遵守的确定性规则（本轮踩坑总结）

1. **network 证据**：必须在**新链写入之前**用 `tenderly + ethpandaops` 生成；`--address` 必须传**小写**（验证器把 controller 规范化为小写，checksum 会导致 RPC 参数序列不匹配）。
2. **endpoints**：全程同一组（两个不同 host、均归档）。`providers()` 强制 host 不同，不能 tenderly 重复。
3. **pending**：必须在 transfer-a 后、safe 前立即抓取，且**重试直到 provider 流无 429/错误**；含一次 429 就永久 FAIL（`original RPC stream has invalid, failed or ambiguous responses`）。
4. **顺序**（同时满足 oracle 与浏览器）：
   `network → deploy-nft → deploy-wallet → mint-a → manifest → eoa-held → eoa(monolith+gateway) → transfer-a → pending → eoa-transferred → mint-c → contract(monolith+gateway) → negative(monolith+gateway) → controls(monolith+gateway) → contract-held → transfer-c → contract-transferred → extension.json → verifier`
   - `eoa-transferred` 期望 C=0：**mint-c 必须在其之后**。
   - `controls` 需要 C 持 token：必须在 transfer-c 之前。
5. **Mint calldata**：`mkcalldata.py <nft> <wallet> <mintToken> <transferToken> <out>`，wallet 必须是**当前链**的 wallet（否则 token 发到旧链合约）。
6. **ethpandaops**：safe 常滞后 10–20 分钟；manifest/oracle 会因此 BLOCKED，等其 epoch 前进即可（`retry-oracle` 已封装）。

---

## 5. 剩余工作的两条路径

### 路径 A（推荐，合规）：新候选 C2 + 新链
1. 修改 **FROZEN** `scripts/h5-demo-acceptance/wallet-sepolia.mjs`：
   - `page.on('response')` 在 `connectOverCDP(noDefaults:true)` 下的捕获（用 CDP Network 或 Playwright 默认模式）；
   - `TN04_PLAY` 允许“主线程 Hls 注入”场景（或改为 `W3_BROWSER_TRANSPORT=cua`）。
2. 修复 18501 的播放：给 `wallet-sepolia.mjs`/controls 采集加“冻结 `pause/removeAttribute/load` + 动态 token”的注入选项（仍属 FROZEN 变更）。
3. 重建 capsule、生成新候选 SHA（C2），按 §4 顺序重跑整链（浏览器/controls 在 transfer 前；pending 重试到干净）。
4. `extension.json` 全部指向新链，跑原版验证器。

### 路径 B（CUA）：人工/浏览器侧操作
- `W3_BROWSER_TRANSPORT=cua` + 人工完成 SIWE/播放/拒绝签名；仍需要“浏览器在 transfer 前”的新链与干净 pending。

**已确认不可能**：在 r19/r22/r23 现有证据上拼接（wallet/NFT/content 交叉绑定 + pending 证据已污染）。

---

## 6. 工具与命令（可直接复用）

- `mint.js`（补丁版）：`/private/tmp/round14-scripts/mint.js`（`.orig` 为原版；仓库副本 `supervision/round14-tools/mint.js`）
  - 补丁点：LavaMoat 兼容解锁（CDP `Input.insertText`）、`popupConn` 优先 `notification.html`、driver 5s race。
- `broadcast.sh`：`/tmp/round18/broadcast.sh <to> <calldata> <gas> <out.json> <opId>`（清理未授权页→广播→自动打开/确认 notification）。
- `wait-manifest.sh`：`/tmp/round23/wait-manifest.sh`（循环至 `MANIFEST_RECORDED`）。
- `run-oracle.sh` / `retry-oracle.sh`：`/tmp/round23/`（后者按 `.exit` 文件判断，自动备份失败输出）。
- `run-verifier.sh`：`/tmp/round23/run-verifier.sh`（输出 `verifier-r23.json`）。
- 浏览器注入：`/tmp/round22/inject-frozen.js`（冻结媒体方法 + 动态 token）、`inject-url.js`、`auto-gw-full.js`、`auto-contract.js`、`auto-neg3.js`。
- 常用诊断：`/tmp/round14/diag-chain.py`、`/tmp/round22/diag-chain.py`、`diag-pending.py`。

---

## 7. 已知坑（本轮新增）

1. **network 报告 checksum 地址** → `public-network` FAIL（参数序列不匹配）。用小写。
2. **pending 内的 429** → `public-chain` FAIL，且不可回退。抓取时重试。
3. **MetaMask `-32002`**（有挂起请求）：关闭全部 `notification.html`；必要时重启 CfT。
4. **mint.js 选错 requester**：残留 `https://example.com/` 未授权页或页面上的 `window.__mmop` 会导致跳过已授权页 → `account/method not authorized`。清理两者后再发。
5. **JWT 过期**：eoa/contract 必须在登录后立即播放（4s/71s 观察窗口内）；注入式播放需动态读 `sessionStorage.streamgate_token`。
6. **18501 播放器**：自带 player `dur=0`；注入需冻结 `pause/removeAttribute/load`，但 H5 的 auth-refresh/`loadVideo` 仍会改变 identity → 原始观察器报 `Identity or decoded playback ended`。
7. **转码后 DB 为空**：`streams`/`stream_qualities` 需手工插入；MinIO 目录需 `640x360/` 与 variant 同名。
8. **worker/observer**：Hls `enableWorker:true` 时 segment 可能不可见；`page.on('response')` 本身可用（r19 的失败实为 token 过期，非 worker）。

---

## 8. 现场保留与清理

- 保留：CfT（pid 42981）、Docker w3 栈、`/tmp/round14..23`、`supervision/` 全部 r14–r23 证据。
- 候选 worktree：`streamgate-wt-freshness` 仍为 `d267ad44` 且干净；仅主仓 `TRANSITION_PLAN.md` 与本文档为工作区修改。
- 若需复现：严格按 §4 顺序；每笔广播后确认 `safe` 追上再跑对应 oracle；pending 必须重试至无 429。