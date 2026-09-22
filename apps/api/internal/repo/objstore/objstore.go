// Package objstore implements service.ObjectStore on the S3 API: a local
// gateway in development, DigitalOcean Spaces in production. It is the only
// package that imports the AWS SDK.
package objstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

	"doelab/api/internal/config"
	"doelab/api/internal/domain"
	"doelab/api/internal/service"
)

// Store is one bucket.
type Store struct {
	client  *s3.Client
	presign *s3.PresignClient
	bucket  string
}

var _ service.ObjectStore = (*Store)(nil)

// New builds a store from configuration. It makes no request.
func New(cfg config.S3) *Store {
	client := s3.New(s3.Options{
		Region:       cfg.Region,
		BaseEndpoint: aws.String(cfg.Endpoint),
		Credentials:  credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		UsePathStyle: cfg.PathStyle,
	})
	return &Store{client: client, presign: s3.NewPresignClient(client), bucket: cfg.Bucket}
}

// EnsureBucket creates the bucket when it does not exist. Development calls
// it at startup; production's bucket is created with the Space, and its key
// may not be allowed to create one.
func (s *Store) EnsureBucket(ctx context.Context) error {
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: &s.bucket})
	if err == nil {
		return nil
	}
	if !isNotFound(err) {
		return fmt.Errorf("head bucket %s: %w", s.bucket, err)
	}
	if _, err := s.client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: &s.bucket}); err != nil {
		var owned *types.BucketAlreadyOwnedByYou
		if errors.As(err, &owned) {
			return nil
		}
		return fmt.Errorf("create bucket %s: %w", s.bucket, err)
	}
	return nil
}

// Put stores body under key.
func (s *Store) Put(ctx context.Context, key, contentType string, body []byte) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: &s.bucket, Key: &key, ContentType: &contentType, Body: bytes.NewReader(body),
	})
	if err != nil {
		return fmt.Errorf("put %s: %w", key, err)
	}
	return nil
}

// Get returns the object at key.
func (s *Store) Get(ctx context.Context, key string) ([]byte, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: &s.bucket, Key: &key})
	if err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("object %s: %w", key, domain.ErrNotFound)
		}
		return nil, fmt.Errorf("get %s: %w", key, err)
	}
	defer func() { _ = out.Body.Close() }()
	body, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", key, err)
	}
	return body, nil
}

// PresignGet returns a URL that downloads the object at key without
// credentials until ttl has passed. The URL is signed locally; no request is
// made, and the object is not checked to exist.
func (s *Store) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	req, err := s.presign.PresignGetObject(ctx, &s3.GetObjectInput{Bucket: &s.bucket, Key: &key}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", fmt.Errorf("presign %s: %w", key, err)
	}
	return req.URL, nil
}

// isNotFound reports whether an S3 error means the bucket or the key is not
// there. HeadBucket answers with a bare 404 and no typed error.
func isNotFound(err error) bool {
	var noKey *types.NoSuchKey
	var noBucket *types.NoSuchBucket
	var notFound *types.NotFound
	if errors.As(err, &noKey) || errors.As(err, &noBucket) || errors.As(err, &notFound) {
		return true
	}
	var api smithy.APIError
	return errors.As(err, &api) && (api.ErrorCode() == "NotFound" || api.ErrorCode() == "NoSuchKey" || api.ErrorCode() == "NoSuchBucket")
}
