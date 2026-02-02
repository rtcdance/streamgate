package streaming

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// RangeHandler handles HTTP range requests for streaming
type RangeHandler struct {
	storageDir string
	logger     *zap.Logger
	cache      *RangeCache
}

// RangeCache caches frequently accessed file ranges
type RangeCache struct {
	entries map[string]*CacheEntry
	mu      sync.RWMutex
	maxSize int64
}

// CacheEntry represents a cached range
type CacheEntry struct {
	Data      []byte
	ExpiresAt time.Time
	Accessed  time.Time
}

// NewRangeHandler creates a new range handler
func NewRangeHandler(storageDir string, logger *zap.Logger) *RangeHandler {
	return &RangeHandler{
		storageDir: storageDir,
		logger:     logger,
		cache:      NewRangeCache(100 * 1024 * 1024), // 100MB cache
	}
}

// NewRangeCache creates a new range cache
func NewRangeCache(maxSize int64) *RangeCache {
	return &RangeCache{
		entries: make(map[string]*CacheEntry),
		maxSize: maxSize,
	}
}

// ServeHTTP handles HTTP range requests
func (rh *RangeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	filePath := r.URL.Path
	if filePath == "" || filePath == "/" {
		http.Error(w, "File path required", http.StatusBadRequest)
		return
	}

	filePath = filepath.Join(rh.storageDir, strings.TrimPrefix(filePath, "/"))

	rh.logger.Debug("Handling range request",
		zap.String("path", filePath),
		zap.String("method", r.Method),
		zap.String("range", r.Header.Get("Range")))

	fileInfo, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "File not found", http.StatusNotFound)
		} else {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
		return
	}

	fileSize := fileInfo.Size()

	rangeHeader := r.Header.Get("Range")
	if rangeHeader == "" {
		rh.serveFullFile(ctx, w, r, filePath, fileSize)
		return
	}

	ranges, err := parseRangeHeader(rangeHeader, fileSize)
	if err != nil {
		rh.logger.Warn("Invalid range header",
			zap.String("range", rangeHeader),
			zap.Error(err))
		http.Error(w, "Invalid range", http.StatusRequestedRangeNotSatisfiable)
		return
	}

	if len(ranges) == 0 {
		rh.serveFullFile(ctx, w, r, filePath, fileSize)
		return
	}

	if len(ranges) == 1 {
		rh.serveSingleRange(ctx, w, r, filePath, fileSize, ranges[0])
		return
	}

	rh.serveMultiRange(ctx, w, r, filePath, fileSize, ranges)
}

// serveFullFile serves the entire file
func (rh *RangeHandler) serveFullFile(ctx context.Context, w http.ResponseWriter, r *http.Request, filePath string, fileSize int64) {
	rh.logger.Debug("Serving full file",
		zap.String("path", filePath),
		zap.Int64("size", fileSize))

	file, err := os.Open(filePath)
	if err != nil {
		http.Error(w, "Failed to open file", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	w.Header().Set("Content-Length", strconv.FormatInt(fileSize, 10))
	w.Header().Set("Content-Type", rh.getContentType(filePath))
	w.Header().Set("Accept-Ranges", "bytes")

	http.ServeContent(w, r, filePath, time.Now(), file)
}

// serveSingleRange serves a single byte range
func (rh *RangeHandler) serveSingleRange(ctx context.Context, w http.ResponseWriter, r *http.Request, filePath string, fileSize int64, fileRange *FileRange) {
	rh.logger.Debug("Serving single range",
		zap.String("path", filePath),
		zap.Int64("start", fileRange.Start),
		zap.Int64("end", fileRange.End))

	file, err := os.Open(filePath)
	if err != nil {
		http.Error(w, "Failed to open file", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	if _, err := file.Seek(fileRange.Start, io.SeekStart); err != nil {
		http.Error(w, "Failed to seek file", http.StatusInternalServerError)
		return
	}

	contentLength := fileRange.End - fileRange.Start + 1

	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", fileRange.Start, fileRange.End, fileSize))
	w.Header().Set("Content-Length", strconv.FormatInt(contentLength, 10))
	w.Header().Set("Content-Type", rh.getContentType(filePath))
	w.Header().Set("Accept-Ranges", "bytes")
	w.WriteHeader(http.StatusPartialContent)

	if _, err := io.CopyN(w, file, contentLength); err != nil && err != io.EOF {
		rh.logger.Error("Failed to send range",
			zap.String("path", filePath),
			zap.Error(err))
	}
}

// serveMultiRange serves multiple byte ranges
func (rh *RangeHandler) serveMultiRange(ctx context.Context, w http.ResponseWriter, r *http.Request, filePath string, fileSize int64, ranges []*FileRange) {
	rh.logger.Debug("Serving multi-range",
		zap.String("path", filePath),
		zap.Int("ranges", len(ranges)))

	boundary := fmt.Sprintf("boundary-%d", time.Now().UnixNano())

	file, err := os.Open(filePath)
	if err != nil {
		http.Error(w, "Failed to open file", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	w.Header().Set("Content-Type", fmt.Sprintf("multipart/byteranges; boundary=%s", boundary))
	w.WriteHeader(http.StatusPartialContent)

	mw := multipartWriter{w: w, boundary: boundary}

	for _, fileRange := range ranges {
		mw.WriteHeader()

		if _, err := file.Seek(fileRange.Start, io.SeekStart); err != nil {
			rh.logger.Error("Failed to seek file",
				zap.String("path", filePath),
				zap.Error(err))
			return
		}

		contentLength := fileRange.End - fileRange.Start + 1
		if _, err := io.CopyN(&mw, file, contentLength); err != nil && err != io.EOF {
			rh.logger.Error("Failed to send range",
				zap.String("path", filePath),
				zap.Error(err))
			return
		}
	}

	mw.Close()
}

// multipartWriter writes multipart responses
type multipartWriter struct {
	w        http.ResponseWriter
	boundary string
	closed   bool
}

// WriteHeader writes a multipart header
func (mw *multipartWriter) WriteHeader() {
	if mw.closed {
		return
	}

	fmt.Fprintf(mw.w, "--%s\r\n", mw.boundary)
	fmt.Fprintf(mw.w, "Content-Type: application/octet-stream\r\n")
	fmt.Fprintf(mw.w, "Content-Range: bytes %d-%d/%d\r\n", 0, 0, 0)
	fmt.Fprintf(mw.w, "\r\n")
}

// Write writes data to the multipart response
func (mw *multipartWriter) Write(p []byte) (int, error) {
	if mw.closed {
		return 0, io.EOF
	}
	return mw.w.Write(p)
}

// Close closes the multipart response
func (mw *multipartWriter) Close() {
	if mw.closed {
		return
	}

	fmt.Fprintf(mw.w, "\r\n--%s--\r\n", mw.boundary)
	mw.closed = true
}

// FileRange represents a byte range
type FileRange struct {
	Start int64
	End   int64
}

// parseRangeHeader parses the Range header
func parseRangeHeader(rangeHeader string, fileSize int64) ([]*FileRange, error) {
	if !strings.HasPrefix(rangeHeader, "bytes=") {
		return nil, fmt.Errorf("invalid range header format")
	}

	rangeSpec := strings.TrimPrefix(rangeHeader, "bytes=")
	rangeParts := strings.Split(rangeSpec, ",")

	ranges := make([]*FileRange, 0, len(rangeParts))

	for _, part := range rangeParts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		fileRange, err := parseRangeSpec(part, fileSize)
		if err != nil {
			return nil, err
		}

		ranges = append(ranges, fileRange)
	}

	return ranges, nil
}

// parseRangeSpec parses a single range specification
func parseRangeSpec(spec string, fileSize int64) (*FileRange, error) {
	parts := strings.Split(spec, "-")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid range specification")
	}

	startStr := strings.TrimSpace(parts[0])
	endStr := strings.TrimSpace(parts[1])

	var start, end int64
	var err error

	if startStr == "" {
		start = fileSize - 1
		if endStr != "" {
			end, err = strconv.ParseInt(endStr, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid end position: %w", err)
			}
			start = fileSize - end
		}
		end = fileSize - 1
	} else if endStr == "" {
		start, err = strconv.ParseInt(startStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid start position: %w", err)
		}
		end = fileSize - 1
	} else {
		start, err = strconv.ParseInt(startStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid start position: %w", err)
		}

		end, err = strconv.ParseInt(endStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid end position: %w", err)
		}
	}

	if start < 0 || end < 0 || start > end || start >= fileSize {
		return nil, fmt.Errorf("invalid range: %d-%d/%d", start, end, fileSize)
	}

	if end >= fileSize {
		end = fileSize - 1
	}

	return &FileRange{Start: start, End: end}, nil
}

// getContentType returns the content type for a file
func (rh *RangeHandler) getContentType(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))

	switch ext {
	case ".mp4":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".ogg":
		return "video/ogg"
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".flac":
		return "audio/flac"
	case ".m3u8":
		return "application/vnd.apple.mpegurl"
	case ".mpd":
		return "application/dash+xml"
	case ".ts":
		return "video/mp2t"
	default:
		return "application/octet-stream"
	}
}

// ServeRange serves a specific byte range
func (rh *RangeHandler) ServeRange(ctx context.Context, filePath string, start, end int64) ([]byte, error) {
	rh.logger.Debug("Serving range",
		zap.String("path", filePath),
		zap.Int64("start", start),
		zap.Int64("end", end))

	filePath = filepath.Join(rh.storageDir, filePath)

	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	fileSize := fileInfo.Size()

	if start < 0 || end < 0 || start > end || start >= fileSize {
		return nil, fmt.Errorf("invalid range: %d-%d/%d", start, end, fileSize)
	}

	if end >= fileSize {
		end = fileSize - 1
	}

	if _, err := file.Seek(start, io.SeekStart); err != nil {
		return nil, fmt.Errorf("failed to seek file: %w", err)
	}

	contentLength := end - start + 1
	data := make([]byte, contentLength)

	if _, err := io.ReadFull(file, data); err != nil {
		return nil, fmt.Errorf("failed to read range: %w", err)
	}

	return data, nil
}

// GetFileInfo returns file information
func (rh *RangeHandler) GetFileInfo(ctx context.Context, filePath string) (*FileInfo, error) {
	filePath = filepath.Join(rh.storageDir, filePath)

	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	return &FileInfo{
		Path:          filePath,
		Size:          fileInfo.Size(),
		ModifiedTime:  fileInfo.ModTime(),
		ContentType:   rh.getContentType(filePath),
		SupportsRange: true,
	}, nil
}

// FileInfo represents file information
type FileInfo struct {
	Path          string
	Size          int64
	ModifiedTime  time.Time
	ContentType   string
	SupportsRange bool
}

// ValidateRange validates a byte range
func (rh *RangeHandler) ValidateRange(ctx context.Context, filePath string, start, end int64) (bool, error) {
	filePath = filepath.Join(rh.storageDir, filePath)

	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return false, fmt.Errorf("failed to stat file: %w", err)
	}

	fileSize := fileInfo.Size()

	if start < 0 || end < 0 || start > end || start >= fileSize {
		return false, nil
	}

	if end >= fileSize {
		return false, nil
	}

	return true, nil
}

// GetCachedRange gets a cached range
func (rh *RangeHandler) GetCachedRange(ctx context.Context, filePath string, start, end int64) ([]byte, bool) {
	cacheKey := rh.getCacheKey(filePath, start, end)

	rh.cache.mu.RLock()
	entry, exists := rh.cache.entries[cacheKey]
	rh.cache.mu.RUnlock()

	if !exists || time.Now().After(entry.ExpiresAt) {
		return nil, false
	}

	entry.Accessed = time.Now()
	return entry.Data, true
}

// CacheRange caches a byte range
func (rh *RangeHandler) CacheRange(ctx context.Context, filePath string, start, end int64, data []byte) {
	cacheKey := rh.getCacheKey(filePath, start, end)

	entry := &CacheEntry{
		Data:      data,
		ExpiresAt: time.Now().Add(5 * time.Minute),
		Accessed:  time.Now(),
	}

	rh.cache.mu.Lock()
	defer rh.cache.mu.Unlock()

	rh.cache.entries[cacheKey] = entry
	rh.evictIfNeeded()
}

// getCacheKey generates a cache key
func (rh *RangeHandler) getCacheKey(filePath string, start, end int64) string {
	return fmt.Sprintf("%s:%d-%d", filePath, start, end)
}

// evictIfNeeded evicts old cache entries if needed
func (rh *RangeHandler) evictIfNeeded() {
	rh.cache.mu.Lock()
	defer rh.cache.mu.Unlock()

	var totalSize int64
	for _, entry := range rh.cache.entries {
		totalSize += int64(len(entry.Data))
	}

	if totalSize <= rh.cache.maxSize {
		return
	}

	var oldestKey string
	var oldestTime time.Time

	for key, entry := range rh.cache.entries {
		if oldestKey == "" || entry.Accessed.Before(oldestTime) {
			oldestKey = key
			oldestTime = entry.Accessed
		}
	}

	if oldestKey != "" {
		delete(rh.cache.entries, oldestKey)
	}
}
