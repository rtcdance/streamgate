package storage

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMaxDownloadSize_Value(t *testing.T) {
	assert.Equal(t, int64(1<<30), maxDownloadSize)
}

func TestObjectStorage_InterfaceMethods(t *testing.T) {
	methods := []string{
		"Upload",
		"UploadStream",
		"UploadWithContentType",
		"UploadStreamWithContentType",
		"Download",
		"DownloadStream",
		"Delete",
		"DeleteObjects",
		"ListObjects",
		"Exists",
		"CreateBucket",
		"PresignedURL",
	}
	assert.Equal(t, 12, len(methods))
}
