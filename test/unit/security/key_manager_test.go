package security_test

import (
	"testing"
	"time"

	"streamgate/pkg/security"
)

func TestKeyManager_GenerateKey(t *testing.T) {
	km := security.NewKeyManager(24*time.Hour, 32)

	keyID, err := km.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	if keyID == "" {
		t.Error("Key ID is empty")
	}

	if km.GetKeyCount() != 1 {
		t.Errorf("Expected 1 key, got %d", km.GetKeyCount())
	}
}

func TestKeyManager_GetKey(t *testing.T) {
	km := security.NewKeyManager(24*time.Hour, 32)

	keyID, _ := km.GenerateKey()
	key, err := km.GetKey(keyID)
	if err != nil {
		t.Fatalf("GetKey failed: %v", err)
	}

	if len(key) != 32 {
		t.Errorf("Expected key length 32, got %d", len(key))
	}
}

func TestKeyManager_GetActiveKey(t *testing.T) {
	km := security.NewKeyManager(24*time.Hour, 32)

	keyID, _ := km.GenerateKey()
	activeKeyID, key, err := km.GetActiveKey()
	if err != nil {
		t.Fatalf("GetActiveKey failed: %v", err)
	}

	if activeKeyID != keyID {
		t.Errorf("Active key ID doesn't match. Expected %s, got %s", keyID, activeKeyID)
	}

	if len(key) != 32 {
		t.Errorf("Expected key length 32, got %d", len(key))
	}
}

func TestKeyManager_RotateKey(t *testing.T) {
	km := security.NewKeyManager(24*time.Hour, 32)

	keyID1, _ := km.GenerateKey()
	keyID2, err := km.RotateKey()
	if err != nil {
		t.Fatalf("RotateKey failed: %v", err)
	}

	if keyID1 == keyID2 {
		t.Error("Rotated key ID should be different")
	}

	if km.GetKeyCount() != 2 {
		t.Errorf("Expected 2 keys, got %d", km.GetKeyCount())
	}

	activeKeyID, _, _ := km.GetActiveKey()
	if activeKeyID != keyID2 {
		t.Errorf("Active key should be rotated key. Expected %s, got %s", keyID2, activeKeyID)
	}
}

func TestKeyManager_ShouldRotate(t *testing.T) {
	km := security.NewKeyManager(1*time.Millisecond, 32)

	km.GenerateKey()

	if km.ShouldRotate() {
		t.Error("Should not rotate immediately")
	}

	time.Sleep(2 * time.Millisecond)

	if !km.ShouldRotate() {
		t.Error("Should rotate after interval")
	}
}

func TestKeyManager_ListKeys(t *testing.T) {
	km := security.NewKeyManager(24*time.Hour, 32)

	km.GenerateKey()
	km.GenerateKey()
	km.GenerateKey()

	keys := km.ListKeys()
	if len(keys) != 3 {
		t.Errorf("Expected 3 keys, got %d", len(keys))
	}
}

func TestKeyManager_GetKeyMetadata(t *testing.T) {
	km := security.NewKeyManager(24*time.Hour, 32)

	keyID, _ := km.GenerateKey()
	metadata, err := km.GetKeyMetadata(keyID)
	if err != nil {
		t.Fatalf("GetKeyMetadata failed: %v", err)
	}

	if metadata.ID != keyID {
		t.Errorf("Metadata ID doesn't match. Expected %s, got %s", keyID, metadata.ID)
	}

	if metadata.Algorithm != "AES-256" {
		t.Errorf("Expected algorithm AES-256, got %s", metadata.Algorithm)
	}

	if !metadata.Active {
		t.Error("Key should be active")
	}
}

func TestKeyManager_RevokeKey(t *testing.T) {
	km := security.NewKeyManager(24*time.Hour, 32)

	keyID, _ := km.GenerateKey()

	err := km.RevokeKey(keyID)
	if err != nil {
		t.Fatalf("RevokeKey failed: %v", err)
	}

	if km.IsKeyActive(keyID) {
		t.Error("Revoked key should not be active")
	}
}

func TestKeyManager_IsKeyActive(t *testing.T) {
	km := security.NewKeyManager(24*time.Hour, 32)

	keyID, _ := km.GenerateKey()

	if !km.IsKeyActive(keyID) {
		t.Error("New key should be active")
	}

	km.RevokeKey(keyID)

	if km.IsKeyActive(keyID) {
		t.Error("Revoked key should not be active")
	}
}

func TestKeyManager_GetKeyCount(t *testing.T) {
	km := security.NewKeyManager(24*time.Hour, 32)

	if km.GetKeyCount() != 0 {
		t.Errorf("Expected 0 keys, got %d", km.GetKeyCount())
	}

	km.GenerateKey()
	if km.GetKeyCount() != 1 {
		t.Errorf("Expected 1 key, got %d", km.GetKeyCount())
	}

	km.GenerateKey()
	if km.GetKeyCount() != 2 {
		t.Errorf("Expected 2 keys, got %d", km.GetKeyCount())
	}
}

func TestKeyManager_GetActiveKeyCount(t *testing.T) {
	km := security.NewKeyManager(24*time.Hour, 32)

	km.GenerateKey()
	km.GenerateKey()

	if km.GetActiveKeyCount() != 2 {
		t.Errorf("Expected 2 active keys, got %d", km.GetActiveKeyCount())
	}

	keys := km.ListKeys()
	km.RevokeKey(keys[0].ID)

	if km.GetActiveKeyCount() != 1 {
		t.Errorf("Expected 1 active key, got %d", km.GetActiveKeyCount())
	}
}

func TestKeyManager_GetRotatedKeyCount(t *testing.T) {
	km := security.NewKeyManager(24*time.Hour, 32)

	km.GenerateKey()
	km.RotateKey()
	km.RotateKey()

	if km.GetRotatedKeyCount() != 2 {
		t.Errorf("Expected 2 rotated keys, got %d", km.GetRotatedKeyCount())
	}
}

func TestKeyManager_GetLastRotationTime(t *testing.T) {
	km := security.NewKeyManager(24*time.Hour, 32)

	km.GenerateKey()

	before := time.Now()
	km.RotateKey()
	after := time.Now()

	lastRotation := km.GetLastRotationTime()
	if lastRotation.Before(before) || lastRotation.After(after.Add(1*time.Second)) {
		t.Error("Last rotation time is incorrect")
	}
}

func TestKeyManager_GetRotationInterval(t *testing.T) {
	interval := 48 * time.Hour
	km := security.NewKeyManager(interval, 32)

	if km.GetRotationInterval() != interval {
		t.Errorf("Expected rotation interval %v, got %v", interval, km.GetRotationInterval())
	}
}

func TestKeyManager_MultipleRotations(t *testing.T) {
	km := security.NewKeyManager(24*time.Hour, 32)

	keyID1, _ := km.GenerateKey()
	keyID2, _ := km.RotateKey()
	keyID3, _ := km.RotateKey()

	if km.GetKeyCount() != 3 {
		t.Errorf("Expected 3 keys, got %d", km.GetKeyCount())
	}

	activeKeyID, _, _ := km.GetActiveKey()
	if activeKeyID != keyID3 {
		t.Errorf("Active key should be latest. Expected %s, got %s", keyID3, activeKeyID)
	}

	// Check that old keys are marked as rotated
	metadata1, _ := km.GetKeyMetadata(keyID1)
	metadata2, _ := km.GetKeyMetadata(keyID2)

	if !metadata1.Rotated {
		t.Error("First key should be marked as rotated")
	}

	if !metadata2.Rotated {
		t.Error("Second key should be marked as rotated")
	}
}

func BenchmarkKeyManager_GenerateKey(b *testing.B) {
	km := security.NewKeyManager(24*time.Hour, 32)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		km.GenerateKey()
	}
}

func BenchmarkKeyManager_GetKey(b *testing.B) {
	km := security.NewKeyManager(24*time.Hour, 32)
	keyID, _ := km.GenerateKey()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		km.GetKey(keyID)
	}
}

func BenchmarkKeyManager_RotateKey(b *testing.B) {
	km := security.NewKeyManager(24*time.Hour, 32)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		km.RotateKey()
	}
}
