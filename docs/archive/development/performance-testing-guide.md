# 性能测试指南

**日期**: 2025-01-28  
**版本**: 1.0.0  
**状态**: ✅ 完成

## 目录

1. [概述](#概述)
2. [基准测试](#基准测试)
3. [负载测试](#负载测试)
4. [压力测试](#压力测试)
5. [性能分析](#性能分析)
6. [优化建议](#优化建议)
7. [最佳实践](#最佳实践)

## 概述

StreamGate 项目包含全面的性能测试套件，用于评估系统性能、识别瓶颈和验证优化效果。

### 测试类型

| 类型 | 目的 | 文件 |
|------|------|------|
| 基准测试 | 测试单个操作的性能 | `test/benchmark/` |
| 负载测试 | 测试系统在高负载下的表现 | `test/load/` |
| 压力测试 | 测试系统极限 | `test/stress/` |

## 基准测试

### 概述

基准测试用于测试单个操作的性能，提供性能基线。

### 基准测试文件

#### 1. auth_benchmark_test.go

认证服务性能基准测试。

**测试用例**:
- `BenchmarkAuthService_Register` - 用户注册性能
- `BenchmarkAuthService_Login` - 用户登录性能
- `BenchmarkAuthService_ValidateToken` - Token 验证性能
- `BenchmarkAuthService_RefreshToken` - Token 刷新性能
- `BenchmarkAuthService_PasswordHashing` - 密码哈希性能
- `BenchmarkAuthService_ConcurrentLogins` - 并发登录性能

**运行方式**:
```bash
go test -bench=BenchmarkAuthService -benchmem ./test/benchmark/auth_benchmark_test.go
```

**预期结果**:
```
BenchmarkAuthService_Register-8          100    10000000 ns/op    1000 B/op    10 allocs/op
BenchmarkAuthService_Login-8             100    15000000 ns/op    1500 B/op    15 allocs/op
BenchmarkAuthService_ValidateToken-8    1000     1000000 ns/op     100 B/op     1 allocs/op
```

#### 2. content_benchmark_test.go

内容服务性能基准测试。

**测试用例**:
- `BenchmarkContentService_Create` - 内容创建性能
- `BenchmarkContentService_GetByID` - 内容查询性能
- `BenchmarkContentService_Update` - 内容更新性能
- `BenchmarkContentService_List` - 内容列表性能
- `BenchmarkContentService_Search` - 内容搜索性能
- `BenchmarkContentService_Delete` - 内容删除性能
- `BenchmarkContentService_ConcurrentOperations` - 并发操作性能

**运行方式**:
```bash
go test -bench=BenchmarkContentService -benchmem ./test/benchmark/content_benchmark_test.go
```

#### 3. storage_benchmark_test.go

存储服务性能基准测试。

**测试用例**:
- `BenchmarkPostgres_*` - PostgreSQL 操作性能
- `BenchmarkRedis_*` - Redis 操作性能
- `BenchmarkObjectStorage_*` - 对象存储操作性能
- `BenchmarkConnectionPool_*` - 连接池性能

**运行方式**:
```bash
go test -bench=BenchmarkPostgres -benchmem ./test/benchmark/storage_benchmark_test.go
go test -bench=BenchmarkRedis -benchmem ./test/benchmark/storage_benchmark_test.go
```

#### 4. api_benchmark_test.go

API 服务性能基准测试。

**测试用例**:
- `BenchmarkAPI_RoutingSimple` - 简单路由性能
- `BenchmarkAPI_RoutingWithMiddleware` - 中间件性能
- `BenchmarkAPI_JSONSerialization` - JSON 序列化性能
- `BenchmarkAPI_JSONDeserialization` - JSON 反序列化性能
- `BenchmarkAPI_POSTRequest` - POST 请求性能
- `BenchmarkAPI_Authentication` - 认证性能
- `BenchmarkAPI_RateLimiting` - 限流性能
- `BenchmarkAPI_ConcurrentRequests` - 并发请求性能

**运行方式**:
```bash
go test -bench=BenchmarkAPI -benchmem ./test/benchmark/api_benchmark_test.go
```

#### 5. web3_benchmark_test.go

Web3 服务性能基准测试。

**测试用例**:
- `BenchmarkWeb3_VerifyNFT` - NFT 验证性能
- `BenchmarkWeb3_VerifySignature` - 签名验证性能
- `BenchmarkWeb3_GetBalance` - 余额查询性能
- `BenchmarkWeb3_CallContractMethod` - 智能合约调用性能
- `BenchmarkWeb3_IsChainSupported` - 链支持检查性能
- `BenchmarkWeb3_ConcurrentVerifications` - 并发验证性能
- `BenchmarkWeb3_MultiChainOperations` - 多链操作性能

**运行方式**:
```bash
go test -bench=BenchmarkWeb3 -benchmem ./test/benchmark/web3_benchmark_test.go
```

### 运行所有基准测试

```bash
# 运行所有基准测试
go test -bench=. -benchmem ./test/benchmark/...

# 运行所有基准测试并保存结果
go test -bench=. -benchmem -benchtime=10s ./test/benchmark/... > benchmark_results.txt

# 运行基准测试并进行 CPU 分析
go test -bench=. -cpuprofile=cpu.prof ./test/benchmark/...
go tool pprof cpu.prof

# 运行基准测试并进行内存分析
go test -bench=. -memprofile=mem.prof ./test/benchmark/...
go tool pprof mem.prof
```

## 负载测试

### 概述

负载测试用于测试系统在高负载下的表现，包括并发处理、吞吐量和资源使用。

### 负载测试文件

#### 1. concurrent_load_test.go

并发负载测试。

**测试用例**:
- `TestLoad_ConcurrentAuthRequests` - 并发认证请求 (100 goroutines × 10 requests)
- `TestLoad_ConcurrentContentOperations` - 并发内容操作 (50 goroutines × 20 requests)
- `TestLoad_ConcurrentCacheOperations` - 并发缓存操作 (100 goroutines × 50 requests)
- `TestLoad_ConcurrentDatabaseOperations` - 并发数据库操作 (50 goroutines × 20 requests)
- `TestLoad_SustainedLoad` - 持续负载测试 (20 goroutines × 10 seconds)

**运行方式**:
```bash
go test -v -run TestLoad_ConcurrentAuthRequests ./test/load/concurrent_load_test.go
```

**预期输出**:
```
Concurrent Auth Load Test Results:
  Total Requests: 1000
  Successful: 990
  Errors: 10
  Duration: 5.234s
  Throughput: 190.56 req/s
```

#### 2. database_load_test.go

数据库负载测试。

**测试用例**:
- `TestLoad_DatabaseConnectionPool` - 连接池负载测试
- `TestLoad_DatabaseQueryPerformance` - 查询性能负载测试
- `TestLoad_DatabaseTransactions` - 事务负载测试
- `TestLoad_DatabaseBulkOperations` - 批量操作负载测试

**运行方式**:
```bash
go test -v -run TestLoad_Database ./test/load/database_load_test.go
```

#### 3. cache_load_test.go

缓存负载测试。

**测试用例**:
- `TestLoad_CacheHitRate` - 缓存命中率测试
- `TestLoad_CacheWritePerformance` - 缓存写入性能测试
- `TestLoad_CacheEviction` - 缓存驱逐测试
- `TestLoad_CacheConsistency` - 缓存一致性测试
- `TestLoad_CacheMemoryUsage` - 缓存内存使用测试

**运行方式**:
```bash
go test -v -run TestLoad_Cache ./test/load/cache_load_test.go
```

### 运行所有负载测试

```bash
# 运行所有负载测试
go test -v ./test/load/...

# 运行负载测试并输出详细日志
go test -v ./test/load/... 2>&1 | tee load_test_results.txt

# 运行特定负载测试
go test -v -run TestLoad_ConcurrentAuthRequests ./test/load/...
```

## 压力测试

### 概述

压力测试用于测试系统在极端条件下的表现，包括资源耗尽、故障恢复等。

### 压力测试场景

1. **资源压力**
   - 内存压力
   - CPU 压力
   - 磁盘 I/O 压力
   - 网络压力

2. **故障恢复**
   - 数据库连接失败
   - 缓存失败
   - 网络超时
   - 服务不可用

## 性能分析

### 使用 pprof 进行性能分析

#### CPU 分析

```bash
# 生成 CPU 分析文件
go test -cpuprofile=cpu.prof -bench=. ./test/benchmark/...

# 分析 CPU 分析文件
go tool pprof cpu.prof

# 在 pprof 中查看 top 函数
(pprof) top
(pprof) list functionName
(pprof) web
```

#### 内存分析

```bash
# 生成内存分析文件
go test -memprofile=mem.prof -bench=. ./test/benchmark/...

# 分析内存分析文件
go tool pprof mem.prof

# 在 pprof 中查看内存使用
(pprof) top
(pprof) alloc_space
(pprof) alloc_objects
```

#### 竞态条件检测

```bash
# 运行竞态条件检测
go test -race ./test/...

# 运行特定测试的竞态条件检测
go test -race -run TestLoad_ConcurrentAuthRequests ./test/load/...
```

### 性能指标收集

```bash
# 收集详细的性能指标
go test -bench=. -benchmem -benchtime=10s -cpuprofile=cpu.prof -memprofile=mem.prof ./test/benchmark/...

# 生成性能报告
go tool pprof -http=:8080 cpu.prof
```

## 优化建议

### 1. 认证服务优化

**当前性能**: ~100-150ms per login

**优化建议**:
- 缓存密码哈希结果
- 使用更快的哈希算法 (e.g., Argon2)
- 实施 Token 缓存
- 优化数据库查询

**预期改进**: 30-50% 性能提升

### 2. 内容服务优化

**当前性能**: ~50-500ms depending on operation

**优化建议**:
- 实施多级缓存
- 优化搜索算法
- 使用数据库索引
- 实施分页优化

**预期改进**: 40-60% 性能提升

### 3. 存储服务优化

**当前性能**: ~10-500ms depending on operation

**优化建议**:
- 优化连接池配置
- 实施连接复用
- 使用批量操作
- 优化查询语句

**预期改进**: 20-40% 性能提升

### 4. API 服务优化

**当前性能**: ~1-10ms per request

**优化建议**:
- 减少中间件数量
- 优化序列化性能
- 实施请求缓存
- 使用异步处理

**预期改进**: 10-30% 性能提升

### 5. 缓存优化

**当前性能**: 命中率 >80%

**优化建议**:
- 优化缓存策略
- 实施缓存预热
- 优化驱逐策略
- 监控缓存命中率

**预期改进**: 缓存命中率提升到 >90%

## 最佳实践

### 1. 定期运行性能测试

```bash
# 每周运行一次性能测试
0 0 * * 0 cd /path/to/streamgate && go test -bench=. ./test/benchmark/... > results/$(date +%Y%m%d).txt
```

### 2. 性能回归检测

```bash
# 比较两次性能测试结果
go test -bench=. -benchmem ./test/benchmark/... > new_results.txt
benchstat old_results.txt new_results.txt
```

### 3. 性能监控

- 定期收集性能指标
- 建立性能基线
- 监控性能趋势
- 及时发现性能回归

### 4. 性能优化流程

1. 运行基准测试建立基线
2. 识别性能瓶颈
3. 实施优化
4. 运行基准测试验证优化
5. 比较优化前后的性能

### 5. 文档记录

- 记录性能测试结果
- 记录优化措施
- 记录性能改进
- 建立性能历史

## 性能目标

| 操作 | 目标 | 当前 | 状态 |
|------|------|------|------|
| 认证登录 | <100ms | ~100-150ms | 🟡 |
| 内容查询 | <50ms | ~20-50ms | ✅ |
| 缓存读取 | <5ms | ~1-5ms | ✅ |
| API 请求 | <10ms | ~1-10ms | ✅ |
| 缓存命中率 | >90% | >80% | 🟡 |
| 吞吐量 | >1000 req/s | ~500-2000 req/s | ✅ |

## 故障排查

### 性能下降

1. 检查系统资源使用
2. 运行性能分析
3. 检查数据库查询
4. 检查缓存命中率
5. 检查网络延迟

### 内存泄漏

1. 运行内存分析
2. 检查 goroutine 泄漏
3. 检查资源释放
4. 使用 pprof 分析

### 高 CPU 使用

1. 运行 CPU 分析
2. 识别热点函数
3. 优化算法
4. 使用缓存

---

**版本**: 1.0.0  
**最后更新**: 2025-01-28  
**状态**: ✅ 完成
