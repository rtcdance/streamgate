package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The reorg detector must reach whatever handler was registered, carrying the
// chain the reorg happened on, so the right evidence gets dropped.
func TestWeb3Service_ReorgHandlerReceivesChainAndContracts(t *testing.T) {
	ws := &Web3Service{reorgChainID: 31337}

	var gotChain int64
	var gotContracts []string
	ws.SetReorgHandler(func(chainID int64, contracts []string) {
		gotChain, gotContracts = chainID, contracts
	})

	ws.notifyReorg([]string{"0xC1", "0xC2"})

	assert.Equal(t, int64(31337), gotChain)
	assert.Equal(t, []string{"0xC1", "0xC2"}, gotContracts)
}

// A detector wired before any handler is registered must not panic: the
// detector runs in the background from service construction.
func TestWeb3Service_ReorgHandlerUnsetIsSafe(t *testing.T) {
	ws := &Web3Service{reorgChainID: 1}
	assert.NotPanics(t, func() { ws.notifyReorg(nil) })
}

// Registering a handler replaces the previous one rather than appending, so a
// re-registration during wiring cannot fan out to a stale config.
func TestWeb3Service_ReorgHandlerReplacement(t *testing.T) {
	ws := &Web3Service{reorgChainID: 1}

	first := 0
	ws.SetReorgHandler(func(int64, []string) { first++ })
	second := 0
	ws.SetReorgHandler(func(int64, []string) { second++ })

	ws.notifyReorg(nil)

	assert.Equal(t, 0, first, "the replaced handler must not be called")
	assert.Equal(t, 1, second)
}
