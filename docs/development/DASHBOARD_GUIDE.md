# StreamGate Dashboard Guide

**Date**: 2025-01-28  
**Version**: 1.0.0  
**Status**: Complete

## Table of Contents

1. [Overview](#overview)
2. [Dashboard Features](#dashboard-features)
3. [Metrics](#metrics)
4. [Alerts](#alerts)
5. [Reports](#reports)
6. [API Reference](#api-reference)
7. [Best Practices](#best-practices)
8. [Troubleshooting](#troubleshooting)

## Overview

The StreamGate Dashboard provides real-time performance monitoring, historical trends, alert management, and comprehensive reporting capabilities. It enables operators to monitor system health, identify issues, and make data-driven decisions.

### Key Capabilities

- **Real-time Monitoring**: Live metrics and status updates
- **Historical Trends**: Track performance over time
- **Alert Management**: Create, manage, and resolve alerts
- **Comprehensive Reports**: Generate performance reports
- **Status Dashboard**: Overall system health overview

## Dashboard Features

### Real-time Metrics

Monitor key performance indicators in real-time:

```go
import "github.com/yourusername/streamgate/pkg/dashboard"

// Create dashboard service
service := dashboard.NewService()

// Record a metric
service.RecordMetric("memory_usage_mb", 250.0, "MB")
service.RecordMetric("cpu_usage_percent", 45.0, "%")
service.RecordMetric("cache_hit_rate", 95.5, "%")

// Get current metrics
metrics := service.GetMetrics()
for name, metric := range metrics {
    fmt.Printf("%s: %v %s (Status: %s)\n", 
        name, metric.Value, metric.Unit, metric.Status)
}
```

### Alert Management

Create and manage alerts:

```go
// Create an alert
service.CreateAlert(
    "High Memory Usage",
    "Memory usage exceeded 500MB",
    "critical",
)

// Get current alerts
alerts := service.GetAlerts()
for _, alert := range alerts {
    fmt.Printf("Alert: %s - %s (Severity: %s)\n",
        alert.Title, alert.Message, alert.Severity)
}

// Resolve an alert
service.ResolveAlert(alertID)
```

### Performance Reports

Generate comprehensive reports:

```go
// Get latest report
report := service.GetLatestReport()
if report != nil {
    fmt.Printf("Report Period: %s\n", report.Period)
    fmt.Printf("Summary: %s\n", report.Summary)
    fmt.Printf("Recommendations:\n")
    for _, rec := range report.Recommendations {
        fmt.Printf("  - %s\n", rec)
    }
}

// Get historical reports
reports := service.GetReports(10)
```

### Dashboard Status

Get overall system status:

```go
// Get dashboard status
status := service.GetDashboardStatus()
fmt.Printf("Total Metrics: %v\n", status["total_metrics"])
fmt.Printf("Critical: %v\n", status["critical_metrics"])
fmt.Printf("Warning: %v\n", status["warning_metrics"])
fmt.Printf("Healthy: %v\n", status["healthy_metrics"])
fmt.Printf("Overall Status: %v\n", status["overall_status"])
```

## Metrics

### Supported Metrics

| Metric | Unit | Threshold | Status |
|--------|------|-----------|--------|
| memory_usage_mb | MB | 500 | Critical if > 500 |
| cpu_usage_percent | % | 80 | Critical if > 80 |
| cache_hit_rate | % | 90 | Warning if < 90 |
| api_latency_ms | ms | 100 | Critical if > 100 |
| goroutine_count | count | 10000 | Warning if > 10000 |
| gc_frequency | count | 1000 | Warning if > 1000 |

### Metric Status

Metrics have three status levels:

- **Healthy**: Within normal operating parameters
- **Warning**: Approaching threshold, attention needed
- **Critical**: Exceeds threshold, immediate action required

### Metric History

Track metric changes over time:

```go
// Get metric history
history := service.GetMetricHistory("memory_usage_mb", 100)

// Analyze trends
if len(history) > 1 {
    first := history[0].Value
    last := history[len(history)-1].Value
    trend := last - first
    
    if trend > 0 {
        fmt.Printf("Memory usage increasing: %v MB\n", trend)
    }
}
```

## Alerts

### Alert Severity Levels

- **Info**: Informational alerts
- **Warning**: Warning alerts requiring attention
- **Critical**: Critical alerts requiring immediate action

### Alert Lifecycle

1. **Creation**: Alert is created when threshold is exceeded
2. **Active**: Alert is active and visible in dashboard
3. **Resolution**: Alert is manually or automatically resolved
4. **History**: Resolved alerts are kept in history

### Alert Management

```go
// Create alert
service.CreateAlert(
    "High CPU Usage",
    "CPU usage exceeded 80%",
    "critical",
)

// Get active alerts
alerts := service.GetAlerts()

// Resolve alert
service.ResolveAlert(alertID)

// Get alert history
history := service.GetAlertHistory(100)
```

## Reports

### Report Generation

Reports are automatically generated every 5 minutes and include:

- Current metrics snapshot
- Active alerts
- Optimization recommendations
- System summary

### Report Contents

```go
type DashboardReport struct {
    ID              string
    Timestamp       time.Time
    Period          string
    Metrics         []*DashboardMetric
    Alerts          []*DashboardAlert
    Recommendations []string
    Summary         string
}
```

### Report Access

```go
// Get latest report
latest := service.GetLatestReport()

// Get historical reports
reports := service.GetReports(10)

// Analyze report
for _, report := range reports {
    fmt.Printf("Report %s: %s\n", report.ID, report.Summary)
}
```

## API Reference

### HTTP Endpoints

#### Get Metrics
```
GET /dashboard/metrics
```

Returns all current metrics.

Response:
```json
{
  "memory_usage_mb": {
    "value": 250.0,
    "unit": "MB",
    "status": "healthy"
  }
}
```

#### Get Alerts
```
GET /dashboard/alerts
```

Returns all active alerts.

Response:
```json
[
  {
    "id": "uuid",
    "title": "High Memory",
    "message": "Memory exceeded 500MB",
    "severity": "critical",
    "resolved": false
  }
]
```

#### Get Metric History
```
GET /dashboard/metrics/history?name=memory_usage_mb&limit=100
```

Returns metric history for specified metric.

#### Get Reports
```
GET /dashboard/reports?limit=10
```

Returns performance reports.

#### Get Dashboard Status
```
GET /dashboard/status
```

Returns overall dashboard status.

Response:
```json
{
  "total_metrics": 5,
  "critical_metrics": 0,
  "warning_metrics": 1,
  "healthy_metrics": 4,
  "overall_status": "warning"
}
```

#### Record Metric
```
POST /dashboard/metrics/record
```

Records a new metric.

Request:
```json
{
  "name": "memory_usage_mb",
  "value": 250.0,
  "unit": "MB"
}
```

#### Create Alert
```
POST /dashboard/alerts/create
```

Creates a new alert.

Request:
```json
{
  "title": "High Memory",
  "message": "Memory exceeded 500MB",
  "severity": "critical"
}
```

#### Resolve Alert
```
POST /dashboard/alerts/resolve?alert_id=uuid
```

Resolves an alert.

## Best Practices

### 1. Monitor Key Metrics

- Memory usage
- CPU usage
- Cache hit rate
- API latency
- Goroutine count
- GC frequency

### 2. Set Appropriate Thresholds

- Memory: 500MB
- CPU: 80%
- Cache hit rate: 90%
- API latency: 100ms
- Goroutines: 10,000
- GC frequency: 1,000

### 3. Respond to Alerts

- Critical alerts: Immediate action
- Warning alerts: Investigation within 1 hour
- Info alerts: Log and monitor

### 4. Review Reports

- Daily: Check daily reports
- Weekly: Analyze weekly trends
- Monthly: Review monthly performance

### 5. Optimize Based on Recommendations

- Implement recommended optimizations
- Monitor impact of changes
- Iterate based on results

## Troubleshooting

### High Memory Usage

**Symptoms**: Memory usage exceeds 500MB

**Diagnosis**:
1. Check for memory leaks
2. Check for large allocations
3. Check for inefficient caching

**Solutions**:
1. Optimize allocations
2. Use object pooling
3. Implement memory limits
4. Force garbage collection

### High CPU Usage

**Symptoms**: CPU usage exceeds 80%

**Diagnosis**:
1. Check goroutine count
2. Check for busy loops
3. Check for inefficient algorithms

**Solutions**:
1. Reduce concurrency
2. Optimize algorithms
3. Use caching
4. Batch process data

### Missing Metrics

**Symptoms**: Metrics not appearing in dashboard

**Diagnosis**:
1. Check metric recording
2. Check metric names
3. Check dashboard service

**Solutions**:
1. Verify metric recording code
2. Check metric names match
3. Restart dashboard service

### Alert Storms

**Symptoms**: Too many alerts being generated

**Diagnosis**:
1. Check alert thresholds
2. Check metric volatility
3. Check alert deduplication

**Solutions**:
1. Adjust thresholds
2. Implement hysteresis
3. Enable alert deduplication
4. Implement alert grouping

## Performance Targets

| Metric | Target | Threshold |
|--------|--------|-----------|
| Dashboard Latency | < 100ms | > 500ms |
| Metric Collection | < 1s | > 5s |
| Alert Generation | < 5s | > 30s |
| Report Generation | < 30s | > 120s |

## Conclusion

The StreamGate Dashboard provides comprehensive performance monitoring and reporting capabilities. By following best practices and responding to alerts, you can maintain optimal system performance and reliability.

---

**Document Status**: Complete  
**Last Updated**: 2025-01-28  
**Version**: 1.0.0

</content>
