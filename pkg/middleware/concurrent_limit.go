package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type ConcurrentStreamLimiter struct {
	mu          sync.RWMutex
	sessions    map[string][]sessionEntry
	maxPerUser  int
	ttl         time.Duration
	cleanupTick time.Duration
	done        chan struct{}
}

type sessionEntry struct {
	ContentID string
	ExpiresAt time.Time
}

func NewConcurrentStreamLimiter(maxPerUser int, ttl time.Duration) *ConcurrentStreamLimiter {
	l := &ConcurrentStreamLimiter{
		sessions:    make(map[string][]sessionEntry),
		maxPerUser:  maxPerUser,
		ttl:         ttl,
		cleanupTick: 5 * time.Minute,
		done:        make(chan struct{}),
	}
	go l.cleanupLoop()
	return l
}

func (l *ConcurrentStreamLimiter) Stop() {
	close(l.done)
}

func (l *ConcurrentStreamLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		wallet := GetWalletAddress(c)
		if wallet == "" {
			c.Next()
			return
		}

		contentID := c.Param("id")
		if contentID == "" {
			c.Next()
			return
		}

		l.mu.Lock()
		now := time.Now()
		active := l.pruneLocked(wallet, now)

		alreadyActive := false
		for _, s := range active {
			if s.ContentID == contentID {
				alreadyActive = true
				break
			}
		}

		if alreadyActive {
			for i := range active {
				if active[i].ContentID == contentID {
					active[i].ExpiresAt = now.Add(l.ttl)
					break
				}
			}
			l.sessions[wallet] = active
			l.mu.Unlock()
			c.Next()
			return
		}

		if len(active) >= l.maxPerUser {
			l.mu.Unlock()
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error":          "concurrent stream limit exceeded",
				"code":           "CONCURRENT_LIMIT",
				"max_streams":    l.maxPerUser,
				"active_streams": len(active),
			})
			return
		}

		active = append(active, sessionEntry{
			ContentID: contentID,
			ExpiresAt: now.Add(l.ttl),
		})
		l.sessions[wallet] = active
		l.mu.Unlock()

		c.Next()
	}
}

func (l *ConcurrentStreamLimiter) pruneLocked(wallet string, now time.Time) []sessionEntry {
	entries := l.sessions[wallet]
	active := make([]sessionEntry, 0, len(entries))
	for _, e := range entries {
		if e.ExpiresAt.After(now) {
			active = append(active, e)
		}
	}
	l.sessions[wallet] = active
	return active
}

func (l *ConcurrentStreamLimiter) cleanupLoop() {
	ticker := time.NewTicker(l.cleanupTick)
	defer ticker.Stop()
	for {
		select {
		case <-l.done:
			return
		case <-ticker.C:
			l.mu.Lock()
			now := time.Now()
			for wallet := range l.sessions {
				l.pruneLocked(wallet, now)
				if len(l.sessions[wallet]) == 0 {
					delete(l.sessions, wallet)
				}
			}
			l.mu.Unlock()
		}
	}
}
