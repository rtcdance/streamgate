package benchmark_test

import (
	"testing"

	"streamgate/pkg/service"
)

func BenchmarkAuthService_Register(b *testing.B) {
	storage := NewMockAuthStorage()
	authService := service.NewAuthService("test-secret", storage)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		username := "testuser" + string(rune(i))
		email := "test" + string(rune(i)) + "@example.com"
		authService.Register(username, "password123", email)
	}
}

func BenchmarkAuthService_Login(b *testing.B) {
	storage := NewMockAuthStorage()
	authService := service.NewAuthService("test-secret", storage)

	// Setup: Create a user
	authService.Register("testuser", "password123", "test@example.com")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		authService.Authenticate("testuser", "password123")
	}
}

func BenchmarkAuthService_ValidateToken(b *testing.B) {
	storage := NewMockAuthStorage()
	authService := service.NewAuthService("test-secret", storage)

	// Setup: Create user and get token
	authService.Register("testuser", "password123", "test@example.com")
	token, _ := authService.Authenticate("testuser", "password123")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		authService.Verify(token)
	}
}

func BenchmarkAuthService_RefreshToken(b *testing.B) {
	storage := NewMockAuthStorage()
	authService := service.NewAuthService("test-secret", storage)

	// Setup: Create user and get token
	authService.Register("testuser", "password123", "test@example.com")
	token, _ := authService.Authenticate("testuser", "password123")

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
	storage := NewMockAuthStorage()
	authService := service.NewAuthService("test-secret", storage)

	// Setup: Create a user
	authService.Register("testuser", "password123", "test@example.com")

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			authService.Authenticate("testuser", "password123")
		}
	})
}

// MockAuthStorage implements service.AuthStorage for testing
type MockAuthStorage struct {
	users map[string]*service.User
}

func NewMockAuthStorage() *MockAuthStorage {
	return &MockAuthStorage{
		users: make(map[string]*service.User),
	}
}

func (m *MockAuthStorage) GetUser(username string) (*service.User, error) {
	user, exists := m.users[username]
	if !exists {
		return nil, nil
	}
	return user, nil
}

func (m *MockAuthStorage) CreateUser(user *service.User) error {
	m.users[user.Username] = user
	return nil
}

func (m *MockAuthStorage) UpdateUser(user *service.User) error {
	m.users[user.Username] = user
	return nil
}
