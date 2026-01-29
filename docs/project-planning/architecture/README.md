# 架构设计文档

本目录包含StreamGate项目的架构设计文档。

## 📄 文档列表

### ARCHITECTURE_DOCUMENTATION_COMPLETE.md
- **内容**: 架构文档完成报告
- **大小**: ~13KB
- **说明**: 
  - 系统架构设计总结
  - 微内核和微服务架构
  - 通信模式和数据流
  - 完成状态和下一步

## 🎯 快速导航

**我想了解...**

- **系统架构** → 查看本文件
- **微内核设计** → 查看本文件
- **微服务架构** → 查看本文件
- **通信模式** → 查看本文件

## 📊 架构概览

### 微内核架构
- 最小化核心
- 可插拔组件
- 9个插件
- 事件驱动

### 双模式部署
- 单体模式（开发）
- 微服务模式（生产）

### 9个微服务
1. API Gateway (9090)
2. Upload (9091)
3. Transcoder (9092)
4. Streaming (9093)
5. Metadata (9005)
6. Cache (9006)
7. Auth (9007)
8. Worker (9008)
9. Monitor (9009)

## 🔗 相关文档

- `README.md` - 项目主文档
- `docs/architecture/` - 其他架构文档
- `.kiro/specs/offchain-content-service/design.md` - 详细设计

---

**最后更新**: 2025-01-28
