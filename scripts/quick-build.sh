#!/bin/bash

# StreamGate Quick Build Script
# 快速编译和运行 StreamGate

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 打印函数
print_header() {
    echo -e "${BLUE}========================================${NC}"
    echo -e "${BLUE}$1${NC}"
    echo -e "${BLUE}========================================${NC}"
}

print_success() {
    echo -e "${GREEN}✓ $1${NC}"
}

print_error() {
    echo -e "${RED}✗ $1${NC}"
}

print_warning() {
    echo -e "${YELLOW}⚠ $1${NC}"
}

# 检查 Go 安装
check_go() {
    print_header "检查 Go 环境"
    
    if ! command -v go &> /dev/null; then
        print_error "Go 未安装"
        exit 1
    fi
    
    GO_VERSION=$(go version | awk '{print $3}')
    print_success "Go 已安装: $GO_VERSION"
}

# 下载依赖
download_deps() {
    print_header "下载 Go 依赖"
    
    echo "运行 go mod download..."
    if go mod download; then
        print_success "依赖下载成功"
    else
        print_warning "依赖下载失败，尝试 go mod tidy..."
        go mod tidy || print_warning "go mod tidy 失败，继续编译..."
    fi
}

# 编译单体应用
build_monolith() {
    print_header "编译单体应用"
    
    mkdir -p bin
    
    echo "编译 cmd/monolith/streamgate..."
    if go build -o bin/streamgate ./cmd/monolith/streamgate; then
        print_success "单体应用编译成功: bin/streamgate"
        ls -lh bin/streamgate
    else
        print_error "单体应用编译失败"
        return 1
    fi
}

# 编译微服务
build_microservices() {
    print_header "编译微服务"
    
    mkdir -p bin
    
    SERVICES=(
        "api-gateway"
        "auth"
        "cache"
        "metadata"
        "monitor"
        "streaming"
        "transcoder"
        "upload"
        "worker"
    )
    
    for service in "${SERVICES[@]}"; do
        echo "编译 cmd/microservices/$service..."
        if go build -o bin/$service ./cmd/microservices/$service; then
            print_success "编译成功: bin/$service"
        else
            print_error "编译失败: $service"
            return 1
        fi
    done
    
    echo ""
    print_success "所有微服务编译成功"
    ls -lh bin/
}

# 编译所有
build_all() {
    print_header "编译所有应用"
    
    build_monolith || return 1
    echo ""
    build_microservices || return 1
}

# 运行单体应用
run_monolith() {
    print_header "运行单体应用"
    
    if [ ! -f bin/streamgate ]; then
        print_error "bin/streamgate 不存在，请先编译"
        return 1
    fi
    
    print_warning "确保以下服务已启动:"
    echo "  - PostgreSQL (localhost:5432)"
    echo "  - Redis (localhost:6379)"
    echo ""
    
    echo "启动单体应用..."
    ./bin/streamgate
}

# 运行微服务
run_microservices() {
    print_header "运行微服务"
    
    print_warning "确保以下服务已启动:"
    echo "  - PostgreSQL (localhost:5432)"
    echo "  - Redis (localhost:6379)"
    echo "  - NATS (localhost:4222)"
    echo "  - Consul (localhost:8500)"
    echo ""
    
    SERVICES=(
        "api-gateway"
        "auth"
        "cache"
        "metadata"
        "monitor"
        "streaming"
        "transcoder"
        "upload"
        "worker"
    )
    
    for service in "${SERVICES[@]}"; do
        if [ ! -f bin/$service ]; then
            print_error "bin/$service 不存在"
            return 1
        fi
        
        echo "启动 $service..."
        ./bin/$service &
        sleep 1
    done
    
    print_success "所有微服务已启动"
    echo ""
    echo "运行中的进程:"
    ps aux | grep bin/ | grep -v grep
}

# 显示帮助
show_help() {
    cat << EOF
StreamGate 快速编译脚本

用法: $0 [命令]

命令:
  check           检查 Go 环境
  deps            下载依赖
  build-monolith  编译单体应用
  build-services  编译微服务
  build-all       编译所有应用
  run-monolith    运行单体应用
  run-services    运行微服务
  full            完整流程 (deps + build-all)
  help            显示此帮助信息

示例:
  $0 check                # 检查环境
  $0 full                 # 完整编译
  $0 build-all            # 编译所有
  $0 run-monolith         # 运行单体应用

EOF
}

# 完整流程
full_process() {
    check_go
    download_deps
    build_all
    
    echo ""
    print_success "编译完成！"
    echo ""
    echo "下一步:"
    echo "  运行单体应用: $0 run-monolith"
    echo "  运行微服务: $0 run-services"
}

# 主函数
main() {
    if [ $# -eq 0 ]; then
        show_help
        exit 0
    fi
    
    case "$1" in
        check)
            check_go
            ;;
        deps)
            download_deps
            ;;
        build-monolith)
            build_monolith
            ;;
        build-services)
            build_microservices
            ;;
        build-all)
            build_all
            ;;
        run-monolith)
            run_monolith
            ;;
        run-services)
            run_microservices
            ;;
        full)
            full_process
            ;;
        help)
            show_help
            ;;
        *)
            print_error "未知命令: $1"
            show_help
            exit 1
            ;;
    esac
}

# 运行主函数
main "$@"
