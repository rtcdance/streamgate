package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
)

// maxDownloadSize caps the amount of data read into memory during Download.
const maxDownloadSize int64 = 1 << 30 // 1 GB

// S3Storage handles S3 storage
type S3Storage struct {
	client   *s3.S3
	uploader *s3manager.Uploader
	region   string
}

// S3Config holds S3 configuration
type S3Config struct {
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	Endpoint        string // Optional: for S3-compatible services
}

// NewS3Storage creates a new S3 storage instance
func NewS3Storage(config S3Config) (*S3Storage, error) {
	awsConfig := &aws.Config{
		Region: aws.String(config.Region),
	}

	// Add credentials if provided
	if config.AccessKeyID != "" && config.SecretAccessKey != "" {
		awsConfig.Credentials = credentials.NewStaticCredentials(
			config.AccessKeyID,
			config.SecretAccessKey,
			"",
		)
	}

	// Add custom endpoint if provided (for S3-compatible services)
	if config.Endpoint != "" {
		awsConfig.Endpoint = aws.String(config.Endpoint)
		awsConfig.S3ForcePathStyle = aws.Bool(true)
	}

	sess, err := session.NewSession(awsConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create AWS session: %w", err)
	}

	return &S3Storage{
		client:   s3.New(sess),
		uploader: s3manager.NewUploader(sess),
		region:   config.Region,
	}, nil
}

// Upload uploads to S3
func (s3s *S3Storage) Upload(ctx context.Context, bucket, key string, data []byte) error {
	return s3s.UploadStream(ctx, bucket, key, bytes.NewReader(data), int64(len(data)))
}

// UploadStream uploads to S3 from an io.Reader using multipart upload,
// which supports true streaming without buffering the entire content in memory.
func (s3s *S3Storage) UploadStream(ctx context.Context, bucket, key string, reader io.Reader, size int64) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	_, err := s3s.uploader.UploadWithContext(ctx, &s3manager.UploadInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   reader,
	})
	if err != nil {
		return fmt.Errorf("failed to upload to S3: %w", err)
	}

	return nil
}

// UploadWithMetadata uploads to S3 with metadata
func (s3s *S3Storage) UploadWithMetadata(ctx context.Context, bucket, key string, data []byte, metadata map[string]*string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	_, err := s3s.client.PutObjectWithContext(ctx, &s3.PutObjectInput{
		Bucket:   aws.String(bucket),
		Key:      aws.String(key),
		Body:     bytes.NewReader(data),
		Metadata: metadata,
	})

	if err != nil {
		return fmt.Errorf("failed to upload to S3: %w", err)
	}

	return nil
}

// Download downloads from S3
func (s3s *S3Storage) Download(ctx context.Context, bucket, key string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	result, err := s3s.client.GetObjectWithContext(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})

	if err != nil {
		return nil, fmt.Errorf("failed to download from S3: %w", err)
	}
	defer func() { _ = result.Body.Close() }()

	buf := new(bytes.Buffer)
	n, err := io.Copy(buf, io.LimitReader(result.Body, maxDownloadSize+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read S3 object: %w", err)
	}
	if n > maxDownloadSize {
		return nil, errors.New("S3 object exceeds maximum download size (1 GB)")
	}

	return buf.Bytes(), nil
}

// Delete deletes from S3
func (s3s *S3Storage) Delete(ctx context.Context, bucket, key string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	_, err := s3s.client.DeleteObjectWithContext(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})

	if err != nil {
		return fmt.Errorf("failed to delete from S3: %w", err)
	}

	return nil
}

// Exists checks if an object exists in S3
func (s3s *S3Storage) Exists(ctx context.Context, bucket, key string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	_, err := s3s.client.HeadObjectWithContext(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})

	if err != nil {
		// Check if error is "not found"
		if aerr, ok := err.(interface{ Code() string }); ok && aerr.Code() == "NotFound" {
			return false, nil
		}
		return false, fmt.Errorf("failed to check object existence: %w", err)
	}

	return true, nil
}

// ListObjects lists objects in a bucket with a prefix
func (s3s *S3Storage) ListObjects(ctx context.Context, bucket, prefix string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	result, err := s3s.client.ListObjectsV2WithContext(ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	})

	if err != nil {
		return nil, fmt.Errorf("failed to list objects: %w", err)
	}

	keys := make([]string, 0, len(result.Contents))
	for _, obj := range result.Contents {
		if obj.Key != nil {
			keys = append(keys, *obj.Key)
		}
	}

	return keys, nil
}

// GetPresignedURL generates a presigned URL for downloading
func (s3s *S3Storage) GetPresignedURL(bucket, key string, expiration time.Duration) (string, error) {
	// Validate that the object reference is valid before generating presigned URL.
	if _, err := s3s.client.HeadObjectWithContext(context.Background(), &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}); err != nil {
		return "", fmt.Errorf("object not found: %w", err)
	}

	req, _ := s3s.client.GetObjectRequest(&s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	url, err := req.Presign(expiration)
	if err != nil {
		return "", fmt.Errorf("failed to generate presigned URL: %w", err)
	}

	return url, nil
}

// UploadWithContentType stores data with a specific content type
func (s3s *S3Storage) UploadWithContentType(ctx context.Context, bucket, key string, data []byte, contentType string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	_, err := s3s.client.PutObjectWithContext(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return fmt.Errorf("failed to upload object with content type: %w", err)
	}
	return nil
}

// CreateBucket creates an S3 bucket if it does not exist
func (s3s *S3Storage) CreateBucket(ctx context.Context, bucket string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	_, err := s3s.client.CreateBucketWithContext(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		// Ignore bucket already owned by you error
		if strings.Contains(err.Error(), "BucketAlreadyOwnedByYou") || strings.Contains(err.Error(), "BucketAlreadyExists") {
			return nil
		}
		return fmt.Errorf("failed to create bucket: %w", err)
	}
	return nil
}
