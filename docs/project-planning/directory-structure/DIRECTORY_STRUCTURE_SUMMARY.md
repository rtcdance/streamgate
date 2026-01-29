# StreamGate - 代码目录结构规划总结

## 概述

已为StreamGate项目规划了一个完整的、清晰的、简洁的代码目录结构。该结构遵循Go最佳实践，支持微内核和微服务两种部署模式。

## 核心特点

### 1. 清晰性 ✅
- 目录结构一目了然
- 相关代码聚集在一起
- 易于导航和理解

### 2. 简洁性 ✅
- 避免过度嵌套（最多4层）
- 每个目录有明确的职责
- 不重复代码

### 3. 可维护性 ✅
- 相关功能组织在一起
- 易于定位和修改代码
- 支持团队协作

### 4. 可扩展性 ✅
- 易于添加新的微服务
- 易于添加新的插件
- 易于添加新的功能

### 5. 标准性 ✅
- 遵循Go项目最佳实践
- 符合行业标准
- 易于被其他开发者理解

## 目录结构概览

```
streamgate/
├── cmd/                    # 应用程序入口点
├── pkg/                    # 核心包和库
├── proto/                  # Protocol Buffers定义
├── config/                 # 配置文件
├── migrations/             # 数据库迁移
├── scripts/                # 脚本文件
├── test/                   # 测试代码
├── deploy/                 # 部署配置
├── docs/                   # 文档
├── examples/               # 示例代码
├── .github/                # GitHub配置
└── 其他文件               # go.mod, Makefile等
```

## 关键目录说明

### cmd/ - 应用程序入口点

```
cmd/
├── monolith/streamgate/    # 单体部署入口
└── microservices/          # 微服务部署入口
    ├── api-gateway/
    ├── upload/
    ├── transcoder/
    ├── streaming/
    ├── metadata/
    ├── cache/
    ├── auth/
    ├── worker/
    └── monitor/
```

**特点**:
- 每个服务只有一个main.go
- 入口点保持简洁
- 业务逻辑在pkg中

### pkg/ - 核心包和库

```
pkg/
├── core/                   # 微内核核心
│   ├── microkernel.go
│   ├── config/
│   ├── logger/
│   ├── event/
│   ├── health/
│   └── lifecycle/
├── plugins/                # 插件实现（9个）
│   ├── api/
│   ├── upload/
│   ├── transcoder/
│   ├── streaming/
│   ├── metadata/
│   ├── cache/
│   ├── auth/
│   ├── worker/
│   └── monitor/
├── models/                 # 数据模型
├── storage/                # 存储层
├── service/                # 业务服务
├── api/                    # API定义
├── middleware/             # 中间件
├── util/                   # 工具函数
└── web3/                   # Web3集成
```

**特点**:
- 核心功能集中在pkg中
- 所有服务共享pkg中的代码
- 易于复用和维护

### proto/ - Protocol Buffers定义

```
proto/
├── v1/                     # v1版本定义
│   ├── common.proto
│   ├── content.proto
│   ├── upload.proto
│   ├── streaming.proto
│   ├── auth.proto
│   └── nft.proto
└── gen/                    # 生成的代码
    ├── go/
    └── python/
```

**特点**:
- 集中管理所有gRPC定义
- 支持多语言生成
- 版本化管理

### deploy/ - 部署配置

```
deploy/
├── docker/                 # Docker镜像定义
│   ├── Dockerfile.monolith
│   ├── Dockerfile.api-gateway
│   └── ...（9个微服务）
├── k8s/                    # Kubernetes配置
│   ├── namespace.yaml
│   ├── configmap.yaml
│   ├── monolith/
│   └── microservices/
└── helm/                   # Helm图表
    ├── Chart.yaml
    ├── values.yaml
    └── templates/
```

**特点**:
- 支持多种部署方式
- 配置集中管理
- 易于扩展

### docs/ - 文档

```
docs/
├── architecture/           # 架构文档
├── api/                    # API文档
├── web3/                   # Web3文档
├── deployment/             # 部署文档
├── development/            # 开发文档
├── operations/             # 运维文档
└── guides/                 # 指南
```

**特点**:
- 文档组织清晰
- 覆盖所有方面
- 易于查找

## 文件数量统计

| 类别 | 文件数 | 说明 |
|------|--------|------|
| cmd/ | 10 | 1个单体 + 9个微服务 |
| pkg/core/ | 15 | 微内核核心 |
| pkg/plugins/ | 50 | 9个插件 |
| pkg/其他/ | 50 | 模型、存储、服务等 |
| proto/ | 10 | Protocol Buffers |
| config/ | 5 | 配置文件 |
| migrations/ | 10 | 数据库迁移 |
| scripts/ | 5 | 脚本 |
| test/ | 50 | 测试代码 |
| deploy/ | 30 | 部署配置 |
| docs/ | 30 | 文档 |
| examples/ | 10 | 示例代码 |
| **总计** | **~300** | 完整项目 |

## 实施方式

### 方式1：使用自动化脚本（推荐）

```bash
# 使脚本可执行
chmod +x scripts/init-directory-structure.sh

# 运行脚本
./scripts/init-directory-structure.sh
```

**优点**:
- 快速创建所有目录和文件
- 避免手动错误
- 可重复使用

### 方式2：手动创建

按照DIRECTORY_STRUCTURE_IMPLEMENTATION.md中的步骤逐步创建。

**优点**:
- 更好地理解项目结构
- 可以根据需要调整

## 命名规范

### 包名
- 使用小写
- 单数形式
- 简洁明了

```go
// 正确
package core
package plugins
package models

// 错误
package Core
package Plugins
package Models
```

### 文件名
- 使用下划线分隔
- 小写字母
- 描述性名称

```
// 正确
config.go
config_loader.go
postgres.go

// 错误
Config.go
configLoader.go
PostgreSQL.go
```

### 接口名
- 以"er"结尾
- 简洁明了

```go
// 正确
type Reader interface {}
type Writer interface {}
type Storage interface {}

// 错误
type IReader interface {}
type ReadWriter interface {}
type StorageInterface interface {}
```

### 常量名
- 使用大写
- 下划线分隔

```go
// 正确
const MAX_WORKERS = 10
const DEFAULT_TIMEOUT = 30

// 错误
const maxWorkers = 10
const default_timeout = 30
```

## 导入路径

```go
// 正确
import "github.com/rtcdance/streamgate/pkg/core"
import "github.com/rtcdance/streamgate/pkg/plugins/upload"
import "github.com/rtcdance/streamgate/pkg/service"

// 错误
import "./pkg/core"
import "../pkg/core"
import "streamgate/pkg/core"
```

## 最佳实践

### 1. 避免循环导入
```go
// 错误：pkg/service/content.go 导入 pkg/plugins/api
// 而 pkg/plugins/api 导入 pkg/service/content

// 正确：使用接口解耦
```

### 2. 保持包的单一职责
```go
// 正确：每个包有明确的职责
pkg/storage/    # 只处理存储
pkg/service/    # 只处理业务逻辑
pkg/api/        # 只处理API定义

// 错误：混合多个职责
pkg/everything/
```

### 3. 使用接口进行抽象
```go
// 正确
type Storage interface {
    Get(key string) (interface{}, error)
    Set(key string, value interface{}) error
}

// 错误
type PostgresDB struct {
    // 直接使用具体实现
}
```

### 4. 组织相关代码
```
// 正确：相关代码在一起
pkg/plugins/transcoder/
    ├── transcoder.go      # 主逻辑
    ├── worker.go          # 工作池
    ├── queue.go           # 任务队列
    ├── scaler.go          # 自动扩展
    └── ffmpeg.go          # FFmpeg集成

// 错误：相关代码分散
pkg/transcoder/transcoder.go
pkg/worker/transcoder_worker.go
pkg/queue/transcoder_queue.go
```

## 迁移指南

如果项目已有代码，按以下步骤迁移：

### 第1步：创建新的目录结构
```bash
./scripts/init-directory-structure.sh
```

### 第2步：逐步迁移代码
- 从cmd/开始
- 然后是pkg/core/
- 然后是pkg/plugins/
- 最后是其他包

### 第3步：更新导入路径
```bash
# 使用sed或其他工具批量更新导入路径
```

### 第4步：运行测试
```bash
make test
```

### 第5步：验证构建
```bash
make build-all
```

## 验证清单

- [ ] 所有目录已创建
- [ ] 所有文件已创建
- [ ] 导入路径正确
- [ ] 没有循环导入
- [ ] 代码可以编译
- [ ] 所有测试通过
- [ ] 文档已更新

## 总结

这个目录结构提供了：

✅ **清晰的组织** - 一目了然的项目结构
✅ **简洁的设计** - 避免过度复杂
✅ **易于维护** - 相关代码聚集
✅ **易于扩展** - 支持添加新功能
✅ **遵循标准** - Go最佳实践
✅ **专业形象** - 企业级项目结构

## 相关文档

- `DIRECTORY_STRUCTURE_PLAN.md` - 详细的目录结构规划
- `DIRECTORY_STRUCTURE_IMPLEMENTATION.md` - 实施指南
- `scripts/init-directory-structure.sh` - 自动化脚本

## 下一步

1. 运行自动化脚本创建目录结构
2. 根据DIRECTORY_STRUCTURE_IMPLEMENTATION.md填充文件
3. 更新README.md中的项目结构部分
4. 开始实现各个模块

---

**建议**: 立即运行自动化脚本，然后逐步填充代码。这样可以确保项目结构清晰、一致、易于维护。

**状态**: ✅ 目录结构规划完成
**日期**: 2025-01-28
**下一步**: 执行自动化脚本创建目录结构
