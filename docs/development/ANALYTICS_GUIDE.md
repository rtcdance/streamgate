# StreamGate Analytics Guide

**Date**: 2025-01-28  
**Status**: Analytics Implementation  
**Version**: 1.0.0

## Overview

StreamGate includes a comprehensive real-time analytics system that collects, aggregates, and analyzes system metrics, user behavior, and performance data. The analytics system enables real-time insights, anomaly detection, and predictive capabilities.

## Architecture

### Components

1. **Event Collector** - Collects analytics events from services
2. **Aggregator** - Aggregates data into time-based buckets
3. **Anomaly Detector** - Detects anomalies in metrics
4. **Predictor** - Makes predictions based on historical data
5. **Analytics Service** - Orchestrates all components
6. **HTTP Handler** - Provides REST API for analytics

### Data Flow

```
Services
   │
   ├─> RecordEvent
   ├─> RecordMetrics
   ├─> RecordUserBehavior
   ├─> RecordPerformanceMetric
   └─> RecordBusinessMetric
        │
        ▼
   Event Collector
        │
        ├─> Aggregator (time-based aggregation)
        ├─> Anomaly Detector (anomaly detection)
        └─> Predictor (ML predictions)
        │
        ▼
   Analytics Service
        │
        ├─> Dashboard Data
        ├─> Aggregations
        ├─> Anomalies
        └─> Predictions
        │
        ▼
   HTTP API / Dashboards
```

## Usage

### Recording Events

```go
import "streamgate/pkg/analytics"

// Create analytics service
service := analytics.NewService()
defer service.Close()

// Record an event
service.RecordEvent(
    "upload_started",           // event type
    "upload-service",           // service ID
    "user123",                  // user ID
    map[string]interface{}{     // metadata
        "file_size": 1024,
        "format": "mp4",
    },
    map[string]string{          // tags
        "region": "us-west",
        "tier": "premium",
    },
)
```

### Recording Metrics

```go
// Record system metrics
service.RecordMetrics(
    "api-gateway",              // service ID
    45.5,                       // CPU usage (%)
    62.3,                       // Memory usage (%)
    78.1,                       // Disk usage (%)
    1250.5,                     // Request rate (req/sec)
    0.02,                       // Error rate (%)
    125.3,                      // Latency (ms)
    0.95,                       // Cache hit rate (%)
)
```

### Recording User Behavior

```go
// Record user behavior
service.RecordUserBehavior(
    "user123",                  // user ID
    "play",                     // action
    "content456",               // content ID
    "192.168.1.1",              // client IP
    "Mozilla/5.0...",           // user agent
    "session789",               // session ID
    5000,                       // duration (ms)
    true,                       // success
    "",                         // error message
)
```

### Recording Performance Metrics

```go
// Record performance metric
service.RecordPerformanceMetric(
    "transcoder",               // service ID
    "transcode_video",          // operation
    2500.5,                     // duration (ms)
    512.0,                      // resource used (MB)
    0.4,                        // throughput (ops/sec)
    true,                       // success
    "",                         // error type
)
```

### Recording Business Metrics

```go
// Record business metric
service.RecordBusinessMetric(
    "revenue",                  // metric type
    99.99,                      // value
    "USD",                      // unit
    map[string]string{          // dimension
        "region": "us-west",
        "tier": "premium",
    },
)
```

### Getting Analytics Data

```go
// Get aggregations
aggs := service.GetAggregations("api-gateway")
for _, agg := range aggs {
    fmt.Printf("Period: %s, Events: %d, Avg Latency: %.2f ms\n",
        agg.Period, agg.EventCount, agg.AvgLatency)
}

// Get anomalies
anomalies := service.GetAnomalies("api-gateway", 10)
for _, anomaly := range anomalies {
    fmt.Printf("Anomaly: %s, Severity: %s, Deviation: %.2f\n",
        anomaly.MetricName, anomaly.Severity, anomaly.Deviation)
}

// Get dashboard data
data := service.GetDashboardData("api-gateway")
fmt.Printf("System Health: %s\n", data.SystemHealth)
```

## HTTP API

### Record Event

```bash
POST /api/v1/analytics/events
Content-Type: application/json

{
  "event_type": "upload_started",
  "service_id": "upload-service",
  "user_id": "user123",
  "metadata": {
    "file_size": 1024,
    "format": "mp4"
  },
  "tags": {
    "region": "us-west"
  }
}
```

### Record Metrics

```bash
POST /api/v1/analytics/metrics
Content-Type: application/json

{
  "service_id": "api-gateway",
  "cpu_usage": 45.5,
  "memory_usage": 62.3,
  "disk_usage": 78.1,
  "request_rate": 1250.5,
  "error_rate": 0.02,
  "latency": 125.3,
  "cache_hit_rate": 0.95
}
```

### Get Aggregations

```bash
GET /api/v1/analytics/aggregations?service_id=api-gateway
```

Response:
```json
[
  {
    "id": "agg-123",
    "timestamp": "2025-01-28T10:00:00Z",
    "service_id": "api-gateway",
    "period": "1m",
    "event_count": 1250,
    "avg_latency": 125.3,
    "p50_latency": 100.0,
    "p95_latency": 250.0,
    "p99_latency": 500.0,
    "error_count": 25,
    "error_rate": 0.02,
    "success_rate": 0.98,
    "throughput": 20.83
  }
]
```

### Get Anomalies

```bash
GET /api/v1/analytics/anomalies?service_id=api-gateway&limit=10
```

Response:
```json
[
  {
    "id": "anom-123",
    "timestamp": "2025-01-28T10:05:00Z",
    "service_id": "api-gateway",
    "metric_name": "cpu_usage",
    "current_value": 85.5,
    "expected_value": 45.0,
    "deviation": 3.2,
    "severity": "high",
    "description": "cpu_usage is 3.20 standard deviations from baseline"
  }
]
```

### Get Predictions

```bash
GET /api/v1/analytics/predictions?service_id=api-gateway&limit=10
```

Response:
```json
[
  {
    "id": "pred-123",
    "timestamp": "2025-01-28T10:05:00Z",
    "prediction_type": "cpu_usage",
    "service_id": "api-gateway",
    "predicted_value": 52.3,
    "confidence": 0.92,
    "time_horizon": "5m",
    "recommendation": "CPU usage expected to remain stable"
  }
]
```

### Get Dashboard Data

```bash
GET /api/v1/analytics/dashboard?service_id=api-gateway
```

Response:
```json
{
  "timestamp": "2025-01-28T10:05:00Z",
  "service_metrics": {
    "api-gateway": {
      "id": "metric-123",
      "timestamp": "2025-01-28T10:05:00Z",
      "service_id": "api-gateway",
      "cpu_usage": 45.5,
      "memory_usage": 62.3,
      "disk_usage": 78.1,
      "request_rate": 1250.5,
      "error_rate": 0.02,
      "latency": 125.3,
      "cache_hit_rate": 0.95
    }
  },
  "aggregations": [...],
  "anomalies": [...],
  "predictions": [...],
  "top_errors": ["connection timeout", "database error"],
  "top_users": ["user123", "user456"],
  "system_health": "healthy"
}
```

## Metrics

### System Metrics

- **CPU Usage** (%) - CPU utilization
- **Memory Usage** (%) - Memory utilization
- **Disk Usage** (%) - Disk utilization
- **Request Rate** (req/sec) - Requests per second
- **Error Rate** (%) - Percentage of failed requests
- **Latency** (ms) - Average response time
- **Cache Hit Rate** (%) - Percentage of cache hits

### Performance Metrics

- **Operation Duration** (ms) - Time to complete operation
- **Resource Used** (MB) - Memory/CPU used
- **Throughput** (ops/sec) - Operations per second
- **Success Rate** (%) - Percentage of successful operations

### Business Metrics

- **Revenue** (USD) - Revenue generated
- **User Count** - Number of active users
- **Content Views** - Number of content views
- **Conversion Rate** (%) - Conversion rate

## Anomaly Detection

### How It Works

1. **Baseline Calculation** - Calculates mean and standard deviation for each metric
2. **Deviation Detection** - Detects when values deviate from baseline
3. **Severity Classification** - Classifies anomalies by severity
4. **Baseline Update** - Updates baseline periodically

### Severity Levels

- **Low** - 2-3 standard deviations from baseline
- **Medium** - 3-5 standard deviations from baseline
- **High** - 5+ standard deviations from baseline
- **Critical** - 5+ standard deviations with error rate > 5%

### Configuration

```go
// Create detector with 2 standard deviation threshold
detector := analytics.NewAnomalyDetector(2.0)
```

## Predictions

### How It Works

1. **Data Collection** - Collects historical metrics
2. **Model Training** - Trains linear regression model
3. **Prediction** - Makes predictions for different time horizons
4. **Recommendation** - Generates actionable recommendations

### Time Horizons

- **5m** - 5 minute prediction
- **15m** - 15 minute prediction
- **1h** - 1 hour prediction

### Prediction Types

- **CPU Usage** - Predicts CPU utilization
- **Memory Usage** - Predicts memory utilization
- **Error Rate** - Predicts error rate
- **Request Rate** - Predicts request rate

## Integration

### With Monitoring

```go
// Subscribe to anomalies
service.collector.Subscribe("anomaly", func(event interface{}) error {
    if anomaly, ok := event.(*analytics.AnomalyDetection); ok {
        // Send alert to monitoring system
        sendAlert(anomaly)
    }
    return nil
})
```

### With Autoscaling

```go
// Get predictions for scaling decisions
predictions := service.GetPredictions("api-gateway", 10)
for _, pred := range predictions {
    if pred.PredictionType == "cpu_usage" && pred.PredictedValue > 80 {
        // Scale up
        scaleUp("api-gateway")
    }
}
```

### With Dashboards

```go
// Get dashboard data for visualization
data := service.GetDashboardData("api-gateway")
// Send to Grafana or custom dashboard
sendToDashboard(data)
```

## Best Practices

1. **Record Metrics Regularly** - Record metrics at consistent intervals
2. **Use Meaningful Tags** - Tag events with relevant metadata
3. **Monitor Anomalies** - Set up alerts for critical anomalies
4. **Review Predictions** - Regularly review prediction accuracy
5. **Tune Thresholds** - Adjust anomaly detection thresholds based on your needs
6. **Archive Old Data** - Archive analytics data older than 30 days

## Performance Considerations

- **Buffer Size** - Default 1000 events per buffer
- **Flush Interval** - Default 5 seconds
- **History Size** - Default 1000 data points per service
- **Aggregation Period** - 1 minute minimum

## Troubleshooting

### No Anomalies Detected

- Ensure at least 10 data points are recorded
- Check anomaly detection threshold
- Verify metrics are being recorded

### Predictions Not Available

- Ensure at least 20 data points are recorded
- Check model accuracy
- Verify metrics have sufficient variance

### High Memory Usage

- Reduce buffer size
- Reduce history size
- Archive old data

## Future Enhancements

- [ ] Advanced ML models (ARIMA, Prophet)
- [ ] Seasonal pattern detection
- [ ] Correlation analysis
- [ ] Root cause analysis
- [ ] Custom metrics
- [ ] Real-time dashboards
- [ ] Alert rules engine
- [ ] Data export (CSV, Parquet)

---

**Document Status**: Analytics Guide  
**Last Updated**: 2025-01-28  
**Version**: 1.0.0
