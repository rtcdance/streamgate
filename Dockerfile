# syntax=docker/dockerfile:1.4
# ============================================================
# StreamGate — NFT-gated streaming platform
# ============================================================
# Usage:
#   docker build .                                          # monolith (default)
#   docker build --build-arg PKG=./cmd/microservices/auth .  # auth service
#   docker buildx bake                                      # all services
#   docker buildx bake monolith                              # single via bake
# ============================================================
ARG GO_VERSION=1.24
ARG DISTROLESS=false

# ============================================================
# Stage 1: Builder — compile with cache mounts
# ============================================================
FROM golang:${GO_VERSION}-alpine3.21 AS builder

ARG GOPROXY_VALUE
ARG PKG=./cmd/monolith/streamgate
ARG VERSION=0.0.0-dev
ARG BUILDTIME

WORKDIR /app

# Build tools
RUN apk add --no-cache git

# Cache-aware dependency download
#   go.mod/go.sum are bound read-only — changes invalidate only this step
#   /go/pkg/mod is persisted across builds (BuildKit cache mount)
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=bind,source=go.mod,target=go.mod \
    --mount=type=bind,source=go.sum,target=go.sum \
    GOPROXY=${GOPROXY_VALUE:-https://proxy.golang.org,direct} \
    go mod download

# Copy source
COPY . .

# Build with compiler cache
#   /root/.cache/go-build is persisted — incremental builds are near-instant
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=linux go build \
        -trimpath \
        -ldflags="-X main.Version=${VERSION} -X main.BuildTime=${BUILDTIME}" \
        -o /app/streamgate \
        ./${PKG}

# ============================================================
# Stage 2: Runtime — choose alpine (default) or distroless
#   docker build .                                        → alpine
#   docker build --target runtime-distroless .            → distroless (minimal)
# ============================================================
FROM alpine:3.21 AS runtime-alpine

ARG EXTRA_PKGS=ffmpeg

# OCI metadata
LABEL org.opencontainers.image.title="StreamGate"
LABEL org.opencontainers.image.description="NFT-gated streaming platform"
LABEL org.opencontainers.image.source="https://github.com/rtcdance/streamgate"
LABEL org.opencontainers.image.version="1.0.0"

# Runtime system dependencies
RUN apk --no-cache add ca-certificates wget ${EXTRA_PKGS}

# Non-root user
RUN addgroup -S appgroup && adduser -S appuser -G appgroup

WORKDIR /app

# Binary + runtime config only
COPY --from=builder /app/streamgate .
COPY --from=builder /app/config ./config

RUN chown -R appuser:appgroup /app
USER appuser

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

CMD ["./streamgate"]

# ============================================================
# Distroless runtime — minimal attack surface, no shell, no package manager.
# Suitable for microservices that don't need ffmpeg or healthcheck wget.
# ============================================================
FROM gcr.io/distroless/static-debian12:nonroot AS runtime-distroless

LABEL org.opencontainers.image.title="StreamGate"
LABEL org.opencontainers.image.description="NFT-gated streaming platform (distroless)"
LABEL org.opencontainers.image.source="https://github.com/rtcdance/streamgate"
LABEL org.opencontainers.image.version="1.0.0"

WORKDIR /app

COPY --from=builder /app/streamgate .
COPY --from=builder /app/config ./config

# Non-root user built into distroless base
USER 65532:65532

EXPOSE 8080

CMD ["./streamgate"]

# ============================================================
# Default target (alpine)
# ============================================================
FROM runtime-alpine
