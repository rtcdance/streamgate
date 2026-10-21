# StreamGate API Documentation

**Version**: 1.0.0  
**Last Updated**: 2025-01-29  
**Status**: ✅ Complete

## Table of Contents

1. [Overview](#overview)
2. [Authentication](#authentication)
3. [REST API](#rest-api)
4. [gRPC API](#grpc-api)
5. [WebSocket API](#websocket-api)
6. [Error Handling](#error-handling)
7. [Rate Limiting](#rate-limiting)
8. [Examples](#examples)

## Overview

StreamGate provides three API interfaces:

- **REST API** - Traditional HTTP/JSON interface for web clients
- **gRPC API** - High-performance binary protocol for service-to-service communication
- **WebSocket API** - Real-time bidirectional communication for streaming and notifications

### Base URLs

| Environment | URL | Port |
|-------------|-----|------|
| Development | `http://localhost:8080` | 8080 |
| Production | `https://api.streamgate.io` | 443 |
| gRPC | `localhost:9090` | 9090 |
| WebSocket | `ws://localhost:8080/ws` | 8080 |

## Authentication

### Web3 Wallet Authentication

StreamGate uses Web3 wallet signatures for authentication instead of traditional passwords.

#### Flow

```
1. Client requests nonce from server
2. Client signs nonce with wallet private key
3. Client sends signature to server
4. Server verifies signature and issues JWT token
5. Client includes JWT in subsequent requests
```

#### Implementation

```bash
# 1. Get nonce
curl -X POST http://localhost:8080/api/v1/auth/nonce \
  -H "Content-Type: application/json" \
  -d '{"wallet_address": "0x742d35Cc6634C0532925a3b844Bc9e7595f42bE"}'

# Response
{
  "nonce": "streamgate_nonce_1234567890",
  "expires_at": "2025-01-29T10:30:00Z"
}

# 2. Sign nonce with wallet (client-side)
# Using ethers.js or web3.js
const signature = await signer.signMessage(nonce);

# 3. Verify signature and get token
curl -X POST http://localhost:8080/api/v1/auth/verify \
  -H "Content-Type: application/json" \
  -d '{
    "wallet_address": "0x742d35Cc6634C0532925a3b844Bc9e7595f42bE",
    "signature": "0x...",
    "nonce": "streamgate_nonce_1234567890"
  }'

# Response
{
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "expires_in": 86400,
  "user": {
    "id": "user_123",
    "wallet_address": "0x742d35Cc6634C0532925a3b844Bc9e7595f42bE",
    "created_at": "2025-01-29T09:30:00Z"
  }
}

# 4. Use token in subsequent requests
curl -X GET http://localhost:8080/api/v1/content \
  -H "Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
```

### NFT Verification

For content protected by NFT ownership:

```bash
# Verify NFT ownership
curl -X POST http://localhost:8080/api/v1/auth/verify-nft \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <token>" \
  -d '{
    "contract_address": "0x...",
    "token_id": "1",
    "chain": "ethereum"
  }'

# Response
{
  "verified": true,
  "owner": "0x742d35Cc6634C0532925a3b844Bc9e7595f42bE",
  "balance": "1",
  "verified_at": "2025-01-29T09:35:00Z"
}
```

## REST API

### Content Management

#### List Content

```http
GET /api/v1/content
Authorization: Bearer <token>
```

**Query Parameters**:
- `page` (int, default: 1) - Page number
- `limit` (int, default: 20) - Items per page
- `sort` (string, default: "created_at") - Sort field
- `order` (string, default: "desc") - Sort order (asc/desc)
- `search` (string) - Search query

**Response**:
```json
{
  "data": [
    {
      "id": "content_123",
      "title": "Sample Video",
      "description": "A sample video content",
      "duration": 3600,
      "size": 1073741824,
      "status": "ready",
      "created_at": "2025-01-29T09:00:00Z",
      "updated_at": "2025-01-29T09:30:00Z"
    }
  ],
  "pagination": {
    "page": 1,
    "limit": 20,
    "total": 100,
    "pages": 5
  }
}
```

#### Get Content Details

```http
GET /api/v1/content/{content_id}
Authorization: Bearer <token>
```

**Response**:
```json
{
  "id": "content_123",
  "title": "Sample Video",
  "description": "A sample video content",
  "duration": 3600,
  "size": 1073741824,
  "status": "ready",
  "formats": [
    {
      "format": "hls",
      "url": "https://cdn.streamgate.io/content_123/manifest.m3u8",
      "bitrates": [500, 1000, 2000, 4000]
    },
    {
      "format": "dash",
      "url": "https://cdn.streamgate.io/content_123/manifest.mpd",
      "bitrates": [500, 1000, 2000, 4000]
    }
  ],
  "created_at": "2025-01-29T09:00:00Z",
  "updated_at": "2025-01-29T09:30:00Z"
}
```

#### Create Content

```http
POST /api/v1/content
Authorization: Bearer <token>
Content-Type: application/json
```

**Request Body**:
```json
{
  "title": "New Video",
  "description": "Video description",
  "nft_contract": "0x...",
  "nft_token_id": "1",
  "chain": "ethereum"
}
```

**Response**: 201 Created
```json
{
  "id": "content_124",
  "title": "New Video",
  "status": "pending",
  "created_at": "2025-01-29T10:00:00Z"
}
```

### File Upload

#### Initiate Upload

```http
POST /api/v1/upload/init
Authorization: Bearer <token>
Content-Type: application/json
```

**Request Body**:
```json
{
  "filename": "video.mp4",
  "size": 1073741824,
  "content_type": "video/mp4"
}
```

**Response**:
```json
{
  "upload_id": "upload_123",
  "chunk_size": 5242880,
  "total_chunks": 205,
  "expires_at": "2025-01-30T10:00:00Z"
}
```

#### Upload Chunk

```http
PUT /api/v1/upload/{upload_id}/chunk/{chunk_number}
Authorization: Bearer <token>
Content-Type: application/octet-stream

[binary chunk data]
```

**Response**:
```json
{
  "chunk_number": 1,
  "status": "uploaded",
  "progress": 0.5
}
```

#### Complete Upload

```http
POST /api/v1/upload/{upload_id}/complete
Authorization: Bearer <token>
Content-Type: application/json
```

**Request Body**:
```json
{
  "content_id": "content_123"
}
```

**Response**:
```json
{
  "upload_id": "upload_123",
  "status": "completed",
  "content_id": "content_123",
  "completed_at": "2025-01-29T10:30:00Z"
}
```

### Streaming

#### Get Manifest

```http
GET /api/v1/streaming/{content_id}/manifest.m3u8
Authorization: Bearer <token>
```

**Response**: HLS manifest (m3u8 format)

#### Get Segment

```http
GET /api/v1/streaming/{content_id}/segment_{number}.ts
Authorization: Bearer <token>
Range: bytes=0-1048575
```

**Response**: Video segment (binary)

### Monitoring

#### Get Metrics

```http
GET /api/v1/metrics
Authorization: Bearer <token>
```

**Response**:
```json
{
  "timestamp": "2025-01-29T10:00:00Z",
  "metrics": {
    "requests_total": 10000,
    "requests_per_second": 100,
    "cache_hit_rate": 0.85,
    "average_response_time_ms": 150,
    "active_connections": 500,
    "transcoding_jobs": 10
  }
}
```

#### Get Health Status

```http
GET /api/v1/health
```

**Response**:
```json
{
  "status": "healthy",
  "timestamp": "2025-01-29T10:00:00Z",
  "services": {
    "database": "healthy",
    "cache": "healthy",
    "storage": "healthy",
    "nats": "healthy"
  }
}
```

## gRPC API

### Service Definition

```protobuf
service ContentService {
  rpc ListContent(ListContentRequest) returns (ListContentResponse);
  rpc GetContent(GetContentRequest) returns (Content);
  rpc CreateContent(CreateContentRequest) returns (Content);
  rpc UpdateContent(UpdateContentRequest) returns (Content);
  rpc DeleteContent(DeleteContentRequest) returns (Empty);
}

service StreamingService {
  rpc GetManifest(GetManifestRequest) returns (Manifest);
  rpc GetSegment(GetSegmentRequest) returns (stream SegmentData);
}

service UploadService {
  rpc InitUpload(InitUploadRequest) returns (UploadSession);
  rpc UploadChunk(stream ChunkData) returns (UploadProgress);
  rpc CompleteUpload(CompleteUploadRequest) returns (UploadResult);
}
```

### Usage Example (Go)

```go
import "streamgate/pkg/api/grpc"

// Create client
conn, err := grpc.Dial("localhost:9090")
defer conn.Close()

client := grpc.NewContentServiceClient(conn)

// List content
resp, err := client.ListContent(ctx, &grpc.ListContentRequest{
  Page:  1,
  Limit: 20,
})

// Get content
content, err := client.GetContent(ctx, &grpc.GetContentRequest{
  ContentId: "content_123",
})
```

## WebSocket API

### Connection

```javascript
const ws = new WebSocket('ws://localhost:8080/ws?token=<jwt_token>');

ws.onopen = () => {
  console.log('Connected');
};

ws.onmessage = (event) => {
  const message = JSON.parse(event.data);
  console.log('Received:', message);
};

ws.onerror = (error) => {
  console.error('Error:', error);
};

ws.onclose = () => {
  console.log('Disconnected');
};
```

### Message Types

#### Subscribe to Events

```json
{
  "type": "subscribe",
  "channel": "transcoding_progress",
  "content_id": "content_123"
}
```

#### Transcoding Progress

```json
{
  "type": "transcoding_progress",
  "content_id": "content_123",
  "progress": 0.75,
  "status": "processing",
  "current_bitrate": 2000,
  "eta_seconds": 300
}
```

#### Upload Progress

```json
{
  "type": "upload_progress",
  "upload_id": "upload_123",
  "progress": 0.5,
  "uploaded_bytes": 536870912,
  "total_bytes": 1073741824,
  "speed_mbps": 50
}
```

#### Streaming Event

```json
{
  "type": "streaming_event",
  "content_id": "content_123",
  "event": "playback_started",
  "timestamp": "2025-01-29T10:00:00Z"
}
```

## Error Handling

### Error Response Format

```json
{
  "error": {
    "code": "INVALID_REQUEST",
    "message": "Invalid request parameters",
    "details": {
      "field": "content_id",
      "reason": "required field missing"
    }
  }
}
```

### Error Codes

| Code | HTTP Status | Description |
|------|-------------|-------------|
| `INVALID_REQUEST` | 400 | Invalid request parameters |
| `UNAUTHORIZED` | 401 | Authentication failed |
| `FORBIDDEN` | 403 | Access denied |
| `NOT_FOUND` | 404 | Resource not found |
| `CONFLICT` | 409 | Resource conflict |
| `RATE_LIMITED` | 429 | Rate limit exceeded |
| `INTERNAL_ERROR` | 500 | Internal server error |
| `SERVICE_UNAVAILABLE` | 503 | Service unavailable |

## Rate Limiting

### Limits

| Endpoint | Limit | Window |
|----------|-------|--------|
| `/api/v1/auth/*` | 10 | 1 minute |
| `/api/v1/content` | 100 | 1 minute |
| `/api/v1/upload/*` | 50 | 1 minute |
| `/api/v1/streaming/*` | 1000 | 1 minute |

### Rate Limit Headers

```
X-RateLimit-Limit: 100
X-RateLimit-Remaining: 95
X-RateLimit-Reset: 1643385600
```

## Examples

### Complete Upload Flow

```bash
# 1. Authenticate
TOKEN=$(curl -s -X POST http://localhost:8080/api/v1/auth/verify \
  -H "Content-Type: application/json" \
  -d '{...}' | jq -r '.token')

# 2. Create content
CONTENT_ID=$(curl -s -X POST http://localhost:8080/api/v1/content \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "title": "My Video",
    "description": "My video description"
  }' | jq -r '.id')

# 3. Initiate upload
UPLOAD_ID=$(curl -s -X POST http://localhost:8080/api/v1/upload/init \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "filename": "video.mp4",
    "size": 1073741824,
    "content_type": "video/mp4"
  }' | jq -r '.upload_id')

# 4. Upload chunks
for i in {1..205}; do
  dd if=video.mp4 bs=5242880 skip=$((i-1)) count=1 | \
  curl -X PUT http://localhost:8080/api/v1/upload/$UPLOAD_ID/chunk/$i \
    -H "Authorization: Bearer $TOKEN" \
    -H "Content-Type: application/octet-stream" \
    --data-binary @-
done

# 5. Complete upload
curl -X POST http://localhost:8080/api/v1/upload/$UPLOAD_ID/complete \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"content_id\": \"$CONTENT_ID\"}"

# 6. Stream content
curl -X GET http://localhost:8080/api/v1/streaming/$CONTENT_ID/manifest.m3u8 \
  -H "Authorization: Bearer $TOKEN"
```

### WebSocket Monitoring

```javascript
const ws = new WebSocket('ws://localhost:8080/ws?token=' + token);

ws.onopen = () => {
  // Subscribe to transcoding progress
  ws.send(JSON.stringify({
    type: 'subscribe',
    channel: 'transcoding_progress',
    content_id: 'content_123'
  }));
};

ws.onmessage = (event) => {
  const msg = JSON.parse(event.data);
  
  if (msg.type === 'transcoding_progress') {
    console.log(`Progress: ${(msg.progress * 100).toFixed(1)}%`);
    console.log(`ETA: ${msg.eta_seconds} seconds`);
  }
};
```

---

**Last Updated**: 2025-01-29  
**Version**: 1.0.0  
**Status**: ✅ Complete
