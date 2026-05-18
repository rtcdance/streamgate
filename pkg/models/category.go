package models

import "time"

type Category struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Description string    `json:"description"`
	ParentID    string    `json:"parent_id"`
	CreatedAt   time.Time `json:"created_at"`
}
