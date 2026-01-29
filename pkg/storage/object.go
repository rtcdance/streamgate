package storage

import (
	"fmt"
	"time"
)

// ObjectStorage is a generic object storage interface wrapper
type ObjectStorage struct {
	s3          *S3Storage
	minio       *MinIOStorage
	storageType string
}

// ObjectStorageConfig holds object storage configuration
type ObjectStorageConfig struct {
	Type            string // s3, minio
	Endpoint        string
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	UseSSL          bool
}

// NewObjectStorage creates a new object storage instance
func NewObjectStorage(config ObjectStorageConfig) (*ObjectStorage, error) {
	storage := &ObjectStorage{
		storageType: config.Type,
	}

	switch config.Type {
	case "s3":
		s3Config := S3Config{
			Region:          config.Region,
			AccessKeyID:     config.AccessKeyID,
			SecretAccessKey: config.SecretAccessKey,
		}
		if config.Endpoint != "" {
			s3Config.Endpoint = config.Endpoint
		}

		s3Storage, err := NewS3Storage(s3Config)
		if err != nil {
			return nil, fmt.Errorf("failed to create S3 storage: %w", err)
		}
		storage.s3 = s3Storage

	case "minio":
		minioConfig := MinIOConfig{
			Endpoint:        config.Endpoint,
			AccessKeyID:     config.AccessKeyID,
			SecretAccessKey: config.SecretAccessKey,
			UseSSL:          config.UseSSL,
		}

		minioStorage, err := NewMinIOStorage(minioConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create MinIO storage: %w", err)
		}
		storage.minio = minioStorage

	default:
		return nil, fmt.Errorf("unsupported storage type: %s", config.Type)
	}

	return storage, nil
}

// Upload uploads data to object storage
func (os *ObjectStorage) Upload(bucket, key string, data []byte) error {
	switch os.storageType {
	case "s3":
		if os.s3 == nil {
			return fmt.Errorf("S3 storage not initialized")
		}
		return os.s3.Upload(bucket, key, data)
	case "minio":
		if os.minio == nil {
			return fmt.Errorf("MinIO storage not initialized")
		}
		return os.minio.Upload(bucket, key, data)
	default:
		return fmt.Errorf("unsupported storage type: %s", os.storageType)
	}
}

// UploadWithMetadata uploads data with metadata to object storage
func (os *ObjectStorage) UploadWithMetadata(bucket, key string, data []byte, metadata map[string]string) error {
	switch os.storageType {
	case "s3":
		if os.s3 == nil {
			return fmt.Errorf("S3 storage not initialized")
		}
		// Convert map[string]string to map[string]*string for AWS SDK
		awsMetadata := make(map[string]*string)
		for k, v := range metadata {
			val := v
			awsMetadata[k] = &val
		}
		return os.s3.UploadWithMetadata(bucket, key, data, awsMetadata)
	case "minio":
		if os.minio == nil {
			return fmt.Errorf("MinIO storage not initialized")
		}
		return os.minio.UploadWithContentType(bucket, key, data, metadata["Content-Type"])
	default:
		return fmt.Errorf("unsupported storage type: %s", os.storageType)
	}
}

// Download downloads data from object storage
func (os *ObjectStorage) Download(bucket, key string) ([]byte, error) {
	switch os.storageType {
	case "s3":
		if os.s3 == nil {
			return nil, fmt.Errorf("S3 storage not initialized")
		}
		return os.s3.Download(bucket, key)
	case "minio":
		if os.minio == nil {
			return nil, fmt.Errorf("MinIO storage not initialized")
		}
		return os.minio.Download(bucket, key)
	default:
		return nil, fmt.Errorf("unsupported storage type: %s", os.storageType)
	}
}

// Delete deletes an object from storage
func (os *ObjectStorage) Delete(bucket, key string) error {
	switch os.storageType {
	case "s3":
		if os.s3 == nil {
			return fmt.Errorf("S3 storage not initialized")
		}
		return os.s3.Delete(bucket, key)
	case "minio":
		if os.minio == nil {
			return fmt.Errorf("MinIO storage not initialized")
		}
		return os.minio.Delete(bucket, key)
	default:
		return fmt.Errorf("unsupported storage type: %s", os.storageType)
	}
}

// Exists checks if an object exists in storage
func (os *ObjectStorage) Exists(bucket, key string) (bool, error) {
	switch os.storageType {
	case "s3":
		if os.s3 == nil {
			return false, fmt.Errorf("S3 storage not initialized")
		}
		return os.s3.Exists(bucket, key)
	case "minio":
		if os.minio == nil {
			return false, fmt.Errorf("MinIO storage not initialized")
		}
		return os.minio.Exists(bucket, key)
	default:
		return false, fmt.Errorf("unsupported storage type: %s", os.storageType)
	}
}

// ListObjects lists objects in a bucket with a prefix
func (os *ObjectStorage) ListObjects(bucket, prefix string) ([]string, error) {
	switch os.storageType {
	case "s3":
		if os.s3 == nil {
			return nil, fmt.Errorf("S3 storage not initialized")
		}
		return os.s3.ListObjects(bucket, prefix)
	case "minio":
		if os.minio == nil {
			return nil, fmt.Errorf("MinIO storage not initialized")
		}
		return os.minio.ListObjects(bucket, prefix)
	default:
		return nil, fmt.Errorf("unsupported storage type: %s", os.storageType)
	}
}

// GetPresignedURL generates a presigned URL for an object
func (os *ObjectStorage) GetPresignedURL(bucket, key string, expiration time.Duration) (string, error) {
	switch os.storageType {
	case "s3":
		if os.s3 == nil {
			return "", fmt.Errorf("S3 storage not initialized")
		}
		return os.s3.GetPresignedURL(bucket, key, expiration)
	case "minio":
		if os.minio == nil {
			return "", fmt.Errorf("MinIO storage not initialized")
		}
		return os.minio.GetPresignedURL(bucket, key, expiration)
	default:
		return "", fmt.Errorf("unsupported storage type: %s", os.storageType)
	}
}

// CreateBucket creates a new bucket (MinIO only)
func (os *ObjectStorage) CreateBucket(bucket string) error {
	switch os.storageType {
	case "minio":
		if os.minio == nil {
			return fmt.Errorf("MinIO storage not initialized")
		}
		return os.minio.CreateBucket(bucket)
	case "s3":
		// S3 bucket creation requires different approach
		return fmt.Errorf("S3 bucket creation not supported through this interface")
	default:
		return fmt.Errorf("unsupported storage type: %s", os.storageType)
	}
}

// GetType returns the storage type
func (os *ObjectStorage) GetType() string {
	return os.storageType
}

// CopyObject copies an object from one location to another
func (os *ObjectStorage) CopyObject(srcBucket, srcKey, dstBucket, dstKey string) error {
	// Download from source
	data, err := os.Download(srcBucket, srcKey)
	if err != nil {
		return fmt.Errorf("failed to download source object: %w", err)
	}

	// Upload to destination
	if err := os.Upload(dstBucket, dstKey, data); err != nil {
		return fmt.Errorf("failed to upload to destination: %w", err)
	}

	return nil
}

// MoveObject moves an object from one location to another
func (os *ObjectStorage) MoveObject(srcBucket, srcKey, dstBucket, dstKey string) error {
	// Copy object
	if err := os.CopyObject(srcBucket, srcKey, dstBucket, dstKey); err != nil {
		return err
	}

	// Delete source
	if err := os.Delete(srcBucket, srcKey); err != nil {
		return fmt.Errorf("failed to delete source object: %w", err)
	}

	return nil
}

// GetObjectSize gets the size of an object
func (os *ObjectStorage) GetObjectSize(bucket, key string) (int64, error) {
	// Download and get size (not efficient, but works for all storage types)
	data, err := os.Download(bucket, key)
	if err != nil {
		return 0, err
	}
	return int64(len(data)), nil
}

// DeleteMultiple deletes multiple objects
func (os *ObjectStorage) DeleteMultiple(bucket string, keys []string) error {
	for _, key := range keys {
		if err := os.Delete(bucket, key); err != nil {
			return fmt.Errorf("failed to delete %s: %w", key, err)
		}
	}
	return nil
}
