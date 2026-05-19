package benchmark_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"streamgate/pkg/models"
	"streamgate/pkg/service"
)

func BenchmarkAuthService_Register(b *testing.B) {
	storage := NewMockAuthStorage()
	authService := service.NewAuthService("test-secret-that-is-at-least-32-chars", storage)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		username := fmt.Sprintf("testuser%d", i)
		email := fmt.Sprintf("test%d@example.com", i)
		_ = authService.Register(context.Background(), username, "password123", email)
	}
}

func BenchmarkAuthService_Login(b *testing.B) {
	storage := NewMockAuthStorage()
	authService := service.NewAuthService("test-secret-that-is-at-least-32-chars", storage)

	// Setup: Create a user
	_ = authService.Register(context.Background(), "testuser", "password123", "test@example.com")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = authService.Authenticate(context.Background(), "testuser", "password123")
	}
}

func BenchmarkAuthService_ValidateToken(b *testing.B) {
	storage := NewMockAuthStorage()
	authService := service.NewAuthService("test-secret-that-is-at-least-32-chars", storage)

	// Setup: Create user and get token
	_ = authService.Register(context.Background(), "testuser", "password123", "test@example.com")
	token, _ := authService.Authenticate(context.Background(), "testuser", "password123")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = authService.Verify(token)
	}
}

func BenchmarkAuthService_RefreshToken(b *testing.B) {
	storage := NewMockAuthStorage()
	authService := service.NewAuthService("test-secret-that-is-at-least-32-chars", storage)

	// Setup: Create user and get token
	_ = authService.Register(context.Background(), "testuser", "password123", "test@example.com")
	token, _ := authService.Authenticate(context.Background(), "testuser", "password123")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = authService.RefreshToken(context.Background(), token)
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
	storage := NewMockAuthStorage()
	authService := service.NewAuthService("test-secret-that-is-at-least-32-chars", storage)

	// Setup: Create a user
	_ = authService.Register(context.Background(), "testuser", "password123", "test@example.com")

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = authService.Authenticate(context.Background(), "testuser", "password123")
		}
	})
}

// MockAuthStorage implements service.AuthStorage for testing
type MockAuthStorage struct {
	mu    sync.Mutex
	users map[string]*models.User
}

func NewMockAuthStorage() *MockAuthStorage {
	return &MockAuthStorage{
		users: make(map[string]*models.User),
	}
}

func (m *MockAuthStorage) GetUser(ctx context.Context, username string) (*models.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	user, exists := m.users[username]
	if !exists {
		return nil, nil
	}
	return user, nil
}

func (m *MockAuthStorage) CreateUser(ctx context.Context, user *models.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.users[user.Username] = user
	return nil
}

func (m *MockAuthStorage) UpdateUser(ctx context.Context, user *models.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.users[user.Username] = user
	return nil
}
