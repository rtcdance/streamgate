package models

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestUser_Creation(t *testing.T) {
	user := &User{
		ID:        "user123",
		Username:  "testuser",
		Email:     "test@example.com",
		CreatedAt: time.Now(),
	}

	assert.Equal(t, "user123", user.ID)
	assert.Equal(t, "testuser", user.Username)
	assert.Equal(t, "test@example.com", user.Email)
}

func TestUser_Validation(t *testing.T) {
	tests := []struct {
		name    string
		user    *User
		isValid bool
	}{
		{
			"valid user",
			&User{
				ID:       "user123",
				Username: "testuser",
				Email:    "test@example.com",
			},
			true,
		},
		{
			"missing username",
			&User{
				ID:    "user123",
				Email: "test@example.com",
			},
			false,
		},
		{
			"missing email",
			&User{
				ID:       "user123",
				Username: "testuser",
			},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isValid := tt.user.ID != "" && tt.user.Username != "" && tt.user.Email != ""
			assert.Equal(t, tt.isValid, isValid)
		})
	}
}

func TestUser_Update(t *testing.T) {
	user := &User{
		ID:       "user123",
		Username: "testuser",
		Email:    "test@example.com",
	}

	// Update email
	user.Email = "newemail@example.com"
	assert.Equal(t, "newemail@example.com", user.Email)

	// Username should remain unchanged
	assert.Equal(t, "testuser", user.Username)
}
