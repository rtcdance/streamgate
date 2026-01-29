package models

import "time"

// User represents a user in the system
type User struct {
	ID            string    `json:"id"`
	Username      string    `json:"username"`
	Email         string    `json:"email"`
	WalletAddress string    `json:"wallet_address"`
	Role          string    `json:"role"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	LastLoginAt   time.Time `json:"last_login_at"`
}

// UserProfile represents user profile information
type UserProfile struct {
	UserID       string            `json:"user_id"`
	DisplayName  string            `json:"display_name"`
	Avatar       string            `json:"avatar"`
	Bio          string            `json:"bio"`
	Preferences  map[string]string `json:"preferences"`
	Metadata     map[string]interface{} `json:"metadata"`
	UpdatedAt    time.Time         `json:"updated_at"`
}

// UserRole defines user roles
type UserRole string

const (
	RoleAdmin    UserRole = "admin"
	RoleUser     UserRole = "user"
	RoleModerator UserRole = "moderator"
)

// UserStatus defines user status
type UserStatus string

const (
	StatusActive   UserStatus = "active"
	StatusInactive UserStatus = "inactive"
	StatusBanned   UserStatus = "banned"
)
