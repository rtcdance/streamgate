#!/bin/bash

# StreamGate - 文档组织脚本
# 将根目录的文档移动到docs/project-planning目录中

set -e

echo "🚀 StreamGate 文档组织"
echo "================================"

# 颜色定义
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# 移动文件的函数
move_file() {
    if [ -f "$1" ]; then
        mv "$1" "$2"
        echo -e "${GREEN}✓${NC} 移动: $1 → $2"
    else
        echo -e "${YELLOW}⚠${NC} 文件不存在: $1"
    fi
}

echo -e "\n${BLUE}第1步: 移动架构文档${NC}"
move_file "ARCHITECTURE_DOCUMENTATION_COMPLETE.md" "docs/project-planning/architecture/"

echo -e "\n${BLUE}第2步: 移动目录结构文档${NC}"
move_file "DIRECTORY_STRUCTURE_PLAN.md" "docs/project-planning/directory-structure/"
move_file "DIRECTORY_STRUCTURE_IMPLEMENTATION.md" "docs/project-planning/directory-structure/"
move_file "DIRECTORY_STRUCTURE_SUMMARY.md" "docs/project-planning/directory-structure/"
move_file "DIRECTORY_STRUCTURE_COMPLETE.md" "docs/project-planning/directory-structure/"

echo -e "\n${BLUE}第3步: 移动实现规划文档${NC}"
move_file "IMPLEMENTATION_READY.md" "docs/project-planning/implementation/"
move_file "PROJECT_IMPLEMENTATION_GUIDE.md" "docs/project-planning/implementation/"
move_file "WEB3_ACTION_PLAN.md" "docs/project-planning/implementation/"
move_file "WEB3_CHECKLIST.md" "docs/project-planning/implementation/"

echo -e "\n${BLUE}第4步: 移动项目状态文档${NC}"
move_file "FINAL_STATUS_REPORT.md" "docs/project-planning/status/"
move_file "PROJECT_STATUS.md" "docs/project-planning/status/"
move_file "SESSION_COMPLETION_SUMMARY.md" "docs/project-planning/status/"
move_file "MICROSERVICES_SETUP_COMPLETE.md" "docs/project-planning/status/"
move_file "README_UPDATE_SUMMARY.md" "docs/project-planning/status/"
move_file "CLEANUP_SUMMARY.md" "docs/project-planning/status/"

echo -e "\n${BLUE}第5步: 检查根目录剩余文档${NC}"
echo "根目录剩余的.md文件:"
ls -1 *.md 2>/dev/null | grep -v README.md || echo "  (仅README.md)"

echo -e "\n${GREEN}================================${NC}"
echo -e "${GREEN}✓ 文档组织完成！${NC}"
echo -e "${GREEN}================================${NC}"

echo -e "\n${BLUE}文档结构:${NC}"
echo "docs/project-planning/"
echo "├── README.md                    # 文档索引"
echo "├── architecture/                # 架构设计"
echo "│   ├── README.md"
echo "│   └── ARCHITECTURE_DOCUMENTATION_COMPLETE.md"
echo "├── directory-structure/         # 目录结构"
echo "│   ├── README.md"
echo "│   ├── DIRECTORY_STRUCTURE_PLAN.md"
echo "│   ├── DIRECTORY_STRUCTURE_IMPLEMENTATION.md"
echo "│   ├── DIRECTORY_STRUCTURE_SUMMARY.md"
echo "│   └── DIRECTORY_STRUCTURE_COMPLETE.md"
echo "├── implementation/              # 实现规划"
echo "│   ├── README.md"
echo "│   ├── IMPLEMENTATION_READY.md"
echo "│   ├── PROJECT_IMPLEMENTATION_GUIDE.md"
echo "│   ├── WEB3_ACTION_PLAN.md"
echo "│   └── WEB3_CHECKLIST.md"
echo "└── status/                      # 项目状态"
echo "    ├── README.md"
echo "    ├── FINAL_STATUS_REPORT.md"
echo "    ├── PROJECT_STATUS.md"
echo "    ├── SESSION_COMPLETION_SUMMARY.md"
echo "    ├── MICROSERVICES_SETUP_COMPLETE.md"
echo "    ├── README_UPDATE_SUMMARY.md"
echo "    └── CLEANUP_SUMMARY.md"

echo -e "\n${BLUE}根目录现在只包含:${NC}"
echo "  - README.md (项目主文档)"
echo "  - 配置文件 (Makefile, docker-compose.yml等)"
echo "  - 源代码目录 (cmd/, pkg/, etc.)"

echo -e "\n✨ 完成！"
