# StreamGate Getting Started Guide

**Version**: 1.0.0  
**Last Updated**: 2025-01-29  
**Status**: ✅ Complete

## Table of Contents

1. [5-Minute Quick Start](#5-minute-quick-start)
2. [30-Minute Setup](#30-minute-setup)
3. [First API Call](#first-api-call)
4. [Web3 Integration](#web3-integration)
5. [Deployment](#deployment)
6. [Next Steps](#next-steps)

## 5-Minute Quick Start

### Prerequisites
- Docker and Docker Compose installed
- Git installed

### Steps

```bash
# 1. Clone repository
git clone https://github.com/rtcdance/streamgate.git
cd streamgate

# 2. Start services
docker-compose up -d

# 3. Wait for services to be ready (30 seconds)
sleep 30

# 4. Check health
curl http://localhost:8080/api/v1/health

# Expected response:
# {"status":"healthy","timestamp":"2025-01-29T10:00:00Z","services":{...}}
```

**Done!** Your StreamGate instance is running.

## 30-Minute Setup

### Local Development Environment

#### 1. Install Dependencies

**macOS**:
```bash
brew install go postgresql redis ffmpeg
brew services start postgresql
brew services start redis
```

**Ubuntu/Debian**:
```bash
sudo apt-get update
sudo apt-get install golang-go postgresql redis-server ffmpeg
sudo systemctl start postgresql
sudo systemctl start redis-server
```

#### 2. Clone and Setup

```bash
# Clone repository
git clone https://github.com/rtcdance/streamgate.git
cd streamgate

# Download Go dependencies
go mod download

# Create environment file
cp .env.example .env

# Edit .env with your settings
nano .env
```

#### 3. Database Setup

```bash
# Create database
createdb streamgate

# Run migrations
psql streamgate < migrations/001_init_schema.sql
psql streamgate < migrations/002_add_content_table.sql
psql streamgate < migrations/003_add_user_table.sql
psql streamgate < migrations/004_add_nft_table.sql
psql streamgate < migrations/005_add_transaction_table.sql
```

#### 4. Build and Run

```bash
# Build monolithic binary
make build-monolith

# Run service
./bin/streamgate

# In another terminal, verify
curl http://localhost:8080/api/v1/health
```

## First API Call

### 1. Get Authentication Nonce

```bash
curl -X POST http://localhost:8080/api/v1/auth/nonce \
  -H "Content-Type: application/json" \
  -d '{
    "wallet_address": "0x742d35Cc6634C0532925a3b844Bc9e7595f42bE"
  }'

# Response:
# {
#   "nonce": "streamgate_nonce_1234567890",
#   "expires_at": "2025-01-29T10:30:00Z"
# }
```

### 2. Sign Nonce (Client-Side)

Using ethers.js:
```javascript
const ethers = require('ethers');

const wallet = new ethers.Wallet(privateKey);
const nonce = "streamgate_nonce_1234567890";
const signature = await wallet.signMessage(nonce);

console.log(signature);
// 0x...
```

### 3. Verify Signature and Get Token

```bash
curl -X POST http://localhost:8080/api/v1/auth/verify \
  -H "Content-Type: application/json" \
  -d '{
    "wallet_address": "0x742d35Cc6634C0532925a3b844Bc9e7595f42bE",
    "signature": "0x...",
    "nonce": "streamgate_nonce_1234567890"
  }'

# Response:
# {
#   "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
#   "expires_in": 86400,
#   "user": {
#     "id": "user_123",
#     "wallet_address": "0x742d35Cc6634C0532925a3b844Bc9e7595f42bE",
#     "created_at": "2025-01-29T09:30:00Z"
#   }
# }
```

### 4. Create Content

```bash
TOKEN="eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."

curl -X POST http://localhost:8080/api/v1/content \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "title": "My First Video",
    "description": "A test video",
    "nft_contract": "0x...",
    "nft_token_id": "1",
    "chain": "ethereum"
  }'

# Response:
# {
#   "id": "content_123",
#   "title": "My First Video",
#   "status": "pending",
#   "created_at": "2025-01-29T10:00:00Z"
# }
```

### 5. Upload File

```bash
CONTENT_ID="content_123"

# Initiate upload
UPLOAD_RESPONSE=$(curl -s -X POST http://localhost:8080/api/v1/upload/init \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "filename": "video.mp4",
    "size": 1073741824,
    "content_type": "video/mp4"
  }')

UPLOAD_ID=$(echo $UPLOAD_RESPONSE | jq -r '.upload_id')

# Upload chunk
curl -X PUT http://localhost:8080/api/v1/upload/$UPLOAD_ID/chunk/1 \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/octet-stream" \
  --data-binary @video.mp4

# Complete upload
curl -X POST http://localhost:8080/api/v1/upload/$UPLOAD_ID/complete \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"content_id\": \"$CONTENT_ID\"}"
```

## Web3 Integration

### NFT Verification

```bash
# Verify NFT ownership
curl -X POST http://localhost:8080/api/v1/auth/verify-nft \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "contract_address": "0x...",
    "token_id": "1",
    "chain": "ethereum"
  }'

# Response:
# {
#   "verified": true,
#   "owner": "0x742d35Cc6634C0532925a3b844Bc9e7595f42bE",
#   "balance": "1",
#   "verified_at": "2025-01-29T09:35:00Z"
# }
```

### Multi-Chain Support

Supported chains:
- Ethereum (mainnet, sepolia)
- Polygon (mainnet, mumbai)
- BSC (mainnet, testnet)
- Solana (mainnet, devnet)

```bash
# Verify on Polygon
curl -X POST http://localhost:8080/api/v1/auth/verify-nft \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "contract_address": "0x...",
    "token_id": "1",
    "chain": "polygon"
  }'

# Verify on Solana
curl -X POST http://localhost:8080/api/v1/auth/verify-nft \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "contract_address": "...",
    "token_id": "1",
    "chain": "solana"
  }'
```

## Deployment

### Docker Compose (Recommended for Testing)

```bash
# Start all services
docker-compose up -d

# View logs
docker-compose logs -f

# Stop services
docker-compose down
```

### Kubernetes (Production)

```bash
# Create namespace
kubectl create namespace streamgate

# Deploy
kubectl apply -f deploy/k8s/

# Check status
kubectl get pods -n streamgate

# Port forward
kubectl port-forward svc/api-gateway 8080:8080 -n streamgate
```

### Cloud Deployment

See [docs/deployment/COMPLETE_DEPLOYMENT_GUIDE.md](../deployment/COMPLETE_DEPLOYMENT_GUIDE.md) for:
- AWS (ECS/EKS)
- Google Cloud (GKE)
- Azure (AKS)

## Next Steps

### For Developers

1. **Explore the Code**
   - Review [README.md](../../README.md)
   - Check [docs/architecture/](../architecture/)
   - Study [pkg/](../../pkg/) structure

2. **Run Tests**
   ```bash
   make test
   ```

3. **Read Documentation**
   - [API Documentation](../api/API_DOCUMENTATION.md)
   - [Development Guide](../development/setup.md)
   - [Web3 Integration](../web3-setup.md)

4. **Start Contributing**
   - Fork repository
   - Create feature branch
   - Submit pull request

### For DevOps

1. **Setup Infrastructure**
   - Review [Deployment Guide](../deployment/COMPLETE_DEPLOYMENT_GUIDE.md)
   - Choose deployment platform
   - Configure environment

2. **Setup Monitoring**
   - Configure Prometheus
   - Setup Grafana dashboards
   - Configure alerts

3. **Plan Operations**
   - Review [Operations Guide](../operations/)
   - Setup backup strategy
   - Plan disaster recovery

### For Web3 Integration

1. **Setup Web3 Environment**
   - Follow [Web3 Setup Guide](../web3-setup.md)
   - Configure RPC endpoints
   - Setup wallet

2. **Integrate NFT Verification**
   - Review [NFT Verification Example](../../examples/nft-verify-demo/)
   - Implement in your application
   - Test with testnet

3. **Deploy to Production**
   - Configure mainnet RPC
   - Setup smart contracts
   - Deploy to production

## Common Tasks

### View Logs

```bash
# Docker Compose
docker-compose logs -f api-gateway

# Kubernetes
kubectl logs -f deployment/api-gateway -n streamgate

# Local
tail -f /var/log/streamgate/app.log
```

### Check Health

```bash
# API health
curl http://localhost:8080/api/v1/health

# Database
psql streamgate -c "SELECT 1"

# Redis
redis-cli ping

# Services
curl http://localhost:8500/v1/status/leader
```

### Scale Services

```bash
# Docker Compose
docker-compose up -d --scale transcoder=3

# Kubernetes
kubectl scale deployment transcoder --replicas=3 -n streamgate
```

### View Metrics

```bash
# Prometheus
curl http://localhost:9090/api/v1/query?query=up

# Grafana
# Open http://localhost:3000
# Default: admin/admin
```

## Troubleshooting

### Service Won't Start

```bash
# Check logs
docker-compose logs api-gateway

# Check port availability
lsof -i :8080

# Check environment
env | grep STREAMGATE
```

### Database Connection Failed

```bash
# Check PostgreSQL
psql -h localhost -U streamgate -d streamgate -c "SELECT 1"

# Check credentials
cat .env | grep DB_

# Create database if missing
createdb streamgate
```

### API Returns 401 Unauthorized

```bash
# Get new token
curl -X POST http://localhost:8080/api/v1/auth/nonce \
  -H "Content-Type: application/json" \
  -d '{"wallet_address": "0x..."}'

# Sign and verify
# (See First API Call section)
```

## Resources

### Documentation
- [README.md](../../README.md) - Project overview
- [API Documentation](../api/API_DOCUMENTATION.md) - Complete API reference
- [Deployment Guide](../deployment/COMPLETE_DEPLOYMENT_GUIDE.md) - Deployment options
- [Troubleshooting Guide](../operations/TROUBLESHOOTING_GUIDE.md) - Common issues

### Examples
- [NFT Verification](../../examples/nft-verify-demo/) - NFT verification example
- [Signature Verification](../../examples/signature-verify-demo/) - Web3 login
- [Streaming](../../examples/streaming-demo/) - Streaming example
- [Upload](../../examples/upload-demo/) - Upload example

### Community
- GitHub Issues: Report bugs and request features
- GitHub Discussions: Ask questions and share ideas
- Documentation: Check docs/ for detailed guides

## Support

### Getting Help

1. **Check Documentation**
   - [FAQ](../web3-faq.md)
   - [Troubleshooting](../operations/TROUBLESHOOTING_GUIDE.md)
   - [Examples](../../examples/)

2. **Search Issues**
   - GitHub Issues
   - Stack Overflow (tag: streamgate)

3. **Ask Community**
   - GitHub Discussions
   - Discord (if available)

---

**Last Updated**: 2025-01-29  
**Version**: 1.0.0  
**Status**: ✅ Complete
