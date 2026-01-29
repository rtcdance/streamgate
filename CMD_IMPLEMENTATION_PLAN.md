# CMD 程序实现计划

**Date**: 2025-01-28  
**Status**: 需要实现  
**Priority**: 高  
**Version**: 1.0.0

## 问题分析

当前 cmd 目录下的程序都是伪代码框架，缺少真实的实现：

### 现状
- ✅ 框架结构完整
- ✅ 配置加载逻辑
- ✅ 日志初始化
- ✅ 优雅关闭处理
- ❌ **缺少真实的业务逻辑实现**
- ❌ **缺少真实的 HTTP/gRPC 服务器**
- ❌ **缺少真实的数据库连接**
- ❌ **缺少真实的缓存连接**
- ❌ **缺少真实的消息队列连接**

## 需要实现的程序

### 1. 单体应用 (Monolith)
**文件**: `cmd/monolith/streamgate/main.go`

需要实现：
- ✅ 框架 (已有)
- ❌ HTTP 服务器 (Gin/Echo)
- ❌ 所有 9 个插件的加载
- ❌ 数据库连接池
- ❌ Redis 连接
- ❌ NATS 连接
- ❌ 路由注册
- ❌ 中间件配置

### 2. 微服务 (9 个)

#### 2.1 API Gateway (Port 9090)
**文件**: `cmd/microservices/api-gateway/main.go`

需要实现：
- ❌ REST API 路由
- ❌ gRPC Gateway
- ❌ 认证中间件
- ❌ 速率限制
- ❌ 请求路由到其他服务
- ❌ 响应聚合

#### 2.2 Upload Service (Port 9091)
**文件**: `cmd/microservices/upload/main.go`

需要实现：
- ❌ 文件上传处理
- ❌ 分块上传支持
- ❌ S3/MinIO 集成
- ❌ 数据库存储
- ❌ 事件发布

#### 2.3 Transcoder Service (Port 9092)
**文件**: `cmd/microservices/transcoder/main.go`

需要实现：
- ❌ 转码任务处理
- ❌ FFmpeg 集成
- ❌ 工作池管理
- ❌ 任务队列
- ❌ 进度跟踪

#### 2.4 Streaming Service (Port 9093)
**文件**: `cmd/microservices/streaming/main.go`

需要实现：
- ❌ HLS/DASH 流媒体
- ❌ 自适应码率
- ❌ 缓存管理
- ❌ 播放列表生成

#### 2.5 Metadata Service (Port 9005)
**文件**: `cmd/microservices/metadata/main.go`

需要实现：
- ❌ 数据库操作
- ❌ 元数据管理
- ❌ 搜索索引
- ❌ 查询优化

#### 2.6 Cache Service (Port 9006)
**文件**: `cmd/microservices/cache/main.go`

需要实现：
- ❌ Redis 连接管理
- ❌ 缓存策略
- ❌ TTL 管理
- ❌ 缓存失效

#### 2.7 Auth Service (Port 9007)
**文件**: `cmd/microservices/auth/main.go`

需要实现：
- ❌ NFT 验证
- ❌ 签名验证
- ❌ 多链支持
- ❌ 令牌生成

#### 2.8 Worker Service (Port 9008)
**文件**: `cmd/microservices/worker/main.go`

需要实现：
- ❌ 后台任务处理
- ❌ 任务队列
- ❌ 调度器
- ❌ 重试逻辑

#### 2.9 Monitor Service (Port 9009)
**文件**: `cmd/microservices/monitor/main.go`

需要实现：
- ❌ 健康检查
- ❌ 指标收集
- ❌ 告警生成
- ❌ 日志聚合

## 实现优先级

### 优先级 1 - 核心服务 (立即实现)
1. API Gateway - 系统入口
2. Upload Service - 核心功能
3. Streaming Service - 核心功能

### 优先级 2 - 支持服务 (本周实现)
4. Metadata Service - 数据管理
5. Cache Service - 性能优化
6. Auth Service - 安全认证

### 优先级 3 - 辅助服务 (本月实现)
7. Worker Service - 后台处理
8. Monitor Service - 监控运维
9. Transcoder Service - 高级功能

## 实现步骤

### 第一步：完成 API Gateway
```go
// 需要实现的关键部分：
1. HTTP 服务器初始化 (Gin)
2. gRPC Gateway 配置
3. 路由注册
4. 中间件配置
5. 服务发现集成
6. 请求路由逻辑
```

### 第二步：完成 Upload Service
```go
// 需要实现的关键部分：
1. 文件上传处理
2. 分块上传支持
3. 存储后端集成
4. 数据库操作
5. 事件发布
```

### 第三步：完成 Streaming Service
```go
// 需要实现的关键部分：
1. HLS 清单生成
2. DASH 清单生成
3. 自适应码率选择
4. 缓存管理
5. 播放列表处理
```

### 第四步：完成其他服务
```go
// 按优先级逐个实现其他服务
```

## 技术栈选择

### Web 框架
- **Gin** - 高性能 HTTP 框架
- **gRPC** - 高性能 RPC 框架
- **Protocol Buffers** - 数据序列化

### 数据库
- **PostgreSQL** - 主数据库
- **Redis** - 缓存层
- **NATS** - 消息队列

### 存储
- **S3/MinIO** - 对象存储
- **PostgreSQL** - 元数据存储

## 代码示例

### API Gateway 实现框架
```go
package main

import (
    "github.com/gin-gonic/gin"
    "google.golang.org/grpc"
)

func main() {
    // 初始化 HTTP 服务器
    router := gin.Default()
    
    // 注册路由
    registerRoutes(router)
    
    // 初始化 gRPC 服务器
    grpcServer := grpc.NewServer()
    
    // 启动服务
    go router.Run(":9090")
    go grpcServer.Serve(listener)
    
    // 等待关闭信号
    <-shutdownChan
}
```

### Upload Service 实现框架
```go
package main

import (
    "github.com/gin-gonic/gin"
)

func main() {
    router := gin.Default()
    
    // 文件上传路由
    router.POST("/upload", handleUpload)
    router.POST("/upload/chunk", handleChunkUpload)
    
    // 启动服务
    router.Run(":9091")
}

func handleUpload(c *gin.Context) {
    // 处理文件上传
    file, _ := c.FormFile("file")
    
    // 保存到存储
    // 发布事件
    // 返回响应
}
```

## 预期工作量

| 服务 | 代码行数 | 工作时间 |
|------|---------|---------|
| API Gateway | 500-800 | 4-6 小时 |
| Upload Service | 400-600 | 3-4 小时 |
| Streaming Service | 400-600 | 3-4 小时 |
| Metadata Service | 300-500 | 2-3 小时 |
| Cache Service | 200-400 | 1-2 小时 |
| Auth Service | 300-500 | 2-3 小时 |
| Worker Service | 300-500 | 2-3 小时 |
| Monitor Service | 200-400 | 1-2 小时 |
| Transcoder Service | 400-600 | 3-4 小时 |
| **总计** | **3,200-5,000** | **21-31 小时** |

## 测试计划

### 单元测试
- 每个服务的业务逻辑测试
- 数据库操作测试
- 缓存操作测试

### 集成测试
- 服务间通信测试
- 数据流测试
- 事件处理测试

### E2E 测试
- 完整业务流程测试
- 多服务协作测试
- 故障恢复测试

## 部署验证

### 本地验证
```bash
# 启动单体应用
go run cmd/monolith/streamgate/main.go

# 启动微服务
go run cmd/microservices/api-gateway/main.go
go run cmd/microservices/upload/main.go
# ... 其他服务
```

### Docker 验证
```bash
# 构建镜像
docker build -f deploy/docker/Dockerfile.api-gateway -t streamgate-api-gateway .

# 运行容器
docker run -p 9090:9090 streamgate-api-gateway
```

### Kubernetes 验证
```bash
# 部署到 K8s
kubectl apply -f deploy/k8s/

# 验证服务
kubectl get pods -n streamgate
kubectl get svc -n streamgate
```

## 下一步行动

### 立即行动
1. ✅ 确认实现计划
2. ⏳ 实现 API Gateway
3. ⏳ 实现 Upload Service
4. ⏳ 实现 Streaming Service

### 本周完成
5. ⏳ 实现 Metadata Service
6. ⏳ 实现 Cache Service
7. ⏳ 实现 Auth Service

### 本月完成
8. ⏳ 实现 Worker Service
9. ⏳ 实现 Monitor Service
10. ⏳ 实现 Transcoder Service

## 结论

虽然 pkg 目录下的库代码已经 100% 完成，但 cmd 目录下的程序仍然是伪代码框架。需要实现真实的服务程序来完成项目。

预计需要 **21-31 小时** 的工作来完成所有 9 个微服务的真实实现。

---

**Status**: 需要实现  
**Priority**: 高  
**Estimated Effort**: 21-31 小时  
**Impact**: 关键 - 没有这些程序，系统无法运行  

**Document Status**: 计划  
**Last Updated**: 2025-01-28  
**Version**: 1.0.0
