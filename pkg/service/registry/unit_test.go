package registry

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnit_CanonicalPayload_FixedOrderAndNormalisation(t *testing.T) {
	a := CanonicalAttestPayload("c1", "0xAbC", "AABB", "ipfs://x")
	b := CanonicalAttestPayload("c1", "0xabc", "aabb", "ipfs://x")
	assert.Equal(t, a, b, "addresses and digests must normalise case")
	assert.Equal(t, a, CanonicalAttestPayload("c1", "0xabc", "AABB", "ipfs://x"))

	// Field order is fixed: swapping arguments changes the canonical form.
	c := CanonicalAttestPayload("c2", "0xabc", "aabb", "ipfs://x")
	assert.NotEqual(t, a, c)
	d := CanonicalAttestPayload("c1", "0xabc", "aabb", "ipfs://y")
	assert.NotEqual(t, a, d)

	// The digest is a pure function of the canonical form.
	assert.Equal(t, PayloadDigest(a), PayloadDigest(b))
	assert.NotEqual(t, PayloadDigest(a), PayloadDigest(c))
	assert.Len(t, PayloadDigest(a), 64)
}

func TestUnit_AttestScope(t *testing.T) {
	assert.Equal(t, "chain:31337:registry:0xdead", AttestScope(31337, "0xDeaD"))
	assert.NotEqual(t, AttestScope(1, "0xdead"), AttestScope(2, "0xdead"),
		"the scope must isolate chains")
	assert.NotEqual(t, AttestScope(1, "0xdead"), AttestScope(1, "0xbeef"),
		"the scope must isolate registries")
}

func TestUnit_ReservationKey(t *testing.T) {
	assert.Equal(t,
		ReservationKey(31337, "0xABC", 5),
		ReservationKey(31337, "0xabc", 5))
	assert.NotEqual(t,
		ReservationKey(31337, "0xabc", 5),
		ReservationKey(31338, "0xabc", 5))
}

func TestUnit_DecodeDigest(t *testing.T) {
	ok, err := DecodeDigest(strings.Repeat("ab", 32))
	require.NoError(t, err)
	assert.NotEqual(t, [32]byte{}, ok)

	for _, bad := range []string{
		"",
		"deadbeef",
		strings.Repeat("zz", 32), // right length, not hex
		strings.Repeat("a", 63),  // odd length
	} {
		_, err := DecodeDigest(bad)
		assert.Error(t, err, "digest %q must be rejected", bad)
	}
}

func TestUnit_ErrorClassification(t *testing.T) {
	// Infrastructure failures leave the broadcast outcome unknown.
	assert.True(t, isAmbiguous(fmt.Errorf("send: %w", context.DeadlineExceeded)))
	assert.True(t, isAmbiguous(&net.OpError{Op: "write", Err: errors.New("broken pipe")}))
	assert.True(t, isAmbiguous(&net.DNSError{IsTimeout: true, Err: "timeout"}))
	// Protocol-level rejections answered by the node are definitive.
	assert.False(t, isAmbiguous(errors.New("nonce too low")))
	assert.False(t, isAmbiguous(errors.New("insufficient funds for gas * price + value")))

	// Re-sending the same raw tx that the node already has is a no-op success.
	assert.True(t, isAlreadyKnown(errors.New("already known")))
	assert.True(t, isAlreadyKnown(errors.New("Known transaction")))
	assert.False(t, isAlreadyKnown(errors.New("nonce too low")))
}

func TestUnit_IntentResponse_NilFieldsOmitted(t *testing.T) {
	in := &Intent{
		ContentID: "c1", ChainID: 31337, RegistryAddress: "0xdead",
		Creator: "0xabc", ContentDigest: "aa", Status: StatusPending,
	}
	resp := intentResponse(in)
	assert.Nil(t, resp.Nonce, "an unallocated nonce must stay absent")
	assert.Nil(t, resp.BlockNumber, "an unmined intent must carry no block")
	assert.Equal(t, StatusPending, resp.Status)
	assert.Equal(t, "0xabc", resp.Creator)
}
