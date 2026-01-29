package upload_test

import (
	"bytes"
	"context"
	"testing"

	"streamgate/pkg/models"
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

	storage := helpers.SetupTestStorage(t)
	if storage == nil {
		return
	}
	defer helpers.CleanupTestStorage(t, storage)

	uploadService := service.NewUploadService(db, storage)

	// Create test file
	fileContent := []byte("test file content")
	filename := "test.txt"

	// Upload file
	upload, err := uploadService.Upload(context.Background(), filename, bytes.NewReader(fileContent))
	helpers.AssertNoError(t, err)
	helpers.AssertNotNil(t, upload)
	helpers.AssertEqual(t, filename, upload.Filename)
}

func TestUploadService_ChunkedUpload(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	storage := helpers.SetupTestStorage(t)
	if storage == nil {
		return
	}
	defer helpers.CleanupTestStorage(t, storage)

	uploadService := service.NewUploadService(db, storage)

	// Create large test file
	fileContent := make([]byte, 10*1024*1024) // 10MB
	for i := range fileContent {
		fileContent[i] = byte(i % 256)
	}

	filename := "large_file.bin"

	// Start chunked upload
	uploadID, err := uploadService.StartChunkedUpload(context.Background(), filename)
	helpers.AssertNoError(t, err)
	helpers.AssertNotEmpty(t, uploadID)

	// Upload chunks
	chunkSize := 1024 * 1024 // 1MB chunks
	for i := 0; i < len(fileContent); i += chunkSize {
		end := i + chunkSize
		if end > len(fileContent) {
			end = len(fileContent)
		}

		chunk := fileContent[i:end]
		err := uploadService.UploadChunk(context.Background(), uploadID, i/chunkSize, bytes.NewReader(chunk))
		helpers.AssertNoError(t, err)
	}

	// Complete upload
	upload, err := uploadService.CompleteChunkedUpload(context.Background(), uploadID)
	helpers.AssertNoError(t, err)
	helpers.AssertNotNil(t, upload)
}

func TestUploadService_GetUploadStatus(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	storage := helpers.SetupTestStorage(t)
	if storage == nil {
		return
	}
	defer helpers.CleanupTestStorage(t, storage)

	uploadService := service.NewUploadService(db, storage)

	// Upload file
	fileContent := []byte("test file content")
	filename := "test.txt"

	upload, err := uploadService.Upload(context.Background(), filename, bytes.NewReader(fileContent))
	helpers.AssertNoError(t, err)

	// Get upload status
	status, err := uploadService.GetUploadStatus(context.Background(), upload.ID)
	helpers.AssertNoError(t, err)
	helpers.AssertNotNil(t, status)
	helpers.AssertEqual(t, "completed", status.Status)
}

func TestUploadService_DeleteUpload(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	storage := helpers.SetupTestStorage(t)
	if storage == nil {
		return
	}
	defer helpers.CleanupTestStorage(t, storage)

	uploadService := service.NewUploadService(db, storage)

	// Upload file
	fileContent := []byte("test file content")
	filename := "test.txt"

	upload, err := uploadService.Upload(context.Background(), filename, bytes.NewReader(fileContent))
	helpers.AssertNoError(t, err)

	// Delete upload
	err = uploadService.DeleteUpload(context.Background(), upload.ID)
	helpers.AssertNoError(t, err)

	// Verify deletion
	_, err = uploadService.GetUploadStatus(context.Background(), upload.ID)
	helpers.AssertError(t, err)
}

func TestUploadService_ResumableUpload(t *testing.T) {
	// Setup
	db := helpers.SetupTestDB(t)
	if db == nil {
		return
	}
	defer helpers.CleanupTestDB(t, db)

	storage := helpers.SetupTestStorage(t)
	if storage == nil {
		return
	}
	defer helpers.CleanupTestStorage(t, storage)

	uploadService := service.NewUploadService(db, storage)

	// Start resumable upload
	filename := "resumable_file.bin"
	uploadID, err := uploadService.StartChunkedUpload(context.Background(), filename)
	helpers.AssertNoError(t, err)

	// Upload first chunk
	chunk1 := []byte("chunk1 content")
	err = uploadService.UploadChunk(context.Background(), uploadID, 0, bytes.NewReader(chunk1))
	helpers.AssertNoError(t, err)

	// Get upload progress
	progress, err := uploadService.GetUploadProgress(context.Background(), uploadID)
	helpers.AssertNoError(t, err)
	helpers.AssertTrue(t, progress > 0)
}
