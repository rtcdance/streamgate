package analytics

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// EventCollector collects analytics events from services
type EventCollector struct {
	mu              sync.RWMutex
	events          chan *AnalyticsEvent
	metrics         chan *MetricsSnapshot
	behaviors       chan *UserBehavior
	perfMetrics     chan *PerformanceMetric
	businessMetrics chan *BusinessMetric
	bufferSize      int
	flushInterval   time.Duration
	eventBuffer     []*AnalyticsEvent
	metricsBuffer   []*MetricsSnapshot
	behaviorBuffer  []*UserBehavior
	perfBuffer      []*PerformanceMetric
	businessBuffer  []*BusinessMetric
	subscribers     map[string][]EventHandler
	ctx             context.Context
	cancel          context.CancelFunc
	wg              sync.WaitGroup
}

// EventHandler is a function that handles analytics events
type EventHandler func(event interface{}) error

// NewEventCollector creates a new event collector
func NewEventCollector(bufferSize int, flushInterval time.Duration) *EventCollector {
	ctx, cancel := context.WithCancel(context.Background())

	ec := &EventCollector{
		events:          make(chan *AnalyticsEvent, bufferSize),
		metrics:         make(chan *MetricsSnapshot, bufferSize),
		behaviors:       make(chan *UserBehavior, bufferSize),
		perfMetrics:     make(chan *PerformanceMetric, bufferSize),
		businessMetrics: make(chan *BusinessMetric, bufferSize),
		bufferSize:      bufferSize,
		flushInterval:   flushInterval,
		eventBuffer:     make([]*AnalyticsEvent, 0, bufferSize),
		metricsBuffer:   make([]*MetricsSnapshot, 0, bufferSize),
		behaviorBuffer:  make([]*UserBehavior, 0, bufferSize),
		perfBuffer:      make([]*PerformanceMetric, 0, bufferSize),
		businessBuffer:  make([]*BusinessMetric, 0, bufferSize),
		subscribers:     make(map[string][]EventHandler),
		ctx:             ctx,
		cancel:          cancel,
	}

	ec.start()
	return ec
}

// start begins the collection process
func (ec *EventCollector) start() {
	ec.wg.Add(1)
	go ec.collectLoop()
}

// collectLoop processes incoming events
func (ec *EventCollector) collectLoop() {
	defer ec.wg.Done()

	ticker := time.NewTicker(ec.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ec.ctx.Done():
			ec.flushAll()
			return

		case event := <-ec.events:
			if event != nil {
				ec.mu.Lock()
				ec.eventBuffer = append(ec.eventBuffer, event)
				if len(ec.eventBuffer) >= ec.bufferSize {
					ec.flushEvents()
				}
				ec.mu.Unlock()
			}

		case metric := <-ec.metrics:
			if metric != nil {
				ec.mu.Lock()
				ec.metricsBuffer = append(ec.metricsBuffer, metric)
				if len(ec.metricsBuffer) >= ec.bufferSize {
					ec.flushMetrics()
				}
				ec.mu.Unlock()
			}

		case behavior := <-ec.behaviors:
			if behavior != nil {
				ec.mu.Lock()
				ec.behaviorBuffer = append(ec.behaviorBuffer, behavior)
				if len(ec.behaviorBuffer) >= ec.bufferSize {
					ec.flushBehaviors()
				}
				ec.mu.Unlock()
			}

		case perfMetric := <-ec.perfMetrics:
			if perfMetric != nil {
				ec.mu.Lock()
				ec.perfBuffer = append(ec.perfBuffer, perfMetric)
				if len(ec.perfBuffer) >= ec.bufferSize {
					ec.flushPerfMetrics()
				}
				ec.mu.Unlock()
			}

		case businessMetric := <-ec.businessMetrics:
			if businessMetric != nil {
				ec.mu.Lock()
				ec.businessBuffer = append(ec.businessBuffer, businessMetric)
				if len(ec.businessBuffer) >= ec.bufferSize {
					ec.flushBusinessMetrics()
				}
				ec.mu.Unlock()
			}

		case <-ticker.C:
			ec.mu.Lock()
			ec.flushAll()
			ec.mu.Unlock()
		}
	}
}

// RecordEvent records an analytics event
func (ec *EventCollector) RecordEvent(eventType, serviceID, userID string, metadata map[string]interface{}, tags map[string]string) {
	event := &AnalyticsEvent{
		ID:        uuid.New().String(),
		Timestamp: time.Now(),
		EventType: eventType,
		ServiceID: serviceID,
		UserID:    userID,
		Metadata:  metadata,
		Tags:      tags,
	}

	select {
	case ec.events <- event:
	case <-ec.ctx.Done():
	}

	ec.notifySubscribers("event", event)
}

// RecordMetrics records system metrics
func (ec *EventCollector) RecordMetrics(serviceID string, cpu, memory, disk, requestRate, errorRate, latency, cacheHitRate float64) {
	metric := &MetricsSnapshot{
		ID:           uuid.New().String(),
		Timestamp:    time.Now(),
		ServiceID:    serviceID,
		CPUUsage:     cpu,
		MemoryUsage:  memory,
		DiskUsage:    disk,
		RequestRate:  requestRate,
		ErrorRate:    errorRate,
		Latency:      latency,
		CacheHitRate: cacheHitRate,
	}

	select {
	case ec.metrics <- metric:
	case <-ec.ctx.Done():
	}

	ec.notifySubscribers("metrics", metric)
}

// RecordUserBehavior records user behavior
func (ec *EventCollector) RecordUserBehavior(userID, action, contentID, clientIP, userAgent, sessionID string, duration int64, success bool, errorMsg string) {
	behavior := &UserBehavior{
		ID:           uuid.New().String(),
		Timestamp:    time.Now(),
		UserID:       userID,
		Action:       action,
		ContentID:    contentID,
		Duration:     duration,
		Success:      success,
		ErrorMessage: errorMsg,
		ClientIP:     clientIP,
		UserAgent:    userAgent,
		SessionID:    sessionID,
	}

	select {
	case ec.behaviors <- behavior:
	case <-ec.ctx.Done():
	}

	ec.notifySubscribers("behavior", behavior)
}

// RecordPerformanceMetric records performance metrics
func (ec *EventCollector) RecordPerformanceMetric(serviceID, operation string, duration, resourceUsed, throughput float64, success bool, errorType string) {
	perfMetric := &PerformanceMetric{
		ID:           uuid.New().String(),
		Timestamp:    time.Now(),
		ServiceID:    serviceID,
		Operation:    operation,
		Duration:     duration,
		Success:      success,
		ErrorType:    errorType,
		ResourceUsed: resourceUsed,
		Throughput:   throughput,
	}

	select {
	case ec.perfMetrics <- perfMetric:
	case <-ec.ctx.Done():
	}

	ec.notifySubscribers("performance", perfMetric)
}

// RecordBusinessMetric records business metrics
func (ec *EventCollector) RecordBusinessMetric(metricType string, value float64, unit string, dimension map[string]string) {
	businessMetric := &BusinessMetric{
		ID:         uuid.New().String(),
		Timestamp:  time.Now(),
		MetricType: metricType,
		Value:      value,
		Unit:       unit,
		Dimension:  dimension,
	}

	select {
	case ec.businessMetrics <- businessMetric:
	case <-ec.ctx.Done():
	}

	ec.notifySubscribers("business", businessMetric)
}

// Subscribe subscribes to analytics events
func (ec *EventCollector) Subscribe(eventType string, handler EventHandler) {
	ec.mu.Lock()
	defer ec.mu.Unlock()

	ec.subscribers[eventType] = append(ec.subscribers[eventType], handler)
}

// notifySubscribers notifies all subscribers of an event
func (ec *EventCollector) notifySubscribers(eventType string, event interface{}) {
	ec.mu.RLock()
	handlers := ec.subscribers[eventType]
	ec.mu.RUnlock()

	for _, handler := range handlers {
		go func(h EventHandler) {
			if err := h(event); err != nil {
				fmt.Printf("error notifying subscriber: %v\n", err)
			}
		}(handler)
	}
}

// notifySubscribersSync notifies all subscribers synchronously
func (ec *EventCollector) notifySubscribersSync(eventType string, event interface{}) {
	ec.mu.RLock()
	handlers := ec.subscribers[eventType]
	ec.mu.RUnlock()

	for _, handler := range handlers {
		if err := handler(event); err != nil {
			fmt.Printf("error notifying subscriber: %v\n", err)
		}
	}
}

// flushEvents flushes event buffer
func (ec *EventCollector) flushEvents() {
	if len(ec.eventBuffer) > 0 {
		ec.eventBuffer = ec.eventBuffer[:0]
	}
}

// flushMetrics flushes metrics buffer
func (ec *EventCollector) flushMetrics() {
	if len(ec.metricsBuffer) > 0 {
		ec.metricsBuffer = ec.metricsBuffer[:0]
	}
}

// flushBehaviors flushes behavior buffer
func (ec *EventCollector) flushBehaviors() {
	if len(ec.behaviorBuffer) > 0 {
		ec.behaviorBuffer = ec.behaviorBuffer[:0]
	}
}

// flushPerfMetrics flushes performance metrics buffer
func (ec *EventCollector) flushPerfMetrics() {
	if len(ec.perfBuffer) > 0 {
		ec.perfBuffer = ec.perfBuffer[:0]
	}
}

// flushBusinessMetrics flushes business metrics buffer
func (ec *EventCollector) flushBusinessMetrics() {
	if len(ec.businessBuffer) > 0 {
		ec.businessBuffer = ec.businessBuffer[:0]
	}
}

// flushAll flushes all buffers
func (ec *EventCollector) flushAll() {
	ec.flushEvents()
	ec.flushMetrics()
	ec.flushBehaviors()
	ec.flushPerfMetrics()
	ec.flushBusinessMetrics()
}

// FlushNow waits for all buffered events to be processed
func (ec *EventCollector) FlushNow() {
	ec.mu.Lock()
	ec.flushAll()
	ec.mu.Unlock()
}

// Close closes the event collector
func (ec *EventCollector) Close() error {
	ec.cancel()
	ec.wg.Wait()

	close(ec.events)
	close(ec.metrics)
	close(ec.behaviors)
	close(ec.perfMetrics)
	close(ec.businessMetrics)

	return nil
}
