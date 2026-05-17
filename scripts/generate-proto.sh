#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
PROTO_DIR="$PROJECT_ROOT/proto/v1"
OUTPUT_DIR="$PROJECT_ROOT"

echo "==> Generating protobuf Go code..."

protoc \
  --proto_path="$PROJECT_ROOT" \
  --go_out="$OUTPUT_DIR" \
  --go_opt=module=streamgate \
  "$PROTO_DIR/common.proto" \
  "$PROTO_DIR/auth.proto" \
  "$PROTO_DIR/content.proto" \
  "$PROTO_DIR/nft.proto" \
  "$PROTO_DIR/streaming.proto" \
  "$PROTO_DIR/upload.proto" \
  "$PROTO_DIR/service.proto"

echo "==> Generating gRPC Go code..."

protoc \
  --proto_path="$PROJECT_ROOT" \
  --go-grpc_out="$OUTPUT_DIR" \
  --go-grpc_opt=module=streamgate \
  "$PROTO_DIR/auth.proto" \
  "$PROTO_DIR/content.proto" \
  "$PROTO_DIR/nft.proto" \
  "$PROTO_DIR/streaming.proto" \
  "$PROTO_DIR/upload.proto" \
  "$PROTO_DIR/service.proto"

echo "==> Done. Generated files:"
find "$PROJECT_ROOT/pkg/api" -name "*.pb.go" -type f | sort