package debug

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Debugger provides debugging capabilities
type Debugger struct {
	mu             sync.RWMutex
	breakpoints    map[string]*Breakpoint
	watchVariables map[string]*WatchVariable
	traces         []*DebugTrace
	logs           []*DebugLog
	maxTraceSize   int
	maxLogSize     int
	ctx            context.Context
	cancel         context.CancelFunc
	wg             sync.WaitGroup
}

// Breakpoint represents a breakpoint
type Breakpoint struct {
	ID        string
	Location  string
	Condition string
	HitCount  int
	Enabled   bool
	Created   time.Time
}

// WatchVariable represents a watched variable
type WatchVariable struct {
	ID      string
	Name    string
	Value   interface{}
	Type    string
	Updated time.Time
	History []interface{}
}

// DebugTrace represents a debug trace
type DebugTrace struct {
	ID        string
	Timestamp time.Time
	Function  string
	File      string
	Line      int
	Message   string
	Level     string // debug, info, warn, error
	Stack     []string
}

// DebugLog represents a debug log
type DebugLog struct {
	ID        string
	Timestamp time.Time
	Level     string // debug, info, warn, error
	Message   string
	Context   map[string]interface{}
}

// NewDebugger creates a new debugger
func NewDebugger() *Debugger {
	ctx, cancel := context.WithCancel(context.Background())

	d := &Debugger{
		breakpoints:    make(map[string]*Breakpoint),
		watchVariables: make(map[string]*WatchVariable),
		traces:         make([]*DebugTrace, 0),
		logs:           make([]*DebugLog, 0),
		maxTraceSize:   10000,
		maxLogSize:     10000,
		ctx:            ctx,
		cancel:         cancel,
	}

	d.start()
	return d
}

// start begins the debugger
func (d *Debugger) start() {
	d.wg.Add(1)
	go d.cleanupLoop()
}

// cleanupLoop periodically cleans up old traces and logs
func (d *Debugger) cleanupLoop() {
	defer d.wg.Done()

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			d.cleanup()
		}
	}
}

// SetBreakpoint sets a breakpoint
func (d *Debugger) SetBreakpoint(location, condition string) string {
	d.mu.Lock()
	defer d.mu.Unlock()

	id := uuid.New().String()
	d.breakpoints[id] = &Breakpoint{
		ID:        id,
		Location:  location,
		Condition: condition,
		Enabled:   true,
		Created:   time.Now(),
	}

	return id
}

// RemoveBreakpoint removes a breakpoint
func (d *Debugger) RemoveBreakpoint(id string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	if _, ok := d.breakpoints[id]; ok {
		delete(d.breakpoints, id)
		return true
	}

	return false
}

// GetBreakpoints returns all breakpoints
func (d *Debugger) GetBreakpoints() []*Breakpoint {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var result []*Breakpoint
	for _, bp := range d.breakpoints {
		result = append(result, bp)
	}

	return result
}

// WatchVariable watches a variable
func (d *Debugger) WatchVariable(name string, value interface{}) string {
	d.mu.Lock()
	defer d.mu.Unlock()

	id := uuid.New().String()
	d.watchVariables[id] = &WatchVariable{
		ID:      id,
		Name:    name,
		Value:   value,
		Type:    fmt.Sprintf("%T", value),
		Updated: time.Now(),
		History: []interface{}{value},
	}

	return id
}

// UpdateWatchVariable updates a watched variable
func (d *Debugger) UpdateWatchVariable(id string, value interface{}) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	if wv, ok := d.watchVariables[id]; ok {
		wv.Value = value
		wv.Updated = time.Now()
		wv.History = append(wv.History, value)

		// Keep history bounded
		if len(wv.History) > 100 {
			wv.History = wv.History[1:]
		}

		return true
	}

	return false
}

// GetWatchVariables returns all watched variables
func (d *Debugger) GetWatchVariables() []*WatchVariable {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var result []*WatchVariable
	for _, wv := range d.watchVariables {
		result = append(result, wv)
	}

	return result
}

// RecordTrace records a debug trace
func (d *Debugger) RecordTrace(function, message, level string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	pc, file, line, _ := runtime.Caller(1)
	fn := runtime.FuncForPC(pc)

	stack := d.getStack()

	trace := &DebugTrace{
		ID:        uuid.New().String(),
		Timestamp: time.Now(),
		Function:  function,
		File:      file,
		Line:      line,
		Message:   message,
		Level:     level,
		Stack:     stack,
	}

	d.traces = append(d.traces, trace)

	// Keep traces bounded
	if len(d.traces) > d.maxTraceSize {
		d.traces = d.traces[1:]
	}
}

// RecordLog records a debug log
func (d *Debugger) RecordLog(level, message string, context map[string]interface{}) {
	d.mu.Lock()
	defer d.mu.Unlock()

	log := &DebugLog{
		ID:        uuid.New().String(),
		Timestamp: time.Now(),
		Level:     level,
		Message:   message,
		Context:   context,
	}

	d.logs = append(d.logs, log)

	// Keep logs bounded
	if len(d.logs) > d.maxLogSize {
		d.logs = d.logs[1:]
	}
}

// GetTraces returns recent traces
func (d *Debugger) GetTraces(limit int) []*DebugTrace {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if len(d.traces) <= limit {
		return d.traces
	}

	return d.traces[len(d.traces)-limit:]
}

// GetLogs returns recent logs
func (d *Debugger) GetLogs(limit int) []*DebugLog {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if len(d.logs) <= limit {
		return d.logs
	}

	return d.logs[len(d.logs)-limit:]
}

// GetLogsByLevel returns logs by level
func (d *Debugger) GetLogsByLevel(level string, limit int) []*DebugLog {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var result []*DebugLog
	for i := len(d.logs) - 1; i >= 0 && len(result) < limit; i-- {
		if d.logs[i].Level == level {
			result = append(result, d.logs[i])
		}
	}

	return result
}

// getStack returns the current call stack
func (d *Debugger) getStack() []string {
	var stack []string
	pcs := make([]uintptr, 10)
	n := runtime.Callers(3, pcs)

	for _, pc := range pcs[:n] {
		fn := runtime.FuncForPC(pc)
		if fn != nil {
			stack = append(stack, fn.Name())
		}
	}

	return stack
}

// cleanup cleans up old traces and logs
func (d *Debugger) cleanup() {
	d.mu.Lock()
	defer d.mu.Unlock()

	cutoff := time.Now().Add(-1 * time.Hour)

	// Clean up traces
	newTraces := make([]*DebugTrace, 0)
	for _, trace := range d.traces {
		if trace.Timestamp.After(cutoff) {
			newTraces = append(newTraces, trace)
		}
	}
	d.traces = newTraces

	// Clean up logs
	newLogs := make([]*DebugLog, 0)
	for _, log := range d.logs {
		if log.Timestamp.After(cutoff) {
			newLogs = append(newLogs, log)
		}
	}
	d.logs = newLogs
}

// Close closes the debugger
func (d *Debugger) Close() error {
	d.cancel()
	d.wg.Wait()
	return nil
}
