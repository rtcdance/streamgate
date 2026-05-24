package storage

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestErrChallengeUsed(t *testing.T) {
	assert.Equal(t, "challenge already used", ErrChallengeUsed.Error())
}

func TestErrChallengeNotFound(t *testing.T) {
	assert.Equal(t, "challenge not found", ErrChallengeNotFound.Error())
}

func TestErrChallengeUsed_Is(t *testing.T) {
	assert.True(t, errors.Is(ErrChallengeUsed, ErrChallengeUsed))
	assert.False(t, errors.Is(ErrChallengeUsed, ErrChallengeNotFound))
}

func TestErrChallengeNotFound_Is(t *testing.T) {
	assert.True(t, errors.Is(ErrChallengeNotFound, ErrChallengeNotFound))
	assert.False(t, errors.Is(ErrChallengeNotFound, ErrChallengeUsed))
}
