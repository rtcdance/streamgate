#!/bin/bash

# StreamGate - 目录结构初始化脚本
# 此脚本自动创建完整的项目目录结构

set -e

echo "🚀 StreamGate 目录结构初始化"
echo "================================"

# 颜色定义
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 创建目录的函数
create_dir() {
    if [ ! -d "$1" ]; then
        mkdir -p "$1"
        echo -e "${GREEN}✓${NC} 创建目录: $1"
    fi
}

# 创建文件的函数
create_file() {
    if [ ! -f "$1" ]; then
        touch "$1"
        echo -e "${GREEN}✓${NC} 创建文件: $1"
    fi
}

echo -e "\n${BLUE}第1阶段: 创建pkg目录结构${NC}"
create_dir "pkg/core/config"
create_dir "pkg/core/logger"
create_dir "pkg/core/event"
create_dir "pkg/core/health"
create_dir "pkg/core/lifecycle"
create_dir "pkg/plugins/api"
create_dir "pkg/plugins/upload"
create_dir "pkg/plugins/transcoder"
create_dir "pkg/plugins/streaming"
create_dir "pkg/plugins/metadata"
create_dir "pkg/plugins/cache"
create_dir "pkg/plugins/auth"
create_dir "pkg/plugins/worker"
create_dir "pkg/plugins/monitor"
create_dir "pkg/models"
create_dir "pkg/storage"
create_dir "pkg/service"
create_dir "pkg/api/v1"
create_dir "pkg/api/grpc"
create_dir "pkg/middleware"
create_dir "pkg/util"
create_dir "pkg/web3"

echo -e "\n${BLUE}第2阶段: 创建proto目录结构${NC}"
create_dir "proto/v1"
create_dir "proto/gen/go"
create_dir "proto/gen/python"

echo -e "\n${BLUE}第3阶段: 创建config目录结构${NC}"
create_dir "config"

echo -e "\n${BLUE}第4阶段: 创建migrations目录结构${NC}"
create_dir "migrations"

echo -e "\n${BLUE}第5阶段: 创建scripts目录结构${NC}"
# scripts目录已存在，只需创建子目录

echo -e "\n${BLUE}第6阶段: 创建test目录结构${NC}"
create_dir "test/unit/core"
create_dir "test/unit/plugins"
create_dir "test/unit/service"
create_dir "test/unit/util"
create_dir "test/integration/api"
create_dir "test/integration/storage"
create_dir "test/integration/web3"
create_dir "test/e2e"
create_dir "test/fixtures"
create_dir "test/mocks"

echo -e "\n${BLUE}第7阶段: 创建deploy目录结构${NC}"
create_dir "deploy/docker"
create_dir "deploy/k8s/monolith"
create_dir "deploy/k8s/microservices/api-gateway"
create_dir "deploy/k8s/microservices/upload"
create_dir "deploy/k8s/microservices/transcoder"
create_dir "deploy/k8s/microservices/streaming"
create_dir "deploy/k8s/microservices/metadata"
create_dir "deploy/k8s/microservices/cache"
create_dir "deploy/k8s/microservices/auth"
create_dir "deploy/k8s/microservices/worker"
create_dir "deploy/k8s/microservices/monitor"
create_dir "deploy/helm/templates"

echo -e "\n${BLUE}第8阶段: 创建docs目录结构${NC}"
create_dir "docs/architecture"
create_dir "docs/api"
create_dir "docs/web3"
create_dir "docs/deployment"
create_dir "docs/development"
create_dir "docs/operations"
create_dir "docs/guides"

echo -e "\n${BLUE}第9阶段: 创建examples目录结构${NC}"
create_dir "examples/nft-verify-demo"
create_dir "examples/signature-verify-demo"
create_dir "examples/upload-demo"
create_dir "examples/streaming-demo"

echo -e "\n${BLUE}第10阶段: 创建GitHub配置目录${NC}"
create_dir ".github/workflows"
create_dir ".github/ISSUE_TEMPLATE"

echo -e "\n${BLUE}第11阶段: 创建核心文件${NC}"

# pkg/core 文件
create_file "pkg/core/microkernel.go"
create_file "pkg/core/config/config.go"
create_file "pkg/core/config/loader.go"
create_file "pkg/core/logger/logger.go"
create_file "pkg/core/logger/formatter.go"
create_file "pkg/core/event/event.go"
create_file "pkg/core/event/publisher.go"
create_file "pkg/core/event/subscriber.go"
create_file "pkg/core/health/health.go"
create_file "pkg/core/health/checker.go"
create_file "pkg/core/lifecycle/lifecycle.go"
create_file "pkg/core/lifecycle/manager.go"

# pkg/models 文件
create_file "pkg/models/content.go"
create_file "pkg/models/user.go"
create_file "pkg/models/task.go"
create_file "pkg/models/nft.go"
create_file "pkg/models/transaction.go"

# pkg/storage 文件
create_file "pkg/storage/db.go"
create_file "pkg/storage/postgres.go"
create_file "pkg/storage/cache.go"
create_file "pkg/storage/redis.go"
create_file "pkg/storage/object.go"
create_file "pkg/storage/s3.go"
create_file "pkg/storage/minio.go"

# pkg/service 文件
create_file "pkg/service/content.go"
create_file "pkg/service/upload.go"
create_file "pkg/service/transcoding.go"
create_file "pkg/service/streaming.go"
create_file "pkg/service/auth.go"
create_file "pkg/service/nft.go"
create_file "pkg/service/web3.go"

# pkg/middleware 文件
create_file "pkg/middleware/auth.go"
create_file "pkg/middleware/logging.go"
create_file "pkg/middleware/ratelimit.go"
create_file "pkg/middleware/cors.go"
create_file "pkg/middleware/tracing.go"
create_file "pkg/middleware/recovery.go"

# pkg/util 文件
create_file "pkg/util/crypto.go"
create_file "pkg/util/hash.go"
create_file "pkg/util/time.go"
create_file "pkg/util/string.go"
create_file "pkg/util/file.go"
create_file "pkg/util/validation.go"

# pkg/web3 文件
create_file "pkg/web3/chain.go"
create_file "pkg/web3/contract.go"
create_file "pkg/web3/nft.go"
create_file "pkg/web3/signature.go"
create_file "pkg/web3/wallet.go"
create_file "pkg/web3/gas.go"
create_file "pkg/web3/ipfs.go"
create_file "pkg/web3/multichain.go"

# pkg/api 文件
create_file "pkg/api/v1/content.go"
create_file "pkg/api/v1/upload.go"
create_file "pkg/api/v1/streaming.go"
create_file "pkg/api/v1/auth.go"
create_file "pkg/api/v1/nft.go"

echo -e "\n${BLUE}第12阶段: 创建插件文件${NC}"

# API网关插件
create_file "pkg/plugins/api/gateway.go"
create_file "pkg/plugins/api/rest.go"
create_file "pkg/plugins/api/auth.go"
create_file "pkg/plugins/api/ratelimit.go"

# 上传插件
create_file "pkg/plugins/upload/handler.go"
create_file "pkg/plugins/upload/chunked.go"
create_file "pkg/plugins/upload/resumable.go"
create_file "pkg/plugins/upload/storage.go"

# 转码插件
create_file "pkg/plugins/transcoder/transcoder.go"
create_file "pkg/plugins/transcoder/worker.go"
create_file "pkg/plugins/transcoder/queue.go"
create_file "pkg/plugins/transcoder/scaler.go"
create_file "pkg/plugins/transcoder/ffmpeg.go"

# 流媒体插件
create_file "pkg/plugins/streaming/handler.go"
create_file "pkg/plugins/streaming/hls.go"
create_file "pkg/plugins/streaming/dash.go"
create_file "pkg/plugins/streaming/adaptive.go"
create_file "pkg/plugins/streaming/cache.go"

# 元数据插件
create_file "pkg/plugins/metadata/handler.go"
create_file "pkg/plugins/metadata/db.go"
create_file "pkg/plugins/metadata/index.go"
create_file "pkg/plugins/metadata/search.go"

# 缓存插件
create_file "pkg/plugins/cache/handler.go"
create_file "pkg/plugins/cache/redis.go"
create_file "pkg/plugins/cache/lru.go"
create_file "pkg/plugins/cache/ttl.go"

# 认证插件
create_file "pkg/plugins/auth/handler.go"
create_file "pkg/plugins/auth/nft.go"
create_file "pkg/plugins/auth/signature.go"
create_file "pkg/plugins/auth/web3.go"
create_file "pkg/plugins/auth/multichain.go"

# 工作插件
create_file "pkg/plugins/worker/handler.go"
create_file "pkg/plugins/worker/job.go"
create_file "pkg/plugins/worker/scheduler.go"
create_file "pkg/plugins/worker/executor.go"

# 监控插件
create_file "pkg/plugins/monitor/handler.go"
create_file "pkg/plugins/monitor/metrics.go"
create_file "pkg/plugins/monitor/health.go"
create_file "pkg/plugins/monitor/alert.go"

echo -e "\n${BLUE}第13阶段: 创建Protocol Buffers文件${NC}"
create_file "proto/v1/common.proto"
create_file "proto/v1/content.proto"
create_file "proto/v1/upload.proto"
create_file "proto/v1/streaming.proto"
create_file "proto/v1/auth.proto"
create_file "proto/v1/nft.proto"

echo -e "\n${BLUE}第14阶段: 创建配置文件${NC}"
create_file "config/config.yaml"
create_file "config/config.dev.yaml"
create_file "config/config.prod.yaml"
create_file "config/config.test.yaml"
create_file "config/prometheus.yml"

echo -e "\n${BLUE}第15阶段: 创建迁移文件${NC}"
create_file "migrations/001_init_schema.sql"
create_file "migrations/002_add_content_table.sql"
create_file "migrations/003_add_user_table.sql"
create_file "migrations/004_add_nft_table.sql"
create_file "migrations/005_add_transaction_table.sql"

echo -e "\n${BLUE}第16阶段: 创建脚本文件${NC}"
create_file "scripts/setup.sh"
create_file "scripts/migrate.sh"
create_file "scripts/deploy.sh"
create_file "scripts/test.sh"
create_file "scripts/docker-build.sh"

echo -e "\n${BLUE}第17阶段: 创建部署文件${NC}"

# Docker文件
create_file "deploy/docker/Dockerfile.monolith"
create_file "deploy/docker/Dockerfile.api-gateway"
create_file "deploy/docker/Dockerfile.upload"
create_file "deploy/docker/Dockerfile.transcoder"
create_file "deploy/docker/Dockerfile.streaming"
create_file "deploy/docker/Dockerfile.metadata"
create_file "deploy/docker/Dockerfile.cache"
create_file "deploy/docker/Dockerfile.auth"
create_file "deploy/docker/Dockerfile.worker"
create_file "deploy/docker/Dockerfile.monitor"

# Kubernetes文件
create_file "deploy/k8s/namespace.yaml"
create_file "deploy/k8s/configmap.yaml"
create_file "deploy/k8s/secret.yaml"
create_file "deploy/k8s/monolith/deployment.yaml"
create_file "deploy/k8s/monolith/service.yaml"
create_file "deploy/k8s/monolith/ingress.yaml"

# Helm文件
create_file "deploy/helm/Chart.yaml"
create_file "deploy/helm/values.yaml"

echo -e "\n${BLUE}第18阶段: 创建文档文件${NC}"

# 架构文档
create_file "docs/architecture/microkernel.md"
create_file "docs/architecture/microservices.md"
create_file "docs/architecture/communication.md"
create_file "docs/architecture/data-flow.md"

# API文档
create_file "docs/api/rest-api.md"
create_file "docs/api/grpc-api.md"
create_file "docs/api/websocket-api.md"

# Web3文档
create_file "docs/web3/nft-verification.md"
create_file "docs/web3/signature-verification.md"
create_file "docs/web3/smart-contracts.md"
create_file "docs/web3/ipfs-integration.md"
create_file "docs/web3/multichain-support.md"

# 部署文档
create_file "docs/deployment/docker-compose.md"
create_file "docs/deployment/kubernetes.md"
create_file "docs/deployment/helm.md"
create_file "docs/deployment/production-setup.md"

# 开发文档
create_file "docs/development/setup.md"
create_file "docs/development/coding-standards.md"
create_file "docs/development/testing.md"
create_file "docs/development/debugging.md"

# 运维文档
create_file "docs/operations/monitoring.md"
create_file "docs/operations/logging.md"
create_file "docs/operations/troubleshooting.md"
create_file "docs/operations/backup-recovery.md"

# 指南
create_file "docs/guides/quick-start.md"
create_file "docs/guides/plugin-development.md"
create_file "docs/guides/service-development.md"
create_file "docs/guides/web3-integration.md"

echo -e "\n${BLUE}第19阶段: 创建GitHub配置文件${NC}"
create_file ".github/workflows/ci.yml"
create_file ".github/workflows/test.yml"
create_file ".github/workflows/build.yml"
create_file ".github/workflows/deploy.yml"
create_file ".github/ISSUE_TEMPLATE/bug_report.md"
create_file ".github/ISSUE_TEMPLATE/feature_request.md"
create_file ".github/PULL_REQUEST_TEMPLATE.md"

echo -e "\n${BLUE}第20阶段: 创建测试文件${NC}"
create_file "test/unit/core/microkernel_test.go"
create_file "test/unit/core/config_test.go"
create_file "test/unit/plugins/api_test.go"
create_file "test/unit/service/content_test.go"
create_file "test/integration/api/rest_test.go"
create_file "test/integration/storage/db_test.go"
create_file "test/e2e/upload_flow_test.go"
create_file "test/e2e/streaming_flow_test.go"
create_file "test/e2e/nft_verification_test.go"
create_file "test/fixtures/content.json"
create_file "test/fixtures/user.json"
create_file "test/fixtures/nft.json"
create_file "test/mocks/storage_mock.go"
create_file "test/mocks/web3_mock.go"
create_file "test/mocks/service_mock.go"

echo -e "\n${BLUE}第21阶段: 创建示例文件${NC}"
create_file "examples/nft-verify-demo/main.go"
create_file "examples/nft-verify-demo/README.md"
create_file "examples/nft-verify-demo/go.mod"
create_file "examples/signature-verify-demo/main.go"
create_file "examples/signature-verify-demo/README.md"
create_file "examples/signature-verify-demo/go.mod"
create_file "examples/upload-demo/main.go"
create_file "examples/upload-demo/README.md"
create_file "examples/streaming-demo/main.go"
create_file "examples/streaming-demo/README.md"

echo -e "\n${GREEN}================================${NC}"
echo -e "${GREEN}✓ 目录结构初始化完成！${NC}"
echo -e "${GREEN}================================${NC}"

echo -e "\n${BLUE}统计信息:${NC}"
echo "目录数: $(find . -type d -not -path '*/\.*' | wc -l)"
echo "文件数: $(find . -type f -not -path '*/\.*' | wc -l)"

echo -e "\n${BLUE}下一步:${NC}"
echo "1. 运行 'go mod tidy' 更新依赖"
echo "2. 运行 'make build-all' 构建所有服务"
echo "3. 查看 DIRECTORY_STRUCTURE_PLAN.md 了解详细信息"

echo -e "\n✨ 完成！"
