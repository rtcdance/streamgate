package upload

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"streamgate/pkg/core"
	"streamgate/pkg/monitoring"
	"streamgate/pkg/service"

	"go.uber.org/zap"
)

type UploadHandler struct {
	svc              *service.UploadService
	logger           *zap.Logger
	kernel           *core.Microkernel
	metricsCollector *monitoring.MetricsCollector
}

func NewUploadHandler(svc *service.UploadService, logger *zap.Logger, kernel *core.Microkernel) *UploadHandler {
	return &UploadHandler{
		svc:              svc,
		logger:           logger,
		kernel:           kernel,
		metricsCollector: monitoring.NewMetricsCollector(logger),
	}
}

func (h *UploadHandler) HealthHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := h.kernel.Health(ctx); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "unhealthy", "error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
}

func (h *UploadHandler) ReadyHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
}

func (h *UploadHandler) UploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	ctx := r.Context()
	wallet := r.Header.Get("X-Wallet-Address")
	if wallet == "" {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "wallet authentication required"})
		return
	}

	if err := r.ParseMultipartForm(500 << 20); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to parse form"})
		return
	}

	file, handler, err := r.FormFile("file")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "no file provided"})
		return
	}
	defer func() { _ = file.Close() }()

	uploadID, err := h.svc.UploadStream(ctx, handler.Filename, file, handler.Size, wallet)
	if err != nil {
		h.logger.Error("Upload failed", zap.Error(err))
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	h.metricsCollector.IncrementCounter("upload_success", map[string]string{})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"upload_id": uploadID,
		"filename":  handler.Filename,
		"size":      handler.Size,
		"status":    "completed",
	})
}

func (h *UploadHandler) InitChunkedUploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	ctx := r.Context()
	wallet := r.Header.Get("X-Wallet-Address")
	if wallet == "" {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "wallet authentication required"})
		return
	}

	var req struct {
		Filename    string `json:"filename"`
		TotalSize   int64  `json:"total_size"`
		TotalChunks int    `json:"total_chunks"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid request body"})
		return
	}

	uploadID, err := h.svc.InitiateChunkedUpload(ctx, req.Filename, req.TotalSize, req.TotalChunks, wallet)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"upload_id":    uploadID,
		"status":       "uploading",
		"total_chunks": req.TotalChunks,
	})
}

func (h *UploadHandler) UploadChunkHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	ctx := r.Context()
	wallet := r.Header.Get("X-Wallet-Address")
	if wallet == "" {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "wallet authentication required"})
		return
	}

	uploadID := r.URL.Query().Get("upload_id")
	chunkIndexStr := r.URL.Query().Get("chunk_index")
	if uploadID == "" || chunkIndexStr == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "missing upload_id or chunk_index"})
		return
	}
	chunkIndex, err := strconv.Atoi(chunkIndexStr)
	if err != nil || chunkIndex < 0 {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid chunk_index"})
		return
	}

	if err := h.svc.UploadChunkStream(ctx, uploadID, chunkIndex, r.Body, r.ContentLength, wallet); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	h.metricsCollector.IncrementCounter("chunk_upload_success", map[string]string{})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"upload_id":   uploadID,
		"chunk_index": chunkIndex,
	})
}

func (h *UploadHandler) CompleteUploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	ctx := r.Context()
	wallet := r.Header.Get("X-Wallet-Address")
	if wallet == "" {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "wallet authentication required"})
		return
	}

	var req struct {
		UploadID    string `json:"upload_id"`
		TotalChunks int    `json:"total_chunks"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid request body"})
		return
	}
	uploadID := req.UploadID
	if uploadID == "" {
		uploadID = r.URL.Query().Get("upload_id")
	}
	if uploadID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "missing upload_id"})
		return
	}
	if req.TotalChunks <= 0 {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "total_chunks is required"})
		return
	}

	info, err := h.svc.GetUploadStatus(ctx, uploadID)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	if !strings.EqualFold(info.OwnerID, wallet) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "not authorized to complete this upload"})
		return
	}

	if err := h.svc.CompleteChunkedUpload(ctx, uploadID, req.TotalChunks); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"upload_id": uploadID,
		"status":    "completed",
	})
}

func (h *UploadHandler) CompleteUploadWithContentHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	ctx := r.Context()
	wallet := r.Header.Get("X-Wallet-Address")
	if wallet == "" {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "wallet authentication required"})
		return
	}

	var req struct {
		UploadID string `json:"upload_id"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&req)
	uploadID := req.UploadID
	if uploadID == "" {
		uploadID = r.URL.Query().Get("upload_id")
	}
	if uploadID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "missing upload_id"})
		return
	}

	info, err := h.svc.GetUploadStatus(ctx, uploadID)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	if !strings.EqualFold(info.OwnerID, wallet) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "not authorized to complete this upload"})
		return
	}

	contentID, err := h.svc.CompleteUploadWithTx(ctx, uploadID)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"upload_id":  uploadID,
		"content_id": contentID,
		"status":     "processed",
	})
}

func (h *UploadHandler) GetUploadStatusHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	ctx := r.Context()
	wallet := r.Header.Get("X-Wallet-Address")
	if wallet == "" {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "wallet authentication required"})
		return
	}

	uploadID := r.URL.Query().Get("upload_id")
	if uploadID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "missing upload_id"})
		return
	}

	info, err := h.svc.GetUploadStatus(ctx, uploadID)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	if !strings.EqualFold(info.OwnerID, wallet) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "not authorized to view this upload"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(info)
}

func (h *UploadHandler) DownloadURLHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	ctx := r.Context()
	wallet := r.Header.Get("X-Wallet-Address")
	if wallet == "" {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "wallet authentication required"})
		return
	}

	uploadID := r.URL.Query().Get("upload_id")
	if uploadID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "missing upload_id"})
		return
	}

	expiry := 60 * time.Minute
	if v := r.URL.Query().Get("expiry_minutes"); v != "" {
		if mins, err := strconv.Atoi(v); err == nil && mins > 0 && mins <= 60 {
			expiry = time.Duration(mins) * time.Minute
		}
	}

	url, err := h.svc.GetDownloadURL(ctx, uploadID, expiry, wallet)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"upload_id":    uploadID,
		"download_url": url,
		"expires_in":   int(expiry.Minutes()),
	})
}

func (h *UploadHandler) ListUploadsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	ctx := r.Context()
	wallet := r.Header.Get("X-Wallet-Address")
	if wallet == "" {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "wallet authentication required"})
		return
	}

	limit := 20
	offset := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	uploads, err := h.svc.ListUploads(ctx, wallet, limit, offset)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"uploads": uploads,
		"limit":   limit,
		"offset":  offset,
	})
}

func (h *UploadHandler) ChunkStatusesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	ctx := r.Context()
	wallet := r.Header.Get("X-Wallet-Address")
	if wallet == "" {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "wallet authentication required"})
		return
	}

	uploadID := r.URL.Query().Get("upload_id")
	if uploadID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "missing upload_id"})
		return
	}

	info, err := h.svc.GetUploadStatus(ctx, uploadID)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	if !strings.EqualFold(info.OwnerID, wallet) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "not authorized"})
		return
	}

	chunks, err := h.svc.GetChunkStatuses(ctx, uploadID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"upload_id": uploadID,
		"chunks":    chunks,
	})
}

func (h *UploadHandler) DeleteUploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	ctx := r.Context()
	wallet := r.Header.Get("X-Wallet-Address")
	if wallet == "" {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "wallet authentication required"})
		return
	}

	uploadID := r.URL.Query().Get("upload_id")
	if uploadID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "missing upload_id"})
		return
	}

	info, err := h.svc.GetUploadStatus(ctx, uploadID)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	if !strings.EqualFold(info.OwnerID, wallet) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "not authorized to delete this upload"})
		return
	}

	if err := h.svc.DeleteUpload(ctx, uploadID); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *UploadHandler) NotFoundHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
}
