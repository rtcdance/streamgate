.PHONY: help build build-all build-all-parallel build-monolith build-api-gateway build-transcoder build-upload build-streaming build-metadata build-cache build-auth build-worker build-monitor build-learn clean test test-ci test-anvil test-testnet h5-demo-acceptance h5-demo-acceptance-spec bench fullchain-test docker-build docker-bake docker-bake-load docker-push docker-up docker-down lint lint-fix lint-verbose fmt migrate-up migrate-down migrate-down-all migrate-reset proto-gen mocks contracts-install contracts-build contracts-test contracts-coverage contracts-deploy-anvil contracts-deploy-sepolia contracts-gas-report fullchain-deploy fullchain-teardown deploy-monolith deploy-microservices deploy-status deploy-teardown deploy-logs one-click-deploy demo demo-down challenge run-monolith run-api-gateway run-transcoder run-upload run-streaming run-learn dev dev-setup version tree profile

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
VERSION := $(shell cat VERSION 2>/dev/null || echo "0.0.0-dev")
LDFLAGS := -ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(shell date -u '+%Y-%m-%d_%H:%M:%S')"

# Default target
help:
	@echo "StreamGate Build System"
	@echo ""
	@echo "Build:"
	@echo "  make build-all               - Build all 11 binaries (sequential)"
	@echo "  make build-all-parallel      - Build all 11 binaries in parallel (make -j4)"
	@echo "  make build-monolith          - Build monolithic binary"
	@echo "  make build-api-gateway       - Build API Gateway binary"
	@echo "  make build-auth              - Build Auth Service binary"
	@echo "  make build-cache             - Build Cache Service binary"
	@echo "  make build-metadata          - Build Metadata Service binary"
	@echo "  make build-monitor           - Build Monitor Service binary"
	@echo "  make build-streaming         - Build Streaming Service binary"
	@echo "  make build-transcoder        - Build Transcoder binary"
	@echo "  make build-upload            - Build Upload Service binary"
	@echo "  make build-worker            - Build Worker Service binary"
	@echo "  make build-learn             - Build learn/example binary"
	@echo "  make clean                   - Remove all built binaries"
	@echo ""
	@echo "Test:"
	@echo "  make test                    - Run all tests with -race + coverage HTML"
	@echo "  make test-ci                 - Run tests with coverage threshold check (default: 40%)"
	@echo "  make test-anvil              - Foundry Anvil Web3 integration tests"
	@echo "  make test-testnet            - Testnet integration tests (needs SEPOLIA_RPC)"
	@echo "  make bench                   - Run benchmarks"
	@echo "  make fullchain-test          - Run full-chain acceptance test"
	@echo "  make h5-demo-acceptance      - Playwright-driven smoke test of all 5 h5-demo HTML pages"
	@echo "  make h5-demo-acceptance-spec SPEC=NN-name - Run a single h5-demo spec"
	@echo ""
	@echo "Database migrations:"
	@echo "  make migrate-up              - Apply all pending migrations"
	@echo "  make migrate-down            - Roll back the most recent migration"
	@echo "  make migrate-down-all        - Roll back every migration"
	@echo "  make migrate-reset           - Down all + up all (full reset)"
	@echo ""
	@echo "Contracts (Foundry):"
	@echo "  make contracts-install       - Install Foundry toolchain"
	@echo "  make contracts-build         - Compile Solidity contracts"
	@echo "  make contracts-test          - Run contract test suite"
	@echo "  make contracts-coverage      - Coverage report (lcov)"
	@echo "  make contracts-deploy-anvil  - Deploy to local Anvil"
	@echo "  make contracts-deploy-sepolia - Deploy to Sepolia testnet"
	@echo "  make contracts-gas-report    - Gas usage report"
	@echo ""
	@echo "Code generation:"
	@echo "  make proto-gen               - Regenerate protobuf gRPC stubs (pkg/api/v1/)"
	@echo "  make mocks                   - Regenerate uber/mock test mocks"
	@echo ""
	@echo "Linting & format:"
	@echo "  make fmt                     - Run gofmt + goimports on the whole tree"
	@echo "  make lint                    - Run golangci-lint"
	@echo "  make lint-fix                - Auto-fix linting issues"
	@echo "  make lint-verbose            - Run linting with verbose output"
	@echo ""
	@echo "Docker:"
	@echo "  make docker-build            - Build Docker images (legacy, per-service Dockerfiles)"
	@echo "  make docker-bake             - Build all Docker images via Bake (recommended)"
	@echo "  make docker-bake-load        - Build & load monolith image via Bake"
	@echo "  make docker-push             - Push built images to registry"
	@echo "  make docker-up               - Start docker-compose dev services"
	@echo "  make docker-down             - Stop docker-compose dev services"
	@echo ""
	@echo "Deploy:"
	@echo "  make demo                    - One-command demo: infra up -> build -> run monolith"
	@echo "  make demo-down               - Stop the demo stack"
	@echo "  make fullchain-deploy        - Deploy monolith + 9 microservices (dual mode)"
	@echo "  make fullchain-teardown      - Stop the fullchain stack (add --volumes to wipe data)"
	@echo "  make one-click-deploy        - Build prebuilt Linux binaries, then start full chain"
	@echo "  make deploy-monolith         - Alias for the 7-container monolith stack"
	@echo "  make deploy-microservices    - Alias for the 16-service microservices stack"
	@echo "  make deploy-status           - Show sg-fc-* container status"
	@echo "  make deploy-teardown         - docker compose -f docker-compose.fullchain.yml down -v"
	@echo "  make deploy-logs             - Tail -f last 100 lines of stack logs"
	@echo ""
	@echo "Run locally (binaries already built):"
	@echo "  make run-monolith            - Run monolithic service"
	@echo "  make run-api-gateway         - Run API Gateway service"
	@echo "  make run-transcoder          - Run Transcoder service"
	@echo "  make run-upload              - Run Upload service"
	@echo "  make run-streaming           - Run Streaming service"
	@echo "  make run-learn               - Run learn/example binary"
	@echo ""
	@echo "Utility:"
	@echo "  make version                 - Print VERSION file contents"
	@echo "  make tree                    - Print a curated directory tree"
	@echo "  make profile                 - Capture a pprof CPU profile (30s)"
	@echo "  make dev                     - Run monolith with hot reload (requires air)"
	@echo "  make dev-setup               - Install dev dependencies (air, golangci-lint, foundry)"
	@echo "  make challenge               - Run a single Web3 security challenge"
	@echo "  make help                    - Print this help text"

# Build all binaries (parallel)
build-all: build-monolith build-api-gateway build-transcoder build-upload build-streaming build-metadata build-cache build-auth build-worker build-monitor build-learn
	@echo "✓ All binaries built successfully"

# Build all binaries in parallel (up to 4 jobs)
build-all-parallel:
	@echo "Building all binaries in parallel..."
	$(MAKE) -j4 build-monolith build-api-gateway build-transcoder build-upload build-streaming build-metadata build-cache build-auth build-worker build-monitor build-learn
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

# Run tests with coverage threshold check
test-ci: COVERAGE_MIN ?= 40
test-ci:
	@echo "Running tests with coverage check (min: $(COVERAGE_MIN)%)..."
	$(GO) test -race -coverprofile=coverage.out ./...
	@COVERAGE=$($(GO) tool cover -func=coverage.out | grep total | awk '{print $3}' | sed 's/%//'); \
	echo "Total coverage: $COVERAGE%"; \
	if [ "$(echo "$COVERAGE < $(COVERAGE_MIN)" | bc -l)" = "1" ]; then \
		echo "✗ Coverage $COVERAGE% is below minimum $(COVERAGE_MIN)%"; \
		exit 1; \
	fi; \
	echo "✓ Coverage $COVERAGE% meets minimum $(COVERAGE_MIN)%"

# Build Docker images (legacy — uses per-service Dockerfiles in deploy/docker/)
# Prefer `make docker-bake` which uses a single parameterized Dockerfile via BuildKit.
docker-build:
	@echo "Building Docker images (legacy)..."
	docker build -f deploy/docker/Dockerfile.monolith -t streamgate:monolith .
	docker build -f deploy/docker/Dockerfile.api-gateway -t streamgate:api-gateway .
	docker build -f deploy/docker/Dockerfile.transcoder -t streamgate:transcoder .
	docker build -f deploy/docker/Dockerfile.upload -t streamgate:upload .
	docker build -f deploy/docker/Dockerfile.streaming -t streamgate:streaming .
	docker build -f deploy/docker/Dockerfile.metadata -t streamgate:metadata .
	docker build -f deploy/docker/Dockerfile.cache -t streamgate:cache .
	docker build -f deploy/docker/Dockerfile.auth -t streamgate:auth .
	docker build -f deploy/docker/Dockerfile.worker -t streamgate:worker .
	docker build -f deploy/docker/Dockerfile.monitor -t streamgate:monitor .
	@echo "✓ Docker images built (legacy)"

# Build all images via Docker Bake (recommended)
#   Uses root Dockerfile parameterized by ARG — single source of truth.
docker-bake:
	@echo "Building all images with Docker Bake..."
	docker buildx bake --load
	@echo "✓ All images built via Bake"

# Build and load monolith only (fastest for local dev)
docker-bake-load:
	@echo "Building monolith with Docker Bake..."
	docker buildx bake monolith --load
	@echo "✓ Monolith image built"

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

# Run Anvil-based Web3 integration tests (requires foundry)
test-anvil:
	@echo "Running Anvil integration tests..."
	$(GO) test -tags=anvil -v -count=1 -timeout 120s ./test/integration/web3/ -run TestAnvil
	@echo "✓ Anvil integration tests complete"

# Run testnet integration tests (requires SEPOLIA_RPC env)
test-testnet:
	@echo "Running testnet integration tests..."
	$(GO) test -tags=testnet -v -count=1 -timeout 60s ./test/integration/web3/
	@echo "✓ Testnet integration tests complete"

# One-command demo: infra → build → run
demo:
	@echo "🚀 StreamGate Demo"
	@echo ""
	@echo "[1/4] Starting infrastructure (postgres + redis + minio)..."
	@docker-compose up -d postgres redis minio 2>/dev/null || true
	@echo "      Waiting for services to be healthy..."
	@for i in 1 2 3 4 5 6 7 8 9 10; do \
		healthy=true; \
		for svc in postgres redis minio; do \
			status=$$(docker inspect --format='{{.State.Health.Status}}' streamgate-$$svc 2>/dev/null); \
			if [ "$$status" != "healthy" ]; then healthy=false; fi; \
		done; \
		$$healthy && break; \
		sleep 2; \
	done; \
	if $$healthy; then echo "      ✅ All services healthy"; else echo "      ⚠️  Timeout waiting — continuing anyway"; fi
	@echo ""
	@echo "[2/4] Ensuring .env..."
	@if [ ! -f .env ]; then cp .env.example .env; echo "      Created .env from .env.example"; else echo "      .env exists"; fi
	@echo ""
	@echo "[3/4] Building monolith..."
	@$(MAKE) build-monolith
	@echo ""
	@echo "[4/4] Starting StreamGate on http://localhost:8080"
	@echo ""
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo "  Demo commands (in another terminal):"
	@echo ""
	@echo "  Health check:"
	@echo "    curl http://localhost:8080/health"
	@echo ""
	@echo "  Wallet sign-in:"
	@echo '    curl -X POST http://localhost:8080/api/v1/auth/login \'
	@echo '      -H "Content-Type: application/json" \'
	@echo '      -d '"'"'{"wallet":"0x..."}'
	@echo ""
	@echo "  NFT verification:"
	@echo '    curl -X POST http://localhost:8080/api/v1/nft/verify \'
	@echo '      -H "Content-Type: application/json" \'
	@echo '      -d '"'"'{"wallet":"0x...","contract":"0x..."}'
	@echo ""
	@echo "  Stop: Ctrl+C"
	@echo "  Cleanup: make demo-down"
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo ""
	@./bin/$(BINARY_MONOLITH)

demo-down:
	@echo "Stopping demo..."
	@-kill $$(pgrep -f bin/streamgate) 2>/dev/null || true
	@docker-compose down
	@echo "Done."

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
	kubectl apply -k deploy/k8s/
	@echo "✓ Deployment complete"

# Deploy local secrets with envsubst (template → literal substitution)
k8s-local-secret:
	@echo "Applying local secrets with envsubst..."
	@envsubst < deploy/k8s/config/local-secret.yaml | kubectl apply -f -
	@echo "✓ Local secrets applied"

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

# Full-chain Docker deployment and acceptance testing

# One-click deploy: build prebuilt Linux binaries, then start full chain
one-click-deploy:
	@echo "=== StreamGate One-Click Deploy ==="
	@echo "[1/4] Building monolith Linux binary..."
	GOOS=linux GOARCH=arm64 go build -o deploy/docker/streamgate-linux ./cmd/monolith/streamgate
	@echo "[2/4] Building api-gateway Linux binary..."
	GOOS=linux GOARCH=arm64 go build -o deploy/docker/api-gateway-linux ./cmd/microservices/api-gateway
	@echo "[3/4] Starting full chain stack..."
	docker compose -f docker-compose.fullchain.yml up -d
	@echo "[4/4] Waiting for services (30s)..."
	@sleep 30
	@echo ""
	@echo "=== Deployment Summary ==="
	@echo "Monolith demo:      http://localhost:18000/demo/"
	@echo "Microservices demo: http://localhost:18001/demo/"
	@echo "Anvil chain:        http://localhost:18545 (chain 31337)"
	@echo "DemoNFT auto-deployed + 3 NFTs minted ✓"
	@echo "Frontend defaults to Anvil (chain 31337) ✓"

fullchain-deploy:
	@./scripts/docker-deploy.sh --build

fullchain-test:
	@./scripts/fullchain-acceptance.sh

h5-demo-acceptance:
	@cd scripts/h5-demo-acceptance && [ -d node_modules ] || npm install --silent
	@cd scripts/h5-demo-acceptance && node run.mjs

h5-demo-acceptance-spec:
	@cd scripts/h5-demo-acceptance && [ -d node_modules ] || npm install --silent
	@cd scripts/h5-demo-acceptance && node run.mjs --only=$(SPEC)

fullchain-teardown:
	@./scripts/docker-teardown.sh --volumes

# Smart contract targets
contracts-install:
	@echo "Installing Foundry..."
	curl -L https://foundry.paradigm.xyz | bash
	foundryup

contracts-build:
	@cd contracts && forge build

contracts-test:
	@cd contracts && forge test -vvv

contracts-coverage:
	@cd contracts && forge coverage --report lcov

contracts-deploy-anvil:
	@cd contracts && forge script script/Deploy.s.sol --rpc-url anvil --broadcast

contracts-deploy-sepolia:
	@cd contracts && forge script script/Deploy.s.sol --rpc-url sepolia --broadcast --verify

contracts-gas-report:
	@cd contracts && forge test --gas-report

# Proto generation
proto-gen:
	@./scripts/generate-proto.sh

# Build CLI learning tool
build-learn:
	@echo "Building learn tool..."
	$(GO) build $(GOFLAGS) -o bin/learn ./cmd/learn
	@echo "OK learn tool built"

# Run CLI learning tool (interactive)
run-learn: build-learn
	@echo "Starting Web3+Go Learning Tool..."
	./bin/learn

# Challenge exercises (fix intentional bugs)
challenge:
	@echo "Running challenge exercises..."
	@for d in examples/challenges/*/; do \
		echo "  Testing $$(basename $$d)..."; \
		(cd "$$d" && $(GO) test -v -count=1 ./... 2>&1 | head -5); \
		echo ""; \
	done
	@echo "Done."

# Database migrations
migrate-up:
	@echo "Applying pending migrations..."
	$(GO) run ./cmd/migrate up
	@echo "Done."

migrate-down:
	@echo "Rolling back last migration..."
	$(GO) run ./cmd/migrate down
	@echo "Done."

migrate-down-all:
	@echo "Rolling back all migrations..."
	$(GO) run ./cmd/migrate down 999
	@echo "Done."

migrate-reset: migrate-down-all migrate-up
	@echo "Schema reset complete."

deploy-monolith:
	docker compose -f docker-compose.fullchain.yml up -d --build postgres redis minio nats anvil h5-demo monolith

deploy-microservices:
	docker compose -f docker-compose.fullchain.yml up -d --build postgres redis minio nats anvil consul h5-demo auth cache metadata monitor streaming transcoder upload worker api-gateway

deploy-status:
	@docker ps --filter "name=sg-fc" --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}" 2>/dev/null

deploy-teardown:
	docker compose -f docker-compose.fullchain.yml down -v

deploy-logs:
	docker compose -f docker-compose.fullchain.yml logs -f --tail=100
