package util

import (
	"time"
)

// GetCurrentTime returns current time
func GetCurrentTime() time.Time {
	return time.Now()
}

// Now is an alias for GetCurrentTime for compatibility
func Now() time.Time {
	return time.Now()
}

// GetCurrentTimeUnix returns current time as Unix timestamp
func GetCurrentTimeUnix() int64 {
	return time.Now().Unix()
}

// GetCurrentTimeUnixNano returns current time as Unix nanosecond timestamp
func GetCurrentTimeUnixNano() int64 {
	return time.Now().UnixNano()
}

// FormatTime formats time to string
func FormatTime(t time.Time, layout string) string {
	return t.Format(layout)
}

// ParseTime parses string to time
func ParseTime(s, layout string) (time.Time, error) {
	return time.Parse(layout, s)
}

// AddDuration adds duration to time
func AddDuration(t time.Time, d time.Duration) time.Time {
	return t.Add(d)
}

// SubDuration subtracts duration from time
func SubDuration(t time.Time, d time.Duration) time.Time {
	return t.Add(-d)
}

// DurationBetween calculates duration between two times
func DurationBetween(t1, t2 time.Time) time.Duration {
	return t2.Sub(t1)
}

// IsAfter checks if t1 is after t2
func IsAfter(t1, t2 time.Time) bool {
	return t1.After(t2)
}

// IsBefore checks if t1 is before t2
func IsBefore(t1, t2 time.Time) bool {
	return t1.Before(t2)
}

// IsEqual checks if t1 equals t2
func IsEqual(t1, t2 time.Time) bool {
	return t1.Equal(t2)
}
