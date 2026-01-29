# StreamGate - 目录结构实施指南

## 概述

本指南提供了逐步创建StreamGate项目目录结构的详细步骤。

## 第1阶段：创建基础目录结构

### 1.1 创建pkg目录结构

```bash
# 创建pkg核心目录
mkdir -p pkg/core/{config,logger,event,health,lifecycle}
mkdir -p pkg/plugins/{api,upload,transcoder,streaming,metadata,cache,auth,worker,monitor}
mkdir -p pkg/{models,storage,service,api/v1,api/grpc,middleware,util,web3}
```

### 1.2 创建proto目录结构

```bash
mkdir -p proto/v1
mkdir -p proto/gen/{go,python}
```

### 1.3 创建config目录结构

```bash
mkdir -p config
```

### 1.4 创建migrations目录结构

```bash
mkdir -p migrations
```

### 1.5 创建scripts目录结构

```bash
mkdir -p scripts
```

### 1.6 创建test目录结构

```bash
mkdir -p test/{unit,integration,e2e,fixtures,mocks}
mkdir -p test/unit/{core,plugins,service,util}
```

### 1.7 创建deploy目录结构

```bash
mkdir -p deploy/docker
mkdir -p deploy/k8s/{monolith,microservices}
mkdir -p deploy/k8s/microservices/{api-gateway,upload,transcoder,streaming,metadata,cache,auth,worker,monitor}
mkdir -p deploy/helm/templates
```

### 1.8 创建docs目录结构

```bash
mkdir -p docs/{architecture,api,web3,deployment,development,operations,guides}
```

### 1.9 创建examples目录结构

```bash
mkdir -p examples/{nft-verify-demo,signature-verify-demo,upload-demo,streaming-demo}
```

### 1.10 创建GitHub配置目录

```bash
mkdir -p .github/{workflows,ISSUE_TEMPLATE}
```

## 第2阶段：创建核心文件

### 2.1 pkg/core/ 文件

**pkg/core/microkernel.go**
```go
package core

// Microkernel 微内核实现
type Microkernel struct {
    // 实现细节
}

// NewMicrokernel 创建新的微内核
func NewMicrokernel() *Microkernel {
    return &Microkernel{}
}
```

**pkg/core/config/config.go**
```go
package config

// Config 配置结构
type Config struct {
    // 配置字段
}

// LoadConfig 加载配置
func LoadConfig() (*Config, error) {
    // 实现细节
    return nil, nil
}
```

**pkg/core/logger/logger.go**
```go
package logger

// Logger 日志记录器
type Logger struct {
    // 实现细节
}

// NewLogger 创建新的日志记录器
func NewLogger(name string) *Logger {
    return &Logger{}
}
```

**pkg/core/event/event.go**
```go
package event

// EventBus 事件总线
type EventBus struct {
    // 实现细节
}

// NewEventBus 创建新的事件总线
func NewEventBus() *EventBus {
    return &EventBus{}
}
```

**pkg/core/health/health.go**
```go
package health

// HealthChecker 健康检查器
type HealthChecker struct {
    // 实现细节
}

// NewHealthChecker 创建新的健康检查器
func NewHealthChecker() *HealthChecker {
    return &HealthChecker{}
}
```

**pkg/core/lifecycle/lifecycle.go**
```go
package lifecycle

// LifecycleManager 生命周期管理器
type LifecycleManager struct {
    // 实现细节
}

// NewLifecycleManager 创建新的生命周期管理器
func NewLifecycleManager() *LifecycleManager {
    return &LifecycleManager{}
}
```

### 2.2 pkg/models/ 文件

**pkg/models/content.go**
```go
package models

// Content 内容模型
type Content struct {
    ID        string
    Title     string
    CreatedAt int64
}
```

**pkg/models/user.go**
```go
package models

// User 用户模型
type User struct {
    ID      string
    Address string
}
```

**pkg/models/task.go**
```go
package models

// Task 任务模型
type Task struct {
    ID     string
    Status string
}
```

**pkg/models/nft.go**
```go
package models

// NFT NFT模型
type NFT struct {
    ID       string
    Contract string
    TokenID  string
}
```

**pkg/models/transaction.go**
```go
package models

// Transaction 交易模型
type Transaction struct {
    ID     string
    Hash   string
    Status string
}
```

### 2.3 pkg/storage/ 文件

**pkg/storage/db.go**
```go
package storage

// Database 数据库接口
type Database interface {
    // 数据库操作
}
```

**pkg/storage/postgres.go**
```go
package storage

// PostgresDB PostgreSQL实现
type PostgresDB struct {
    // 实现细节
}
```

**pkg/storage/cache.go**
```go
package storage

// Cache 缓存接口
type Cache interface {
    // 缓存操作
}
```

**pkg/storage/redis.go**
```go
package storage

// RedisCache Redis实现
type RedisCache struct {
    // 实现细节
}
```

**pkg/storage/object.go**
```go
package storage

// ObjectStorage 对象存储接口
type ObjectStorage interface {
    // 对象存储操作
}
```

**pkg/storage/s3.go**
```go
package storage

// S3Storage S3实现
type S3Storage struct {
    // 实现细节
}
```

**pkg/storage/minio.go**
```go
package storage

// MinIOStorage MinIO实现
type MinIOStorage struct {
    // 实现细节
}
```

### 2.4 pkg/service/ 文件

**pkg/service/content.go**
```go
package service

// ContentService 内容服务
type ContentService struct {
    // 实现细节
}

// NewContentService 创建新的内容服务
func NewContentService() *ContentService {
    return &ContentService{}
}
```

**pkg/service/upload.go**
```go
package service

// UploadService 上传服务
type UploadService struct {
    // 实现细节
}

// NewUploadService 创建新的上传服务
func NewUploadService() *UploadService {
    return &UploadService{}
}
```

**pkg/service/transcoding.go**
```go
package service

// TranscodingService 转码服务
type TranscodingService struct {
    // 实现细节
}

// NewTranscodingService 创建新的转码服务
func NewTranscodingService() *TranscodingService {
    return &TranscodingService{}
}
```

**pkg/service/streaming.go**
```go
package service

// StreamingService 流媒体服务
type StreamingService struct {
    // 实现细节
}

// NewStreamingService 创建新的流媒体服务
func NewStreamingService() *StreamingService {
    return &StreamingService{}
}
```

**pkg/service/auth.go**
```go
package service

// AuthService 认证服务
type AuthService struct {
    // 实现细节
}

// NewAuthService 创建新的认证服务
func NewAuthService() *AuthService {
    return &AuthService{}
}
```

**pkg/service/nft.go**
```go
package service

// NFTService NFT服务
type NFTService struct {
    // 实现细节
}

// NewNFTService 创建新的NFT服务
func NewNFTService() *NFTService {
    return &NFTService{}
}
```

**pkg/service/web3.go**
```go
package service

// Web3Service Web3服务
type Web3Service struct {
    // 实现细节
}

// NewWeb3Service 创建新的Web3服务
func NewWeb3Service() *Web3Service {
    return &Web3Service{}
}
```

### 2.5 pkg/middleware/ 文件

**pkg/middleware/auth.go**
```go
package middleware

// AuthMiddleware 认证中间件
func AuthMiddleware() {
    // 实现细节
}
```

**pkg/middleware/logging.go**
```go
package middleware

// LoggingMiddleware 日志中间件
func LoggingMiddleware() {
    // 实现细节
}
```

**pkg/middleware/ratelimit.go**
```go
package middleware

// RateLimitMiddleware 速率限制中间件
func RateLimitMiddleware() {
    // 实现细节
}
```

**pkg/middleware/cors.go**
```go
package middleware

// CORSMiddleware CORS中间件
func CORSMiddleware() {
    // 实现细节
}
```

**pkg/middleware/tracing.go**
```go
package middleware

// TracingMiddleware 追踪中间件
func TracingMiddleware() {
    // 实现细节
}
```

**pkg/middleware/recovery.go**
```go
package middleware

// RecoveryMiddleware 恢复中间件
func RecoveryMiddleware() {
    // 实现细节
}
```

### 2.6 pkg/util/ 文件

**pkg/util/crypto.go**
```go
package util

// Crypto 加密工具
type Crypto struct{}
```

**pkg/util/hash.go**
```go
package util

// Hash 哈希工具
type Hash struct{}
```

**pkg/util/time.go**
```go
package util

// Time 时间工具
type Time struct{}
```

**pkg/util/string.go**
```go
package util

// String 字符串工具
type String struct{}
```

**pkg/util/file.go**
```go
package util

// File 文件工具
type File struct{}
```

**pkg/util/validation.go**
```go
package util

// Validation 验证工具
type Validation struct{}
```

### 2.7 pkg/web3/ 文件

**pkg/web3/chain.go**
```go
package web3

// ChainManager 链管理器
type ChainManager struct{}
```

**pkg/web3/contract.go**
```go
package web3

// ContractManager 智能合约管理器
type ContractManager struct{}
```

**pkg/web3/nft.go**
```go
package web3

// NFTManager NFT管理器
type NFTManager struct{}
```

**pkg/web3/signature.go**
```go
package web3

// SignatureVerifier 签名验证器
type SignatureVerifier struct{}
```

**pkg/web3/wallet.go**
```go
package web3

// WalletManager 钱包管理器
type WalletManager struct{}
```

**pkg/web3/gas.go**
```go
package web3

// GasManager Gas管理器
type GasManager struct{}
```

**pkg/web3/ipfs.go**
```go
package web3

// IPFSManager IPFS管理器
type IPFSManager struct{}
```

**pkg/web3/multichain.go**
```go
package web3

// MultiChainManager 多链管理器
type MultiChainManager struct{}
```

## 第3阶段：创建插件框架

### 3.1 API网关插件

**pkg/plugins/api/gateway.go**
```go
package api

// Gateway gRPC网关
type Gateway struct{}
```

**pkg/plugins/api/rest.go**
```go
package api

// REST REST API处理
type REST struct{}
```

**pkg/plugins/api/auth.go**
```go
package api

// Auth 认证中间件
type Auth struct{}
```

**pkg/plugins/api/ratelimit.go**
```go
package api

// RateLimit 速率限制
type RateLimit struct{}
```

### 3.2 上传插件

**pkg/plugins/upload/handler.go**
```go
package upload

// Handler 上传处理器
type Handler struct{}
```

**pkg/plugins/upload/chunked.go**
```go
package upload

// Chunked 分块上传
type Chunked struct{}
```

**pkg/plugins/upload/resumable.go**
```go
package upload

// Resumable 可恢复上传
type Resumable struct{}
```

**pkg/plugins/upload/storage.go**
```go
package upload

// Storage 存储接口
type Storage interface{}
```

### 3.3 转码插件

**pkg/plugins/transcoder/transcoder.go**
```go
package transcoder

// Transcoder 转码器
type Transcoder struct{}
```

**pkg/plugins/transcoder/worker.go**
```go
package transcoder

// Worker 工作池
type Worker struct{}
```

**pkg/plugins/transcoder/queue.go**
```go
package transcoder

// Queue 任务队列
type Queue struct{}
```

**pkg/plugins/transcoder/scaler.go**
```go
package transcoder

// Scaler 自动扩展
type Scaler struct{}
```

**pkg/plugins/transcoder/ffmpeg.go**
```go
package transcoder

// FFmpeg FFmpeg集成
type FFmpeg struct{}
```

### 3.4 流媒体插件

**pkg/plugins/streaming/handler.go**
```go
package streaming

// Handler 流媒体处理器
type Handler struct{}
```

**pkg/plugins/streaming/hls.go**
```go
package streaming

// HLS HLS支持
type HLS struct{}
```

**pkg/plugins/streaming/dash.go**
```go
package streaming

// DASH DASH支持
type DASH struct{}
```

**pkg/plugins/streaming/adaptive.go**
```go
package streaming

// Adaptive 自适应码率
type Adaptive struct{}
```

**pkg/plugins/streaming/cache.go**
```go
package streaming

// Cache 缓存管理
type Cache struct{}
```

### 3.5 元数据插件

**pkg/plugins/metadata/handler.go**
```go
package metadata

// Handler 元数据处理器
type Handler struct{}
```

**pkg/plugins/metadata/db.go**
```go
package metadata

// DB 数据库操作
type DB struct{}
```

**pkg/plugins/metadata/index.go**
```go
package metadata

// Index 索引管理
type Index struct{}
```

**pkg/plugins/metadata/search.go**
```go
package metadata

// Search 搜索功能
type Search struct{}
```

### 3.6 缓存插件

**pkg/plugins/cache/handler.go**
```go
package cache

// Handler 缓存处理器
type Handler struct{}
```

**pkg/plugins/cache/redis.go**
```go
package cache

// Redis Redis集成
type Redis struct{}
```

**pkg/plugins/cache/lru.go**
```go
package cache

// LRU LRU缓存
type LRU struct{}
```

**pkg/plugins/cache/ttl.go**
```go
package cache

// TTL TTL管理
type TTL struct{}
```

### 3.7 认证插件

**pkg/plugins/auth/handler.go**
```go
package auth

// Handler 认证处理器
type Handler struct{}
```

**pkg/plugins/auth/nft.go**
```go
package auth

// NFT NFT验证
type NFT struct{}
```

**pkg/plugins/auth/signature.go**
```go
package auth

// Signature 签名验证
type Signature struct{}
```

**pkg/plugins/auth/web3.go**
```go
package auth

// Web3 Web3集成
type Web3 struct{}
```

**pkg/plugins/auth/multichain.go**
```go
package auth

// MultiChain 多链支持
type MultiChain struct{}
```

### 3.8 工作插件

**pkg/plugins/worker/handler.go**
```go
package worker

// Handler 工作处理器
type Handler struct{}
```

**pkg/plugins/worker/job.go**
```go
package worker

// Job 任务定义
type Job struct{}
```

**pkg/plugins/worker/scheduler.go**
```go
package worker

// Scheduler 任务调度
type Scheduler struct{}
```

**pkg/plugins/worker/executor.go**
```go
package worker

// Executor 任务执行器
type Executor struct{}
```

### 3.9 监控插件

**pkg/plugins/monitor/handler.go**
```go
package monitor

// Handler 监控处理器
type Handler struct{}
```

**pkg/plugins/monitor/metrics.go**
```go
package monitor

// Metrics 指标收集
type Metrics struct{}
```

**pkg/plugins/monitor/health.go**
```go
package monitor

// Health 健康检查
type Health struct{}
```

**pkg/plugins/monitor/alert.go**
```go
package monitor

// Alert 告警系统
type Alert struct{}
```

## 第4阶段：创建API定义

### 4.1 Protocol Buffers定义

**proto/v1/common.proto**
```protobuf
syntax = "proto3";

package streamgate.v1;

message Error {
    int32 code = 1;
    string message = 2;
}
```

**proto/v1/content.proto**
```protobuf
syntax = "proto3";

package streamgate.v1;

message Content {
    string id = 1;
    string title = 2;
    int64 created_at = 3;
}
```

**proto/v1/upload.proto**
```protobuf
syntax = "proto3";

package streamgate.v1;

message UploadRequest {
    string filename = 1;
    bytes data = 2;
}

message UploadResponse {
    string file_id = 1;
}
```

**proto/v1/streaming.proto**
```protobuf
syntax = "proto3";

package streamgate.v1;

message StreamRequest {
    string content_id = 1;
    string format = 2;
}

message StreamResponse {
    string manifest_url = 1;
}
```

**proto/v1/auth.proto**
```protobuf
syntax = "proto3";

package streamgate.v1;

message AuthRequest {
    string address = 1;
    string signature = 2;
}

message AuthResponse {
    string token = 1;
}
```

**proto/v1/nft.proto**
```protobuf
syntax = "proto3";

package streamgate.v1;

message NFTVerifyRequest {
    string address = 1;
    string contract = 2;
    string token_id = 3;
}

message NFTVerifyResponse {
    bool verified = 1;
}
```

### 4.2 REST API定义

**pkg/api/v1/content.go**
```go
package v1

// ContentAPI 内容API
type ContentAPI struct{}
```

**pkg/api/v1/upload.go**
```go
package v1

// UploadAPI 上传API
type UploadAPI struct{}
```

**pkg/api/v1/streaming.go**
```go
package v1

// StreamingAPI 流媒体API
type StreamingAPI struct{}
```

**pkg/api/v1/auth.go**
```go
package v1

// AuthAPI 认证API
type AuthAPI struct{}
```

**pkg/api/v1/nft.go**
```go
package v1

// NFTAPI NFT API
type NFTAPI struct{}
```

## 第5阶段：创建配置文件

### 5.1 配置文件

**config/config.yaml**
```yaml
deployment:
  mode: monolith
  service_name: streamgate

server:
  port: 8080
  read_timeout: 30s
  write_timeout: 30s

grpc:
  port: 9090

database:
  driver: postgres
  host: localhost
  port: 5432
  user: postgres
  password: postgres
  dbname: streamgate

cache:
  driver: redis
  host: localhost
  port: 6379

storage:
  driver: minio
  endpoint: localhost:9000
  access_key: minioadmin
  secret_key: minioadmin
  bucket: streamgate

eventbus:
  type: nats
  url: nats://localhost:4222

registry:
  type: consul
  address: localhost:8500
```

**config/config.dev.yaml**
```yaml
deployment:
  mode: monolith

server:
  port: 8080

database:
  host: localhost
  port: 5432
```

**config/config.prod.yaml**
```yaml
deployment:
  mode: microservice

server:
  port: 8080

database:
  host: prod-db.example.com
  port: 5432
```

## 第6阶段：创建部署配置

### 6.1 Docker配置

**deploy/docker/Dockerfile.monolith**
```dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o streamgate ./cmd/monolith/streamgate

FROM alpine:latest
COPY --from=builder /app/streamgate /app/
ENTRYPOINT ["/app/streamgate"]
```

**deploy/docker/Dockerfile.api-gateway**
```dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o api-gateway ./cmd/microservices/api-gateway

FROM alpine:latest
COPY --from=builder /app/api-gateway /app/
ENTRYPOINT ["/app/api-gateway"]
```

### 6.2 Kubernetes配置

**deploy/k8s/namespace.yaml**
```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: streamgate
```

**deploy/k8s/monolith/deployment.yaml**
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: streamgate-monolith
  namespace: streamgate
spec:
  replicas: 1
  selector:
    matchLabels:
      app: streamgate-monolith
  template:
    metadata:
      labels:
        app: streamgate-monolith
    spec:
      containers:
      - name: streamgate
        image: streamgate:monolith
        ports:
        - containerPort: 8080
```

## 第7阶段：创建文档

### 7.1 架构文档

**docs/architecture/microkernel.md**
```markdown
# 微内核架构

## 概述

StreamGate使用微内核架构...

## 核心组件

- Plugin Manager
- Event Bus
- Config Manager
- Logger
- Health Manager
- Lifecycle Manager
```

**docs/architecture/microservices.md**
```markdown
# 微服务架构

## 概述

StreamGate支持微服务部署模式...

## 9个微服务

1. API Gateway
2. Upload Service
3. Transcoder Service
...
```

### 7.2 API文档

**docs/api/rest-api.md**
```markdown
# REST API文档

## 内容API

### 获取内容

GET /api/v1/content/{id}

### 创建内容

POST /api/v1/content
```

**docs/api/grpc-api.md**
```markdown
# gRPC API文档

## 内容服务

### GetContent

```

### 7.3 部署文档

**docs/deployment/docker-compose.md**
```markdown
# Docker Compose部署

## 启动所有服务

docker-compose up -d

## 检查服务状态

docker-compose ps
```

## 总结

按照这个实施指南，你可以逐步创建一个清晰、简洁、可维护的项目目录结构。

### 关键要点

1. **分阶段实施** - 按照7个阶段逐步创建
2. **从基础开始** - 先创建目录结构，再创建文件
3. **遵循规范** - 使用标准的Go项目结构
4. **保持简洁** - 避免过度设计
5. **文档完整** - 为每个部分创建文档

### 预期结果

- ✅ 清晰的目录结构
- ✅ 易于导航的代码组织
- ✅ 可维护的项目布局
- ✅ 可扩展的架构
- ✅ 专业的项目形象

---

**建议**: 使用脚本自动化创建目录结构，然后逐步填充文件。
