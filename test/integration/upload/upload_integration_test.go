package upload_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"streamgate/pkg/service"
	"streamgate/test/helpers"
)

func TestUploadService_SingleFileUpload(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	objStorage := helpers.SetupTestStorage(t)
	if objStorage == nil {
		return
	}
	defer helpers.CleanupTestStorage(t, objStorage)

	uploadService := service.NewUploadService(db, objStorage, "test-bucket")

	// Create test file
	fileContent := []byte("test file content")
	filename := "test.txt"

	// Upload file
	uploadID, err := uploadService.Upload(context.Background(), filename, fileContent, "user-123")
	require.NoError(t, err)
	require.NotEmpty(t, uploadID)

	// Get upload status
	upload, err := uploadService.GetUploadStatus(context.Background(), uploadID)
	require.NoError(t, err)
	require.NotNil(t, upload)
	require.Equal(t, filename, upload.Filename)
	require.Equal(t, "completed", upload.Status)
}

func TestUploadService_ChunkedUpload(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	objStorage := helpers.SetupTestStorage(t)
	if objStorage == nil {
		return
	}
	defer helpers.CleanupTestStorage(t, objStorage)

	uploadService := service.NewUploadService(db, objStorage, "test-bucket")

	// Create large test file
	fileContent := make([]byte, 10*1024*1024) // 10MB
	for i := range fileContent {
		fileContent[i] = byte(i % 256)
	}

	filename := "large_file.bin"
	totalSize := int64(len(fileContent))
	chunkSize := 1024 * 1024 // 1MB chunks
	totalChunks := len(fileContent) / chunkSize

	// Start chunked upload
	uploadID, err := uploadService.InitiateChunkedUpload(context.Background(), filename, totalSize, totalChunks, "user-123")
	require.NoError(t, err)
	require.NotEmpty(t, uploadID)

	// Upload chunks
	for i := 0; i < len(fileContent); i += chunkSize {
		end := i + chunkSize
		if end > len(fileContent) {
			end = len(fileContent)
		}

		chunk := fileContent[i:end]
		err := uploadService.UploadChunk(context.Background(), uploadID, i/chunkSize, chunk)
		require.NoError(t, err)
	}

	// Complete upload
	err = uploadService.CompleteChunkedUpload(context.Background(), uploadID, totalChunks)
	require.NoError(t, err)

	// Verify upload
	upload, err := uploadService.GetUploadStatus(context.Background(), uploadID)
	require.NoError(t, err)
	require.NotNil(t, upload)
	require.Equal(t, "completed", upload.Status)
}

func TestUploadService_GetUploadStatus(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	objStorage := helpers.SetupTestStorage(t)
	if objStorage == nil {
		return
	}
	defer helpers.CleanupTestStorage(t, objStorage)

	uploadService := service.NewUploadService(db, objStorage, "test-bucket")

	// Upload file
	fileContent := []byte("test file content")
	filename := "test.txt"

	uploadID, err := uploadService.Upload(context.Background(), filename, fileContent, "user-123")
	require.NoError(t, err)

	// Get upload status
	status, err := uploadService.GetUploadStatus(context.Background(), uploadID)
	require.NoError(t, err)
	require.NotNil(t, status)
	require.Equal(t, "completed", status.Status)
}

func TestUploadService_DeleteUpload(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	objStorage := helpers.SetupTestStorage(t)
	if objStorage == nil {
		return
	}
	defer helpers.CleanupTestStorage(t, objStorage)

	uploadService := service.NewUploadService(db, objStorage, "test-bucket")

	// Upload file
	fileContent := []byte("test file content")
	filename := "test.txt"

	uploadID, err := uploadService.Upload(context.Background(), filename, fileContent, "user-123")
	require.NoError(t, err)

	// Delete upload
	err = uploadService.DeleteUpload(context.Background(), uploadID)
	require.NoError(t, err)

	// Verify deletion
	_, err = uploadService.GetUploadStatus(context.Background(), uploadID)
	require.Error(t, err)
}

func TestUploadService_ListUploads(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	objStorage := helpers.SetupTestStorage(t)
	if objStorage == nil {
		return
	}
	defer helpers.CleanupTestStorage(t, objStorage)

	uploadService := service.NewUploadService(db, objStorage, "test-bucket")

	// Upload multiple files
	for i := 0; i < 3; i++ {
		fileContent := []byte("test file content")
		filename := "test.txt"
		_, err := uploadService.Upload(context.Background(), filename, fileContent, "user-123")
		require.NoError(t, err)
	}

	// List uploads
	uploads, err := uploadService.ListUploads(context.Background(), "user-123", 10, 0)
	require.NoError(t, err)
	require.True(t, len(uploads) >= 3)
}
