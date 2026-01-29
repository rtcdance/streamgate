# 部署文档

本目录包含StreamGate项目的部署相关文档。

## 📄 文档列表

### QUICK_START.md
- **内容**: 快速开始指南
- **大小**: ~5KB
- **说明**:
  - 5分钟快速开始
  - 3种部署方式
  - 常用命令
  - 常见问题

### deployment-architecture.md
- **内容**: 部署架构设计
- **大小**: ~8KB
- **说明**:
  - 部署模式说明
  - 架构设计
  - 扩展策略
  - 监控方案

## 🎯 快速导航

**我想...**

- **快速开始** → 查看 `QUICK_START.md`
- **了解部署架构** → 查看 `deployment-architecture.md`
- **了解Docker Compose** → 查看 `QUICK_START.md` 的Docker Compose部分
- **了解Kubernetes** → 查看 `deployment-architecture.md`

## 🚀 快速开始

### 本地开发（单体模式）

```bash
make build-monolith
./bin/streamgate
```

### Docker Compose（微服务模式）

```bash
docker-compose up -d
```

### 构建所有服务

```bash
make build-all
```

## 📊 部署模式

### 单体模式（开发）
- 单个二进制文件
- 所有插件在一个进程中
- 内存事件总线
- 适合：开发、测试、调试

### 微服务模式（生产）
- 9个独立服务
- gRPC通信
- NATS事件总线
- 适合：生产、高流量、独立扩展

## 🔗 相关资源

- `QUICK_START.md` - 快速开始指南
- `deployment-architecture.md` - 部署架构
- `../project-planning/implementation/` - 实现规划
- `../../README.md` - 项目主文档
- `../../cmd/README.md` - 命令行工具文档

## 📚 完整文档

- **快速开始** - `QUICK_START.md`
- **架构设计** - `deployment-architecture.md`
- **项目规划** - `../project-planning/`
- **开发指南** - `../development/`
- **Web3指南** - `../web3/`

---

**最后更新**: 2025-01-28
