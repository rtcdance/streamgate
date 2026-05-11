# Build stage
FROM golang:1.24-alpine3.21 AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git make

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o streamgate ./cmd/monolith/streamgate

# Runtime stage
FROM alpine:3.21

# Install runtime dependencies
RUN apk --no-cache add ca-certificates ffmpeg wget

# Create non-root user
RUN addgroup -S appgroup && adduser -S appuser -G appgroup

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/streamgate .

# Copy config files
COPY --from=builder /app/config ./config

# Copy API documentation
COPY --from=builder /app/docs ./docs

# Set ownership
RUN chown -R appuser:appgroup /app

# Switch to non-root user
USER appuser

# Expose ports
EXPOSE 8080 9090

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

# Run application
CMD ["./streamgate"]
