.PHONY: help build build-all build-monolith build-api-gateway build-transcoder build-upload build-streaming clean test docker-build docker-up docker-down lint lint-fix lint-verbose

# Variables
BINARY_MONOLITH := streamgate
BINARY_API_GATEWAY := api-gateway
BINARY_TRANSCODER := transcoder
BINARY_UPLOAD := upload
BINARY_STREAMING := streaming
BINARY_METADATA := metadata
BINARY_CACHE := cache
BINARY_AUTH := auth
BINARY_WORKER := worker
BINARY_MONITOR := monitor

GO := go
GOFLAGS := -v
LDFLAGS := -ldflags "-X main.Version=1.0.0 -X main.BuildTime=$(shell date -u '+%Y-%m-%d_%H:%M:%S')"

# Default target
help:
	@echo "StreamGate Build System"
	@echo ""
	@echo "Available targets:"
	@echo "  make build-all          - Build all binaries"
	@echo "  make build-monolith     - Build monolithic binary"
	@echo "  make build-api-gateway  - Build API Gateway binary"
	@echo "  make build-transcoder   - Build Transcoder binary"
	@echo "  make build-upload       - Build Upload Service binary"
	@echo "  make build-streaming    - Build Streaming Service binary"
	@echo "  make build-metadata     - Build Metadata Service binary"
	@echo "  make build-cache        - Build Cache Service binary"
	@echo "  make build-auth         - Build Auth Service binary"
	@echo "  make build-worker       - Build Worker Service binary"
	@echo "  make build-monitor      - Build Monitor Service binary"
	@echo "  make clean              - Remove all built binaries"
	@echo "  make test               - Run tests"
	@echo "  make docker-build       - Build Docker images"
	@echo "  make docker-up          - Start Docker Compose services"
	@echo "  make docker-down        - Stop Docker Compose services"
	@echo "  make run-monolith       - Run monolithic service"
	@echo "  make run-api-gateway    - Run API Gateway service"
	@echo "  make run-transcoder     - Run Transcoder service"
	@echo ""
	@echo "Linting targets:"
	@echo "  make lint               - Run linting checks"
	@echo "  make lint-fix           - Auto-fix linting issues"
	@echo "  make lint-verbose       - Run linting with verbose output"
	@echo ""

# Build all binaries
build-all: build-monolith build-api-gateway build-transcoder build-upload build-streaming build-metadata build-cache build-auth build-worker build-monitor
	@echo "✓ All binaries built successfully"

# Build monolithic binary
build-monolith:
	@echo "Building $(BINARY_MONOLITH)..."
	$(GO) build $(GOFLAGS) $(LDFLAGS) -o bin/$(BINARY_MONOLITH) ./cmd/monolith/streamgate
	@echo "✓ $(BINARY_MONOLITH) built"

# Build API Gateway binary
build-api-gateway:
	@echo "Building $(BINARY_API_GATEWAY)..."
	$(GO) build $(GOFLAGS) $(LDFLAGS) -o bin/$(BINARY_API_GATEWAY) ./cmd/microservices/api-gateway
	@echo "✓ $(BINARY_API_GATEWAY) built"

# Build Transcoder binary
build-transcoder:
	@echo "Building $(BINARY_TRANSCODER)..."
	$(GO) build $(GOFLAGS) $(LDFLAGS) -o bin/$(BINARY_TRANSCODER) ./cmd/microservices/transcoder
	@echo "✓ $(BINARY_TRANSCODER) built"

# Build Upload Service binary
build-upload:
	@echo "Building $(BINARY_UPLOAD)..."
	$(GO) build $(GOFLAGS) $(LDFLAGS) -o bin/$(BINARY_UPLOAD) ./cmd/microservices/upload
	@echo "✓ $(BINARY_UPLOAD) built"

# Build Streaming Service binary
build-streaming:
	@echo "Building $(BINARY_STREAMING)..."
	$(GO) build $(GOFLAGS) $(LDFLAGS) -o bin/$(BINARY_STREAMING) ./cmd/microservices/streaming
	@echo "✓ $(BINARY_STREAMING) built"

# Build Metadata Service binary
build-metadata:
	@echo "Building $(BINARY_METADATA)..."
	$(GO) build $(GOFLAGS) $(LDFLAGS) -o bin/$(BINARY_METADATA) ./cmd/microservices/metadata
	@echo "✓ $(BINARY_METADATA) built"

# Build Cache Service binary
build-cache:
	@echo "Building $(BINARY_CACHE)..."
	$(GO) build $(GOFLAGS) $(LDFLAGS) -o bin/$(BINARY_CACHE) ./cmd/microservices/cache
	@echo "✓ $(BINARY_CACHE) built"

# Build Auth Service binary
build-auth:
	@echo "Building $(BINARY_AUTH)..."
	$(GO) build $(GOFLAGS) $(LDFLAGS) -o bin/$(BINARY_AUTH) ./cmd/microservices/auth
	@echo "✓ $(BINARY_AUTH) built"

# Build Worker Service binary
build-worker:
	@echo "Building $(BINARY_WORKER)..."
	$(GO) build $(GOFLAGS) $(LDFLAGS) -o bin/$(BINARY_WORKER) ./cmd/microservices/worker
	@echo "✓ $(BINARY_WORKER) built"

# Build Monitor Service binary
build-monitor:
	@echo "Building $(BINARY_MONITOR)..."
	$(GO) build $(GOFLAGS) $(LDFLAGS) -o bin/$(BINARY_MONITOR) ./cmd/microservices/monitor
	@echo "✓ $(BINARY_MONITOR) built"

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	rm -rf bin/
	@echo "✓ Clean complete"

# Run tests
test:
	@echo "Running tests..."
	$(GO) test -v -race -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "✓ Tests complete (coverage: coverage.html)"

# Build Docker images
docker-build:
	@echo "Building Docker images..."
	docker build -f Dockerfile.monolith -t streamgate:monolith .
	docker build -f Dockerfile.api-gateway -t streamgate:api-gateway .
	docker build -f Dockerfile.transcoder -t streamgate:transcoder .
	docker build -f Dockerfile.upload -t streamgate:upload .
	docker build -f Dockerfile.streaming -t streamgate:streaming .
	docker build -f Dockerfile.metadata -t streamgate:metadata .
	docker build -f Dockerfile.cache -t streamgate:cache .
	docker build -f Dockerfile.auth -t streamgate:auth .
	docker build -f Dockerfile.worker -t streamgate:worker .
	docker build -f Dockerfile.monitor -t streamgate:monitor .
	@echo "✓ Docker images built"

# Start Docker Compose services
docker-up:
	@echo "Starting Docker Compose services..."
	docker-compose up -d
	@echo "✓ Services started"
	@echo "  API Gateway: http://localhost:8080"
	@echo "  Prometheus: http://localhost:9090"
	@echo "  Jaeger: http://localhost:16686"

# Stop Docker Compose services
docker-down:
	@echo "Stopping Docker Compose services..."
	docker-compose down
	@echo "✓ Services stopped"

# Run monolithic service
run-monolith: build-monolith
	@echo "Running monolithic service..."
	./bin/$(BINARY_MONOLITH)

# Run API Gateway service
run-api-gateway: build-api-gateway
	@echo "Running API Gateway service..."
	./bin/$(BINARY_API_GATEWAY)

# Run Transcoder service
run-transcoder: build-transcoder
	@echo "Running Transcoder service..."
	./bin/$(BINARY_TRANSCODER)

# Run Upload Service
run-upload: build-upload
	@echo "Running Upload Service..."
	./bin/$(BINARY_UPLOAD)

# Run Streaming Service
run-streaming: build-streaming
	@echo "Running Streaming Service..."
	./bin/$(BINARY_STREAMING)

# Development setup
dev-setup:
	@echo "Setting up development environment..."
	$(GO) mod download
	$(GO) install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	$(GO) install github.com/cosmtrek/air@latest
	@echo "✓ Development environment ready"

# Lint code
lint:
	@echo "Linting code..."
	./scripts/lint.sh
	@echo "✓ Lint complete"

# Lint code with verbose output
lint-verbose:
	@echo "Linting code (verbose)..."
	./scripts/lint.sh -v
	@echo "✓ Lint complete"

# Auto-fix linting issues
lint-fix:
	@echo "Auto-fixing linting issues..."
	./scripts/lint-fix.sh
	@echo "✓ Auto-fix complete"

# Format code
fmt:
	@echo "Formatting code..."
	$(GO) fmt ./...
	@echo "✓ Format complete"

# Generate mocks
mocks:
	@echo "Generating mocks..."
	$(GO) generate ./...
	@echo "✓ Mocks generated"

# Run with hot reload (requires air)
dev:
	@echo "Running with hot reload..."
	air

# Build and push Docker images
docker-push: docker-build
	@echo "Pushing Docker images..."
	docker push streamgate:monolith
	docker push streamgate:api-gateway
	docker push streamgate:transcoder
	docker push streamgate:upload
	docker push streamgate:streaming
	docker push streamgate:metadata
	docker push streamgate:cache
	docker push streamgate:auth
	docker push streamgate:worker
	docker push streamgate:monitor
	@echo "✓ Images pushed"

# Deploy to Kubernetes
k8s-deploy:
	@echo "Deploying to Kubernetes..."
	kubectl apply -f k8s/
	@echo "✓ Deployment complete"

# Check Kubernetes status
k8s-status:
	@echo "Kubernetes status:"
	kubectl get deployments
	kubectl get pods
	kubectl get services

# View Kubernetes logs
k8s-logs:
	@echo "Streaming logs from all pods..."
	kubectl logs -f -l app=streamgate --all-containers=true

# Benchmark
bench:
	@echo "Running benchmarks..."
	$(GO) test -bench=. -benchmem ./...
	@echo "✓ Benchmarks complete"

# Profile
profile:
	@echo "Running profiling..."
	$(GO) test -cpuprofile=cpu.prof -memprofile=mem.prof -bench=. ./...
	$(GO) tool pprof -http=:8081 cpu.prof
	@echo "✓ Profiling complete (open http://localhost:8081)"

# Version info
version:
	@echo "StreamGate Version Information"
	@echo "Go Version: $$($(GO) version)"
	@echo "Build Time: $$(date -u '+%Y-%m-%d_%H:%M:%S')"
	@echo "Git Commit: $$(git rev-parse --short HEAD 2>/dev/null || echo 'unknown')"

# Show directory structure
tree:
	@echo "Project Structure:"
	@echo "cmd/"
	@echo "├── monolith/"
	@echo "│   └── streamgate/          # Monolithic deployment"
	@echo "└── microservices/"
	@echo "    ├── api-gateway/         # API Gateway service"
	@echo "    ├── transcoder/          # Transcoder service"
	@echo "    ├── upload/              # Upload service"
	@echo "    └── streaming/           # Streaming service"
