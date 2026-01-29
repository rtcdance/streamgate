# StreamGate 快速运行指南

**日期**: 2025-01-29  
**版本**: 1.0.0  
**状态**: ✅ 完整

## 5 分钟快速启动

### 前置条件检查

```bash
# 检查 Go 版本
go version  # 需要 1.21+

# 检查 Docker
docker --version
docker-compose --version
```

### 方案 A: Docker Compose (最简单)

```bash
# 1. 启动所有服务
docker-compose up -d

# 2. 等待服务启动 (30秒)
sleep 30

# 3. 检查健康状态
curl http://localhost:8080/api/v1/health

# 4. 查看日志
docker-compose logs -f api-gateway
```

**预期输出**:
```json
{
  "status": "healthy",
  "timestamp": "2025-01-29T10:00:00Z",
  "services": {
    "database": "healthy",
    "cache": "healthy",
    "storage": "healthy"
  }
}
```

### 方案 B: 本地编译运行

```bash
# 1. 下载依赖
go mod download
go mod tidy

# 2. 编译单体应用
make build-monolith

# 3. 启动基础设施
docker-compose up -d postgres redis

# 4. 运行应用
./bin/streamgate

# 5. 在另一个终端测试
curl http://localhost:8080/api/v1/health
```

### 方案 C: 使用快速脚本

```bash
# 完整编译和启动
bash scripts/quick-build.sh full

# 运行单体应用
bash scripts/quick-build.sh run-monolith
```

## 详细启动步骤

### 步骤 1: 准备环境

```bash
# 克隆项目
git clone https://github.com/rtcdance/streamgate.git
cd streamgate

# 复制环境配置
cp .env.example .env

# 查看配置
cat .env
```

### 步骤 2: 启动基础设施

#### 选项 A: Docker Compose (推荐)

```bash
# 启动所有服务
docker-compose up -d

# 验证服务
docker-compose ps

# 预期输出:
# NAME                COMMAND                  SERVICE             STATUS
# streamgate-postgres "docker-entrypoint..."   postgres            Up 2 minutes
# streamgate-redis    "redis-server"           redis               Up 2 minutes
# streamgate-nats     "nats-server"            nats                Up 2 minutes
# streamgate-consul   "agent -server..."       consul              Up 2 minutes
```

#### 选项 B: 手动启动

```bash
# PostgreSQL
docker run -d -p 5432:5432 \
  -e POSTGRES_PASSWORD=streamgate \
  -e POSTGRES_DB=streamgate \
  postgres:15

# Redis
docker run -d -p 6379:6379 redis:7

# NATS
docker run -d -p 4222:4222 nats:latest

# Consul
docker run -d -p 8500:8500 consul:latest
```

### 步骤 3: 初始化数据库

```bash
# 运行迁移
psql -h localhost -U streamgate -d streamgate < migrations/001_init_schema.sql
psql -h localhost -U streamgate -d streamgate < migrations/002_add_content_table.sql
psql -h localhost -U streamgate -d streamgate < migrations/003_add_user_table.sql
psql -h localhost -U streamgate -d streamgate < migrations/004_add_nft_table.sql
psql -h localhost -U streamgate -d streamgate < migrations/005_add_transaction_table.sql

# 验证
psql -h localhost -U streamgate -d streamgate -c "\dt"
```

### 步骤 4: 编译应用

```bash
# 下载依赖
go mod download
go mod tidy

# 编译单体应用
make build-monolith

# 或编译所有微服务
make build-all

# 验证编译
ls -lh bin/
```

### 步骤 5: 运行应用

#### 运行单体应用

```bash
# 启动
./bin/streamgate

# 预期输出:
# 2025-01-29T10:00:00Z  INFO  streamgate-monolith  Starting StreamGate Monolithic Mode...
# 2025-01-29T10:00:00Z  INFO  streamgate-monolith  Configuration loaded
# 2025-01-29T10:00:00Z  INFO  streamgate-monolith  StreamGate Monolithic Mode started successfully
```

#### 运行微服务

```bash
# 在不同的终端启动各服务
./bin/api-gateway &
./bin/auth &
./bin/cache &
./bin/metadata &
./bin/monitor &
./bin/streaming &
./bin/transcoder &
./bin/upload &
./bin/worker &

# 查看运行的进程
ps aux | grep bin/
```

### 步骤 6: 验证应用

```bash
# 健康检查
curl http://localhost:8080/api/v1/health

# 查看日志
tail -f /var/log/streamgate/app.log

# 访问 Consul UI
open http://localhost:8500

# 访问 Prometheus
open http://localhost:9090

# 访问 Grafana
open http://localhost:3000
```

## API 测试

### 1. 认证

```bash
# 获取 nonce
curl -X POST http://localhost:8080/api/v1/auth/nonce \
  -H "Content-Type: application/json" \
  -d '{
    "wallet_address": "0x742d35Cc6634C0532925a3b844Bc9e7595f42bE"
  }'

# 响应:
# {
#   "nonce": "streamgate_nonce_1234567890",
#   "expires_at": "2025-01-29T10:30:00Z"
# }
```

### 2. 创建内容

```bash
TOKEN="your_jwt_token"

curl -X POST http://localhost:8080/api/v1/content \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "title": "My Video",
    "description": "Test video",
    "nft_contract": "0x...",
    "nft_token_id": "1",
    "chain": "ethereum"
  }'
```

### 3. 上传文件

```bash
# 初始化上传
curl -X POST http://localhost:8080/api/v1/upload/init \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "filename": "video.mp4",
    "size": 1073741824,
    "content_type": "video/mp4"
  }'

# 上传分块
curl -X PUT http://localhost:8080/api/v1/upload/{upload_id}/chunk/1 \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/octet-stream" \
  --data-binary @video.mp4
```

## 常见问题

### Q1: 端口被占用

```bash
# 查找占用端口的进程
lsof -i :8080

# 杀死进程
kill -9 <PID>

# 或使用不同端口
PORT=8081 ./bin/streamgate
```

### Q2: 数据库连接失败

```bash
# 检查 PostgreSQL
psql -h localhost -U streamgate -d streamgate -c "SELECT 1"

# 检查 Redis
redis-cli ping

# 检查环境变量
env | grep DB_
```

### Q3: 依赖下载失败

```bash
# 设置代理
export GOPROXY=https://goproxy.cn

# 重试
go mod download
go mod tidy
```

### Q4: 编译失败

```bash
# 清理缓存
go clean -cache
go clean -modcache

# 重新下载
go mod download

# 重新编译
make build-all
```

## 监控和调试

### 查看日志

```bash
# Docker Compose
docker-compose logs -f api-gateway

# 本地运行
tail -f /var/log/streamgate/app.log

# 实时日志
journalctl -u streamgate -f
```

### 性能监控

```bash
# CPU 和内存
top -p $(pgrep -f streamgate)

# 网络连接
netstat -an | grep 8080

# 磁盘使用
df -h

# 进程信息
ps aux | grep streamgate
```

### 调试

```bash
# 启用调试日志
LOG_LEVEL=debug ./bin/streamgate

# 启用 pprof
PPROF_ENABLED=true ./bin/streamgate

# 访问 pprof
curl http://localhost:6060/debug/pprof/

# CPU 分析
go tool pprof http://localhost:6060/debug/pprof/profile?seconds=30

# 内存分析
go tool pprof http://localhost:6060/debug/pprof/heap
```

## 停止应用

### Docker Compose

```bash
# 停止所有服务
docker-compose down

# 停止并删除数据
docker-compose down -v
```

### 本地运行

```bash
# 停止单体应用
pkill -f "bin/streamgate"

# 停止所有微服务
pkill -f "bin/"

# 或使用 Ctrl+C
```

## 下一步

### 开发

1. 阅读 [Architecture Deep Dive](docs/guides/ARCHITECTURE_DEEP_DIVE.md)
2. 查看 [Testing Guide](docs/guides/TESTING_GUIDE.md)
3. 运行测试: `make test`

### 部署

1. 阅读 [Production Operations](docs/guides/PRODUCTION_OPERATIONS.md)
2. 配置 Kubernetes: `kubectl apply -f deploy/k8s/`
3. 设置监控: `docker-compose up -d prometheus grafana`

### Web3 集成

1. 配置 RPC: 编辑 `.env` 中的 `ETH_RPC_URL`
2. 测试 NFT 验证: `curl -X POST http://localhost:8080/api/v1/auth/verify-nft`
3. 查看示例: `examples/nft-verify-demo/`

## 性能基准

| 操作 | 时间 |
|------|------|
| 启动单体应用 | ~2-3s |
| 启动所有微服务 | ~10-15s |
| 首次 API 请求 | ~100-200ms |
| 缓存命中 | ~1-5ms |
| 数据库查询 | ~20-50ms |

## 资源需求

| 资源 | 最小 | 推荐 |
|------|------|------|
| CPU | 2 核 | 4 核 |
| 内存 | 2GB | 4GB |
| 磁盘 | 10GB | 50GB |
| 网络 | 100Mbps | 1Gbps |

## 支持

### 文档
- [README.md](README.md) - 项目概述
- [API Documentation](docs/api/API_DOCUMENTATION.md) - API 参考
- [Troubleshooting Guide](docs/operations/TROUBLESHOOTING_GUIDE.md) - 故障排查

### 示例
- [NFT Verification](examples/nft-verify-demo/) - NFT 验证示例
- [Signature Verification](examples/signature-verify-demo/) - 签名验证示例
- [Streaming](examples/streaming-demo/) - 流媒体示例
- [Upload](examples/upload-demo/) - 上传示例

### 社区
- GitHub Issues: 报告 bug
- GitHub Discussions: 提问和讨论
- 文档: 查看 docs/ 目录

---

**最后更新**: 2025-01-29  
**版本**: 1.0.0  
**状态**: ✅ 完成
