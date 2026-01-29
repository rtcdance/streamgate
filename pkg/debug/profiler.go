package debug

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Profiler provides profiling capabilities
type Profiler struct {
	mu              sync.RWMutex
	cpuProfiles     []*CPUProfile
	memProfiles     []*MemProfile
	goroutineProfiles []*GoroutineProfile
	blockProfiles   []*BlockProfile
	maxProfileSize  int
	ctx             context.Context
	cancel          context.CancelFunc
	wg              sync.WaitGroup
}

// CPUProfile represents a CPU profile
type CPUProfile struct {
	ID        string
	Timestamp time.Time
	Duration  time.Duration
	Samples   int
	TopFunctions []FunctionSample
}

// FunctionSample represents a function sample
type FunctionSample struct {
	Function string
	Count    int
	Percent  float64
}

// MemProfile represents a memory profile
type MemProfile struct {
	ID        string
	Timestamp time.Time
	Alloc     uint64
	TotalAlloc uint64
	Sys       uint64
	NumGC     uint32
	Goroutines int
}

// GoroutineProfile represents a goroutine profile
type GoroutineProfile struct {
	ID        string
	Timestamp time.Time
	Count     int
	Running   int
	Blocked   int
	Waiting   int
	Stacks    []string
}

// BlockProfile represents a block profile
type BlockProfile struct {
	ID        string
	Timestamp time.Time
	Contention time.Duration
	Count     int
	TopBlocks []BlockSample
}

// BlockSample represents a block sample
type BlockSample struct {
	Function string
	Count    int
	Contention time.Duration
}

// NewProfiler creates a new profiler
func NewProfiler() *Profiler {
	ctx, cancel := context.WithCancel(context.Background())

	p := &Profiler{
		cpuProfiles:    make([]*CPUProfile, 0),
		memProfiles:    make([]*MemProfile, 0),
		goroutineProfiles: make([]*GoroutineProfile, 0),
		blockProfiles:  make([]*BlockProfile, 0),
		maxProfileSize: 1000,
		ctx:            ctx,
		cancel:         cancel,
	}

	p.start()
	return p
}

// start begins the profiler
func (p *Profiler) start() {
	p.wg.Add(1)
	go p.profilingLoop()
}

// profilingLoop periodically profiles the system
func (p *Profiler) profilingLoop() {
	defer p.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			p.profileSystem()
		}
	}
}

// profileSystem profiles the system
func (p *Profiler) profileSystem() {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Profile memory
	p.profileMemory()

	// Profile goroutines
	p.profileGoroutines()

	// Profile blocks
	p.profileBlocks()
}

// profileMemory profiles memory usage
func (p *Profiler) profileMemory() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	profile := &MemProfile{
		ID:         uuid.New().String(),
		Timestamp:  time.Now(),
		Alloc:      m.Alloc,
		TotalAlloc: m.TotalAlloc,
		Sys:        m.Sys,
		NumGC:      m.NumGC,
		Goroutines: runtime.NumGoroutine(),
	}

	p.memProfiles = append(p.memProfiles, profile)

	// Keep profiles bounded
	if len(p.memProfiles) > p.maxProfileSize {
		p.memProfiles = p.memProfiles[1:]
	}
}

// profileGoroutines profiles goroutines
func (p *Profiler) profileGoroutines() {
	count := runtime.NumGoroutine()

	// Get goroutine stacks
	buf := make([]byte, 1024*1024)
	n := runtime.Stack(buf, true)
	stacks := string(buf[:n])

	profile := &GoroutineProfile{
		ID:        uuid.New().String(),
		Timestamp: time.Now(),
		Count:     count,
		Running:   count, // Simplified - would need more analysis
		Blocked:   0,
		Waiting:   0,
		Stacks:    []string{stacks},
	}

	p.goroutineProfiles = append(p.goroutineProfiles, profile)

	// Keep profiles bounded
	if len(p.goroutineProfiles) > p.maxProfileSize {
		p.goroutineProfiles = p.goroutineProfiles[1:]
	}
}

// profileBlocks profiles block contention
func (p *Profiler) profileBlocks() {
	profile := &BlockProfile{
		ID:         uuid.New().String(),
		Timestamp:  time.Now(),
		Contention: 0,
		Count:      0,
		TopBlocks:  []BlockSample{},
	}

	p.blockProfiles = append(p.blockProfiles, profile)

	// Keep profiles bounded
	if len(p.blockProfiles) > p.maxProfileSize {
		p.blockProfiles = p.blockProfiles[1:]
	}
}

// RecordCPUProfile records a CPU profile
func (p *Profiler) RecordCPUProfile(duration time.Duration, samples int, topFunctions []FunctionSample) {
	p.mu.Lock()
	defer p.mu.Unlock()

	profile := &CPUProfile{
		ID:           uuid.New().String(),
		Timestamp:    time.Now(),
		Duration:     duration,
		Samples:      samples,
		TopFunctions: topFunctions,
	}

	p.cpuProfiles = append(p.cpuProfiles, profile)

	// Keep profiles bounded
	if len(p.cpuProfiles) > p.maxProfileSize {
		p.cpuProfiles = p.cpuProfiles[1:]
	}
}

// GetMemProfiles returns recent memory profiles
func (p *Profiler) GetMemProfiles(limit int) []*MemProfile {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if len(p.memProfiles) <= limit {
		return p.memProfiles
	}

	return p.memProfiles[len(p.memProfiles)-limit:]
}

// GetLatestMemProfile returns the latest memory profile
func (p *Profiler) GetLatestMemProfile() *MemProfile {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if len(p.memProfiles) == 0 {
		return nil
	}

	return p.memProfiles[len(p.memProfiles)-1]
}

// GetGoroutineProfiles returns recent goroutine profiles
func (p *Profiler) GetGoroutineProfiles(limit int) []*GoroutineProfile {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if len(p.goroutineProfiles) <= limit {
		return p.goroutineProfiles
	}

	return p.goroutineProfiles[len(p.goroutineProfiles)-limit:]
}

// GetLatestGoroutineProfile returns the latest goroutine profile
func (p *Profiler) GetLatestGoroutineProfile() *GoroutineProfile {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if len(p.goroutineProfiles) == 0 {
		return nil
	}

	return p.goroutineProfiles[len(p.goroutineProfiles)-1]
}

// GetCPUProfiles returns recent CPU profiles
func (p *Profiler) GetCPUProfiles(limit int) []*CPUProfile {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if len(p.cpuProfiles) <= limit {
		return p.cpuProfiles
	}

	return p.cpuProfiles[len(p.cpuProfiles)-limit:]
}

// GetBlockProfiles returns recent block profiles
func (p *Profiler) GetBlockProfiles(limit int) []*BlockProfile {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if len(p.blockProfiles) <= limit {
		return p.blockProfiles
	}

	return p.blockProfiles[len(p.blockProfiles)-limit:]
}

// GetMemoryTrend returns memory usage trend
func (p *Profiler) GetMemoryTrend(limit int) []uint64 {
	p.mu.RLock()
	defer p.mu.RUnlock()

	profiles := p.GetMemProfiles(limit)
	var trend []uint64

	for _, profile := range profiles {
		trend = append(trend, profile.Alloc)
	}

	return trend
}

// GetGoroutineTrend returns goroutine count trend
func (p *Profiler) GetGoroutineTrend(limit int) []int {
	p.mu.RLock()
	defer p.mu.RUnlock()

	profiles := p.GetGoroutineProfiles(limit)
	var trend []int

	for _, profile := range profiles {
		trend = append(trend, profile.Count)
	}

	return trend
}

// DetectMemoryLeak detects potential memory leaks
func (p *Profiler) DetectMemoryLeak() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if len(p.memProfiles) < 10 {
		return false
	}

	// Check if memory is consistently increasing
	increasing := 0
	for i := 1; i < len(p.memProfiles); i++ {
		if p.memProfiles[i].Alloc > p.memProfiles[i-1].Alloc {
			increasing++
		}
	}

	// If memory increased in 80% of samples, likely a leak
	return float64(increasing)/float64(len(p.memProfiles)-1) > 0.8
}

// DetectGoroutineLeak detects potential goroutine leaks
func (p *Profiler) DetectGoroutineLeak() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if len(p.goroutineProfiles) < 10 {
		return false
	}

	// Check if goroutine count is consistently increasing
	increasing := 0
	for i := 1; i < len(p.goroutineProfiles); i++ {
		if p.goroutineProfiles[i].Count > p.goroutineProfiles[i-1].Count {
			increasing++
		}
	}

	// If goroutines increased in 80% of samples, likely a leak
	return float64(increasing)/float64(len(p.goroutineProfiles)-1) > 0.8
}

// GetOptimizationRecommendations returns optimization recommendations
func (p *Profiler) GetOptimizationRecommendations() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var recommendations []string

	// Check memory
	if p.DetectMemoryLeak() {
		recommendations = append(recommendations, "Potential memory leak detected - review memory allocation patterns")
	}

	// Check goroutines
	if p.DetectGoroutineLeak() {
		recommendations = append(recommendations, "Potential goroutine leak detected - ensure goroutines are properly cleaned up")
	}

	// Check memory usage
	if latest := p.GetLatestMemProfile(); latest != nil {
		if latest.Alloc > 1024*1024*1024 { // 1GB
			recommendations = append(recommendations, fmt.Sprintf("High memory usage: %d MB - consider optimization", latest.Alloc/1024/1024))
		}
	}

	// Check goroutine count
	if latest := p.GetLatestGoroutineProfile(); latest != nil {
		if latest.Count > 10000 {
			recommendations = append(recommendations, fmt.Sprintf("High goroutine count: %d - consider pooling", latest.Count))
		}
	}

	return recommendations
}

// Close closes the profiler
func (p *Profiler) Close() error {
	p.cancel()
	p.wg.Wait()
	return nil
}
