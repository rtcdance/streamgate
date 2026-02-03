package util

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetCurrentTime(t *testing.T) {
	before := time.Now()
	result := GetCurrentTime()
	after := time.Now()

	assert.False(t, result.Before(before))
	assert.False(t, result.After(after))
}

func TestNow(t *testing.T) {
	before := time.Now()
	result := Now()
	after := time.Now()

	assert.False(t, result.Before(before))
	assert.False(t, result.After(after))
}

func TestGetCurrentTimeUnix(t *testing.T) {
	before := time.Now().Unix()
	result := GetCurrentTimeUnix()
	after := time.Now().Unix()

	assert.GreaterOrEqual(t, result, before)
	assert.LessOrEqual(t, result, after)
}

func TestGetCurrentTimeUnixNano(t *testing.T) {
	before := time.Now().UnixNano()
	result := GetCurrentTimeUnixNano()
	after := time.Now().UnixNano()

	assert.GreaterOrEqual(t, result, before)
	assert.LessOrEqual(t, result, after)
}

func TestFormatTime(t *testing.T) {
	tests := []struct {
		name   string
		t      time.Time
		layout string
		want   string
	}{
		{
			name:   "RFC3339 format",
			t:      time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
			layout: time.RFC3339,
			want:   "2024-01-01T12:00:00Z",
		},
		{
			name:   "custom format",
			t:      time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
			layout: "2006-01-02 15:04:05",
			want:   "2024-01-01 12:00:00",
		},
		{
			name:   "ANSIC format",
			t:      time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
			layout: time.ANSIC,
			want:   "Mon Jan  1 12:00:00 2024",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatTime(tt.t, tt.layout)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestParseTime(t *testing.T) {
	tests := []struct {
		name    string
		s       string
		layout  string
		want    time.Time
		wantErr bool
	}{
		{
			name:   "RFC3339 format",
			s:      "2024-01-01T12:00:00Z",
			layout: time.RFC3339,
			want:   time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
		},
		{
			name:   "custom format",
			s:      "2024-01-01 12:00:00",
			layout: "2006-01-02 15:04:05",
			want:   time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
		},
		{
			name:    "invalid format",
			s:       "invalid",
			layout:  time.RFC3339,
			wantErr: true,
		},
		{
			name:    "mismatched format",
			s:       "2024-01-01",
			layout:  time.RFC3339,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseTime(tt.s, tt.layout)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestAddDuration(t *testing.T) {
	base := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		t    time.Time
		d    time.Duration
		want time.Time
	}{
		{
			name: "add positive duration",
			t:    base,
			d:    time.Hour,
			want: time.Date(2024, 1, 1, 13, 0, 0, 0, time.UTC),
		},
		{
			name: "add negative duration",
			t:    base,
			d:    -time.Hour,
			want: time.Date(2024, 1, 1, 11, 0, 0, 0, time.UTC),
		},
		{
			name: "add zero duration",
			t:    base,
			d:    0,
			want: base,
		},
		{
			name: "add multiple hours",
			t:    base,
			d:    2*time.Hour + 30*time.Minute,
			want: time.Date(2024, 1, 1, 14, 30, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := AddDuration(tt.t, tt.d)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestSubDuration(t *testing.T) {
	base := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		t    time.Time
		d    time.Duration
		want time.Time
	}{
		{
			name: "subtract positive duration",
			t:    base,
			d:    time.Hour,
			want: time.Date(2024, 1, 1, 11, 0, 0, 0, time.UTC),
		},
		{
			name: "subtract negative duration (adds)",
			t:    base,
			d:    -time.Hour,
			want: time.Date(2024, 1, 1, 13, 0, 0, 0, time.UTC),
		},
		{
			name: "subtract zero duration",
			t:    base,
			d:    0,
			want: base,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SubDuration(tt.t, tt.d)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestDurationBetween(t *testing.T) {
	t1 := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	t2 := time.Date(2024, 1, 1, 14, 30, 0, 0, time.UTC)

	tests := []struct {
		name string
		t1   time.Time
		t2   time.Time
		want time.Duration
	}{
		{
			name: "positive duration",
			t1:   t1,
			t2:   t2,
			want: 2*time.Hour + 30*time.Minute,
		},
		{
			name: "negative duration",
			t1:   t2,
			t2:   t1,
			want: -(2*time.Hour + 30*time.Minute),
		},
		{
			name: "zero duration",
			t1:   t1,
			t2:   t1,
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DurationBetween(tt.t1, tt.t2)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestIsAfter(t *testing.T) {
	t1 := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	t2 := time.Date(2024, 1, 1, 11, 0, 0, 0, time.UTC)
	t3 := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		t1       time.Time
		t2       time.Time
		expected bool
	}{
		{
			name:     "t1 is after t2",
			t1:       t1,
			t2:       t2,
			expected: true,
		},
		{
			name:     "t1 is before t2",
			t1:       t2,
			t2:       t1,
			expected: false,
		},
		{
			name:     "t1 equals t2",
			t1:       t1,
			t2:       t3,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsAfter(tt.t1, tt.t2)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsBefore(t *testing.T) {
	t1 := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	t2 := time.Date(2024, 1, 1, 11, 0, 0, 0, time.UTC)
	t3 := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		t1       time.Time
		t2       time.Time
		expected bool
	}{
		{
			name:     "t1 is before t2",
			t1:       t2,
			t2:       t1,
			expected: true,
		},
		{
			name:     "t1 is after t2",
			t1:       t1,
			t2:       t2,
			expected: false,
		},
		{
			name:     "t1 equals t2",
			t1:       t1,
			t2:       t3,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsBefore(tt.t1, tt.t2)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsEqual(t *testing.T) {
	t1 := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	t2 := time.Date(2024, 1, 1, 11, 0, 0, 0, time.UTC)
	t3 := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		t1       time.Time
		t2       time.Time
		expected bool
	}{
		{
			name:     "t1 equals t2",
			t1:       t1,
			t2:       t3,
			expected: true,
		},
		{
			name:     "t1 not equal to t2",
			t1:       t1,
			t2:       t2,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsEqual(tt.t1, tt.t2)
			assert.Equal(t, tt.expected, result)
		})
	}
}
