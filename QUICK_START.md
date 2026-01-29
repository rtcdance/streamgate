# StreamGate 快速开始指南

**Date**: 2025-01-28  
**Status**: ✅ READY  
**Version**: 1.0.0

## 前置要求

- Go 1.21+
- Docker (可选)
- Kubernetes (可选)

## 编译

### 编译单个服务

```bash
# API Gateway
go build -o api-gateway ./cmd/microservices/api-gateway

# Upload Service
go build -o upload ./cmd/microservices/upload

# Streaming Service
go build -o streaming ./cmd/microservices/streaming

# Metadata Service
go build -o metadata ./cmd/microservices/metadata

# Cache Service
go build -o cache ./cmd/microservices/cache

# Auth Service
go build -o auth ./cmd/microservices/auth

# Worker Service
go build -o worker ./cmd/microservices/worker

# Monitor Service
go build -o monitor ./cmd/microservices/monitor

# Transcoder Service
go build -o transcoder ./cmd/microservices/transcoder
```

### 编译所有服务

```bash
go build ./cmd/microservices/...
```

### 编译单体应用

```bash
go build -o streamgate ./cmd/monolith/streamgate
```

## 运行

### 运行单个服务

```bash
# API Gateway (Port 9090)
./api-gateway

# Upload Service (Port 9091)
./upload

# Streaming Service (Port 9093)
./streaming

# 其他服务...
```

### 运行所有服务 (Docker Compose)

```bash
docker-compose up
```

### 运行单体应用

```bash
./streamgate
```

## 测试

### 运行所有测试

```bash
go test ./...
```

### 运行特定包的测试

```bash
go test ./pkg/plugins/upload/...
go test ./pkg/plugins/streaming/...
```

### 运行 E2E 测试

```bash
go test ./test/e2e/...
```

## 验证

### 检查 API Gateway 健康状态

```bash
curl http://localhost:9090/health
```

### 检查其他服务

```bash
curl http://localhost:9091/health  # Upload
curl http://localhost:9093/health  # Streaming
curl http://localhost:9005/health  # Metadata
curl http://localhost:9006/health  # Cache
curl http://localhost:9007/health  # Auth
curl http://localhost:9008/health  # Worker
curl http://localhost:9009/health  # Monitor
curl http://localhost:9092/health  # Transcoder
```

## 配置

### 环境变量

```bash
export SERVER_PORT=9090
export SERVER_READ_TIMEOUT=15
export SERVER_WRITE_TIMEOUT=15
export DB_HOST=localhost
export DB_PORT=5432
export REDIS_HOST=localhost
export REDIS_PORT=6379
export NATS_URL=nats://localhost:4222
```

### 配置文件

- `config/config.yaml` - 默认配置
- `config/config.dev.yaml` - 开发配置
- `config/config.prod.yaml` - 生产配置
- `config/config.test.yaml` - 测试配置

## 部署

### Docker

```bash
# 构建镜像
docker build -f deploy/docker/Dockerfile.api-gateway -t streamgate-api-gateway .

# 运行容器
docker run -p 9090:9090 streamgate-api-gateway
```

### Kubernetes

```bash
# 部署
kubectl apply -f deploy/k8s/

# 检查 Pod
kubectl get pods -n streamgate

# 查看日志
kubectl logs -n streamgate <pod-name>
```

### Helm

```bash
# 安装
helm install streamgate deploy/helm/

# 升级
helm upgrade streamgate deploy/helm/

# 卸载
helm uninstall streamgate
```

## 常见问题

### Q: 编译失败，提示缺少依赖
A: 运行 `go mod download` 下载所有依赖

### Q: 运行时提示连接数据库失败
A: 确保 PostgreSQL 已启动，或修改配置文件中的数据库连接信息

### Q: 运行时提示连接 Redis 失败
A: 确保 Redis 已启动，或修改配置文件中的 Redis 连接信息

### Q: 如何查看日志
A: 日志会输出到标准输出，可以重定向到文件：
```bash
./api-gateway > api-gateway.log 2>&1
```

## 文档

- [FINAL_VERIFICATION_REPORT.md](FINAL_VERIFICATION_REPORT.md) - 最终验证报告
- [IMPORT_PATH_FIX_SUMMARY.md](IMPORT_PATH_FIX_SUMMARY.md) - Import 路径修复总结
- [MICROSERVICES_IMPLEMENTATION_GUIDE.md](MICROSERVICES_IMPLEMENTATION_GUIDE.md) - 微服务实现指南
- [docs/deployment/QUICK_START.md](docs/deployment/QUICK_START.md) - 部署快速开始

## 支持

如有问题，请查看：
- [README.md](README.md) - 项目概述
- [docs/](docs/) - 完整文档
- [test/](test/) - 测试示例

---

**Status**: ✅ READY
**Last Updated**: 2025-01-28
**Version**: 1.0.0
