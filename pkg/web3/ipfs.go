package web3

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	shell "github.com/ipfs/go-ipfs-api"
	"go.uber.org/zap"
)

const defaultIPFSTimeout = 30 * time.Second

// IPFSClient handles IPFS operations
type IPFSClient struct {
	shell   *shell.Shell
	logger  *zap.Logger
	timeout time.Duration
}

// NewIPFSClient creates a new IPFS client
func NewIPFSClient(ipfsURL string, logger *zap.Logger) (*IPFSClient, error) {
	logger.Info("Connecting to IPFS", zap.String("url", ipfsURL))

	sh := shell.NewShell(ipfsURL)

	connCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	versionCh := make(chan string, 1)
	errCh := make(chan error, 1)
	go func() {
		version, err := sh.ID()
		if err != nil {
			errCh <- err
			return
		}
		versionCh <- version.AgentVersion
	}()

	var agentVersion string
	select {
	case <-connCtx.Done():
		logger.Error("Failed to connect to IPFS: timeout")
		return nil, fmt.Errorf("failed to connect to IPFS: timeout")
	case err := <-errCh:
		logger.Error("Failed to connect to IPFS", zap.Error(err))
		return nil, fmt.Errorf("failed to connect to IPFS: %w", err)
	case agentVersion = <-versionCh:
	}

	logger.Info("Connected to IPFS",
		zap.String("version", agentVersion))

	return &IPFSClient{
		shell:   sh,
		logger:  logger,
		timeout: defaultIPFSTimeout,
	}, nil
}

func (ic *IPFSClient) runWithContext(ctx context.Context, fn func() error) error {
	timeout := ic.timeout
	if deadline, ok := ctx.Deadline(); ok {
		if d := time.Until(deadline); d < timeout {
			timeout = d
		}
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	done := make(chan error, 1)
	go func() { done <- fn() }()

	select {
	case err := <-done:
		return err
	case <-timer.C:
		return context.DeadlineExceeded
	case <-ctx.Done():
		return ctx.Err()
	}
}

// UploadFile uploads a file to IPFS
func (ic *IPFSClient) UploadFile(ctx context.Context, filename string, data []byte) (string, error) {
	ic.logger.Debug("Uploading file to IPFS",
		zap.String("filename", filename),
		zap.Int("size", len(data)))

	var cid string
	err := ic.runWithContext(ctx, func() error {
		reader := bytes.NewReader(data)
		var err error
		cid, err = ic.shell.Add(reader)
		return err
	})
	if err != nil {
		ic.logger.Error("Failed to upload file to IPFS",
			zap.String("filename", filename),
			zap.Error(err))
		return "", fmt.Errorf("failed to upload file to IPFS: %w", err)
	}

	ic.logger.Info("File uploaded to IPFS",
		zap.String("filename", filename),
		zap.String("cid", cid))
	return cid, nil
}

// DownloadFile downloads a file from IPFS
func (ic *IPFSClient) DownloadFile(ctx context.Context, cid string) ([]byte, error) {
	ic.logger.Debug("Downloading file from IPFS", zap.String("cid", cid))

	var data []byte
	err := ic.runWithContext(ctx, func() error {
		reader, err := ic.shell.Cat(cid)
		if err != nil {
			return err
		}
		defer func() { _ = reader.Close() }()

		data, err = io.ReadAll(io.LimitReader(reader, 512<<20))
		return err
	})
	if err != nil {
		ic.logger.Error("Failed to download file from IPFS",
			zap.String("cid", cid),
			zap.Error(err))
		return nil, fmt.Errorf("failed to download file from IPFS: %w", err)
	}

	ic.logger.Debug("File downloaded from IPFS",
		zap.String("cid", cid),
		zap.Int("size", len(data)))
	return data, nil
}

// PinFile pins a file on IPFS
func (ic *IPFSClient) PinFile(ctx context.Context, cid string) error {
	ic.logger.Debug("Pinning file on IPFS", zap.String("cid", cid))

	err := ic.runWithContext(ctx, func() error {
		return ic.shell.Pin(cid)
	})
	if err != nil {
		ic.logger.Error("Failed to pin file on IPFS",
			zap.String("cid", cid),
			zap.Error(err))
		return fmt.Errorf("failed to pin file on IPFS: %w", err)
	}

	ic.logger.Info("File pinned on IPFS", zap.String("cid", cid))
	return nil
}

// UnpinFile unpins a file from IPFS
func (ic *IPFSClient) UnpinFile(ctx context.Context, cid string) error {
	ic.logger.Debug("Unpinning file from IPFS", zap.String("cid", cid))

	err := ic.runWithContext(ctx, func() error {
		return ic.shell.Unpin(cid)
	})
	if err != nil {
		ic.logger.Error("Failed to unpin file from IPFS",
			zap.String("cid", cid),
			zap.Error(err))
		return fmt.Errorf("failed to unpin file from IPFS: %w", err)
	}

	ic.logger.Info("File unpinned from IPFS", zap.String("cid", cid))
	return nil
}

// GetFileInfo gets information about a file on IPFS
func (ic *IPFSClient) GetFileInfo(ctx context.Context, cid string) (*FileInfo, error) {
	ic.logger.Debug("Getting file info from IPFS", zap.String("cid", cid))

	var stat *shell.ObjectStats
	err := ic.runWithContext(ctx, func() error {
		var err error
		stat, err = ic.shell.ObjectStat(cid)
		return err
	})
	if err != nil {
		ic.logger.Error("Failed to get file info from IPFS",
			zap.String("cid", cid),
			zap.Error(err))
		return nil, fmt.Errorf("failed to get file info from IPFS: %w", err)
	}

	fileInfo := &FileInfo{
		CID:     cid,
		Size:    int64(stat.CumulativeSize),
		Type:    "file",
		Hash:    stat.Hash,
		Blocks:  stat.NumLinks,
		CumSize: int64(stat.CumulativeSize),
	}

	ic.logger.Debug("File info retrieved from IPFS", zap.String("cid", cid))
	return fileInfo, nil
}

// FileInfo contains file information
type FileInfo struct {
	CID     string
	Size    int64
	Type    string
	Hash    string
	Blocks  int
	CumSize int64
}

// IPFSGateway represents an IPFS gateway
type IPFSGateway struct {
	URL string
}

// GetGatewayURL gets the gateway URL for a CID
func (ig *IPFSGateway) GetGatewayURL(cid string) string {
	return fmt.Sprintf("%s/ipfs/%s", ig.URL, cid)
}

// GetHTTPGatewayURL gets the HTTP gateway URL for a CID
func GetHTTPGatewayURL(cid string) string {
	// Use public IPFS gateway
	return fmt.Sprintf("https://ipfs.io/ipfs/%s", cid)
}

// GetCloudflareGatewayURL gets the Cloudflare gateway URL for a CID
func GetCloudflareGatewayURL(cid string) string {
	return fmt.Sprintf("https://cloudflare-ipfs.com/ipfs/%s", cid)
}

// HybridStorage handles hybrid storage (local + IPFS)
type HybridStorage struct {
	ipfsClient *IPFSClient
	logger     *zap.Logger
	threshold  int64 // File size threshold for IPFS (in bytes)
}

// NewHybridStorage creates a new hybrid storage
func NewHybridStorage(ipfsClient *IPFSClient, logger *zap.Logger, threshold int64) *HybridStorage {
	return &HybridStorage{
		ipfsClient: ipfsClient,
		logger:     logger,
		threshold:  threshold,
	}
}

// Store stores a file using hybrid storage
func (hs *HybridStorage) Store(ctx context.Context, filename string, data []byte) (*StorageLocation, error) {
	hs.logger.Debug("Storing file with hybrid storage",
		zap.String("filename", filename),
		zap.Int("size", len(data)))

	location := &StorageLocation{
		Filename: filename,
		Size:     int64(len(data)),
	}

	// Check if file should go to IPFS
	if int64(len(data)) > hs.threshold {
		// Upload to IPFS
		cid, err := hs.ipfsClient.UploadFile(ctx, filename, data)
		if err != nil {
			hs.logger.Error("Failed to upload file to IPFS",
				zap.String("filename", filename),
				zap.Error(err))
			return nil, err
		}

		location.Storage = "ipfs"
		location.CID = cid
		location.URL = GetHTTPGatewayURL(cid)

		hs.logger.Info("File stored on IPFS",
			zap.String("filename", filename),
			zap.String("cid", cid))
	} else {
		// Store locally
		location.Storage = "local"
		location.URL = fmt.Sprintf("/files/%s", filename)

		hs.logger.Info("File stored locally", zap.String("filename", filename))
	}

	return location, nil
}

// StorageLocation represents a file storage location
type StorageLocation struct {
	Filename string
	Size     int64
	Storage  string // "local" or "ipfs"
	CID      string
	URL      string
}
