package optimization

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/google/uuid"
)

// MemoryMetrics represents memory performance metrics
type MemoryMetrics struct {
	ID           string
	Timestamp    time.Time
	Alloc        uint64
	TotalAlloc   uint64
	Sys          uint64
	NumGC        uint32
	PauseTotalNs uint64
	PauseNs      uint64
	HeapAlloc    uint64
	HeapSys      uint64
	HeapIdle     uint64
	HeapInuse    uint64
	HeapReleased uint64
	HeapObjects  uint64
	StackInuse   uint64
	StackSys     uint64
	MSpanInuse   uint64
	MCacheInuse  uint64
	Mallocs      uint64
	Frees        uint64
	LiveObjects  uint64
}

// CPUMetrics represents CPU performance metrics
type CPUMetrics struct {
	ID              string
	Timestamp       time.Time
	NumGoroutine    int
	NumCPU          int
	CPUUsage        float64
	ContextSwitches uint64
	Threads         int
}

// ResourceOptimizer optimizes system resources
type ResourceOptimizer struct {
	mu              sync.RWMutex
	memoryMetrics   []*MemoryMetrics
	cpuMetrics      []*CPUMetrics
	memoryTrends    []*MemoryMetrics
	cpuTrends       []*CPUMetrics
	maxMetricsSize  int
	memoryThreshold uint64
	cpuThreshold    float64
	ctx             context.Context
	cancel          context.CancelFunc
	wg              sync.WaitGroup
	lastMemStats    runtime.MemStats
	lastCPUTime     time.Time
}

// NewResourceOptimizer creates a new resource optimizer
func NewResourceOptimizer(memoryThreshold uint64, cpuThreshold float64) *ResourceOptimizer {
	ctx, cancel := context.WithCancel(context.Background())

	optimizer := &ResourceOptimizer{
		memoryMetrics:   make([]*MemoryMetrics, 0),
		cpuMetrics:      make([]*CPUMetrics, 0),
		memoryTrends:    make([]*MemoryMetrics, 0),
		cpuTrends:       make([]*CPUMetrics, 0),
		maxMetricsSize:  10000,
		memoryThreshold: memoryThreshold,
		cpuThreshold:    cpuThreshold,
		ctx:             ctx,
		cancel:          cancel,
		lastCPUTime:     time.Now(),
	}

	optimizer.start()
	return optimizer
}

// start begins the resource optimizer
func (ro *ResourceOptimizer) start() {
	ro.wg.Add(1)
	go ro.monitoringLoop()
}

// monitoringLoop periodically monitors resources
func (ro *ResourceOptimizer) monitoringLoop() {
	defer ro.wg.Done()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ro.ctx.Done():
			return
		case <-ticker.C:
			ro.recordMetrics()
		}
	}
}

// recordMetrics records current resource metrics
func (ro *ResourceOptimizer) recordMetrics() {
	ro.mu.Lock()
	defer ro.mu.Unlock()

	// Record memory metrics
	memMetric := ro.recordMemoryMetrics()
	ro.memoryMetrics = append(ro.memoryMetrics, memMetric)
	if len(ro.memoryMetrics) > ro.maxMetricsSize {
		ro.memoryMetrics = ro.memoryMetrics[1:]
	}

	// Record CPU metrics
	cpuMetric := ro.recordCPUMetrics()
	ro.cpuMetrics = append(ro.cpuMetrics, cpuMetric)
	if len(ro.cpuMetrics) > ro.maxMetricsSize {
		ro.cpuMetrics = ro.cpuMetrics[1:]
	}

	// Track trends
	ro.updateTrends(memMetric, cpuMetric)
}

// recordMemoryMetrics records memory metrics
func (ro *ResourceOptimizer) recordMemoryMetrics() *MemoryMetrics {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	metric := &MemoryMetrics{
		ID:           uuid.New().String(),
		Timestamp:    time.Now(),
		Alloc:        m.Alloc,
		TotalAlloc:   m.TotalAlloc,
		Sys:          m.Sys,
		NumGC:        m.NumGC,
		PauseTotalNs: m.PauseTotalNs,
		HeapAlloc:    m.HeapAlloc,
		HeapSys:      m.HeapSys,
		HeapIdle:     m.HeapIdle,
		HeapInuse:    m.HeapInuse,
		HeapReleased: m.HeapReleased,
		HeapObjects:  m.HeapObjects,
		StackInuse:   m.StackInuse,
		StackSys:     m.StackSys,
		MSpanInuse:   m.MSpanInuse,
		MCacheInuse:  m.MCacheInuse,
		Mallocs:      m.Mallocs,
		Frees:        m.Frees,
		LiveObjects:  m.Mallocs - m.Frees,
	}

	if metric.HeapAlloc > ro.memoryThreshold {
		metric.PauseNs = m.PauseNs[(m.NumGC+255)%256]
	}

	ro.lastMemStats = m
	return metric
}

// recordCPUMetrics records CPU metrics
func (ro *ResourceOptimizer) recordCPUMetrics() *CPUMetrics {
	metric := &CPUMetrics{
		ID:           uuid.New().String(),
		Timestamp:    time.Now(),
		NumGoroutine: runtime.NumGoroutine(),
		NumCPU:       runtime.NumCPU(),
	}

	// Calculate CPU usage
	now := time.Now()
	elapsed := now.Sub(ro.lastCPUTime).Seconds()
	if elapsed > 0 {
		metric.CPUUsage = float64(runtime.NumGoroutine()) / float64(runtime.NumCPU()) * 100
	}

	ro.lastCPUTime = now
	return metric
}

// updateTrends updates memory and CPU trends
func (ro *ResourceOptimizer) updateTrends(memMetric *MemoryMetrics, cpuMetric *CPUMetrics) {
	// Track memory trends
	if memMetric.HeapAlloc > ro.memoryThreshold {
		ro.memoryTrends = append(ro.memoryTrends, memMetric)
		if len(ro.memoryTrends) > 1000 {
			ro.memoryTrends = ro.memoryTrends[1:]
		}
	}

	// Track CPU trends
	if cpuMetric.CPUUsage > ro.cpuThreshold {
		ro.cpuTrends = append(ro.cpuTrends, cpuMetric)
		if len(ro.cpuTrends) > 1000 {
			ro.cpuTrends = ro.cpuTrends[1:]
		}
	}
}

// GetMemoryMetrics returns memory metrics
func (ro *ResourceOptimizer) GetMemoryMetrics(limit int) []*MemoryMetrics {
	ro.mu.RLock()
	defer ro.mu.RUnlock()

	if len(ro.memoryMetrics) <= limit {
		return ro.memoryMetrics
	}

	return ro.memoryMetrics[len(ro.memoryMetrics)-limit:]
}

// GetCPUMetrics returns CPU metrics
func (ro *ResourceOptimizer) GetCPUMetrics(limit int) []*CPUMetrics {
	ro.mu.RLock()
	defer ro.mu.RUnlock()

	if len(ro.cpuMetrics) <= limit {
		return ro.cpuMetrics
	}

	return ro.cpuMetrics[len(ro.cpuMetrics)-limit:]
}

// GetMemoryTrends returns memory trends
func (ro *ResourceOptimizer) GetMemoryTrends() []*MemoryMetrics {
	ro.mu.RLock()
	defer ro.mu.RUnlock()

	return ro.memoryTrends
}

// GetCPUTrends returns CPU trends
func (ro *ResourceOptimizer) GetCPUTrends() []*CPUMetrics {
	ro.mu.RLock()
	defer ro.mu.RUnlock()

	return ro.cpuTrends
}

// GetMemoryStats returns current memory statistics
func (ro *ResourceOptimizer) GetMemoryStats() map[string]interface{} {
	ro.mu.RLock()
	defer ro.mu.RUnlock()

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	stats := make(map[string]interface{})
	stats["alloc_mb"] = float64(m.Alloc) / 1024 / 1024
	stats["total_alloc_mb"] = float64(m.TotalAlloc) / 1024 / 1024
	stats["sys_mb"] = float64(m.Sys) / 1024 / 1024
	stats["num_gc"] = m.NumGC
	stats["heap_alloc_mb"] = float64(m.HeapAlloc) / 1024 / 1024
	stats["heap_sys_mb"] = float64(m.HeapSys) / 1024 / 1024
	stats["heap_idle_mb"] = float64(m.HeapIdle) / 1024 / 1024
	stats["heap_inuse_mb"] = float64(m.HeapInuse) / 1024 / 1024
	stats["heap_objects"] = m.HeapObjects
	stats["live_objects"] = m.Mallocs - m.Frees

	return stats
}

// GetCPUStats returns current CPU statistics
func (ro *ResourceOptimizer) GetCPUStats() map[string]interface{} {
	ro.mu.RLock()
	defer ro.mu.RUnlock()

	stats := make(map[string]interface{})
	stats["num_goroutine"] = runtime.NumGoroutine()
	stats["num_cpu"] = runtime.NumCPU()
	stats["cpu_usage"] = float64(runtime.NumGoroutine()) / float64(runtime.NumCPU()) * 100

	return stats
}

// GetOptimizationRecommendations returns optimization recommendations
func (ro *ResourceOptimizer) GetOptimizationRecommendations() []string {
	ro.mu.RLock()
	defer ro.mu.RUnlock()

	var recommendations []string

	// Check memory usage
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	if m.HeapAlloc > ro.memoryThreshold {
		recommendations = append(recommendations, fmt.Sprintf("Memory usage is high: %.2f MB", float64(m.HeapAlloc)/1024/1024))
	}

	// Check for memory leaks
	if len(ro.memoryTrends) > 100 {
		firstMetric := ro.memoryTrends[0]
		lastMetric := ro.memoryTrends[len(ro.memoryTrends)-1]

		if lastMetric.HeapAlloc > uint64(float64(firstMetric.HeapAlloc)*1.5) {
			recommendations = append(recommendations, "Potential memory leak detected - heap allocation increased significantly")
		}
	}

	// Check CPU usage
	cpuUsage := float64(runtime.NumGoroutine()) / float64(runtime.NumCPU()) * 100
	if cpuUsage > ro.cpuThreshold {
		recommendations = append(recommendations, fmt.Sprintf("CPU usage is high: %.2f%%", cpuUsage))
	}

	// Check goroutine count
	if runtime.NumGoroutine() > 10000 {
		recommendations = append(recommendations, fmt.Sprintf("High goroutine count: %d - consider reducing concurrency", runtime.NumGoroutine()))
	}

	// Check GC frequency
	if m.NumGC > 1000 {
		recommendations = append(recommendations, fmt.Sprintf("High GC frequency: %d collections - consider optimizing allocations", m.NumGC))
	}

	return recommendations
}

// ForceGC forces garbage collection
func (ro *ResourceOptimizer) ForceGC() {
	runtime.GC()
}

// Close closes the resource optimizer
func (ro *ResourceOptimizer) Close() error {
	ro.cancel()
	ro.wg.Wait()
	return nil
}
