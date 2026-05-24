package abiutil

import (
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const validABIJSON = `[{"type":"function","name":"balanceOf","inputs":[{"name":"account","type":"address"}],"outputs":[{"name":"","type":"uint256"}],"stateMutability":"view"},{"type":"function","name":"transfer","inputs":[{"name":"to","type":"address"},{"name":"amount","type":"uint256"}],"outputs":[{"name":"","type":"bool"}],"stateMutability":"nonpayable"},{"type":"event","name":"Transfer","inputs":[{"name":"from","type":"address","indexed":true},{"name":"to","type":"address","indexed":true},{"name":"value","type":"uint256","indexed":false}]}]`

const invalidABIJSON = `not valid json`

const emptyABIJSON = `[]`

func TestMustParseABI_Valid(t *testing.T) {
	parsed := MustParseABI("ERC20", validABIJSON)
	assert.NotNil(t, parsed)

	_, ok := parsed.Methods["balanceOf"]
	assert.True(t, ok, "balanceOf method should exist")

	_, ok = parsed.Methods["transfer"]
	assert.True(t, ok, "transfer method should exist")

	_, ok = parsed.Events["Transfer"]
	assert.True(t, ok, "Transfer event should exist")
}

func TestMustParseABI_Invalid(t *testing.T) {
	assert.Panics(t, func() {
		MustParseABI("bad", invalidABIJSON)
	})
}

func TestMustParseABI_Empty(t *testing.T) {
	parsed := MustParseABI("empty", emptyABIJSON)
	assert.NotNil(t, parsed)
	assert.Equal(t, 0, len(parsed.Methods))
	assert.Equal(t, 0, len(parsed.Events))
}

func TestGetOrParseABI_Valid(t *testing.T) {
	abiCache = sync.Map{}

	parsed, err := GetOrParseABI(validABIJSON)
	require.NoError(t, err)
	assert.NotNil(t, parsed)

	_, ok := parsed.Methods["balanceOf"]
	assert.True(t, ok)
}

func TestGetOrParseABI_Invalid(t *testing.T) {
	abiCache = sync.Map{}

	_, err := GetOrParseABI(invalidABIJSON)
	require.Error(t, err)
}

func TestGetOrParseABI_Caching(t *testing.T) {
	abiCache = sync.Map{}

	parsed1, err := GetOrParseABI(validABIJSON)
	require.NoError(t, err)

	parsed2, err := GetOrParseABI(validABIJSON)
	require.NoError(t, err)

	assert.Equal(t, parsed1, parsed2)

	cached, ok := abiCache.Load(validABIJSON)
	assert.True(t, ok, "should be cached")
	assert.Equal(t, parsed1, cached.(abi.ABI))
}

func TestGetOrParseABI_DifferentABIs(t *testing.T) {
	abiCache = sync.Map{}

	parsed1, err := GetOrParseABI(validABIJSON)
	require.NoError(t, err)

	parsed2, err := GetOrParseABI(emptyABIJSON)
	require.NoError(t, err)

	assert.NotEqual(t, len(parsed1.Methods), len(parsed2.Methods))
}

func TestMustParseABI_PreservesMethods(t *testing.T) {
	parsed := MustParseABI("ERC20", validABIJSON)

	balanceOf, ok := parsed.Methods["balanceOf"]
	require.True(t, ok)
	assert.Len(t, balanceOf.Inputs, 1)
	assert.Equal(t, "account", balanceOf.Inputs[0].Name)
	assert.Len(t, balanceOf.Outputs, 1)

	transfer, ok := parsed.Methods["transfer"]
	require.True(t, ok)
	assert.Len(t, transfer.Inputs, 2)
	assert.Len(t, transfer.Outputs, 1)
}

func TestMustParseABI_PreservesEvents(t *testing.T) {
	parsed := MustParseABI("ERC20", validABIJSON)

	transferEvent, ok := parsed.Events["Transfer"]
	require.True(t, ok)
	assert.Len(t, transferEvent.Inputs, 3)
}

func TestGetOrParseABI_Empty(t *testing.T) {
	abiCache = sync.Map{}

	parsed, err := GetOrParseABI(emptyABIJSON)
	require.NoError(t, err)
	assert.NotNil(t, parsed)
	assert.Equal(t, 0, len(parsed.Methods))
}

func TestMustParseABI_NilJSON(t *testing.T) {
	assert.Panics(t, func() {
		MustParseABI("nil", "")
	})
}

func TestGetOrParseABI_EmptyString(t *testing.T) {
	abiCache = sync.Map{}

	_, err := GetOrParseABI("")
	require.Error(t, err)
}
