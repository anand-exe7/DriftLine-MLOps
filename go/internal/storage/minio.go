// Package storage wraps MinIO (S3-compatible) for model artifact files.
//
// Object layout, shared with ml-service's MinIO fetcher:
//
//	{bucket}/{model_name}/{version}/model.onnx
//	{bucket}/{model_name}/{version}/feature_schema.json
//
// The "{model_name}/{version}" prefix is the artifact key stored in
// model_versions.artifact_key.
package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"driftline/go/internal/config"
)

const (
	ModelFile  = "model.onnx"
	SchemaFile = "feature_schema.json"
)

// ErrNotFound is returned when the requested object doesn't exist.
var ErrNotFound = errors.New("storage: object not found")

type MinIOStore struct {
	client *minio.Client
	bucket string
}

// NewMinIOStore connects to MinIO and makes sure the bucket exists.
func NewMinIOStore(ctx context.Context, cfg config.MinIOConfig) (*MinIOStore, error) {
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: new client: %w", err)
	}

	exists, err := client.BucketExists(ctx, cfg.Bucket)
	if err != nil {
		return nil, fmt.Errorf("storage: check bucket %q: %w", cfg.Bucket, err)
	}
	if !exists {
		if err := client.MakeBucket(ctx, cfg.Bucket, minio.MakeBucketOptions{}); err != nil {
			return nil, fmt.Errorf("storage: create bucket %q: %w", cfg.Bucket, err)
		}
	}
	return &MinIOStore{client: client, bucket: cfg.Bucket}, nil
}

// ArtifactKey builds the object prefix for one model version.
func ArtifactKey(modelName, version string) string {
	return path.Join(modelName, version)
}

// UploadModelArtifact stores the .onnx file under {artifactKey}/model.onnx.
// size may be -1 when unknown; MinIO then uses a multipart upload.
func (s *MinIOStore) UploadModelArtifact(ctx context.Context, artifactKey string, r io.Reader, size int64) error {
	return s.put(ctx, path.Join(artifactKey, ModelFile), r, size, "application/octet-stream")
}

// UploadFeatureSchema stores feature_schema.json next to the model, which
// ml-service needs to turn a feature map into an ordered input tensor.
func (s *MinIOStore) UploadFeatureSchema(ctx context.Context, artifactKey string, r io.Reader, size int64) error {
	return s.put(ctx, path.Join(artifactKey, SchemaFile), r, size, "application/json")
}

// DownloadModelArtifact streams {artifactKey}/model.onnx. The caller must Close it.
func (s *MinIOStore) DownloadModelArtifact(ctx context.Context, artifactKey string) (io.ReadCloser, error) {
	key := path.Join(artifactKey, ModelFile)
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("storage: get %s: %w", key, err)
	}
	// GetObject is lazy: it only talks to the server on first Read/Stat, so
	// Stat now to turn a missing object into an error here, not mid-stream.
	if _, err := obj.Stat(); err != nil {
		obj.Close()
		return nil, s.wrap(key, err)
	}
	return obj, nil
}

// DeleteModelArtifact removes every object under the artifact prefix. Used to
// clean up when the DB insert after an upload fails.
func (s *MinIOStore) DeleteModelArtifact(ctx context.Context, artifactKey string) error {
	for _, name := range []string{ModelFile, SchemaFile} {
		key := path.Join(artifactKey, name)
		if err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{}); err != nil {
			return fmt.Errorf("storage: remove %s: %w", key, err)
		}
	}
	return nil
}

// Ping checks the bucket is reachable (for /healthz).
func (s *MinIOStore) Ping(ctx context.Context) error {
	_, err := s.client.BucketExists(ctx, s.bucket)
	return err
}

func (s *MinIOStore) put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	_, err := s.client.PutObject(ctx, s.bucket, key, r, size, minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return fmt.Errorf("storage: put %s: %w", key, err)
	}
	return nil
}

func (s *MinIOStore) wrap(key string, err error) error {
	if minio.ToErrorResponse(err).Code == minio.NoSuchKey {
		return fmt.Errorf("%w: %s", ErrNotFound, key)
	}
	return fmt.Errorf("storage: stat %s: %w", key, err)
}
