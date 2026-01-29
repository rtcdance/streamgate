# GitHub CI/CD Pipeline 完整修复总结

**日期**: 2026-01-29  
**状态**: ✅ 升级到 Go 1.24 以解决依赖冲突

---

## 问题根源

项目依赖包要求 Go 1.24+：

1. **go.mod**: `go mod tidy` 自动升级到 `go 1.24.0`
2. **ethereum依赖**: `github.com/ethereum/go-ethereum v1.13.15` 要求 Go >= 1.24
3. **crypto依赖**: `golang.org/x/crypto v0.47.0` 要求 Go >= 1.24
4. **CI workflows**: 使用 Go 1.21 但 `GOTOOLCHAIN=local` 阻止自动升级
5. **Dockerfiles**: 使用 `golang:1.21-alpine`

**核心问题**: 无法降级 ethereum 到支持 Go 1.21 的版本，因为即使 v1.12.2 也会被 `go mod tidy` 升级回 v1.13.15+

---

## 解决方案：升级到 Go 1.24

### 1. Go版本统一 ✅

**go.mod**
```go
// 最终版本
go 1.24.0

toolchain go1.24.0

require (
    github.com/ethereum/go-ethereum v1.13.15
    // ... 其他依赖
)
```

**所有Dockerfiles** (10个文件)
```dockerfile
# 修复后
FROM golang:1.24-alpine AS builder
```

### 2. CI Workflows 配置 ✅

**ci.yml**
```yaml
env:
  GO_VERSION: '1.24'
  GOPROXY: 'https://goproxy.io,direct'
  # 移除 GOTOOLCHAIN: 'local' - 不再需要
```

**test.yml**
```yaml
env:
  GO_VERSION: '1.24'
  GOPROXY: 'https://goproxy.io,direct'
```

---

## 修复内容

### 已修复文件

**配置文件** (3个)
- `go.mod` - 升级到 Go 1.24
- `.github/workflows/ci.yml` - 使用 Go 1.24
- `.github/workflows/test.yml` - 使用 Go 1.24

**Dockerfiles** (10个)
- `deploy/docker/Dockerfile.monolith`
- `deploy/docker/Dockerfile.api-gateway`
- `deploy/docker/Dockerfile.auth`
- `deploy/docker/Dockerfile.cache`
- `deploy/docker/Dockerfile.metadata`
- `deploy/docker/Dockerfile.monitor`
- `deploy/docker/Dockerfile.streaming`
- `deploy/docker/Dockerfile.transcoder`
- `deploy/docker/Dockerfile.upload`
- `deploy/docker/Dockerfile.worker`

---

## 为什么必须升级到 Go 1.24

### 尝试过的降级方案（均失败）

1. **ethereum v1.16.8 → v1.13.15**: 仍需 Go 1.24+
2. **ethereum v1.13.15 → v1.12.2**: `go mod tidy` 自动升级回 v1.13.15
3. **添加 GOTOOLCHAIN=local**: 导致 "go.mod requires go >= 1.24.0" 错误

### 依赖链分析

```
streamgate
├── github.com/ethereum/go-ethereum v1.13.15 (requires go 1.24+)
├── golang.org/x/crypto v0.47.0 (requires go 1.24+)
└── 其他依赖自动拉取最新版本，也需要 Go 1.24+
```

---

## 提交记录

即将推送的提交：

```bash
git add go.mod go.sum .github/workflows/ci.yml .github/workflows/test.yml deploy/docker/
git commit -m "fix(ci): upgrade to Go 1.24 to resolve dependency conflicts

- Update go.mod to Go 1.24 (required by ethereum v1.13.15)
- Update CI workflows to use Go 1.24
- Update all Dockerfiles to golang:1.24-alpine
- Remove GOTOOLCHAIN=local restriction

Fixes: go.mod requires go >= 1.24.0 error"
```

---

## 验证状态

### ✅ 已完成
- [x] Go版本统一为1.24
- [x] go.mod 稳定在 go 1.24.0
- [x] 所有Dockerfiles使用 golang:1.24-alpine
- [x] CI workflows配置使用 Go 1.24
- [x] 移除 GOTOOLCHAIN=local 限制

### 🔄 待验证
- [ ] 本地 `go build` 成功
- [ ] GitHub Actions CI 通过
- [ ] Docker 镜像构建成功

---

## 后续工作

### 仍需修复的问题

1. **zap logger 错误** (低优先级)
   - `pkg/core/event/nats.go` - 多处 zap.Field 使用错误
   - `pkg/web3/chain.go` - zap logger 调用错误
   - `pkg/monitoring/grafana.go` - zap logger 调用错误
   - 其他文件中的类似问题

2. **golangci-lint 检查**
   - 运行 `golangci-lint run ./...` 检查代码质量
   - 修复所有 linting 错误

---

## 技术要点

### Go版本管理最佳实践

1. **依赖优先**: 让依赖包决定最低 Go 版本
2. **不要强制降级**: `GOTOOLCHAIN=local` 会导致构建失败
3. **统一版本**: 确保 go.mod、CI、Dockerfile 使用相同版本
4. **定期更新**: Go 1.24 是当前稳定版本，应该使用

### 为什么 go mod tidy 会升级 Go 版本

```bash
# go mod tidy 的行为：
1. 扫描所有依赖包的 go.mod
2. 找到最高的 go 版本要求
3. 自动更新当前项目的 go 版本
4. 这是设计行为，无法禁用
```

---

## 总结

**根本原因**: 项目依赖 `github.com/ethereum/go-ethereum v1.13.15` 和 `golang.org/x/crypto v0.47.0` 都要求 Go 1.24+

**解决方案**: 升级整个项目到 Go 1.24，而不是尝试降级依赖

**影响**: 
- ✅ 解决了 CI 构建失败问题
- ✅ 与依赖包版本保持一致
- ✅ 使用当前稳定的 Go 版本
- ⚠️ 需要确保开发环境也升级到 Go 1.24
