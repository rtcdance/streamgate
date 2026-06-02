package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSHA256(t *testing.T) {
	data := []byte("test data")
	result := SHA256(data)
	assert.Equal(t, SHA256Hash(data), result)
	assert.Len(t, result, 64)
}

func TestHashSHA256(t *testing.T) {
	data := []byte("test data")
	result := HashSHA256(data)
	assert.Equal(t, SHA256Hash(data), result)
	assert.Len(t, result, 64)
}

func TestVerifySHA256(t *testing.T) {
	data := []byte("test data")
	hash := SHA256Hash(data)

	assert.True(t, VerifySHA256(data, hash))
	assert.False(t, VerifySHA256([]byte("other data"), hash))
}

func TestHashString(t *testing.T) {
	result := HashString("hello")
	assert.Len(t, result, 64)
	assert.Equal(t, SHA256Hash([]byte("hello")), result)

	result2 := HashString("hello")
	assert.Equal(t, result, result2)
}
