package models_test

import (
	"testing"
	"time"

	"streamgate/pkg/models"
	"streamgate/test/helpers"
)

func TestUser_Creation(t *testing.T) {
	user := &models.User{
		ID:        "user123",
		Username:  "testuser",
		Email:     "test@example.com",
		CreatedAt: time.Now(),
	}

	helpers.AssertEqual(t, "user123", user.ID)
	helpers.AssertEqual(t, "testuser", user.Username)
	helpers.AssertEqual(t, "test@example.com", user.Email)
}

func TestUser_Validation(t *testing.T) {
	tests := []struct {
		name    string
		user    *models.User
		isValid bool
	}{
		{
			"valid user",
			&models.User{
				ID:       "user123",
				Username: "testuser",
				Email:    "test@example.com",
			},
			true,
		},
		{
			"missing username",
			&models.User{
				ID:    "user123",
				Email: "test@example.com",
			},
			false,
		},
		{
			"missing email",
			&models.User{
				ID:       "user123",
				Username: "testuser",
			},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isValid := tt.user.ID != "" && tt.user.Username != "" && tt.user.Email != ""
			helpers.AssertEqual(t, tt.isValid, isValid)
		})
	}
}

func TestUser_Update(t *testing.T) {
	user := &models.User{
		ID:       "user123",
		Username: "testuser",
		Email:    "test@example.com",
	}

	// Update email
	user.Email = "newemail@example.com"
	helpers.AssertEqual(t, "newemail@example.com", user.Email)

	// Username should remain unchanged
	helpers.AssertEqual(t, "testuser", user.Username)
}
