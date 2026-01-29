package auth_test

import (
	"context"
	"testing"

	"streamgate/pkg/models"
	"streamgate/pkg/service"
	"streamgate/test/helpers"
)

func TestAuthService_RegisterAndLogin(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	authService := service.NewAuthService(db)

	// Test registration
	user := &models.User{
		Username: "testuser",
		Email:    "test@example.com",
		Password: "password123",
	}

	err := authService.Register(context.Background(), user)
	helpers.AssertNoError(t, err)
	helpers.AssertNotNil(t, user.ID)

	// Test login
	token, err := authService.Login(context.Background(), user.Email, "password123")
	helpers.AssertNoError(t, err)
	helpers.AssertNotEmpty(t, token)
}

func TestAuthService_InvalidPassword(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	authService := service.NewAuthService(db)

	// Register user
	user := &models.User{
		Username: "testuser",
		Email:    "test@example.com",
		Password: "password123",
	}

	err := authService.Register(context.Background(), user)
	helpers.AssertNoError(t, err)

	// Try login with wrong password
	_, err = authService.Login(context.Background(), user.Email, "wrongpassword")
	helpers.AssertError(t, err)
}

func TestAuthService_DuplicateEmail(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	authService := service.NewAuthService(db)

	// Register first user
	user1 := &models.User{
		Username: "user1",
		Email:    "test@example.com",
		Password: "password123",
	}

	err := authService.Register(context.Background(), user1)
	helpers.AssertNoError(t, err)

	// Try register with same email
	user2 := &models.User{
		Username: "user2",
		Email:    "test@example.com",
		Password: "password456",
	}

	err = authService.Register(context.Background(), user2)
	helpers.AssertError(t, err)
}

func TestAuthService_TokenValidation(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	authService := service.NewAuthService(db)

	// Register and login
	user := &models.User{
		Username: "testuser",
		Email:    "test@example.com",
		Password: "password123",
	}

	err := authService.Register(context.Background(), user)
	helpers.AssertNoError(t, err)

	token, err := authService.Login(context.Background(), user.Email, "password123")
	helpers.AssertNoError(t, err)

	// Validate token
	claims, err := authService.ValidateToken(token)
	helpers.AssertNoError(t, err)
	helpers.AssertNotNil(t, claims)
}

func TestAuthService_RefreshToken(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	authService := service.NewAuthService(db)

	// Register and login
	user := &models.User{
		Username: "testuser",
		Email:    "test@example.com",
		Password: "password123",
	}

	err := authService.Register(context.Background(), user)
	helpers.AssertNoError(t, err)

	token, err := authService.Login(context.Background(), user.Email, "password123")
	helpers.AssertNoError(t, err)

	// Refresh token
	newToken, err := authService.RefreshToken(token)
	helpers.AssertNoError(t, err)
	helpers.AssertNotEmpty(t, newToken)
	helpers.AssertNotEqual(t, token, newToken)
}
