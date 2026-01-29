package upload

// ChunkedUploader handles chunked uploads
type ChunkedUploader struct{}

// UploadChunk uploads a chunk
func (cu *ChunkedUploader) UploadChunk(uploadID string, chunkIndex int, data []byte) error {
	return nil
}

// CompleteUpload completes chunked upload
func (cu *ChunkedUploader) CompleteUpload(uploadID string) (string, error) {
	return "file_id", nil
}
