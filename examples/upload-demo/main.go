package main

import (
	"fmt"
	"strings"
	"time"
)

// Chunked upload splits a large file into small pieces and uploads them
// independently. This allows resumable upload — if the network fails mid-way,
// only the failed chunk needs retransmission.
//
// In production, StreamGate validates NFT ownership before accepting uploads,
// transcodes the uploaded video into HLS, and stores segments in S3/MinIO.

func main() {
	// Simulate a 10MB file split into 1MB chunks
	fileSize := 10 * 1024 * 1024 // 10MB
	chunkSize := 1 * 1024 * 1024 // 1MB chunks
	totalChunks := (fileSize + chunkSize - 1) / chunkSize

	uploadID := "upload_abc123"
	chunkHashes := make([]string, totalChunks)

	fmt.Println("=== Chunked Upload Demo ===")
	fmt.Println()
	fmt.Printf("Upload ID: %s\n", uploadID)
	fmt.Printf("File size: %.1f MB\n", float64(fileSize)/(1024*1024))
	fmt.Printf("Chunk size: %.1f MB\n", float64(chunkSize)/(1024*1024))
	fmt.Printf("Total chunks: %d\n", totalChunks)
	fmt.Println()

	// Step 1: Upload each chunk
	for i := 0; i < totalChunks; i++ {
		chunkHashes[i] = uploadChunk(uploadID, i+1, totalChunks)
		time.Sleep(30 * time.Millisecond) // simulate network
	}

	// Step 2: Complete the upload
	completeUpload(uploadID, chunkHashes)

	fmt.Println()
	fmt.Println("── What just happened ──")
	fmt.Println("1. File was split into", totalChunks, "chunks of 1MB each")
	fmt.Println("2. Each chunk was uploaded independently with hash verification")
	fmt.Println("3. If a chunk failed (network issue), only that chunk retries")
	fmt.Println("4. Final CompleteUpload call assembles all chunks into the file")
	fmt.Println()
	fmt.Println("📖 Next: read pkg/service/upload.go for the real implementation")
}

func uploadChunk(uploadID string, chunkNum, total int) string {
	hash := fmt.Sprintf("hash_%s_chunk_%d", uploadID, chunkNum)
	fmt.Printf("  Uploading chunk %2d/%d ... hash=%s ✓\n", chunkNum, total, hash)
	return hash
}

func completeUpload(uploadID string, chunkHashes []string) {
	fmt.Println()
	fmt.Printf("--- Complete Upload %s ---\n", uploadID)
	fmt.Printf("  Chunks received: %d\n", len(chunkHashes))
	fmt.Printf("  Integrity check: %s\n", strings.Join(chunkHashes, ":"))
	fmt.Println("  Status: ✅ assembled and ready for transcoding")
}
