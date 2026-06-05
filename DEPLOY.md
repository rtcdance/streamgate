# StreamGate Deployment Guide

## 概述

StreamGate 支持两种部署模式：**单体（Monolith）** 和 **微服务（Microservices）**。两种模式共享同一套基础设施（PostgreSQL、Redis、MinIO、NATS、Anvil），区别在于应用服务的组织方式。

---

## 一键部署

### 双模式部署（完整栈）

```bash
make fullchain-deploy
# 或
./scripts/docker-deploy.sh --build
```

### 单体模式部署

```bash
make deploy-monolith
./scripts/docker-deploy-monolith.sh [--build] [--clean]
```

启动的服务：`postgres` + `redis` + `minio` + `nats` + `anvil` + `h5-demo` + `streamgate`

### 微服务模式部署

```bash
make deploy-microservices
./scripts/docker-deploy-microservices.sh [--build] [--clean]
```

启动所有基础设施 + 9 个独立微服务。

### 拆除

```bash
make fullchain-teardown            # 停止容器，保留数据
docker compose -f docker-compose.fullchain.yml down -v  # 停止容器并清除数据
```

---

## 单体模式（Monolith）

所有功能在单一进程中通过 plugin 机制加载运行。

### 架构

```
┌──────────────┐     ┌──────────────┐
│  浏览器       │────▶│  nginx       │
│  :18000/demo/ │     │  :18000      │
└──────────────┘     └──────┬───────┘
                            │ /api/ proxy
                            ▼
                    ┌──────────────┐
                    │  streamgate  │  ← 单体: 内置所有 plugin
                    │  :18080      │     auth, upload, transcode,
                    │  :19090(gRPC)│     streaming, worker, cache...
                    └──────────────┘
```

> 前端 `http://localhost:18000/` → nginx `proxy_pass http://streamgate:8080` → monolith

### 端口

| 服务 | 端口 | 说明 |
|------|------|------|
| Monolith (HTTP) | `:18080` | 单体直连 |
| Monolith (gRPC) | `:19090` | gRPC 端点 |
| H5 Demo (nginx) | `:18000` | 前端页面 |
| MinIO | `:19000` | 对象存储 |
| MinIO Console | `:19001` | 管理界面 (minioadmin/minioadmin) |
| PostgreSQL | `:15432` | 数据库 (postgres/postgres/streamgate) |
| Redis | `:16379` | 缓存 |
| NATS | `:14222` | 消息队列 |
| Anvil RPC | `:18545` | 本地以太坊测试网 |

### 适用场景

- 本地开发测试
- 单机部署
- 快速验证全链路流程
- 资源受限环境

---

## 微服务模式（Microservices）

每个功能独立部署为独立容器，通过 NATS 和 Consul 服务发现通信。

### 架构

```
┌──────────────┐          ┌──────────────┐
│  浏览器       │─────────▶│  api-gateway │  ← 微服务入口，直连
│  :18000/demo/ │ 28080   │  :28080      │
│  Backend URL  │          └────┬──────┬──┘
│  = :28080     │               │      │
└──────────────┘    ┌───────────┘      └────────────┐
                    ▼                               ▼
            ┌──────────────┐              ┌──────────────┐
            │  auth        │              │  transcoder   │
            │  :28086      │              │  :28081       │
            └──────────────┘              └──────────────┘
            ┌──────────────┐              ┌──────────────┐
            │  upload      │              │  streaming    │
            │  :28082      │              │  :28083       │
            └──────────────┘              └──────────────┘
            ┌──────────────┐              ┌──────────────┐
            │  metadata    │              │  cache        │
            │  :28084      │              │  :28085       │
            └──────────────┘              └──────────────┘
            ┌──────────────┐              ┌──────────────┐
            │  worker      │              │  monitor      │
            │  :28087      │              │  :28088       │
            └──────────────┘              └──────────────┘
```

> 微服务模式下，前端 Backend URL 设为 `http://localhost:28080` 直连 api-gateway。
> nginx 仍会 proxy 到 monolith，但**微服务模式建议绕过 nginx，直接调用 api-gateway:28080**。

### 服务端口

#### 应用服务

| 微服务 | HTTP 端口 | gRPC 端口 | 功能 |
|--------|-----------|-----------|------|
| API Gateway | `:28080` | `:29091` | API 入口，路由分发 |
| Auth | `:28086` | `:29097` | 钱包认证，JWT 签发 |
| Upload | `:28082` | `:29093` | 文件上传，分片合并 |
| Transcoder | `:28081` | `:29092` | FFmpeg 转码，HLS 输出 |
| Streaming | `:28083` | `:29094` | HLS 流媒体分发 |
| Metadata | `:28084` | `:29095` | 元数据索引与查询 |
| Cache | `:28085` | `:29096` | 缓存加速 |
| Worker | `:28087` | `:29098` | 后台任务调度 |
| Monitor | `:28088` | `:29099` | 指标收集，健康监控 |

#### 基础设施

| 服务 | 端口 | 说明 |
|------|------|------|
| PostgreSQL | `:25432` | 数据库 (postgres/postgres/streamgate) |
| Redis | `:26379` | 缓存 (password: dev) |
| MinIO API | `:29000` | 对象存储 (streamgate/streamgate123) |
| MinIO Console | `:29001` | 管理界面 |
| NATS Client | `:24222` | 消息队列 |
| NATS Monitor | `:28222` | NATS 监控 |
| Anvil RPC | `:18545` | 本地以太坊测试网 |
| Consul | `:28500` | 服务注册发现 |
| Prometheus | `:29090` | 指标存储 |
| Grafana | `:23000` | 监控面板 |
| Jaeger UI | `:26686` | 分布式追踪 |

### 适用场景

- 生产环境部署
- 水平扩展需求
- 独立服务灰度发布
- 服务级监控与告警

---

## H5 Demo 前端

前端页面由 nginx 容器（`sg-fc-h5-demo`）提供。

### 访问方式

| 路径 | 说明 |
|------|------|
| `http://localhost:18000/` | 首页（推荐，单体模式下 API 走 monolith） |
| `http://localhost:18000/demo/` | 功能演示页面 |

### 页面功能

| 步骤 | 功能 | 依赖 |
|------|------|------|
| Dashboard | 8 个微服务健康状态卡片 | API |
| Step 1 | Backend URL 配置 | — |
| Step 2 | 钱包连接 (MetaMask / Demo Mode) | Anvil |
| Step 3 | Challenge 签名登录 | Auth |
| Step 4 | NFT 所有权验证 | Anvil + NFT Contract |
| Step 5 | 视频文件上传 | Upload |
| Step 6 | 自动转码 + 进度展示 | Transcoder |
| Step 7 | 受保护 HLS 播放 | Streaming + Auth |
| RPC | 多 RPC 端点健康状态 | Web3 |

### 特性

- **深色主题**：暖橙 (#f4a261) 强调色，深蓝 (#07111f) 背景
- **左侧栏导航**：Dashboard / Workflow / Upload / Transcode / Playback / RPC
- **键盘快捷键**：1-6 快速切换视图
- **浏览器缓存控制**：JS 文件带 `?v2` 参数 + nginx `no-store` 头

---

## 快速验证流程

```bash
# 1. 部署（任选一种）
make deploy-monolith              # 单体模式
# 或
make deploy-microservices         # 微服务模式

# 2. 打开浏览器
open http://localhost:18000/

# 3. 点击 Demo Mode → 自动连接 Anvil
# 4. 点击 Sign & Login → 自动签发 JWT
# 5. 点击 Verify NFT Ownership → 返回 has_nft: true
# 6. 上传视频 → 自动触发转码 → progress 0% → 100%
# 7. 输入 Video ID → Play → HLS 播放
```

---

## 转码全链路

```
Upload File
    │
    ▼
POST /api/v1/upload           → upload_id
    │
    ▼
POST /upload/:id/complete-upload
    │  └→ CompleteUploadWithTx() → content_id
    │  └→ TranscodingService.Transcode() → task_id (auto-submit)
    ▼
GET /transcode/status/:task_id
    │  └→ Poll every 3s
    │  └→ status: pending → processing → completed
    ▼
GET /streaming/:content_id/manifest.m3u8
    │
    ▼
HLS Playback (hls.js)
```
