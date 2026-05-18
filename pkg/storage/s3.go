package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
)

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

func (s3s *S3Storage) UploadStream(ctx context.Context, bucket, key string, reader io.Reader, size int64) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	contentType := detectContentTypeByExt(key)
	_, err := s3s.uploader.UploadWithContext(ctx, &s3manager.UploadInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		Body:        reader,
		ContentType: aws.String(contentType),
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

// Download downloads from S3 and returns the entire content as a byte slice.
// Only safe for objects smaller than maxDownloadSize (1 GB).
func (s3s *S3Storage) Download(ctx context.Context, bucket, key string) ([]byte, error) {
	rc, err := s3s.DownloadStream(ctx, bucket, key)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()

	buf := new(bytes.Buffer)
	n, err := io.Copy(buf, io.LimitReader(rc, maxDownloadSize+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read S3 object: %w", err)
	}
	if n > maxDownloadSize {
		return nil, errors.New("S3 object exceeds maximum download size (1 GB)")
	}

	return buf.Bytes(), nil
}

// DownloadStream returns an io.ReadCloser for streaming an object from S3.
// The caller must close the reader when done.
func (s3s *S3Storage) DownloadStream(ctx context.Context, bucket, key string) (io.ReadCloser, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)

	result, err := s3s.client.GetObjectWithContext(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to download from S3: %w", err)
	}

	return &readCloserWithCancelS3{ReadCloser: result.Body, cancel: cancel}, nil
}

type readCloserWithCancelS3 struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (r *readCloserWithCancelS3) Close() error {
	defer r.cancel()
	return r.ReadCloser.Close()
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

func (s3s *S3Storage) DeleteObjects(ctx context.Context, bucket string, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	objects := make([]*s3.ObjectIdentifier, len(keys))
	for i, k := range keys {
		objects[i] = &s3.ObjectIdentifier{Key: aws.String(k)}
	}

	_, err := s3s.client.DeleteObjectsWithContext(ctx, &s3.DeleteObjectsInput{
		Bucket: aws.String(bucket),
		Delete: &s3.Delete{
			Objects: objects,
			Quiet:   aws.Bool(true),
		},
	})
	if err != nil {
		return fmt.Errorf("failed to delete objects from S3: %w", err)
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

	var keys []string
	var continuationToken *string

	for {
		input := &s3.ListObjectsV2Input{
			Bucket:            aws.String(bucket),
			Prefix:            aws.String(prefix),
			ContinuationToken: continuationToken,
		}

		result, err := s3s.client.ListObjectsV2WithContext(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("failed to list objects: %w", err)
		}

		for _, obj := range result.Contents {
			if obj.Key != nil {
				keys = append(keys, *obj.Key)
			}
		}

		if result.IsTruncated == nil || !*result.IsTruncated {
			break
		}
		continuationToken = result.NextContinuationToken
		if continuationToken == nil {
			break
		}
	}

	return keys, nil
}

func (s3s *S3Storage) PresignedURL(ctx context.Context, bucket, key string, expiration time.Duration) (string, error) {
	req, _ := s3s.client.GetObjectRequest(&s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	presignedURL, err := req.Presign(expiration)
	if err != nil {
		return "", fmt.Errorf("failed to generate presigned URL: %w", err)
	}
	return presignedURL, nil
}

func (s3s *S3Storage) PresignedUploadURL(ctx context.Context, bucket, key string, expiration time.Duration) (string, error) {
	req, _ := s3s.client.PutObjectRequest(&s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	presignedURL, err := req.Presign(expiration)
	if err != nil {
		return "", fmt.Errorf("failed to generate presigned upload URL: %w", err)
	}
	return presignedURL, nil
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

func (s3s *S3Storage) UploadStreamWithContentType(ctx context.Context, bucket, key string, reader io.Reader, size int64, contentType string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	_, err := s3s.uploader.UploadWithContext(ctx, &s3manager.UploadInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		Body:        reader,
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return fmt.Errorf("failed to upload stream to S3: %w", err)
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
		var aerr awserr.Error
		if errors.As(err, &aerr) {
			switch aerr.Code() {
			case "BucketAlreadyOwnedByYou", "BucketAlreadyExists":
				return nil
			}
		}
		return fmt.Errorf("failed to create bucket: %w", err)
	}
	return nil
}
