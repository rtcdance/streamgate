package debug

import (
	"context"
	"sync"
	"time"
)

// Service provides debugging and profiling functionality
type Service struct {
	mu       sync.RWMutex
	debugger *Debugger
	profiler *Profiler
	ctx      context.Context
	cancel   context.CancelFunc
}

// NewService creates a new debug service
func NewService() *Service {
	ctx, cancel := context.WithCancel(context.Background())

	service := &Service{
		debugger: NewDebugger(),
		profiler: NewProfiler(),
		ctx:      ctx,
		cancel:   cancel,
	}

	return service
}

// SetBreakpoint sets a breakpoint
func (s *Service) SetBreakpoint(location, condition string) string {
	return s.debugger.SetBreakpoint(location, condition)
}

// RemoveBreakpoint removes a breakpoint
func (s *Service) RemoveBreakpoint(id string) bool {
	return s.debugger.RemoveBreakpoint(id)
}

// GetBreakpoints returns all breakpoints
func (s *Service) GetBreakpoints() []*Breakpoint {
	return s.debugger.GetBreakpoints()
}

// WatchVariable watches a variable
func (s *Service) WatchVariable(name string, value interface{}) string {
	return s.debugger.WatchVariable(name, value)
}

// UpdateWatchVariable updates a watched variable
func (s *Service) UpdateWatchVariable(id string, value interface{}) bool {
	return s.debugger.UpdateWatchVariable(id, value)
}

// GetWatchVariables returns all watched variables
func (s *Service) GetWatchVariables() []*WatchVariable {
	return s.debugger.GetWatchVariables()
}

// RecordTrace records a debug trace
func (s *Service) RecordTrace(function, message, level string) {
	s.debugger.RecordTrace(function, message, level)
}

// RecordLog records a debug log
func (s *Service) RecordLog(level, message string, context map[string]interface{}) {
	s.debugger.RecordLog(level, message, context)
}

// GetTraces returns recent traces
func (s *Service) GetTraces(limit int) []*DebugTrace {
	return s.debugger.GetTraces(limit)
}

// GetLogs returns recent logs
func (s *Service) GetLogs(limit int) []*DebugLog {
	return s.debugger.GetLogs(limit)
}

// GetLogsByLevel returns logs by level
func (s *Service) GetLogsByLevel(level string, limit int) []*DebugLog {
	return s.debugger.GetLogsByLevel(level, limit)
}

// RecordCPUProfile records a CPU profile
func (s *Service) RecordCPUProfile(duration interface{}, samples int, topFunctions []FunctionSample) {
	// Type assertion to time.Duration
	dur, ok := duration.(time.Duration)
	if !ok {
		// If not time.Duration, try to convert from other types
		dur = time.Second // default fallback
	}
	s.profiler.RecordCPUProfile(dur, samples, topFunctions)
}

// GetMemProfiles returns recent memory profiles
func (s *Service) GetMemProfiles(limit int) []*MemProfile {
	return s.profiler.GetMemProfiles(limit)
}

// GetLatestMemProfile returns the latest memory profile
func (s *Service) GetLatestMemProfile() *MemProfile {
	return s.profiler.GetLatestMemProfile()
}

// GetGoroutineProfiles returns recent goroutine profiles
func (s *Service) GetGoroutineProfiles(limit int) []*GoroutineProfile {
	return s.profiler.GetGoroutineProfiles(limit)
}

// GetLatestGoroutineProfile returns the latest goroutine profile
func (s *Service) GetLatestGoroutineProfile() *GoroutineProfile {
	return s.profiler.GetLatestGoroutineProfile()
}

// GetCPUProfiles returns recent CPU profiles
func (s *Service) GetCPUProfiles(limit int) []*CPUProfile {
	return s.profiler.GetCPUProfiles(limit)
}

// GetBlockProfiles returns recent block profiles
func (s *Service) GetBlockProfiles(limit int) []*BlockProfile {
	return s.profiler.GetBlockProfiles(limit)
}

// GetMemoryTrend returns memory usage trend
func (s *Service) GetMemoryTrend(limit int) []uint64 {
	return s.profiler.GetMemoryTrend(limit)
}

// GetGoroutineTrend returns goroutine count trend
func (s *Service) GetGoroutineTrend(limit int) []int {
	return s.profiler.GetGoroutineTrend(limit)
}

// DetectMemoryLeak detects potential memory leaks
func (s *Service) DetectMemoryLeak() bool {
	return s.profiler.DetectMemoryLeak()
}

// DetectGoroutineLeak detects potential goroutine leaks
func (s *Service) DetectGoroutineLeak() bool {
	return s.profiler.DetectGoroutineLeak()
}

// GetOptimizationRecommendations returns optimization recommendations
func (s *Service) GetOptimizationRecommendations() []string {
	return s.profiler.GetOptimizationRecommendations()
}

// ProfileNow profiles the system immediately
func (s *Service) ProfileNow() {
	s.profiler.ProfileNow()
}

// Close closes the debug service
func (s *Service) Close() error {
	s.cancel()

	if err := s.debugger.Close(); err != nil {
		return err
	}

	if err := s.profiler.Close(); err != nil {
		return err
	}

	return nil
}
