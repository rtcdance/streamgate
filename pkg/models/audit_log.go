package models

import "time"

type AuditLog struct {
	ID         int64     `json:"id"`
	Action     string    `json:"action"`
	Actor      string    `json:"actor"`
	Resource   string    `json:"resource"`
	ResourceID string    `json:"resource_id"`
	Success    bool      `json:"success"`
	ErrorMsg   string    `json:"error_msg"`
	Details    string    `json:"details"`
	CreatedAt  time.Time `json:"created_at"`
}
