package gateway

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAPIVersion(t *testing.T) {
	assert.Equal(t, "v1", APIVersion)
}

func TestAPIPrefix(t *testing.T) {
	assert.Equal(t, "/api/v1", APIPrefix)
}
