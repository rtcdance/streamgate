package api

import (
	"container/heap"
	"sync"
	"time"
)

const (
	defaultMaxBuckets   = 100000
	defaultCleanupEvery = 5 * time.Minute
	defaultBucketMaxAge = 30 * time.Minute
)

type bucket struct {
	tokens     float64
	maxTokens  float64
	refill     float64
	lastRefill time.Time
	lastAccess time.Time
	index      int
}

func (b *bucket) refillTokens() {
	now := time.Now()
	elapsed := now.Sub(b.lastRefill).Seconds()
	b.tokens += elapsed * b.refill
	if b.tokens > b.maxTokens {
		b.tokens = b.maxTokens
	}
	b.lastRefill = now
}

type bucketEntry struct {
	ip         string
	lastAccess time.Time
}

type bucketHeap []*bucketEntry

func (h bucketHeap) Len() int           { return len(h) }
func (h bucketHeap) Less(i, j int) bool { return h[i].lastAccess.Before(h[j].lastAccess) }
func (h bucketHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
}
func (h *bucketHeap) Push(x interface{}) {
	item := x.(*bucketEntry)
	*h = append(*h, item)
}
func (h *bucketHeap) Pop() interface{} {
	old := *h
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	*h = old[:n-1]
	return item
}

type RateLimiter struct {
	stopOnce sync.Once
	limit      float64
	burst      int
	buckets    sync.Map
	mu         sync.Mutex
	maxBuckets int
	bucketHeap *bucketHeap
	stopCh     chan struct{}
	cleanupMu  sync.Mutex
}

func NewRateLimiter(limit int) *RateLimiter {
	if limit <= 0 {
		limit = 1
	}
	rl := &RateLimiter{
		limit:      float64(limit),
		burst:      limit,
		maxBuckets: defaultMaxBuckets,
		bucketHeap: &bucketHeap{},
		stopCh:     make(chan struct{}),
	}
	heap.Init(rl.bucketHeap)
	go rl.cleanupLoop()
	return rl
}

func (r *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(defaultCleanupEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			r.cleanup()
		case <-r.stopCh:
			return
		}
	}
}

func (r *RateLimiter) cleanup() {
	r.cleanupMu.Lock()
	defer r.cleanupMu.Unlock()

	now := time.Now()
	cutoff := now.Add(-defaultBucketMaxAge)
	removed := 0

	for r.bucketHeap.Len() > 0 {
		oldest := (*r.bucketHeap)[0]
		if oldest.lastAccess.After(cutoff) {
			break
		}
		heap.Pop(r.bucketHeap)
		r.buckets.Delete(oldest.ip)
		removed++
	}

	if removed > 0 {
		r.buckets.Range(func(key, _ interface{}) bool {
			if r.bucketHeap.Len() >= r.maxBuckets {
				return false
			}
			return true
		})
	}
}

func (r *RateLimiter) Allow(clientIP string) bool {
	now := time.Now()

	val, loaded := r.buckets.LoadOrStore(clientIP, &bucket{
		tokens:     float64(r.burst) - 1,
		maxTokens:  float64(r.burst),
		refill:     r.limit,
		lastRefill: now,
		lastAccess: now,
	})

	b := val.(*bucket)
	

	if !loaded {
		r.cleanupMu.Lock()
		heap.Push(r.bucketHeap, &bucketEntry{ip: clientIP, lastAccess: now})
		r.cleanupMu.Unlock()
		return true
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	b.refillTokens()
	b.lastAccess = now

	if b.tokens < 1 {
		return false
	}

	b.tokens--
	return true
}

func (r *RateLimiter) Stop() {
	close(r.stopCh)
}
