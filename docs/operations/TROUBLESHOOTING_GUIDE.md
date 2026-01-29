# StreamGate Troubleshooting Guide

**Version**: 1.0.0  
**Last Updated**: 2025-01-29  
**Status**: ✅ Complete

## Table of Contents

1. [Common Issues](#common-issues)
2. [Error Codes](#error-codes)
3. [Performance Issues](#performance-issues)
4. [Deployment Issues](#deployment-issues)
5. [Web3 Issues](#web3-issues)
6. [Debugging](#debugging)

## Common Issues

### Service Won't Start

#### Symptom
```
Error: listen tcp :8080: bind: address already in use
```

#### Solution
```bash
# Find process using port
lsof -i :8080

# Kill process
kill -9 <PID>

# Or use different port
PORT=8081 ./bin/streamgate
```

### Database Connection Failed

#### Symptom
```
Error: failed to connect to database: connection refused
```

#### Solution
```bash
# Check PostgreSQL is running
sudo systemctl status postgresql

# Check connection parameters
psql -h localhost -U streamgate -d streamgate -c "SELECT 1"

# Check credentials in .env
cat .env | grep DB_

# Verify database exists
psql -l | grep streamgate

# Create database if missing
createdb streamgate
psql streamgate < migrations/001_init_schema.sql
```

### Redis Connection Failed

#### Symptom
```
Error: failed to connect to redis: connection refused
```

#### Solution
```bash
# Check Redis is running
redis-cli ping

# Start Redis
redis-server

# Check Redis configuration
redis-cli CONFIG GET "*"

# Check connection parameters
redis-cli -h localhost -p 6379 ping
```

### File Upload Fails

#### Symptom
```
Error: failed to upload file: permission denied
```

#### Solution
```bash
# Check MinIO is running
curl http://localhost:9000/minio/health/live

# Check MinIO credentials
export MINIO_ROOT_USER=minioadmin
export MINIO_ROOT_PASSWORD=minioadmin

# Check bucket exists
mc ls minio/streamgate

# Create bucket if missing
mc mb minio/streamgate

# Check file permissions
ls -la /data/minio/
```

### Transcoding Hangs

#### Symptom
```
Transcoding job stuck at 0% for hours
```

#### Solution
```bash
# Check FFmpeg is installed
ffmpeg -version

# Check worker service is running
docker-compose ps transcoder

# Check job queue
redis-cli LLEN transcoding:queue

# Check worker logs
docker-compose logs transcoder

# Restart worker service
docker-compose restart transcoder

# Clear stuck jobs
redis-cli DEL transcoding:queue
```

### High Memory Usage

#### Symptom
```
Container using 90%+ of available memory
```

#### Solution
```bash
# Check memory usage
docker stats

# Reduce cache size in config.yaml
cache:
  max_size: 1000000000  # 1GB instead of 10GB

# Reduce connection pool
database:
  max_connections: 20  # instead of 100

# Restart service
docker-compose restart api-gateway

# Monitor memory
watch -n 1 'docker stats --no-stream'
```

### Slow API Response

#### Symptom
```
API requests taking 5+ seconds
```

#### Solution
```bash
# Check database performance
psql streamgate -c "SELECT * FROM pg_stat_statements ORDER BY mean_time DESC LIMIT 10;"

# Check Redis performance
redis-cli --stat

# Check network latency
ping <service-host>

# Check CPU usage
top

# Enable query logging
ALTER SYSTEM SET log_statement = 'all';
SELECT pg_reload_conf();

# Check slow queries
tail -f /var/log/postgresql/postgresql.log | grep "duration:"
```

## Error Codes

### Authentication Errors

| Code | Status | Cause | Solution |
|------|--------|-------|----------|
| `INVALID_SIGNATURE` | 401 | Signature verification failed | Verify wallet signature is correct |
| `EXPIRED_TOKEN` | 401 | JWT token expired | Request new token |
| `INVALID_TOKEN` | 401 | Token format invalid | Check token format |
| `NFT_NOT_OWNED` | 403 | User doesn't own required NFT | Verify NFT ownership |
| `INSUFFICIENT_BALANCE` | 403 | Insufficient token balance | Check wallet balance |

### Upload Errors

| Code | Status | Cause | Solution |
|------|--------|-------|----------|
| `UPLOAD_FAILED` | 400 | Upload failed | Check file size and format |
| `CHUNK_MISMATCH` | 400 | Chunk checksum mismatch | Retry upload |
| `STORAGE_FULL` | 507 | Storage quota exceeded | Clean up old files |
| `INVALID_FORMAT` | 415 | Unsupported file format | Use supported format |

### Streaming Errors

| Code | Status | Cause | Solution |
|------|--------|-------|----------|
| `MANIFEST_NOT_FOUND` | 404 | Manifest not generated | Wait for transcoding |
| `SEGMENT_NOT_FOUND` | 404 | Segment not available | Check content status |
| `STREAM_UNAVAILABLE` | 503 | Streaming service down | Check service status |

### Web3 Errors

| Code | Status | Cause | Solution |
|------|--------|-------|----------|
| `RPC_ERROR` | 502 | RPC endpoint error | Check RPC URL and network |
| `CONTRACT_ERROR` | 400 | Smart contract error | Check contract address |
| `CHAIN_MISMATCH` | 400 | Wrong blockchain | Use correct chain |
| `GAS_ESTIMATION_FAILED` | 400 | Gas estimation failed | Check account balance |

## Performance Issues

### Slow Content Delivery

#### Diagnosis
```bash
# Check cache hit rate
curl http://localhost:8080/api/v1/metrics | jq '.metrics.cache_hit_rate'

# Check CDN performance
curl -I https://cdn.streamgate.io/content_123/manifest.m3u8

# Check segment delivery time
time curl -o /dev/null https://cdn.streamgate.io/content_123/segment_1.ts
```

#### Solution
```bash
# Increase cache size
cache:
  max_size: 10000000000  # 10GB

# Enable compression
streaming:
  compression: true

# Use CDN
cdn:
  enabled: true
  provider: cloudflare

# Optimize segments
transcoding:
  segment_duration: 6  # seconds
  target_bitrates: [500, 1000, 2000, 4000]
```

### High CPU Usage

#### Diagnosis
```bash
# Check CPU usage
top -b -n 1 | head -20

# Check process CPU usage
ps aux | grep streamgate

# Check goroutine count
curl http://localhost:8080/debug/pprof/goroutine?debug=1
```

#### Solution
```bash
# Reduce transcoding workers
transcoding:
  worker_count: 2  # instead of 8

# Reduce concurrent connections
server:
  max_connections: 100  # instead of 1000

# Enable CPU profiling
PPROF_ENABLED=true ./bin/streamgate

# Analyze profile
go tool pprof http://localhost:6060/debug/pprof/profile?seconds=30
```

### High Disk I/O

#### Diagnosis
```bash
# Check disk I/O
iostat -x 1

# Check disk usage
df -h

# Check inode usage
df -i
```

#### Solution
```bash
# Enable compression
storage:
  compression: true

# Cleanup old files
find /data -mtime +30 -delete

# Use SSD for database
# Move PostgreSQL data to SSD

# Enable write-ahead log (WAL) optimization
ALTER SYSTEM SET wal_buffers = '16MB';
SELECT pg_reload_conf();
```

## Deployment Issues

### Kubernetes Pod Crash Loop

#### Diagnosis
```bash
# Check pod status
kubectl describe pod <pod-name> -n streamgate

# Check pod logs
kubectl logs <pod-name> -n streamgate

# Check previous logs
kubectl logs <pod-name> -n streamgate --previous
```

#### Solution
```bash
# Check resource limits
kubectl get pod <pod-name> -o yaml | grep -A 5 resources

# Increase resource limits
kubectl set resources deployment api-gateway \
  --limits=cpu=2,memory=2Gi \
  --requests=cpu=500m,memory=512Mi \
  -n streamgate

# Check liveness probe
kubectl get pod <pod-name> -o yaml | grep -A 5 livenessProbe

# Disable liveness probe temporarily
kubectl set probe deployment api-gateway --liveness --initial-delay-seconds=60 -n streamgate
```

### Service Discovery Issues

#### Diagnosis
```bash
# Check Consul status
curl http://localhost:8500/v1/status/leader

# Check registered services
curl http://localhost:8500/v1/catalog/services

# Check service health
curl http://localhost:8500/v1/health/service/api-gateway
```

#### Solution
```bash
# Restart Consul
docker-compose restart consul

# Re-register service
curl -X PUT http://localhost:8500/v1/agent/service/register \
  -d '{
    "ID": "api-gateway",
    "Name": "api-gateway",
    "Port": 8080
  }'

# Check DNS resolution
nslookup api-gateway.service.consul
```

### Network Issues

#### Diagnosis
```bash
# Check network connectivity
ping <service-host>

# Check DNS resolution
nslookup <service-name>

# Check port connectivity
nc -zv <service-host> <port>

# Check firewall rules
sudo ufw status
```

#### Solution
```bash
# Allow port through firewall
sudo ufw allow 8080

# Check network policy
kubectl get networkpolicy -n streamgate

# Update network policy
kubectl apply -f deploy/k8s/network-policy.yaml

# Check service endpoints
kubectl get endpoints -n streamgate
```

## Web3 Issues

### RPC Connection Failed

#### Symptom
```
Error: failed to connect to RPC endpoint
```

#### Solution
```bash
# Check RPC URL
echo $ETH_RPC_URL

# Test RPC endpoint
curl -X POST $ETH_RPC_URL \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}'

# Check network connectivity
ping $(echo $ETH_RPC_URL | cut -d'/' -f3)

# Use backup RPC
ETH_RPC_URL=https://eth-mainnet.g.alchemy.com/v2/YOUR_KEY ./bin/streamgate
```

### NFT Verification Failed

#### Symptom
```
Error: failed to verify NFT ownership
```

#### Solution
```bash
# Check contract address
curl -X POST $ETH_RPC_URL \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc":"2.0",
    "method":"eth_getCode",
    "params":["0x...", "latest"],
    "id":1
  }'

# Check token balance
curl -X POST $ETH_RPC_URL \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc":"2.0",
    "method":"eth_call",
    "params":[{
      "to":"0x...",
      "data":"0x70a08231..."
    },"latest"],
    "id":1
  }'

# Verify contract ABI
# Check if contract implements ERC-721 or ERC-1155
```

### Signature Verification Failed

#### Symptom
```
Error: signature verification failed
```

#### Solution
```bash
# Check signature format
# Should be 0x + 130 hex characters (65 bytes)

# Verify message hash
# Use ethers.js or web3.js to verify locally

# Check wallet address
# Ensure address is checksummed (0x...)

# Test signature verification
curl -X POST http://localhost:8080/api/v1/auth/verify \
  -H "Content-Type: application/json" \
  -d '{
    "wallet_address": "0x...",
    "signature": "0x...",
    "nonce": "..."
  }'
```

### Gas Estimation Failed

#### Symptom
```
Error: gas estimation failed
```

#### Solution
```bash
# Check account balance
curl -X POST $ETH_RPC_URL \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc":"2.0",
    "method":"eth_getBalance",
    "params":["0x...", "latest"],
    "id":1
  }'

# Check gas price
curl -X POST $ETH_RPC_URL \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc":"2.0",
    "method":"eth_gasPrice",
    "params":[],
    "id":1
  }'

# Increase gas limit
# Edit config.yaml
web3:
  gas_limit: 500000  # instead of 300000
```

## Debugging

### Enable Debug Logging

```bash
# Set log level
LOG_LEVEL=debug ./bin/streamgate

# Enable pprof
PPROF_ENABLED=true ./bin/streamgate

# Access pprof
curl http://localhost:6060/debug/pprof/
```

### CPU Profiling

```bash
# Collect CPU profile
curl http://localhost:6060/debug/pprof/profile?seconds=30 > cpu.prof

# Analyze profile
go tool pprof cpu.prof

# Top functions
(pprof) top10

# Graph
(pprof) web
```

### Memory Profiling

```bash
# Collect memory profile
curl http://localhost:6060/debug/pprof/heap > mem.prof

# Analyze profile
go tool pprof mem.prof

# Check allocations
(pprof) alloc_space

# Check in-use memory
(pprof) inuse_space
```

### Goroutine Analysis

```bash
# Check goroutine count
curl http://localhost:6060/debug/pprof/goroutine?debug=1

# Check for goroutine leaks
curl http://localhost:6060/debug/pprof/goroutine?debug=2
```

### Database Debugging

```bash
# Enable query logging
ALTER SYSTEM SET log_statement = 'all';
SELECT pg_reload_conf();

# Check slow queries
SELECT query, mean_time, calls FROM pg_stat_statements 
ORDER BY mean_time DESC LIMIT 10;

# Check table sizes
SELECT schemaname, tablename, pg_size_pretty(pg_total_relation_size(schemaname||'.'||tablename)) 
FROM pg_tables 
ORDER BY pg_total_relation_size(schemaname||'.'||tablename) DESC;

# Check index usage
SELECT schemaname, tablename, indexname, idx_scan 
FROM pg_stat_user_indexes 
ORDER BY idx_scan DESC;
```

### Network Debugging

```bash
# Capture network traffic
tcpdump -i eth0 -w capture.pcap

# Analyze with Wireshark
wireshark capture.pcap

# Check connection states
netstat -an | grep ESTABLISHED | wc -l

# Check DNS resolution
dig @localhost api-gateway.service.consul
```

---

**Last Updated**: 2025-01-29  
**Version**: 1.0.0  
**Status**: ✅ Complete
