package gateway

import (
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rtcdance/streamgate/pkg/middleware"
	"github.com/rtcdance/streamgate/pkg/service"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// maxUploadSize limits the per-request upload size at the HTTP layer.
// It shadows the per-wallet quota in service.DefaultMaxUploadSize and must
// not exceed it — clients that pass this limit are rejected early without
// consuming storage bandwidth.
const maxUploadSize int64 = 500 * 1024 * 1024 // 500MB

// allowedVideoExtensions is the whitelist of accepted file extensions.
var allowedVideoExtensions = map[string]string{
	".mp4": "mp4", ".webm": "webm", ".avi": "avi",
	".mkv": "mkv", ".mov": "mov", ".mpeg": "mpeg", ".mpg": "mpeg",
}

// videoMagicBytes contains the file signatures for common video formats.
var videoMagicBytes = []struct {
	offset int
	bytes  []byte
	format string
}{
	{0, []byte{0x00, 0x00, 0x00, 0x18, 0x66, 0x74, 0x79, 0x70}, "mp4"},
	{0, []byte{0x66, 0x74, 0x79, 0x70, 0x69, 0x73, 0x6F, 0x6D}, "mp4"}, // ftypisom
	{0, []byte{0x66, 0x74, 0x79, 0x70, 0x6D, 0x70, 0x34, 0x32}, "mp4"}, // ftypmp42
	{0, []byte{0x1A, 0x45, 0xDF, 0xA3}, "webm"},                        // WebM
	{0, []byte{0x00, 0x00, 0x01, 0xBA}, "mpeg"},                        // MPEG
	{0, []byte{0x00, 0x00, 0x01, 0xB3}, "mpeg"},                        // MPEG
	{4, []byte{0x66, 0x74, 0x79, 0x70, 0x4D, 0x53, 0x4E, 0x56}, "mp4"}, // ftypMSNV (WMV)
	{0, []byte{0x2E, 0x52, 0x4D, 0x46}, "rmvb"},                        // RMVB
	{0, []byte{0x52, 0x49, 0x46, 0x46}, "avi"},                         // AVI (RIFF header)
}

// detectVideoFormat checks the first bytes of a reader to confirm it's a known
// video container format. Returns the detected format name or empty string.
// The reader is consumed; the caller must handle that.
func detectVideoFormat(r io.Reader) string {
	header := make([]byte, 16)
	n, _ := io.ReadFull(r, header)
	if n < 4 {
		return ""
	}
	header = header[:n]
	for _, m := range videoMagicBytes {
		if m.offset+len(m.bytes) > len(header) {
			continue
		}
		match := true
		for i, b := range m.bytes {
			if header[m.offset+i] != b {
				match = false
				break
			}
		}
		if match {
			return m.format
		}
	}
	// MP4 fallback: check ftyp box at offset 4
	if n >= 8 && string(header[4:8]) == "ftyp" {
		return "mp4"
	}
	return ""
}

// readFileHeader is a helper used by callers that need to read magic bytes
// before passing the remaining stream to storage. Returns the format name.
// The returned io.Reader yields the full content (header + rest).
func readFileHeader(r io.Reader) (format string, combined io.Reader, err error) {
	header := make([]byte, 16)
	n, err := io.ReadFull(r, header)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return "", nil, err
	}
	header = header[:n]
	format = detectVideoFormat(bytesReader(header))
	combined = io.MultiReader(bytesReader(header), r)
	return format, combined, nil
}

// bytesReader is a small helper to avoid importing bytes for a single use.
func bytesReader(b []byte) io.Reader { return &byteReader{b: b} }

type byteReader struct {
	b   []byte
	off int
}

func (r *byteReader) Read(p []byte) (int, error) {
	if r.off >= len(r.b) {
		return 0, io.EOF
	}
	n := copy(p, r.b[r.off:])
	r.off += n
	return n, nil
}

// sanitizeObjectKey rejects values containing path traversal sequences.
func sanitizeObjectKey(val string) (string, bool) {
	if strings.Contains(val, "..") || strings.ContainsAny(val, "/\\") {
		return "", false
	}
	return val, true
}

func formatAllowedExtensions() string {
	exts := make([]string, 0, len(allowedVideoExtensions))
	for ext := range allowedVideoExtensions {
		exts = append(exts, ext)
	}
	return strings.Join(exts, ", ")
}

// RegisterUploadRoutes registers file upload routes.
// If uploadSvc is nil, all routes return 503 Service Unavailable.
func RegisterUploadRoutes(router gin.IRouter, log *zap.Logger, uploadSvc *service.UploadService) {
	upload := router.Group(APIPrefix + "/upload")
	upload.Use(func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxUploadSize)
		c.Next()
	})

	upload.POST("", handleWholeFileUpload(uploadSvc, log))
	upload.GET("/list", handleUploadList(uploadSvc, log))
	upload.POST("/init", handleChunkedUploadInit(uploadSvc, log))
	upload.POST("/chunk", handleChunkUpload(uploadSvc, log))
	upload.POST("/:id/complete", handleChunkedUploadComplete(uploadSvc, log))
	upload.POST("/:id/complete-upload", handleCompleteUpload(uploadSvc, log))
	upload.GET("/:id/status", handleUploadStatus(uploadSvc, log))
	upload.GET("/:id/download-url", handleDownloadURL(uploadSvc, log))
	upload.POST("/:id/batch-chunks", handleBatchChunkUpload(uploadSvc, log))
	upload.GET("/:id/chunks", handleChunkStatuses(uploadSvc, log))
	upload.POST("/presigned-init", handlePresignedUploadInit(uploadSvc, log))
	upload.POST("/:id/complete-presigned", handleCompletePresignedUpload(uploadSvc, log))
	upload.DELETE("/:id", handleDeleteUpload(uploadSvc, log))

	log.Info("Upload routes registered")
}

func handleWholeFileUpload(uploadSvc *service.UploadService, log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		if uploadSvc == nil {
			abortWithError(c, http.StatusServiceUnavailable, ErrUploadFailed, "upload service unavailable")
			return
		}

		wallet := middleware.GetWalletAddress(c)
		if wallet == "" {
			abortWithError(c, http.StatusUnauthorized, ErrUnauthorized, "wallet authentication required")
			return
		}

		file, err := c.FormFile("file")
		if err != nil {
			abortWithErrorDetail(c, http.StatusBadRequest, ErrInvalidRequest, "no file provided", err.Error())
			return
		}
		if file.Size > maxUploadSize {
			abortWithError(c, http.StatusRequestEntityTooLarge, ErrPayloadTooLarge,
				fmt.Sprintf("file size %d exceeds maximum allowed size %d", file.Size, maxUploadSize))
			return
		}

		ext := strings.ToLower(filepath.Ext(file.Filename))
		if _, allowed := allowedVideoExtensions[ext]; !allowed && ext != "" {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest,
				fmt.Sprintf("file extension %s not allowed; accepted: mp4, webm, avi, mkv, mov, mpeg", ext))
			return
		}

		src, err := file.Open()
		if err != nil {
			abortWithError(c, http.StatusInternalServerError, ErrUploadFailed, "failed to read file")
			return
		}
		defer func() { _ = src.Close() }()

		format, videoSrc, err := readFileHeader(src)
		if err != nil {
			abortWithError(c, http.StatusInternalServerError, ErrUploadFailed, "failed to read file header")
			return
		}
		if format == "" && file.Size > 0 {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "unsupported video format; accepted: mp4, webm, avi, mkv, mov, mpeg")
			return
		}

		if ext != "" {
			expectedFormat := allowedVideoExtensions[ext]
			if expectedFormat != "" && format != expectedFormat {
				abortWithError(c, http.StatusBadRequest, ErrInvalidRequest,
					fmt.Sprintf("file extension .%s does not match actual format %s", ext, format))
				return
			}
		}

		if err := uploadSvc.CheckStorageQuota(c.Request.Context(), wallet, file.Size); err != nil {
			abortWithError(c, http.StatusInsufficientStorage, ErrUploadFailed, "storage quota exceeded")
			return
		}

		uploadID, err := uploadSvc.UploadStream(c.Request.Context(), file.Filename, videoSrc, file.Size, wallet)
		if err != nil {
			abortWithErrorDetail(c, http.StatusInternalServerError, ErrUploadFailed, "upload failed", err.Error())
			return
		}
		respondCreated(c, gin.H{
			"upload_id": uploadID,
			"filename":  file.Filename,
			"size":      file.Size,
			"format":    format,
			"status":    "completed",
		})
	}
}

func handleChunkStatuses(uploadSvc *service.UploadService, log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		if uploadSvc == nil {
			abortWithError(c, http.StatusServiceUnavailable, ErrUploadFailed, "upload service unavailable")
			return
		}

		wallet := middleware.GetWalletAddress(c)
		if wallet == "" {
			abortWithError(c, http.StatusUnauthorized, ErrUnauthorized, "wallet authentication required")
			return
		}

		uploadID := c.Param("id")
		if _, ok := sanitizeObjectKey(uploadID); !ok {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "upload_id contains invalid characters")
			return
		}

		info, err := uploadSvc.GetUploadStatus(c.Request.Context(), uploadID)
		if err != nil {
			abortWithErrorDetail(c, http.StatusNotFound, ErrNotFound, "upload not found", err.Error())
			return
		}
		if !strings.EqualFold(info.OwnerID, wallet) {
			abortWithError(c, http.StatusForbidden, ErrForbidden, "not authorized to view this upload")
			return
		}

		chunks, err := uploadSvc.GetChunkStatuses(c.Request.Context(), uploadID)
		if err != nil {
			abortWithErrorDetail(c, http.StatusInternalServerError, ErrUploadFailed, "failed to get chunk statuses", err.Error())
			return
		}

		uploaded := 0
		for _, ch := range chunks {
			if ch.Uploaded {
				uploaded++
			}
		}

		respondOK(c, gin.H{
			"upload_id":       uploadID,
			"total_chunks":    len(chunks),
			"uploaded_chunks": uploaded,
			"chunks":          chunks,
		})
	}
}

func handleDeleteUpload(uploadSvc *service.UploadService, log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		if uploadSvc == nil {
			abortWithError(c, http.StatusServiceUnavailable, ErrUploadFailed, "upload service unavailable")
			return
		}

		wallet := middleware.GetWalletAddress(c)
		if wallet == "" {
			abortWithError(c, http.StatusUnauthorized, ErrUnauthorized, "wallet authentication required")
			return
		}

		uploadID := c.Param("id")
		if _, ok := sanitizeObjectKey(uploadID); !ok {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "upload_id contains invalid characters")
			return
		}

		info, err := uploadSvc.GetUploadStatus(c.Request.Context(), uploadID)
		if err != nil {
			abortWithErrorDetail(c, http.StatusNotFound, ErrNotFound, "upload not found", err.Error())
			return
		}
		if !strings.EqualFold(info.OwnerID, wallet) {
			abortWithError(c, http.StatusForbidden, ErrForbidden, "not authorized to delete this upload")
			return
		}

		if err := uploadSvc.DeleteUpload(c.Request.Context(), uploadID); err != nil {
			abortWithErrorDetail(c, http.StatusInternalServerError, ErrUploadFailed, "failed to delete upload", err.Error())
			return
		}

		respondNoContent(c)
	}
}

func handleChunkedUploadInit(uploadSvc *service.UploadService, log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		if uploadSvc == nil {
			abortWithError(c, http.StatusServiceUnavailable, ErrUploadFailed, "upload service unavailable")
			return
		}

		wallet := middleware.GetWalletAddress(c)
		if wallet == "" {
			abortWithError(c, http.StatusUnauthorized, ErrUnauthorized, "wallet authentication required")
			return
		}
		var req struct {
			Filename    string `json:"filename" binding:"required"`
			TotalSize   int64  `json:"total_size" binding:"required"`
			TotalChunks int    `json:"total_chunks" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "filename, total_size, and total_chunks are required")
			return
		}
		if req.TotalSize > maxUploadSize {
			abortWithError(c, http.StatusRequestEntityTooLarge, ErrPayloadTooLarge, fmt.Sprintf("file size %d exceeds maximum allowed size %d", req.TotalSize, maxUploadSize))
			return
		}
		if req.TotalSize <= 0 {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "total_size must be positive")
			return
		}
		if req.TotalChunks <= 0 {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "total_chunks must be positive")
			return
		}
		if req.TotalChunks > 10000 {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "total_chunks exceeds maximum allowed")
			return
		}

		ext := strings.ToLower(filepath.Ext(req.Filename))
		if _, ok := allowedVideoExtensions[ext]; !ok {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, fmt.Sprintf("file extension %q not allowed; accepted: %s", ext, formatAllowedExtensions()))
			return
		}

		uploadID, err := uploadSvc.InitiateChunkedUpload(c.Request.Context(), req.Filename, req.TotalSize, req.TotalChunks, wallet)
		if err != nil {
			abortWithErrorDetail(c, http.StatusInternalServerError, ErrUploadFailed, "failed to initiate chunked upload", err.Error())
			return
		}
		respondCreated(c, gin.H{
			"upload_id":    uploadID,
			"status":       "uploading",
			"total_chunks": req.TotalChunks,
		})
	}
}

func handleChunkUpload(uploadSvc *service.UploadService, log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		if uploadSvc == nil {
			abortWithError(c, http.StatusServiceUnavailable, ErrUploadFailed, "upload service unavailable")
			return
		}

		wallet := middleware.GetWalletAddress(c)
		if wallet == "" {
			abortWithError(c, http.StatusUnauthorized, ErrUnauthorized, "wallet authentication required")
			return
		}

		uploadID := c.PostForm("upload_id")
		chunkIndexStr := c.PostForm("chunk_index")
		if uploadID == "" || chunkIndexStr == "" {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "upload_id and chunk_index are required")
			return
		}
		if _, ok := sanitizeObjectKey(uploadID); !ok {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "upload_id contains invalid characters")
			return
		}
		chunkIndex, err := strconv.Atoi(chunkIndexStr)
		if err != nil {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "invalid chunk_index")
			return
		}
		if chunkIndex < 0 {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "chunk_index must be non-negative")
			return
		}
		file, err := c.FormFile("chunk")
		if err != nil {
			abortWithErrorDetail(c, http.StatusBadRequest, ErrInvalidRequest, "no chunk file provided", err.Error())
			return
		}
		if file.Size > maxUploadSize {
			abortWithError(c, http.StatusRequestEntityTooLarge, ErrPayloadTooLarge, "chunk exceeds maximum allowed size")
			return
		}
		src, err := file.Open()
		if err != nil {
			abortWithError(c, http.StatusInternalServerError, ErrUploadFailed, "failed to read chunk")
			return
		}
		defer func() { _ = src.Close() }()

		var reader io.Reader = src
		if chunkIndex == 0 {
			detected, combined, _ := readFileHeader(src)
			reader = combined
			if detected == "" {
				abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "first chunk does not match any known video format")
				return
			}
		}

		if err := uploadSvc.UploadChunkStream(c.Request.Context(), uploadID, chunkIndex, reader, file.Size, wallet); err != nil {
			abortWithErrorDetail(c, http.StatusInternalServerError, ErrUploadFailed, "chunk upload failed", err.Error())
			return
		}
		respondOK(c, gin.H{
			"upload_id":   uploadID,
			"chunk_index": chunkIndex,
			"size":        file.Size,
		})
	}
}

func handleBatchChunkUpload(uploadSvc *service.UploadService, log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		if uploadSvc == nil {
			abortWithError(c, http.StatusServiceUnavailable, ErrUploadFailed, "upload service unavailable")
			return
		}

		wallet := middleware.GetWalletAddress(c)
		if wallet == "" {
			abortWithError(c, http.StatusUnauthorized, ErrUnauthorized, "wallet authentication required")
			return
		}

		uploadID := c.Param("id")
		if _, ok := sanitizeObjectKey(uploadID); !ok {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "upload_id contains invalid characters")
			return
		}

		form, err := c.MultipartForm()
		if err != nil {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "multipart form required")
			return
		}

		type chunkResult struct {
			Index  int    `json:"index"`
			Status string `json:"status"`
			Error  string `json:"error,omitempty"`
		}

		results := make([]chunkResult, 0, len(form.File))
		var hasErr bool
		for fieldName, headers := range form.File {
			idx, parseErr := strconv.Atoi(fieldName)
			if parseErr != nil || idx < 0 || len(headers) == 0 {
				continue
			}
			f, openErr := headers[0].Open()
			if openErr != nil {
				results = append(results, chunkResult{Index: idx, Status: "failed", Error: openErr.Error()})
				hasErr = true
				continue
			}
			uploadErr := uploadSvc.UploadChunkStream(c.Request.Context(), uploadID, idx, f, headers[0].Size, wallet)
			_ = f.Close()
			if uploadErr != nil {
				results = append(results, chunkResult{Index: idx, Status: "failed", Error: uploadErr.Error()})
				hasErr = true
			} else {
				results = append(results, chunkResult{Index: idx, Status: "uploaded"})
			}
		}

		status := http.StatusOK
		if hasErr {
			status = http.StatusMultiStatus
		}
		respond(c, status, gin.H{
			"upload_id": uploadID,
			"results":   results,
		})
	}
}

func handleChunkedUploadComplete(uploadSvc *service.UploadService, log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		if uploadSvc == nil {
			abortWithError(c, http.StatusServiceUnavailable, ErrUploadFailed, "upload service unavailable")
			return
		}

		wallet := middleware.GetWalletAddress(c)
		if wallet == "" {
			abortWithError(c, http.StatusUnauthorized, ErrUnauthorized, "wallet authentication required")
			return
		}

		uploadID := c.Param("id")
		if _, ok := sanitizeObjectKey(uploadID); !ok {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "upload_id contains invalid characters")
			return
		}

		uploadInfo, err := uploadSvc.GetUploadStatus(c.Request.Context(), uploadID)
		if err != nil {
			abortWithError(c, http.StatusNotFound, ErrNotFound, "upload not found")
			return
		}
		if !strings.EqualFold(uploadInfo.OwnerID, wallet) {
			abortWithError(c, http.StatusForbidden, ErrForbidden, "not authorized to complete this upload")
			return
		}

		var req struct {
			TotalChunks int `json:"total_chunks" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "total_chunks is required")
			return
		}

		if err := uploadSvc.CompleteChunkedUpload(c.Request.Context(), uploadID, req.TotalChunks); err != nil {
			abortWithErrorDetail(c, http.StatusInternalServerError, ErrUploadFailed, "failed to complete chunked upload", err.Error())
			return
		}

		info, err := uploadSvc.GetUploadStatus(c.Request.Context(), uploadID)
		if err != nil {
			respondOK(c, gin.H{"upload_id": uploadID, "status": "completed"})
			return
		}
		respondOK(c, gin.H{
			"upload_id": uploadID,
			"status":    info.Status,
			"hash":      info.Hash,
			"size":      info.Size,
		})
	}
}

func handlePresignedUploadInit(uploadSvc *service.UploadService, log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		if uploadSvc == nil {
			abortWithError(c, http.StatusServiceUnavailable, ErrUploadFailed, "upload service unavailable")
			return
		}

		wallet := middleware.GetWalletAddress(c)
		if wallet == "" {
			abortWithError(c, http.StatusUnauthorized, ErrUnauthorized, "wallet authentication required")
			return
		}

		var req struct {
			Filename    string `json:"filename" binding:"required"`
			TotalSize   int64  `json:"total_size" binding:"required"`
			ContentType string `json:"content_type"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "filename and total_size are required")
			return
		}
		if req.TotalSize <= 0 {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "total_size must be positive")
			return
		}
		if req.TotalSize > maxUploadSize {
			abortWithError(c, http.StatusRequestEntityTooLarge, ErrPayloadTooLarge,
				fmt.Sprintf("file size %d exceeds maximum allowed size %d", req.TotalSize, maxUploadSize))
			return
		}

		ext := filepath.Ext(req.Filename)
		if _, allowed := allowedVideoExtensions[ext]; !allowed && ext != "" {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest,
				fmt.Sprintf("file extension %s not allowed; accepted: mp4, webm, avi, mkv, mov, mpeg", ext))
			return
		}

		uploadID, presignedURL, storageKey, err := uploadSvc.InitiatePresignedUpload(
			c.Request.Context(), req.Filename, req.TotalSize, req.ContentType, wallet)
		if err != nil {
			abortWithErrorDetail(c, http.StatusInternalServerError, ErrUploadFailed, "failed to initiate presigned upload", err.Error())
			return
		}

		respondCreated(c, gin.H{
			"upload_id":     uploadID,
			"presigned_url": presignedURL,
			"storage_key":   storageKey,
			"expires_in":    int((2 * time.Hour).Seconds()),
			"status":        "url_generated",
		})
	}
}

func handleCompletePresignedUpload(uploadSvc *service.UploadService, log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		if uploadSvc == nil {
			abortWithError(c, http.StatusServiceUnavailable, ErrUploadFailed, "upload service unavailable")
			return
		}

		wallet := middleware.GetWalletAddress(c)
		if wallet == "" {
			abortWithError(c, http.StatusUnauthorized, ErrUnauthorized, "wallet authentication required")
			return
		}

		uploadID := c.Param("id")
		if _, ok := sanitizeObjectKey(uploadID); !ok {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "upload_id contains invalid characters")
			return
		}
		contentID, err := uploadSvc.CompleteUploadWithTx(c.Request.Context(), uploadID)
		if err != nil {
			abortWithErrorDetail(c, http.StatusInternalServerError, ErrUploadFailed, "failed to complete presigned upload", err.Error())
			return
		}

		info, _ := uploadSvc.GetUploadStatus(c.Request.Context(), uploadID)
		status := "completed"
		if info != nil {
			status = info.Status
		}
		respondOK(c, gin.H{
			"upload_id":  uploadID,
			"content_id": contentID,
			"status":     status,
		})
	}
}

func handleCompleteUpload(uploadSvc *service.UploadService, log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		if uploadSvc == nil {
			abortWithError(c, http.StatusServiceUnavailable, ErrUploadFailed, "upload service unavailable")
			return
		}

		wallet := middleware.GetWalletAddress(c)
		if wallet == "" {
			abortWithError(c, http.StatusUnauthorized, ErrUnauthorized, "wallet authentication required")
			return
		}

		uploadID := c.Param("id")
		if _, ok := sanitizeObjectKey(uploadID); !ok {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "upload_id contains invalid characters")
			return
		}

		info, err := uploadSvc.GetUploadStatus(c.Request.Context(), uploadID)
		if err != nil {
			abortWithError(c, http.StatusNotFound, ErrNotFound, "upload not found")
			return
		}
		if !strings.EqualFold(info.OwnerID, wallet) {
			abortWithError(c, http.StatusForbidden, ErrForbidden, "not authorized to complete this upload")
			return
		}

		contentID, err := uploadSvc.CompleteUploadWithTx(c.Request.Context(), uploadID)
		if err != nil {
			abortWithErrorDetail(c, http.StatusInternalServerError, ErrUploadFailed, "failed to complete upload", err.Error())
			return
		}

		info, err = uploadSvc.GetUploadStatus(c.Request.Context(), uploadID)
		if err != nil {
			respondOK(c, gin.H{"upload_id": uploadID, "content_id": contentID, "status": "processed"})
			return
		}
		respondOK(c, gin.H{
			"upload_id":  uploadID,
			"content_id": contentID,
			"status":     info.Status,
		})
	}
}

func handleUploadStatus(uploadSvc *service.UploadService, log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		if uploadSvc == nil {
			abortWithError(c, http.StatusServiceUnavailable, ErrUploadFailed, "upload service unavailable")
			return
		}

		wallet := middleware.GetWalletAddress(c)
		if wallet == "" {
			abortWithError(c, http.StatusUnauthorized, ErrUnauthorized, "wallet authentication required")
			return
		}

		uploadID := c.Param("id")
		if _, ok := sanitizeObjectKey(uploadID); !ok {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "upload_id contains invalid characters")
			return
		}
		info, err := uploadSvc.GetUploadStatus(c.Request.Context(), uploadID)
		if err != nil {
			abortWithError(c, http.StatusNotFound, ErrNotFound, "upload not found")
			return
		}
		if !strings.EqualFold(info.OwnerID, wallet) {
			abortWithError(c, http.StatusForbidden, ErrForbidden, "not authorized to view this upload")
			return
		}
		progress, _ := uploadSvc.GetUploadProgress(c.Request.Context(), uploadID)
		respondOK(c, gin.H{
			"upload_id":    info.ID,
			"filename":     info.Filename,
			"size":         info.Size,
			"content_type": info.ContentType,
			"hash":         info.Hash,
			"status":       info.Status,
			"progress":     progress,
			"owner_id":     info.OwnerID,
			"created_at":   info.CreatedAt.Format(time.RFC3339),
		})
	}
}

func handleUploadList(uploadSvc *service.UploadService, log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		if uploadSvc == nil {
			abortWithError(c, http.StatusServiceUnavailable, ErrUploadFailed, "upload service unavailable")
			return
		}

		wallet := middleware.GetWalletAddress(c)
		if wallet == "" {
			abortWithError(c, http.StatusUnauthorized, ErrUnauthorized, "wallet address required")
			return
		}

		limit := 50
		offset := 0
		if v := c.Query("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
				limit = n
			}
		}
		if v := c.Query("offset"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n >= 0 {
				offset = n
			}
		}
		uploads, err := uploadSvc.ListUploads(c.Request.Context(), wallet, limit, offset)
		if err != nil {
			abortWithErrorDetail(c, http.StatusInternalServerError, ErrUploadFailed, "failed to list uploads", err.Error())
			return
		}
		if uploads == nil {
			uploads = []*service.UploadInfo{}
		}

		type uploadItem struct {
			ID        string `json:"id"`
			Filename  string `json:"filename"`
			Size      int64  `json:"size"`
			Status    string `json:"status"`
			CreatedAt string `json:"created_at"`
		}
		items := make([]uploadItem, 0, len(uploads))
		for _, u := range uploads {
			items = append(items, uploadItem{
				ID: u.ID, Filename: u.Filename, Size: u.Size,
				Status: u.Status, CreatedAt: u.CreatedAt.Format(time.RFC3339),
			})
		}

		respondOK(c, gin.H{"uploads": items, "total": len(items)})
	}
}

func handleDownloadURL(uploadSvc *service.UploadService, log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		if uploadSvc == nil {
			abortWithError(c, http.StatusServiceUnavailable, ErrUploadFailed, "upload service unavailable")
			return
		}

		wallet := middleware.GetWalletAddress(c)
		if wallet == "" {
			abortWithError(c, http.StatusUnauthorized, ErrUnauthorized, "wallet authentication required")
			return
		}

		uploadID := c.Param("id")
		if _, ok := sanitizeObjectKey(uploadID); !ok {
			abortWithError(c, http.StatusBadRequest, ErrInvalidRequest, "upload_id contains invalid characters")
			return
		}
		expiry := 1 * time.Hour
		if v := c.Query("expiry_minutes"); v != "" {
			if mins, err := strconv.Atoi(v); err == nil && mins > 0 && mins <= 60 {
				expiry = time.Duration(mins) * time.Minute
			}
		}

		url, err := uploadSvc.GetDownloadURL(c.Request.Context(), uploadID, expiry, wallet)
		if err != nil {
			abortWithErrorDetail(c, http.StatusInternalServerError, ErrUploadFailed, "failed to generate download URL", err.Error())
			return
		}
		respondOK(c, gin.H{
			"upload_id":    uploadID,
			"download_url": url,
			"expires_in":   int(expiry.Minutes()),
		})
	}
}
