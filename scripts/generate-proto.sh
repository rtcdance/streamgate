#!/bin/bash

set -e

echo "Generating gRPC code from protocol buffers..."

# Check if protoc is installed
if ! command -v protoc &> /dev/null; then
    echo "Error: protoc is not installed"
    echo "Please install Protocol Buffers compiler:"
    echo "  macOS: brew install protobuf"
    echo "  Ubuntu: apt-get install protobuf-compiler"
    exit 1
fi

# Check if protoc-gen-go is installed
if ! command -v protoc-gen-go &> /dev/null; then
    echo "Installing protoc-gen-go..."
    go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
fi

# Check if protoc-gen-go-grpc is installed
if ! command -v protoc-gen-go-grpc &> /dev/null; then
    echo "Installing protoc-gen-go-grpc..."
    go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
fi

# Create output directory
mkdir -p pkg/api/v1

# Generate Go code from proto files
protoc --proto_path=proto \
    --go_out=pkg/api/v1 \
    --go_opt=paths=source_relative \
    --go-grpc_out=pkg/api/v1 \
    --go-grpc_opt=paths=source_relative \
    proto/v1/*.proto

echo "✓ gRPC code generated successfully"
