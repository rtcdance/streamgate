# Test 目录结构分析与优化建议

**日期**: 2025-01-28  
**状态**: 📋 分析完成  
**版本**: 1.0.0

## 当前结构分析

### 📁 现有目录结构

```
test/
├── deployment/          # 部署测试 (2 files)
├── e2e/                # 端到端测试 (11 files)
├── fixtures/           # 测试数据 (3 files)
├── integration/        # 集成测试 (10 subdirs)
├── load/               # 负载测试 (1 file)
├── mocks/              # Mock 对象 (3 files)
├── performance/        # 性能测试 (1 file)
├── scaling/            # 扩展测试 (1 file)
├── security/           # 安全测试 (1 file)
└── unit/               # 单元测试 (9 subdirs)
```

### ⚠️ 发现的问题

#### 1. 目录重复和混乱

**问题**:
- `test/scaling/` 和 `test/unit/scaling/` 重复
- `test/security/` 和 `test/unit/security/` 重复
- `test/deployment/` 应该归类到 `test/e2e/` 或 `test/integration/`

**影响**: 
- 开发者不清楚应该在哪里添加测试
- 测试分类不清晰
- 维护困难

#### 2. 缺少关键测试目录

**缺失的测试**:
- ❌ `test/unit/storage/` - Storage 层单元测试
- ❌ `test/unit/service/` 只有 1 个文件，应该有更多
- ❌ `test/unit/middleware/` - Middleware 测试
- ❌ `test/unit/web3/` - Web3 单元测试
- ❌ `test/integration/service/` - Service 层集成测试
- ❌ `test/integration/web3/` - Web3 集成测试（目录存在但为空）

#### 3. 测试文件命名不一致

**问题**:
- `test/deployment/blue-green-test.go` - 使用连字符
- `test/e2e/nft_verification_test.go` - 使用下划线
- `test/unit/core/config_test.go` - 使用下划线

**建议**: 统一使用下划线命名

#### 4. 测试覆盖不完整

**pkg 目录覆盖情况**:

| pkg 模块 | 单元测试 | 集成测试 | E2E 测试 | 状态 |
|---------|---------|---------|---------|------|
| analytics | ✅ | ✅ | ✅ | 完整 |
| api | ✅ | ✅ | ❌ | 缺少 E2E |
| core | ✅ | ❌ | ❌ | 缺少集成/E2E |
| dashboard | ✅ | ✅ | ✅ | 完整 |
| debug | ✅ | ✅ | ✅ | 完整 |
| middleware | ❌ | ❌ | ❌ | 完全缺失 |
| ml | ✅ | ✅ | ✅ | 完整 |
| models | ❌ | ❌ | ❌ | 完全缺失 |
| monitoring | ❌ | ❌ | ❌ | 完全缺失 |
| optimization | ✅ | ✅ | ✅ | 完整 |
| plugins | ✅ | ❌ | ❌ | 缺少集成/E2E |
| scaling | ✅ | ✅ | ✅ | 完整 |
| security | ✅ | ✅ | ✅ | 完整 |
| service | ⚠️ | ❌ | ❌ | 严重不足 |
| storage | ❌ | ⚠️ | ❌ | 严重不足 |
| util | ❌ | ❌ | ❌ | 完全缺失 |
| web3 | ❌ | ❌ | ✅ | 缺少单元/集成 |

**覆盖率**: 约 40% 的模块有完整测试

## 🎯 优化建议

### 方案 A: 标准 Go 项目结构（推荐）

```
test/
├── unit/                    # 单元测试（按 pkg 结构镜像）
│   ├── analytics/
│   ├── api/
│   ├── core/
│   ├── dashboard/
│   ├── debug/
│   ├── middleware/         # 新增
│   ├── ml/
│   ├── models/             # 新增
│   ├── monitoring/         # 新增
│   ├── optimization/
│   ├── plugins/
│   ├── scaling/
│   ├── security/
│   ├── service/            # 扩充
│   ├── storage/            # 新增
│   ├── util/               # 新增
│   └── web3/               # 新增
│
├── integration/             # 集成测试（按功能分组）
│   ├── analytics/
│   ├── api/
│   ├── auth/               # 新增
│   ├── content/            # 新增
│   ├── dashboard/
│   ├── debug/
│   ├── ml/
│   ├── optimization/
│   ├── scaling/
│   ├── security/
│   ├── service/            # 新增
│   ├── storage/
│   ├── streaming/          # 新增
│   ├── upload/             # 新增
│   └── web3/
│
├── e2e/                     # 端到端测试（按用户场景）
│   ├── analytics_e2e_test.go
│   ├── auth_flow_test.go           # 新增
│   ├── content_management_test.go  # 新增
│   ├── dashboard_e2e_test.go
│   ├── debug_e2e_test.go
│   ├── deployment_test.go          # 合并 deployment/
│   ├── ml_e2e_test.go
│   ├── nft_verification_test.go
│   ├── optimization_e2e_test.go
│   ├── scaling_e2e_test.go
│   ├── security_e2e_test.go
│   ├── streaming_flow_test.go
│   ├── transcoding_flow_test.go    # 新增
│   └── upload_flow_test.go
│
├── performance/             # 性能测试
│   ├── api_benchmark_test.go       # 新增
│   ├── cache_benchmark_test.go     # 新增
│   ├── db_benchmark_test.go        # 新增
│   └── performance_test.go
│
├── load/                    # 负载测试
│   ├── load_test.go
│   └── stress_test.go              # 新增
│
├── security/                # 安全测试
│   ├── security_audit_test.go
│   ├── penetration_test.go         # 新增
│   └── vulnerability_scan_test.go  # 新增
│
├── fixtures/                # 测试数据
│   ├── content.json
│   ├── nft.json
│   ├── stream.json                 # 新增
│   ├── transcoding.json            # 新增
│   ├── upload.json                 # 新增
│   └── user.json
│
├── mocks/                   # Mock 对象
│   ├── service_mock.go
│   ├── storage_mock.go
│   └── web3_mock.go
│
├── testdata/                # 测试二进制数据（新增）
│   ├── videos/
│   ├── images/
│   └── audio/
│
└── helpers/                 # 测试辅助函数（新增）
    ├── assert.go
    ├── fixtures.go
    └── setup.go
```

**优点**:
- ✅ 清晰的分层结构
- ✅ 易于查找和维护
- ✅ 符合 Go 社区标准
- ✅ 测试覆盖完整

**缺点**:
- ⚠️ 需要重构现有测试
- ⚠️ 需要添加缺失的测试

### 方案 B: 按功能模块组织（备选）

```
test/
├── auth/                    # 认证模块测试
│   ├── unit/
│   ├── integration/
│   └── e2e/
│
├── content/                 # 内容模块测试
│   ├── unit/
│   ├── integration/
│   └── e2e/
│
├── streaming/               # 流媒体模块测试
│   ├── unit/
│   ├── integration/
│   └── e2e/
│
├── upload/                  # 上传模块测试
│   ├── unit/
│   ├── integration/
│   └── e2e/
│
├── web3/                    # Web3 模块测试
│   ├── unit/
│   ├── integration/
│   └── e2e/
│
├── performance/             # 性能测试
├── load/                    # 负载测试
├── security/                # 安全测试
├── fixtures/                # 测试数据
└── mocks/                   # Mock 对象
```

**优点**:
- ✅ 按业务功能组织
- ✅ 易于理解业务逻辑
- ✅ 适合大型项目

**缺点**:
- ⚠️ 不符合 Go 标准
- ⚠️ 可能导致重复
- ⚠️ 跨模块测试困难

## 🔧 具体优化步骤

### 第一阶段：清理和重组（1-2 天）

#### 1. 合并重复目录

```bash
# 移动 test/scaling/ 到 test/e2e/
mv test/scaling/hpa-test.go test/e2e/hpa_scaling_test.go

# 移动 test/deployment/ 到 test/e2e/
mv test/deployment/blue-green-test.go test/e2e/blue_green_deployment_test.go
mv test/deployment/canary-test.go test/e2e/canary_deployment_test.go

# 删除空目录
rmdir test/scaling
rmdir test/deployment
```

#### 2. 统一命名规范

```bash
# 将所有连字符改为下划线
# blue-green-test.go -> blue_green_test.go
```

#### 3. 创建缺失的测试目录

```bash
# 单元测试
mkdir -p test/unit/middleware
mkdir -p test/unit/models
mkdir -p test/unit/monitoring
mkdir -p test/unit/storage
mkdir -p test/unit/util
mkdir -p test/unit/web3

# 集成测试
mkdir -p test/integration/auth
mkdir -p test/integration/content
mkdir -p test/integration/service
mkdir -p test/integration/streaming
mkdir -p test/integration/upload

# 测试辅助
mkdir -p test/helpers
mkdir -p test/testdata/{videos,images,audio}
```

### 第二阶段：补充缺失测试（3-5 天）

#### 1. Storage 层测试（高优先级）

**test/unit/storage/postgres_test.go**:
```go
package storage_test

import (
    "testing"
    "streamgate/pkg/storage"
)

func TestPostgresDB_Connect(t *testing.T) {
    db := storage.NewPostgresDB()
    err := db.Connect("postgres://localhost/test")
    if err != nil {
        t.Fatalf("Failed to connect: %v", err)
    }
    defer db.Close()
}

func TestPostgresDB_Query(t *testing.T) {
    // Test query operations
}
```

**test/unit/storage/redis_test.go**:
```go
package storage_test

func TestRedisCache_SetGet(t *testing.T) {
    cache := storage.NewRedisCache()
    // Test cache operations
}
```

**test/unit/storage/s3_test.go**:
```go
package storage_test

func TestS3Storage_Upload(t *testing.T) {
    // Test S3 upload
}
```

#### 2. Service 层测试（高优先级）

**test/unit/service/auth_test.go**:
```go
package service_test

func TestAuthService_Authenticate(t *testing.T) {
    // Test authentication
}

func TestAuthService_Register(t *testing.T) {
    // Test registration
}
```

**test/unit/service/nft_test.go**:
```go
package service_test

func TestNFTService_VerifyNFT(t *testing.T) {
    // Test NFT verification
}
```

**test/unit/service/upload_test.go**:
```go
package service_test

func TestUploadService_Upload(t *testing.T) {
    // Test file upload
}

func TestUploadService_ChunkedUpload(t *testing.T) {
    // Test chunked upload
}
```

**test/unit/service/streaming_test.go**:
```go
package service_test

func TestStreamingService_GetStream(t *testing.T) {
    // Test stream retrieval
}

func TestStreamingService_GenerateHLS(t *testing.T) {
    // Test HLS generation
}
```

**test/unit/service/transcoding_test.go**:
```go
package service_test

func TestTranscodingService_Transcode(t *testing.T) {
    // Test transcoding
}
```

#### 3. Middleware 测试（中优先级）

**test/unit/middleware/auth_test.go**:
```go
package middleware_test

func TestAuthMiddleware(t *testing.T) {
    // Test auth middleware
}
```

**test/unit/middleware/ratelimit_test.go**:
```go
package middleware_test

func TestRateLimitMiddleware(t *testing.T) {
    // Test rate limiting
}
```

#### 4. 集成测试（中优先级）

**test/integration/service/service_integration_test.go**:
```go
package service_test

func TestServiceIntegration(t *testing.T) {
    // Test service layer integration
}
```

**test/integration/storage/storage_integration_test.go**:
```go
package storage_test

func TestStorageIntegration(t *testing.T) {
    // Test storage layer integration
}
```

### 第三阶段：测试辅助工具（1-2 天）

#### test/helpers/setup.go

```go
package helpers

import (
    "testing"
    "streamgate/pkg/storage"
    "streamgate/pkg/service"
)

// SetupTestDB creates a test database
func SetupTestDB(t *testing.T) *storage.Database {
    db, err := storage.NewDatabase(storage.DatabaseConfig{
        Type: "postgres",
        DSN:  "postgres://test:test@localhost/test_db?sslmode=disable",
    })
    if err != nil {
        t.Fatalf("Failed to setup test DB: %v", err)
    }
    return db
}

// SetupTestStorage creates test storage
func SetupTestStorage(t *testing.T) *storage.ObjectStorage {
    storage, err := storage.NewObjectStorage(storage.ObjectStorageConfig{
        Type:            "minio",
        Endpoint:        "localhost:9000",
        AccessKeyID:     "test",
        SecretAccessKey: "testtest",
        UseSSL:          false,
    })
    if err != nil {
        t.Fatalf("Failed to setup test storage: %v", err)
    }
    return storage
}

// CleanupTestDB cleans up test database
func CleanupTestDB(t *testing.T, db *storage.Database) {
    if err := db.Close(); err != nil {
        t.Errorf("Failed to cleanup DB: %v", err)
    }
}
```

#### test/helpers/fixtures.go

```go
package helpers

import (
    "encoding/json"
    "os"
    "testing"
)

// LoadFixture loads a JSON fixture
func LoadFixture(t *testing.T, filename string, v interface{}) {
    data, err := os.ReadFile("../fixtures/" + filename)
    if err != nil {
        t.Fatalf("Failed to load fixture: %v", err)
    }
    if err := json.Unmarshal(data, v); err != nil {
        t.Fatalf("Failed to parse fixture: %v", err)
    }
}
```

#### test/helpers/assert.go

```go
package helpers

import "testing"

// AssertNoError asserts no error occurred
func AssertNoError(t *testing.T, err error) {
    t.Helper()
    if err != nil {
        t.Fatalf("Expected no error, got: %v", err)
    }
}

// AssertEqual asserts two values are equal
func AssertEqual(t *testing.T, expected, actual interface{}) {
    t.Helper()
    if expected != actual {
        t.Fatalf("Expected %v, got %v", expected, actual)
    }
}
```

## 📊 优化后的测试覆盖目标

### 覆盖率目标

| 层级 | 当前覆盖率 | 目标覆盖率 | 优先级 |
|------|-----------|-----------|--------|
| Storage | 10% | 80% | 🔴 高 |
| Service | 20% | 80% | 🔴 高 |
| Middleware | 0% | 70% | 🟡 中 |
| Models | 0% | 60% | 🟡 中 |
| Util | 0% | 70% | 🟡 中 |
| Web3 | 30% | 70% | 🟡 中 |
| Plugins | 50% | 80% | 🟢 低 |
| Core | 60% | 80% | 🟢 低 |
| 其他 | 70% | 85% | 🟢 低 |

### 测试数量目标

| 测试类型 | 当前数量 | 目标数量 | 增加 |
|---------|---------|---------|------|
| 单元测试 | ~50 | ~150 | +100 |
| 集成测试 | ~20 | ~50 | +30 |
| E2E 测试 | ~15 | ~25 | +10 |
| 性能测试 | ~10 | ~20 | +10 |
| 负载测试 | ~8 | ~15 | +7 |
| 安全测试 | ~18 | ~30 | +12 |
| **总计** | **~121** | **~290** | **+169** |

## 🎯 最终目标结构

```
test/
├── unit/                    # 单元测试 (~150 tests)
│   ├── 17 subdirectories
│   └── ~150 test files
│
├── integration/             # 集成测试 (~50 tests)
│   ├── 13 subdirectories
│   └── ~50 test files
│
├── e2e/                     # E2E 测试 (~25 tests)
│   └── ~25 test files
│
├── performance/             # 性能测试 (~20 tests)
│   └── ~4 test files
│
├── load/                    # 负载测试 (~15 tests)
│   └── ~2 test files
│
├── security/                # 安全测试 (~30 tests)
│   └── ~3 test files
│
├── fixtures/                # 测试数据
│   └── ~10 JSON files
│
├── testdata/                # 二进制测试数据
│   └── videos, images, audio
│
├── mocks/                   # Mock 对象
│   └── ~5 mock files
│
├── helpers/                 # 测试辅助
│   └── ~3 helper files
│
└── README.md               # 测试文档
```

## 📝 实施计划

### Week 1: 清理和重组
- [ ] 合并重复目录
- [ ] 统一命名规范
- [ ] 创建缺失目录
- [ ] 更新 README

### Week 2: Storage 和 Service 测试
- [ ] Storage 单元测试 (7 files)
- [ ] Service 单元测试 (6 files)
- [ ] Storage 集成测试 (3 files)
- [ ] Service 集成测试 (3 files)

### Week 3: Middleware 和 Util 测试
- [ ] Middleware 单元测试 (5 files)
- [ ] Util 单元测试 (5 files)
- [ ] Models 单元测试 (5 files)
- [ ] Web3 单元测试 (5 files)

### Week 4: 集成和 E2E 测试
- [ ] 补充集成测试 (10 files)
- [ ] 补充 E2E 测试 (5 files)
- [ ] 性能测试优化 (3 files)
- [ ] 测试辅助工具 (3 files)

## 🚀 快速开始

### 立即执行的优化（今天）

```bash
# 1. 合并重复目录
mv test/scaling/hpa-test.go test/e2e/hpa_scaling_test.go
mv test/deployment/*.go test/e2e/
rmdir test/scaling test/deployment

# 2. 创建缺失目录
mkdir -p test/unit/{middleware,models,monitoring,storage,util,web3}
mkdir -p test/integration/{auth,content,service,streaming,upload}
mkdir -p test/helpers test/testdata

# 3. 创建测试辅助文件
touch test/helpers/{setup.go,fixtures.go,assert.go}

# 4. 更新 README
# 编辑 test/README.md
```

### 本周完成的任务

1. ✅ 清理目录结构
2. ✅ 创建缺失目录
3. ⏳ 添加 Storage 测试
4. ⏳ 添加 Service 测试

## 📚 参考资料

- [Go Testing Best Practices](https://golang.org/doc/tutorial/add-a-test)
- [Table Driven Tests](https://github.com/golang/go/wiki/TableDrivenTests)
- [Testing with Mocks](https://github.com/golang/mock)
- [Integration Testing](https://www.ardanlabs.com/blog/2019/10/integration-testing-in-go.html)

---

**分析状态**: ✅ 完成  
**优化方案**: 方案 A（推荐）  
**预计工作量**: 4 周  
**优先级**: 🔴 高（Storage 和 Service 测试）

