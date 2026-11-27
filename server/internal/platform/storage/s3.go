package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

// S3Config holds connection settings for AWS S3 or S3-compatible object storage (such as MinIO).
type S3Config struct {
	Endpoint         string
	Region           string
	Bucket           string
	AccessKey        string
	SecretKey        string
	UsePathStyle     bool
	AutoCreateBucket bool
	HTTPClient       *http.Client
}

// S3 implements Storage backed by an S3-compatible object bucket.
type S3 struct {
	client *s3.Client
	bucket string
	region string
}

var _ Storage = (*S3)(nil)

// NewS3 constructs an S3 Storage driver and optionally ensures the target bucket exists.
func NewS3(ctx context.Context, cfg S3Config) (*S3, error) {
	bucket := strings.TrimSpace(cfg.Bucket)
	if bucket == "" {
		return nil, errors.New("s3 bucket must not be empty")
	}
	accessKey := strings.TrimSpace(cfg.AccessKey)
	secretKey := strings.TrimSpace(cfg.SecretKey)
	if accessKey == "" || secretKey == "" {
		return nil, errors.New("s3 access_key and secret_key must not be empty")
	}

	region := strings.TrimSpace(cfg.Region)
	if region == "" {
		region = "us-east-1"
	}

	opts := s3.Options{
		Region: region,
		Credentials: aws.NewCredentialsCache(
			credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
		),
		UsePathStyle: cfg.UsePathStyle,
	}
	if endpoint := strings.TrimSpace(cfg.Endpoint); endpoint != "" {
		opts.BaseEndpoint = aws.String(endpoint)
	}
	if cfg.HTTPClient != nil {
		opts.HTTPClient = cfg.HTTPClient
	}

	client := s3.New(opts)
	store := &S3{
		client: client,
		bucket: bucket,
		region: region,
	}

	if cfg.AutoCreateBucket {
		if err := store.ensureBucket(ctx); err != nil {
			return nil, err
		}
	}
	return store, nil
}

func (s *S3) ensureBucket(ctx context.Context) error {
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(s.bucket),
	})
	if err == nil {
		return nil
	}
	if !isS3BucketNotFound(err) {
		return fmt.Errorf("head s3 bucket %s: %w", s.bucket, err)
	}

	input := &s3.CreateBucketInput{
		Bucket: aws.String(s.bucket),
	}
	if s.region != "" && s.region != "us-east-1" {
		input.CreateBucketConfiguration = &types.CreateBucketConfiguration{
			LocationConstraint: types.BucketLocationConstraint(s.region),
		}
	}
	_, err = s.client.CreateBucket(ctx, input)
	if err != nil && !isS3BucketAlreadyExists(err) {
		return fmt.Errorf("create s3 bucket %s: %w", s.bucket, err)
	}
	return nil
}

// Driver returns "s3".
func (s *S3) Driver() string {
	return DriverS3
}

// Put uploads r to key in the configured S3 bucket and returns the written byte count.
func (s *S3) Put(ctx context.Context, key string, r io.Reader) (int64, error) {
	cleanKey, err := validateKey(key)
	if err != nil {
		return 0, err
	}

	body, size, cleanup, err := prepareSeekableBody(r)
	if err != nil {
		return 0, err
	}
	defer cleanup()

	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(cleanKey),
		Body:          body,
		ContentLength: aws.Int64(size),
	})
	if err != nil {
		return 0, fmt.Errorf("put s3 object %s: %w", cleanKey, err)
	}
	return size, nil
}

// Get opens key from the configured S3 bucket for streaming reads.
func (s *S3) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	cleanKey, err := validateKey(key)
	if err != nil {
		return nil, err
	}

	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(cleanKey),
	})
	if err != nil {
		if isS3ObjectNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get s3 object %s: %w", cleanKey, err)
	}
	return out.Body, nil
}

// Delete removes key from the configured S3 bucket, treating missing objects as already deleted.
func (s *S3) Delete(ctx context.Context, key string) error {
	cleanKey, err := validateKey(key)
	if err != nil {
		return err
	}

	_, err = s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(cleanKey),
	})
	if err != nil && !isS3ObjectNotFound(err) {
		return fmt.Errorf("delete s3 object %s: %w", cleanKey, err)
	}
	return nil
}

// Stat returns size metadata for key in the configured S3 bucket.
func (s *S3) Stat(ctx context.Context, key string) (ObjectInfo, error) {
	cleanKey, err := validateKey(key)
	if err != nil {
		return ObjectInfo{}, err
	}

	out, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(cleanKey),
	})
	if err != nil {
		if isS3ObjectNotFound(err) {
			return ObjectInfo{}, ErrNotFound
		}
		return ObjectInfo{}, fmt.Errorf("stat s3 object %s: %w", cleanKey, err)
	}
	return ObjectInfo{
		Key:  cleanKey,
		Size: aws.ToInt64(out.ContentLength),
	}, nil
}

func prepareSeekableBody(r io.Reader) (io.ReadSeeker, int64, func(), error) {
	if rs, ok := r.(io.ReadSeeker); ok {
		cur, err := rs.Seek(0, io.SeekCurrent)
		if err == nil {
			end, err := rs.Seek(0, io.SeekEnd)
			if err == nil {
				if _, err := rs.Seek(cur, io.SeekStart); err == nil {
					return rs, end - cur, func() {}, nil
				}
			}
		}
	}

	tmp, err := os.CreateTemp("", "tianshu-s3-put-*")
	if err != nil {
		return nil, 0, nil, fmt.Errorf("create s3 upload buffer: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}

	n, err := io.Copy(tmp, r)
	if err != nil {
		cleanup()
		return nil, 0, nil, fmt.Errorf("buffer s3 upload stream: %w", err)
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		cleanup()
		return nil, 0, nil, fmt.Errorf("rewind s3 upload buffer: %w", err)
	}
	return tmp, n, cleanup, nil
}

func isS3ObjectNotFound(err error) bool {
	if err == nil {
		return false
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		if apiErr.ErrorCode() == "NoSuchBucket" {
			return false
		}
		switch apiErr.ErrorCode() {
		case "NoSuchKey", "NotFound", "404":
			return true
		}
	}
	var noSuchKey *types.NoSuchKey
	if errors.As(err, &noSuchKey) {
		return true
	}
	var notFound *types.NotFound
	if errors.As(err, &notFound) {
		return true
	}
	var respErr *smithyhttp.ResponseError
	return errors.As(err, &respErr) && respErr.HTTPStatusCode() == http.StatusNotFound
}

func isS3BucketNotFound(err error) bool {
	if err == nil {
		return false
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "NoSuchBucket", "NotFound", "404":
			return true
		}
	}
	var notFound *types.NotFound
	if errors.As(err, &notFound) {
		return true
	}
	var respErr *smithyhttp.ResponseError
	return errors.As(err, &respErr) && respErr.HTTPStatusCode() == http.StatusNotFound
}

func isS3BucketAlreadyExists(err error) bool {
	var owned *types.BucketAlreadyOwnedByYou
	if errors.As(err, &owned) {
		return true
	}
	var exists *types.BucketAlreadyExists
	if errors.As(err, &exists) {
		return true
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "BucketAlreadyOwnedByYou", "BucketAlreadyExists":
			return true
		}
	}
	return false
}
