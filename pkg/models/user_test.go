package models

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestUser_Creation(t *testing.T) {
	now := time.Now()
	user := &User{
		ID:            "user123",
		Username:      "testuser",
		Password:      "hashedpassword",
		Email:         "test@example.com",
		WalletAddress: "0x0987654321098765432109876543210987654321",
		Role:          "user",
		Status:        "active",
		CreatedAt:     now,
		UpdatedAt:     now,
		LastLoginAt:   now,
	}

	assert.Equal(t, "user123", user.ID)
	assert.Equal(t, "testuser", user.Username)
	assert.Equal(t, "hashedpassword", user.Password)
	assert.Equal(t, "test@example.com", user.Email)
	assert.Equal(t, "0x0987654321098765432109876543210987654321", user.WalletAddress)
	assert.Equal(t, "user", user.Role)
	assert.Equal(t, "active", user.Status)
}

func TestUser_ZeroValues(t *testing.T) {
	user := &User{}

	assert.Equal(t, "", user.ID)
	assert.Equal(t, "", user.Username)
	assert.Equal(t, "", user.Email)
	assert.Equal(t, "", user.WalletAddress)
	assert.True(t, user.CreatedAt.IsZero())
	assert.True(t, user.LastLoginAt.IsZero())
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
		{
			"missing id",
			&User{
				Username: "testuser",
				Email:    "test@example.com",
			},
			false,
		},
		{
			"all missing",
			&User{},
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

func TestUser_PasswordField(t *testing.T) {
	user := &User{
		ID:       "user123",
		Username: "testuser",
		Password: "secret",
		Email:    "test@example.com",
	}

	data, err := json.Marshal(user)
	assert.NoError(t, err)

	var decoded map[string]interface{}
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	_, hasPassword := decoded["Password"]
	assert.False(t, hasPassword, "Password field should be excluded from JSON due to '-' tag")
}

func TestUser_WalletAssociation(t *testing.T) {
	user := &User{
		ID:            "user123",
		Username:      "walletuser",
		WalletAddress: "0xABCDEF1234567890ABCDEF1234567890ABCDEF12",
	}

	assert.Equal(t, "0xABCDEF1234567890ABCDEF1234567890ABCDEF12", user.WalletAddress)
	assert.NotEmpty(t, user.WalletAddress)
}

func TestUser_Update(t *testing.T) {
	user := &User{
		ID:       "user123",
		Username: "testuser",
		Email:    "test@example.com",
	}

	user.Email = "newemail@example.com"
	assert.Equal(t, "newemail@example.com", user.Email)
	assert.Equal(t, "testuser", user.Username)
}

func TestUser_JSONMarshaling(t *testing.T) {
	now := time.Now()
	user := &User{
		ID:            "json-user",
		Username:      "jsonuser",
		Email:         "json@example.com",
		WalletAddress: "0x1234",
		Role:          "admin",
		Status:        "active",
		CreatedAt:     now,
	}

	data, err := json.Marshal(user)
	assert.NoError(t, err)

	var decoded User
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)
	assert.Equal(t, user.ID, decoded.ID)
	assert.Equal(t, user.Username, decoded.Username)
	assert.Equal(t, user.Email, decoded.Email)
	assert.Equal(t, user.WalletAddress, decoded.WalletAddress)
	assert.Equal(t, user.Role, decoded.Role)
}

func TestUserProfile(t *testing.T) {
	profile := &UserProfile{
		UserID:      "user123",
		DisplayName: "Display Name",
		Avatar:      "https://example.com/avatar.png",
		Bio:         "Hello world",
		Preferences: map[string]string{"theme": "dark", "lang": "en"},
		Metadata:    map[string]interface{}{"level": float64(5)},
		UpdatedAt:   time.Now(),
	}

	assert.Equal(t, "user123", profile.UserID)
	assert.Equal(t, "Display Name", profile.DisplayName)
	assert.Equal(t, "dark", profile.Preferences["theme"])
	assert.Equal(t, float64(5), profile.Metadata["level"])
}

func TestUserProfile_JSONMarshaling(t *testing.T) {
	profile := &UserProfile{
		UserID:      "json-profile",
		DisplayName: "JSON User",
		Preferences: map[string]string{"theme": "light"},
	}

	data, err := json.Marshal(profile)
	assert.NoError(t, err)

	var decoded UserProfile
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)
	assert.Equal(t, profile.UserID, decoded.UserID)
	assert.Equal(t, "light", decoded.Preferences["theme"])
}

func TestUserRole_Constants(t *testing.T) {
	assert.Equal(t, UserRole("admin"), RoleAdmin)
	assert.Equal(t, UserRole("user"), RoleUser)
	assert.Equal(t, UserRole("moderator"), RoleModerator)
}

func TestUserStatus_Constants(t *testing.T) {
	assert.Equal(t, UserStatus("active"), StatusActive)
	assert.Equal(t, UserStatus("inactive"), StatusInactive)
	assert.Equal(t, UserStatus("banned"), StatusBanned)
}
