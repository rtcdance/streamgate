package benchmark_test

import (
	"context"
	"testing"

	"streamgate/pkg/models"
	"streamgate/pkg/service"
	"streamgate/test/helpers"
)

func BenchmarkAuthService_Register(b *testing.B) {
	db := helpers.SetupTestDB(&testing.T{})
	if db == nil {
		b.Skip("Database not available")
	}
	defer helpers.CleanupTestDB(&testing.T{}, db)

	authService := service.NewAuthService(db)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		user := &models.User{
			Username: "testuser" + string(rune(i)),
			Email:    "test" + string(rune(i)) + "@example.com",
			Password: "password123",
		}
		authService.Register(context.Background(), user)
	}
}

func BenchmarkAuthService_Login(b *testing.B) {
	db := helpers.SetupTestDB(&testing.T{})
	if db == nil {
		b.Skip("Database not available")
	}
	defer helpers.CleanupTestDB(&testing.T{}, db)

	authService := service.NewAuthService(db)

	// Setup: Create a user
	user := &models.User{
		Username: "testuser",
		Email:    "test@example.com",
		Password: "password123",
	}
	authService.Register(context.Background(), user)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		authService.Login(context.Background(), user.Email, "password123")
	}
}

func BenchmarkAuthService_ValidateToken(b *testing.B) {
	db := helpers.SetupTestDB(&testing.T{})
	if db == nil {
		b.Skip("Database not available")
	}
	defer helpers.CleanupTestDB(&testing.T{}, db)

	authService := service.NewAuthService(db)

	// Setup: Create user and get token
	user := &models.User{
		Username: "testuser",
		Email:    "test@example.com",
		Password: "password123",
	}
	authService.Register(context.Background(), user)
	token, _ := authService.Login(context.Background(), user.Email, "password123")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		authService.ValidateToken(token)
	}
}

func BenchmarkAuthService_RefreshToken(b *testing.B) {
	db := helpers.SetupTestDB(&testing.T{})
	if db == nil {
		b.Skip("Database not available")
	}
	defer helpers.CleanupTestDB(&testing.T{}, db)

	authService := service.NewAuthService(db)

	// Setup: Create user and get token
	user := &models.User{
		Username: "testuser",
		Email:    "test@example.com",
		Password: "password123",
	}
	authService.Register(context.Background(), user)
	token, _ := authService.Login(context.Background(), user.Email, "password123")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		authService.RefreshToken(token)
	}
}

func BenchmarkAuthService_PasswordHashing(b *testing.B) {
	password := "password123"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Simulate password hashing
		_ = hashPassword(password)
	}
}

func hashPassword(password string) string {
	// Simplified password hashing for benchmark
	return "hashed_" + password
}

func BenchmarkAuthService_ConcurrentLogins(b *testing.B) {
	db := helpers.SetupTestDB(&testing.T{})
	if db == nil {
		b.Skip("Database not available")
	}
	defer helpers.CleanupTestDB(&testing.T{}, db)

	authService := service.NewAuthService(db)

	// Setup: Create a user
	user := &models.User{
		Username: "testuser",
		Email:    "test@example.com",
		Password: "password123",
	}
	authService.Register(context.Background(), user)

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			authService.Login(context.Background(), user.Email, "password123")
		}
	})
}
