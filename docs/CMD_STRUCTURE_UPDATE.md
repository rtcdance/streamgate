# CMD Directory Structure Update

## Overview

The `cmd/` directory has been reorganized to provide clear separation between monolithic and microservice deployments.

## New Structure

```
cmd/
├── monolith/                    # Monolithic deployment
│   └── streamgate/              # Single binary (all plugins in one process)
│       └── main.go
│
├── microservices/               # Microservice deployment
│   ├── api-gateway/             # API Gateway service
│   │   └── main.go
│   ├── transcoder/              # Transcoder service (high-concurrency)
│   │   └── main.go
│   ├── upload/                  # Upload service
│   │   └── main.go
│   └── streaming/               # Streaming service
│       └── main.go
│
└── README.md                    # Detailed deployment guide
```

## Old Structure (Removed)

```
cmd/
├── streamgate/                  # (REMOVED)
├── streamgate-monolith/         # (MOVED to cmd/monolith/streamgate/)
├── streamgate-api-gateway/      # (MOVED to cmd/microservices/api-gateway/)
├── streamgate-transcoder/       # (MOVED to cmd/microservices/transcoder/)
├── streamgate-upload/           # (MOVED to cmd/microservices/upload/)
├── streamgate-streaming/        # (MOVED to cmd/microservices/streaming/)
└── README.md
```

## Changes

### 1. Clear Separation

**Before**: All binaries at the same level
```
cmd/streamgate-monolith/
cmd/streamgate-api-gateway/
cmd/streamgate-transcoder/
...
```

**After**: Grouped by deployment mode
```
cmd/monolith/streamgate/
cmd/microservices/api-gateway/
cmd/microservices/transcoder/
...
```

### 2. Simplified Names

**Before**: Long prefixed names
- `streamgate-monolith`
- `streamgate-api-gateway`
- `streamgate-transcoder`

**After**: Short descriptive names
- `monolith/streamgate`
- `microservices/api-gateway`
- `microservices/transcoder`

### 3. Clearer Intent

The new structure makes it immediately obvious:
- **`cmd/monolith/`**: For development and testing
- **`cmd/microservices/`**: For production deployment

## Build Commands

### Old Commands (No longer work)

```bash
# These paths no longer exist
go build -o bin/streamgate-monolith ./cmd/streamgate-monolith
go build -o bin/streamgate-api-gateway ./cmd/streamgate-api-gateway
```

### New Commands

```bash
# Monolithic
go build -o bin/streamgate ./cmd/monolith/streamgate

# Microservices
go build -o bin/api-gateway ./cmd/microservices/api-gateway
go build -o bin/transcoder ./cmd/microservices/transcoder
go build -o bin/upload ./cmd/microservices/upload
go build -o bin/streaming ./cmd/microservices/streaming
```

### Using Makefile (Recommended)

```bash
# Build all
make build-all

# Build specific service
make build-monolith
make build-api-gateway
make build-transcoder
make build-upload
make build-streaming
```

## Docker Build

### Old Dockerfiles (Update required)

```dockerfile
# Old path
RUN go build -o streamgate ./cmd/streamgate-monolith
```

### New Dockerfiles

```dockerfile
# Monolithic
RUN go build -o streamgate ./cmd/monolith/streamgate

# API Gateway
RUN go build -o api-gateway ./cmd/microservices/api-gateway

# Transcoder
RUN go build -o transcoder ./cmd/microservices/transcoder
```

## Benefits

### 1. Clarity

The structure immediately communicates:
- What is monolithic vs microservice
- Which services are available
- How to navigate the codebase

### 2. Scalability

Easy to add new microservices:
```bash
# Add new service
mkdir -p cmd/microservices/new-service
# Create main.go
```

### 3. Consistency

All microservices follow the same pattern:
```
cmd/microservices/
├── api-gateway/
├── transcoder/
├── upload/
├── streaming/
└── new-service/    # Easy to add
```

### 4. Documentation

The structure is self-documenting:
- Developers immediately understand deployment modes
- New team members can navigate easily
- Clear separation of concerns

## Migration Guide

### For Developers

1. **Update build scripts**:
   ```bash
   # Old
   go build ./cmd/streamgate-monolith
   
   # New
   go build ./cmd/monolith/streamgate
   ```

2. **Update import paths** (if any):
   ```go
   // No changes needed - only directory structure changed
   ```

3. **Update documentation**:
   - Update any references to old paths
   - Use new paths in examples

### For CI/CD

1. **Update build pipelines**:
   ```yaml
   # Old
   - go build -o bin/streamgate-monolith ./cmd/streamgate-monolith
   
   # New
   - go build -o bin/streamgate ./cmd/monolith/streamgate
   ```

2. **Update Docker builds**:
   ```dockerfile
   # Update all Dockerfiles with new paths
   RUN go build -o streamgate ./cmd/monolith/streamgate
   ```

3. **Update deployment scripts**:
   ```bash
   # Update any scripts that reference old paths
   ```

### For Operations

1. **Binary names changed**:
   ```bash
   # Old
   ./bin/streamgate-monolith
   ./bin/streamgate-api-gateway
   
   # New
   ./bin/streamgate
   ./bin/api-gateway
   ```

2. **Docker image tags** (optional):
   ```bash
   # Consider updating image names
   streamgate:monolith
   streamgate:api-gateway
   ```

## Verification

Check the new structure:

```bash
# List structure
tree cmd/

# Verify builds
make build-all

# Check binaries
ls -lh bin/
```

Expected output:
```
bin/
├── streamgate      # Monolithic
├── api-gateway     # Microservice
├── transcoder      # Microservice
├── upload          # Microservice
└── streaming       # Microservice
```

## Summary

The new `cmd/` directory structure provides:

✅ **Clear separation** between monolithic and microservice deployments
✅ **Simplified names** without redundant prefixes
✅ **Better organization** for scalability
✅ **Self-documenting** structure
✅ **Easier navigation** for developers
✅ **Consistent patterns** for adding new services

This structure aligns with Go best practices and makes the project more maintainable and professional.
