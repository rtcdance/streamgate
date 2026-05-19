package models

import "time"

type PlaybackEvent struct {
	ID               string    `json:"id"`
	ContentID        string    `json:"content_id"`
	WalletAddress    string    `json:"wallet_address"`
	EventType        string    `json:"event_type"`
	DurationSeconds  int       `json:"duration_seconds"`
	PlaybackTokenJTI string    `json:"playback_token_jti"`
	UserAgent        string    `json:"user_agent"`
	IPAddress        string    `json:"ip_address"`
	CreatedAt        time.Time `json:"created_at"`
}

type ContentStats struct {
	ContentID         string    `json:"content_id"`
	TotalPlays        int       `json:"total_plays"`
	UniqueViewers     int       `json:"unique_viewers"`
	TotalWatchSeconds int64     `json:"total_watch_seconds"`
	AvgWatchSeconds   int       `json:"avg_watch_seconds"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type PlaybackEventType string

const (
	PlaybackEventStart   PlaybackEventType = "start"
	PlaybackEventSegment PlaybackEventType = "segment"
	PlaybackEventEnd     PlaybackEventType = "end"
)
