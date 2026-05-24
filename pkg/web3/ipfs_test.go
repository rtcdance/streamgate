package web3

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestIPFSGateway_GetGatewayURL(t *testing.T) {
	gw := &IPFSGateway{URL: "https://ipfs.io"}
	url := gw.GetGatewayURL("QmTest123")
	assert.Equal(t, "https://ipfs.io/ipfs/QmTest123", url)
}

func TestGetHTTPGatewayURL(t *testing.T) {
	url := GetHTTPGatewayURL("QmTest123")
	assert.Equal(t, "https://ipfs.io/ipfs/QmTest123", url)
}

func TestGetCloudflareGatewayURL(t *testing.T) {
	url := GetCloudflareGatewayURL("QmTest123")
	assert.Equal(t, "https://cloudflare-ipfs.com/ipfs/QmTest123", url)
}

func TestNewHybridStorage(t *testing.T) {
	hs := NewHybridStorage(nil, nil, 1024)
	assert.NotNil(t, hs)
	assert.Equal(t, int64(1024), hs.threshold)
}

func TestHybridStorage_Store_Local(t *testing.T) {
	hs := NewHybridStorage(nil, zap.NewNop(), 1024)

	location, err := hs.Store(nil, "test.txt", make([]byte, 100))
	assert.NoError(t, err)
	assert.Equal(t, "local", location.Storage)
	assert.Equal(t, "/files/test.txt", location.URL)
	assert.Equal(t, int64(100), location.Size)
}

func TestHybridStorage_Store_IPFS(t *testing.T) {
	hs := NewHybridStorage(nil, zap.NewNop(), 10)

	location, err := hs.Store(nil, "big.txt", make([]byte, 100))
	assert.Error(t, err)
	assert.Nil(t, location)
}

func TestFileInfo_Fields(t *testing.T) {
	fi := &FileInfo{
		CID:     "QmTest",
		Size:    1024,
		Type:    "file",
		Hash:    "hash123",
		Blocks:  5,
		CumSize: 2048,
	}
	assert.Equal(t, "QmTest", fi.CID)
	assert.Equal(t, int64(1024), fi.Size)
	assert.Equal(t, "file", fi.Type)
}

func TestStorageLocation_Fields(t *testing.T) {
	sl := &StorageLocation{
		Filename: "test.mp4",
		Size:     5000,
		Storage:  "ipfs",
		CID:      "QmTest",
		URL:      "https://ipfs.io/ipfs/QmTest",
	}
	assert.Equal(t, "ipfs", sl.Storage)
	assert.Equal(t, "QmTest", sl.CID)
}
