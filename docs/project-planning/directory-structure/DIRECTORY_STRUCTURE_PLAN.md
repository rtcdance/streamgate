# StreamGate - 代码目录结构规划

## 设计原则

1. **清晰性** - 目录结构一目了然，易于导航
2. **简洁性** - 避免过度嵌套，最多3-4层
3. **可维护性** - 相关代码聚集在一起
4. **可扩展性** - 易于添加新功能和服务
5. **标准性** - 遵循Go项目最佳实践

## 完整目录结构

```
streamgate/
│
├── cmd/                                    # 应用程序入口点
│   ├── monolith/                          # 单体部署模式
│   │   └── streamgate/
│   │       └── main.go                    # 单体应用入口
│   │
│   └── microservices/                     # 微服务部署模式
│       ├── api-gateway/
│       │   └── main.go                    # API网关入口
│       ├── upload/
│       │   └── main.go                    # 上传服务入口
│       ├── transcoder/
│       │   └── main.go                    # 转码服务入口
│       ├── streaming/
│       │   └── main.go                    # 流媒体服务入口
│       ├── metadata/
│       │   └── main.go                    # 元数据服务入口
│       ├── cache/
│       │   └── main.go                    # 缓存服务入口
│       ├── auth/
│       │   └── main.go                    # 认证服务入口
│       ├── worker/
│       │   └── main.go                    # 工作服务入口
│       └── monitor/
│           └── main.go                    # 监控服务入口
│
├── pkg/                                   # 核心包和库
│   ├── core/                              # 微内核核心
│   │   ├── microkernel.go                 # 微内核实现
│   │   ├── config/
│   │   │   ├── config.go                  # 配置管理
│   │   │   └── loader.go                  # 配置加载器
│   │   ├── logger/
│   │   │   ├── logger.go                  # 日志记录器
│   │   │   └── formatter.go               # 日志格式化
│   │   ├── event/
│   │   │   ├── event.go                   # 事件总线
│   │   │   ├── publisher.go               # 事件发布者
│   │   │   └── subscriber.go              # 事件订阅者
│   │   ├── health/
│   │   │   ├── health.go                  # 健康检查
│   │   │   └── checker.go                 # 健康检查器
│   │   └── lifecycle/
│   │       ├── lifecycle.go               # 生命周期管理
│   │       └── manager.go                 # 生命周期管理器
│   │
│   ├── plugins/                           # 插件实现
│   │   ├── api/                           # API网关插件
│   │   │   ├── gateway.go                 # gRPC网关
│   │   │   ├── rest.go                    # REST API处理
│   │   │   ├── auth.go                    # 认证中间件
│   │   │   └── ratelimit.go               # 速率限制
│   │   │
│   │   ├── upload/                        # 上传插件
│   │   │   ├── handler.go                 # 上传处理器
│   │   │   ├── chunked.go                 # 分块上传
│   │   │   ├── resumable.go               # 可恢复上传
│   │   │   └── storage.go                 # 存储接口
│   │   │
│   │   ├── transcoder/                    # 转码插件
│   │   │   ├── transcoder.go              # 转码器
│   │   │   ├── worker.go                  # 工作池
│   │   │   ├── queue.go                   # 任务队列
│   │   │   ├── scaler.go                  # 自动扩展
│   │   │   └── ffmpeg.go                  # FFmpeg集成
│   │   │
│   │   ├── streaming/                     # 流媒体插件
│   │   │   ├── handler.go                 # 流媒体处理器
│   │   │   ├── hls.go                     # HLS支持
│   │   │   ├── dash.go                    # DASH支持
│   │   │   ├── adaptive.go                # 自适应码率
│   │   │   └── cache.go                   # 缓存管理
│   │   │
│   │   ├── metadata/                      # 元数据插件
│   │   │   ├── handler.go                 # 元数据处理器
│   │   │   ├── db.go                      # 数据库操作
│   │   │   ├── index.go                   # 索引管理
│   │   │   └── search.go                  # 搜索功能
│   │   │
│   │   ├── cache/                         # 缓存插件
│   │   │   ├── handler.go                 # 缓存处理器
│   │   │   ├── redis.go                   # Redis集成
│   │   │   ├── lru.go                     # LRU缓存
│   │   │   └── ttl.go                     # TTL管理
│   │   │
│   │   ├── auth/                          # 认证插件
│   │   │   ├── handler.go                 # 认证处理器
│   │   │   ├── nft.go                     # NFT验证
│   │   │   ├── signature.go               # 签名验证
│   │   │   ├── web3.go                    # Web3集成
│   │   │   └── multichain.go              # 多链支持
│   │   │
│   │   ├── worker/                        # 工作插件
│   │   │   ├── handler.go                 # 工作处理器
│   │   │   ├── job.go                     # 任务定义
│   │   │   ├── scheduler.go               # 任务调度
│   │   │   └── executor.go                # 任务执行器
│   │   │
│   │   └── monitor/                       # 监控插件
│   │       ├── handler.go                 # 监控处理器
│   │       ├── metrics.go                 # 指标收集
│   │       ├── health.go                  # 健康检查
│   │       └── alert.go                   # 告警系统
│   │
│   ├── models/                            # 数据模型
│   │   ├── content.go                     # 内容模型
│   │   ├── user.go                        # 用户模型
│   │   ├── task.go                        # 任务模型
│   │   ├── nft.go                         # NFT模型
│   │   └── transaction.go                 # 交易模型
│   │
│   ├── storage/                           # 存储层
│   │   ├── db.go                          # 数据库接口
│   │   ├── postgres.go                    # PostgreSQL实现
│   │   ├── cache.go                       # 缓存接口
│   │   ├── redis.go                       # Redis实现
│   │   ├── object.go                      # 对象存储接口
│   │   ├── s3.go                          # S3实现
│   │   └── minio.go                       # MinIO实现
│   │
│   ├── service/                           # 业务服务层
│   │   ├── content.go                     # 内容服务
│   │   ├── upload.go                      # 上传服务
│   │   ├── transcoding.go                 # 转码服务
│   │   ├── streaming.go                   # 流媒体服务
│   │   ├── auth.go                        # 认证服务
│   │   ├── nft.go                         # NFT服务
│   │   └── web3.go                        # Web3服务
│   │
│   ├── api/                               # API定义
│   │   ├── v1/
│   │   │   ├── content.go                 # 内容API
│   │   │   ├── upload.go                  # 上传API
│   │   │   ├── streaming.go               # 流媒体API
│   │   │   ├── auth.go                    # 认证API
│   │   │   └── nft.go                     # NFT API
│   │   └── grpc/
│   │       ├── content.proto              # 内容gRPC定义
│   │       ├── upload.proto               # 上传gRPC定义
│   │       ├── streaming.proto            # 流媒体gRPC定义
│   │       ├── auth.proto                 # 认证gRPC定义
│   │       └── nft.proto                  # NFT gRPC定义
│   │
│   ├── middleware/                        # 中间件
│   │   ├── auth.go                        # 认证中间件
│   │   ├── logging.go                     # 日志中间件
│   │   ├── ratelimit.go                   # 速率限制中间件
│   │   ├── cors.go                        # CORS中间件
│   │   ├── tracing.go                     # 追踪中间件
│   │   └── recovery.go                    # 恢复中间件
│   │
│   ├── util/                              # 工具函数
│   │   ├── crypto.go                      # 加密工具
│   │   ├── hash.go                        # 哈希工具
│   │   ├── time.go                        # 时间工具
│   │   ├── string.go                      # 字符串工具
│   │   ├── file.go                        # 文件工具
│   │   └── validation.go                  # 验证工具
│   │
│   └── web3/                              # Web3集成
│       ├── chain.go                       # 链管理
│       ├── contract.go                    # 智能合约
│       ├── nft.go                         # NFT操作
│       ├── signature.go                   # 签名验证
│       ├── wallet.go                      # 钱包集成
│       ├── gas.go                         # Gas管理
│       ├── ipfs.go                        # IPFS集成
│       └── multichain.go                  # 多链支持
│
├── proto/                                 # Protocol Buffers定义
│   ├── v1/
│   │   ├── content.proto
│   │   ├── upload.proto
│   │   ├── streaming.proto
│   │   ├── auth.proto
│   │   ├── nft.proto
│   │   ├── common.proto
│   │   └── errors.proto
│   └── gen/                               # 生成的代码
│       ├── go/
│       └── python/
│
├── config/                                # 配置文件
│   ├── config.yaml                        # 主配置文件
│   ├── config.dev.yaml                    # 开发配置
│   ├── config.prod.yaml                   # 生产配置
│   ├── config.test.yaml                   # 测试配置
│   └── prometheus.yml                     # Prometheus配置
│
├── migrations/                            # 数据库迁移
│   ├── 001_init_schema.sql
│   ├── 002_add_content_table.sql
│   ├── 003_add_user_table.sql
│   ├── 004_add_nft_table.sql
│   └── 005_add_transaction_table.sql
│
├── scripts/                               # 脚本文件
│   ├── setup.sh                           # 设置脚本
│   ├── migrate.sh                         # 迁移脚本
│   ├── deploy.sh                          # 部署脚本
│   ├── test.sh                            # 测试脚本
│   └── docker-build.sh                    # Docker构建脚本
│
├── test/                                  # 测试文件
│   ├── unit/                              # 单元测试
│   │   ├── core/
│   │   ├── plugins/
│   │   ├── service/
│   │   └── util/
│   ├── integration/                       # 集成测试
│   │   ├── api/
│   │   ├── storage/
│   │   └── web3/
│   ├── e2e/                               # 端到端测试
│   │   ├── upload_flow_test.go
│   │   ├── streaming_flow_test.go
│   │   └── nft_verification_test.go
│   ├── fixtures/                          # 测试数据
│   │   ├── content.json
│   │   ├── user.json
│   │   └── nft.json
│   └── mocks/                             # Mock对象
│       ├── storage_mock.go
│       ├── web3_mock.go
│       └── service_mock.go
│
├── deploy/                                # 部署配置
│   ├── docker/
│   │   ├── Dockerfile.monolith
│   │   ├── Dockerfile.api-gateway
│   │   ├── Dockerfile.upload
│   │   ├── Dockerfile.transcoder
│   │   ├── Dockerfile.streaming
│   │   ├── Dockerfile.metadata
│   │   ├── Dockerfile.cache
│   │   ├── Dockerfile.auth
│   │   ├── Dockerfile.worker
│   │   └── Dockerfile.monitor
│   ├── k8s/                               # Kubernetes配置
│   │   ├── namespace.yaml
│   │   ├── configmap.yaml
│   │   ├── secret.yaml
│   │   ├── monolith/
│   │   │   ├── deployment.yaml
│   │   │   ├── service.yaml
│   │   │   └── ingress.yaml
│   │   └── microservices/
│   │       ├── api-gateway/
│   │       ├── upload/
│   │       ├── transcoder/
│   │       ├── streaming/
│   │       ├── metadata/
│   │       ├── cache/
│   │       ├── auth/
│   │       ├── worker/
│   │       └── monitor/
│   └── helm/                              # Helm图表
│       ├── Chart.yaml
│       ├── values.yaml
│       └── templates/
│
├── docs/                                  # 文档
│   ├── architecture/                      # 架构文档
│   │   ├── microkernel.md
│   │   ├── microservices.md
│   │   ├── communication.md
│   │   └── data-flow.md
│   ├── api/                               # API文档
│   │   ├── rest-api.md
│   │   ├── grpc-api.md
│   │   └── websocket-api.md
│   ├── web3/                              # Web3文档
│   │   ├── nft-verification.md
│   │   ├── signature-verification.md
│   │   ├── smart-contracts.md
│   │   ├── ipfs-integration.md
│   │   └── multichain-support.md
│   ├── deployment/                        # 部署文档
│   │   ├── docker-compose.md
│   │   ├── kubernetes.md
│   │   ├── helm.md
│   │   └── production-setup.md
│   ├── development/                       # 开发文档
│   │   ├── setup.md
│   │   ├── coding-standards.md
│   │   ├── testing.md
│   │   └── debugging.md
│   ├── operations/                        # 运维文档
│   │   ├── monitoring.md
│   │   ├── logging.md
│   │   ├── troubleshooting.md
│   │   └── backup-recovery.md
│   └── guides/                            # 指南
│       ├── quick-start.md
│       ├── plugin-development.md
│       ├── service-development.md
│       └── web3-integration.md
│
├── examples/                              # 示例代码
│   ├── nft-verify-demo/
│   │   ├── main.go
│   │   ├── README.md
│   │   └── go.mod
│   ├── signature-verify-demo/
│   │   ├── main.go
│   │   ├── README.md
│   │   └── go.mod
│   ├── upload-demo/
│   │   ├── main.go
│   │   └── README.md
│   └── streaming-demo/
│       ├── main.go
│       └── README.md
│
├── .kiro/                                 # Kiro配置
│   └── specs/
│       └── offchain-content-service/
│           ├── requirements.md
│           ├── design.md
│           └── tasks.md
│
├── .github/                               # GitHub配置
│   ├── workflows/
│   │   ├── ci.yml
│   │   ├── test.yml
│   │   ├── build.yml
│   │   └── deploy.yml
│   ├── ISSUE_TEMPLATE/
│   │   ├── bug_report.md
│   │   └── feature_request.md
│   └── PULL_REQUEST_TEMPLATE.md
│
├── .gitignore                             # Git忽略文件
├── .env.example                           # 环境变量示例
├── go.mod                                 # Go模块定义
├── go.sum                                 # Go模块校验和
├── Makefile                               # Make构建文件
├── docker-compose.yml                     # Docker Compose配置
├── Dockerfile                             # 基础Dockerfile
├── LICENSE                                # 许可证
└── README.md                              # 项目README
```

## 目录说明

### 顶层目录

| 目录 | 说明 |
|------|------|
| `cmd/` | 应用程序入口点（单体和微服务） |
| `pkg/` | 核心包和库（可复用代码） |
| `proto/` | Protocol Buffers定义 |
| `config/` | 配置文件 |
| `migrations/` | 数据库迁移脚本 |
| `scripts/` | 辅助脚本 |
| `test/` | 测试代码 |
| `deploy/` | 部署配置（Docker、K8s、Helm） |
| `docs/` | 文档 |
| `examples/` | 示例代码 |
| `.kiro/` | Kiro配置 |
| `.github/` | GitHub配置 |

### pkg/core/ - 微内核核心

| 文件/目录 | 说明 |
|----------|------|
| `microkernel.go` | 微内核实现 |
| `config/` | 配置管理 |
| `logger/` | 日志记录 |
| `event/` | 事件总线 |
| `health/` | 健康检查 |
| `lifecycle/` | 生命周期管理 |

### pkg/plugins/ - 插件实现

| 目录 | 说明 |
|------|------|
| `api/` | API网关插件 |
| `upload/` | 上传插件 |
| `transcoder/` | 转码插件 |
| `streaming/` | 流媒体插件 |
| `metadata/` | 元数据插件 |
| `cache/` | 缓存插件 |
| `auth/` | 认证插件 |
| `worker/` | 工作插件 |
| `monitor/` | 监控插件 |

### pkg/ - 其他核心包

| 目录 | 说明 |
|------|------|
| `models/` | 数据模型 |
| `storage/` | 存储层（数据库、缓存、对象存储） |
| `service/` | 业务服务层 |
| `api/` | API定义（REST和gRPC） |
| `middleware/` | 中间件 |
| `util/` | 工具函数 |
| `web3/` | Web3集成 |

### deploy/ - 部署配置

| 目录 | 说明 |
|------|------|
| `docker/` | Docker镜像定义 |
| `k8s/` | Kubernetes配置 |
| `helm/` | Helm图表 |

### docs/ - 文档

| 目录 | 说明 |
|------|------|
| `architecture/` | 架构文档 |
| `api/` | API文档 |
| `web3/` | Web3文档 |
| `deployment/` | 部署文档 |
| `development/` | 开发文档 |
| `operations/` | 运维文档 |
| `guides/` | 指南 |

## 关键设计决策

### 1. cmd/ 目录结构
- 分离单体和微服务入口
- 每个微服务只有一个main.go
- 保持入口点简洁，逻辑在pkg中

### 2. pkg/ 目录结构
- `core/` - 微内核核心，所有服务共享
- `plugins/` - 插件实现，可选加载
- `models/` - 数据模型，跨服务共享
- `storage/` - 存储层，统一接口
- `service/` - 业务逻辑，可复用
- `api/` - API定义，REST和gRPC
- `middleware/` - 中间件，可复用
- `util/` - 工具函数，通用
- `web3/` - Web3集成，专用

### 3. 避免的问题
- ❌ 不在cmd中放置业务逻辑
- ❌ 不创建过深的嵌套（最多4层）
- ❌ 不混合不同关注点的代码
- ❌ 不重复代码，使用pkg中的共享代码

### 4. 命名规范
- 包名使用小写，单数形式
- 文件名使用下划线分隔
- 接口名以"er"结尾（Reader, Writer）
- 常量使用大写

### 5. 导入路径
```go
// 正确
import "github.com/rtcdance/streamgate/pkg/core"
import "github.com/rtcdance/streamgate/pkg/plugins/upload"

// 错误
import "./pkg/core"
import "../pkg/core"
```

## 文件数量估计

| 类别 | 文件数 | 说明 |
|------|--------|------|
| cmd/ | 10 | 1个单体 + 9个微服务 |
| pkg/core/ | 15 | 微内核核心 |
| pkg/plugins/ | 50 | 9个插件，每个5-6个文件 |
| pkg/models/ | 5 | 数据模型 |
| pkg/storage/ | 8 | 存储层 |
| pkg/service/ | 10 | 业务服务 |
| pkg/api/ | 15 | API定义 |
| pkg/middleware/ | 6 | 中间件 |
| pkg/util/ | 8 | 工具函数 |
| pkg/web3/ | 10 | Web3集成 |
| proto/ | 10 | Protocol Buffers |
| config/ | 5 | 配置文件 |
| migrations/ | 10 | 数据库迁移 |
| scripts/ | 5 | 脚本 |
| test/ | 50 | 测试代码 |
| deploy/ | 30 | 部署配置 |
| docs/ | 30 | 文档 |
| examples/ | 10 | 示例代码 |
| **总计** | **~300** | 完整项目 |

## 实施步骤

### 第1步：创建基础目录结构
```bash
mkdir -p pkg/{core,plugins,models,storage,service,api,middleware,util,web3}
mkdir -p pkg/plugins/{api,upload,transcoder,streaming,metadata,cache,auth,worker,monitor}
mkdir -p proto/v1 proto/gen/{go,python}
mkdir -p config migrations scripts test/{unit,integration,e2e,fixtures,mocks}
mkdir -p deploy/{docker,k8s,helm} deploy/k8s/{monolith,microservices}
mkdir -p docs/{architecture,api,web3,deployment,development,operations,guides}
mkdir -p examples/{nft-verify-demo,signature-verify-demo,upload-demo,streaming-demo}
mkdir -p .github/{workflows,ISSUE_TEMPLATE}
```

### 第2步：创建核心文件
- pkg/core/microkernel.go
- pkg/core/config/config.go
- pkg/core/logger/logger.go
- pkg/core/event/event.go
- pkg/models/*.go
- pkg/storage/*.go

### 第3步：创建插件框架
- 每个插件目录中的handler.go
- 插件特定的实现文件

### 第4步：创建API定义
- proto/v1/*.proto
- pkg/api/v1/*.go
- pkg/api/grpc/*.proto

### 第5步：创建部署配置
- deploy/docker/Dockerfile.*
- deploy/k8s/*.yaml
- deploy/helm/Chart.yaml

### 第6步：创建文档
- docs/architecture/*.md
- docs/api/*.md
- docs/deployment/*.md

## 总结

这个目录结构：

✅ **清晰** - 一目了然，易于导航
✅ **简洁** - 避免过度嵌套
✅ **可维护** - 相关代码聚集
✅ **可扩展** - 易于添加新功能
✅ **标准** - 遵循Go最佳实践
✅ **专业** - 企业级项目结构

---

**建议**: 按照这个结构逐步创建目录和文件，确保代码组织清晰、易于维护。
