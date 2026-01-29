# StreamGate 快速开始指南

## 🚀 5分钟快速开始

### 前置条件

- Go 1.21+
- Docker & Docker Compose
- Make

### 方式1：本地开发（单体模式）

```bash
# 1. 克隆项目
git clone https://github.com/rtcdance/streamgate.git
cd streamgate

# 2. 安装依赖
go mod download

# 3. 启动基础设施
docker-compose up -d

# 4. 构建单体二进制
make build-monolith

# 5. 运行服务
./bin/streamgate

# 6. 测试
curl http://localhost:8080/health
```

### 方式2：Docker Compose（微服务模式）

```bash
# 1. 克隆项目
git clone https://github.com/rtcdance/streamgate.git
cd streamgate

# 2. 启动所有服务
docker-compose up -d

# 3. 检查服务状态
docker-compose ps

# 4. 访问服务
# API Gateway: http://localhost:8080
# Consul UI: http://localhost:8500
# Prometheus: http://localhost:9090
# Jaeger: http://localhost:16686

# 5. 查看日志
docker-compose logs -f api-gateway
```

### 方式3：构建所有二进制

```bash
# 1. 构建所有9个微服务
make build-all

# 2. 查看生成的二进制
ls -la bin/

# 3. 运行单个服务
./bin/api-gateway &
./bin/upload &
./bin/transcoder &
./bin/streaming &
```

## 📊 9个微服务

| 服务 | 端口 | 说明 |
|------|------|------|
| API Gateway | 9090 | REST API、gRPC网关、认证 |
| Upload | 9091 | 文件上传、分块上传 |
| Transcoder | 9092 | 视频转码、工作池、自动扩展 |
| Streaming | 9093 | HLS/DASH流媒体 |
| Metadata | 9005 | 元数据管理、数据库 |
| Cache | 9006 | 分布式缓存、Redis |
| Auth | 9007 | NFT验证、签名验证 |
| Worker | 9008 | 后台任务、任务队列 |
| Monitor | 9009 | 健康监控、指标收集 |

## 🛠️ 常用命令

### 构建

```bash
make build-all              # 构建所有服务
make build-monolith         # 构建单体
make build-api-gateway      # 构建API网关
make build-transcoder       # 构建转码器
make docker-build           # 构建Docker镜像
```

### 运行

```bash
make run-monolith           # 运行单体
make docker-up              # 启动Docker Compose
make docker-down            # 停止Docker Compose
```

### 测试

```bash
make test                   # 运行测试
make lint                   # 代码检查
make fmt                    # 代码格式化
```

### 部署

```bash
make k8s-deploy             # 部署到Kubernetes
make k8s-status             # 检查K8s状态
make k8s-logs               # 查看K8s日志
```

## 📁 项目结构

```
streamgate/
├── cmd/                    # 应用程序入口
│   ├── monolith/          # 单体部署
│   └── microservices/      # 9个微服务
├── pkg/                    # 核心包和库
│   ├── core/              # 微内核核心
│   ├── plugins/           # 9个插件
│   ├── models/            # 数据模型
│   ├── storage/           # 存储层
│   ├── service/           # 业务服务
│   ├── api/               # API定义
│   ├── middleware/        # 中间件
│   ├── util/              # 工具函数
│   └── web3/              # Web3集成
├── proto/                 # Protocol Buffers
├── config/                # 配置文件
├── migrations/            # 数据库迁移
├── scripts/               # 脚本
├── test/                  # 测试
├── deploy/                # 部署配置
├── docs/                  # 文档
└── examples/              # 示例代码
```

## 🔍 检查服务健康

```bash
# API Gateway
curl http://localhost:8080/health

# 所有微服务
curl http://localhost:9005/health  # Metadata
curl http://localhost:9006/health  # Cache
curl http://localhost:9007/health  # Auth
curl http://localhost:9008/health  # Worker
curl http://localhost:9009/health  # Monitor
```

## 📊 监控和可观测性

### Prometheus
```bash
# 访问Prometheus
open http://localhost:9090

# 查看指标
curl http://localhost:8080/metrics
```

### Jaeger
```bash
# 访问Jaeger UI
open http://localhost:16686

# 查看分布式追踪
```

### Consul
```bash
# 访问Consul UI
open http://localhost:8500

# 查看服务注册
# 查看健康检查
# 查看键值存储
```

## 🐳 Docker Compose 服务

```bash
# 查看所有服务
docker-compose ps

# 查看特定服务日志
docker-compose logs -f api-gateway
docker-compose logs -f transcoder

# 进入容器
docker-compose exec api-gateway sh

# 重启服务
docker-compose restart api-gateway

# 停止所有服务
docker-compose down

# 清理所有数据
docker-compose down -v
```

## 🔧 配置

### 环境变量

```bash
# 复制示例配置
cp .env.example .env

# 编辑配置
vim .env
```

### 配置文件

```bash
# 主配置
config/config.yaml

# 开发配置
config/config.dev.yaml

# 生产配置
config/config.prod.yaml

# Prometheus配置
config/prometheus.yml
```

## 📚 详细文档

- **[cmd/README.md](../cmd/README.md)** - 部署模式详解
- **[deployment-architecture.md](deployment-architecture.md)** - 架构和设计
- **[docs/project-planning/](../project-planning/)** - 项目规划文档
- **[README.md](../../README.md)** - 项目主文档

## 🆘 常见问题

### Q: 如何扩展转码器？

```bash
# Docker Compose
docker-compose up -d --scale transcoder=3

# Kubernetes
kubectl scale deployment streamgate-transcoder --replicas=8
```

### Q: 如何查看转码器指标？

```bash
grpcurl -plaintext localhost:9092 streamgate.Transcoder/GetMetrics
```

### Q: 如何重启服务？

```bash
# Docker Compose
docker-compose restart api-gateway

# Kubernetes
kubectl rollout restart deployment/streamgate-api-gateway
```

### Q: 如何查看日志？

```bash
# Docker Compose
docker-compose logs -f api-gateway

# Kubernetes
kubectl logs -f deployment/streamgate-api-gateway
```

## 🚀 下一步

1. **了解架构** → 查看 `docs/project-planning/architecture/`
2. **了解目录结构** → 查看 `docs/project-planning/directory-structure/`
3. **了解实现计划** → 查看 `docs/project-planning/implementation/`
4. **开始开发** → 查看 `docs/development/`

## 📞 获取帮助

- 查看 `README.md` 了解项目概述
- 查看 `docs/project-planning/` 了解详细规划
- 查看 `docs/deployment/` 了解部署详情
- 查看 `docs/development/` 了解开发指南

---

**最后更新**: 2025-01-28
**版本**: 1.0
**状态**: ✅ 完成
