package upload_test

import (
	"testing"

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

	uploadService := service.NewUploadService(db.GetDB(), storage, "test-bucket")

	// Create test file
	fileContent := []byte("test file content")
	filename := "test.txt"

	// Upload file
	uploadID, err := uploadService.Upload(filename, fileContent, "user-123")
	helpers.AssertNoError(t, err)
	helpers.AssertNotEmpty(t, uploadID)

	// Get upload status
	upload, err := uploadService.GetUploadStatus(uploadID)
	helpers.AssertNoError(t, err)
	helpers.AssertNotNil(t, upload)
	helpers.AssertEqual(t, filename, upload.Filename)
	helpers.AssertEqual(t, "completed", upload.Status)
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

	uploadService := service.NewUploadService(db.GetDB(), storage, "test-bucket")

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
	uploadID, err := uploadService.InitiateChunkedUpload(filename, totalSize, totalChunks, "user-123")
	helpers.AssertNoError(t, err)
	helpers.AssertNotEmpty(t, uploadID)

	// Upload chunks
	for i := 0; i < len(fileContent); i += chunkSize {
		end := i + chunkSize
		if end > len(fileContent) {
			end = len(fileContent)
		}

		chunk := fileContent[i:end]
		err := uploadService.UploadChunk(uploadID, i/chunkSize, chunk)
		helpers.AssertNoError(t, err)
	}

	// Complete upload
	err = uploadService.CompleteChunkedUpload(uploadID, totalChunks)
	helpers.AssertNoError(t, err)

	// Verify upload
	upload, err := uploadService.GetUploadStatus(uploadID)
	helpers.AssertNoError(t, err)
	helpers.AssertNotNil(t, upload)
	helpers.AssertEqual(t, "completed", upload.Status)
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

	uploadService := service.NewUploadService(db.GetDB(), storage, "test-bucket")

	// Upload file
	fileContent := []byte("test file content")
	filename := "test.txt"

	uploadID, err := uploadService.Upload(filename, fileContent, "user-123")
	helpers.AssertNoError(t, err)

	// Get upload status
	status, err := uploadService.GetUploadStatus(uploadID)
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

	uploadService := service.NewUploadService(db.GetDB(), storage, "test-bucket")

	// Upload file
	fileContent := []byte("test file content")
	filename := "test.txt"

	uploadID, err := uploadService.Upload(filename, fileContent, "user-123")
	helpers.AssertNoError(t, err)

	// Delete upload
	err = uploadService.DeleteUpload(uploadID)
	helpers.AssertNoError(t, err)

	// Verify deletion
	_, err = uploadService.GetUploadStatus(uploadID)
	helpers.AssertError(t, err)
}

func TestUploadService_ListUploads(t *testing.T) {
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

	uploadService := service.NewUploadService(db.GetDB(), storage, "test-bucket")

	// Upload multiple files
	for i := 0; i < 3; i++ {
		fileContent := []byte("test file content")
		filename := "test.txt"
		_, err := uploadService.Upload(filename, fileContent, "user-123")
		helpers.AssertNoError(t, err)
	}

	// List uploads
	uploads, err := uploadService.ListUploads("user-123", 10, 0)
	helpers.AssertNoError(t, err)
	helpers.AssertTrue(t, len(uploads) >= 3)
}
