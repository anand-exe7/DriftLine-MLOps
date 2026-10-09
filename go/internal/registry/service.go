package registry

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"regexp"

	"driftline/go/internal/models"
	"driftline/go/internal/storage"
)

// Sentinel errors. Lower layers wrap them with context (fmt.Errorf("...: %w")),
// and the handler maps them to HTTP status codes with errors.Is.
var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("already exists")
	ErrInvalid  = errors.New("invalid input")
)

// Same rule as the CHECK constraints in the migrations and ml-service's
// path guard: names become object-storage path segments, so keep them tame.
var namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// ArtifactStore is the subset of storage.MinIOStore the registry needs.
type ArtifactStore interface {
	UploadModelArtifact(ctx context.Context, artifactKey string, r io.Reader, size int64) error
	UploadFeatureSchema(ctx context.Context, artifactKey string, r io.Reader, size int64) error
	DownloadModelArtifact(ctx context.Context, artifactKey string) (io.ReadCloser, error)
	DeleteModelArtifact(ctx context.Context, artifactKey string) error
}

type Service struct {
	repo  Repository
	store ArtifactStore
	log   *slog.Logger
}

func NewService(repo Repository, store ArtifactStore, log *slog.Logger) *Service {
	return &Service{repo: repo, store: store, log: log}
}

type CreateModelInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	TaskType    string `json:"task_type"`
}

func (s *Service) CreateModel(ctx context.Context, in CreateModelInput) (models.Model, error) {
	if !namePattern.MatchString(in.Name) {
		return models.Model{}, fmt.Errorf("%w: name must match %s", ErrInvalid, namePattern)
	}
	if in.TaskType == "" {
		in.TaskType = "binary_classification"
	}
	m := models.Model{Name: in.Name, Description: in.Description, TaskType: in.TaskType}
	if err := s.repo.CreateModel(ctx, &m); err != nil {
		return models.Model{}, err
	}
	s.log.InfoContext(ctx, "model created", "model", m.Name)
	return m, nil
}

func (s *Service) GetModel(ctx context.Context, name string) (models.Model, error) {
	return s.repo.GetModelByName(ctx, name)
}

func (s *Service) ListModels(ctx context.Context) ([]models.Model, error) {
	return s.repo.ListModels(ctx)
}

type RegisterVersionInput struct {
	ModelName     string
	Version       string
	Artifact      io.Reader // the .onnx file
	ArtifactSize  int64     // -1 if unknown
	FeatureSchema []byte    // feature_schema.json (required: ml-service can't serve without it)
	Metrics       []byte    // optional JSON
	BaselineStats []byte    // optional baseline_stats.json for the drift engine
}

// featureSchema is the part of feature_schema.json the registry validates.
type featureSchema struct {
	Framework string   `json:"framework"`
	Features  []string `json:"features"`
}

// RegisterVersion uploads the artifact to object storage, then records the
// version in the DB. If the DB write fails the uploaded objects are deleted,
// so storage never holds artifacts the registry doesn't know about.
func (s *Service) RegisterVersion(ctx context.Context, in RegisterVersionInput) (models.ModelVersion, error) {
	if !namePattern.MatchString(in.Version) {
		return models.ModelVersion{}, fmt.Errorf("%w: version must match %s", ErrInvalid, namePattern)
	}
	if in.Artifact == nil {
		return models.ModelVersion{}, fmt.Errorf("%w: model artifact is required", ErrInvalid)
	}

	var schema featureSchema
	if err := json.Unmarshal(in.FeatureSchema, &schema); err != nil {
		return models.ModelVersion{}, fmt.Errorf("%w: feature_schema is not valid JSON: %v", ErrInvalid, err)
	}
	if len(schema.Features) == 0 {
		return models.ModelVersion{}, fmt.Errorf("%w: feature_schema.features must list the model's input features", ErrInvalid)
	}
	for name, raw := range map[string][]byte{"metrics": in.Metrics, "baseline_stats": in.BaselineStats} {
		if len(raw) > 0 && !json.Valid(raw) {
			return models.ModelVersion{}, fmt.Errorf("%w: %s is not valid JSON", ErrInvalid, name)
		}
	}
	metrics := in.Metrics
	if len(metrics) == 0 {
		metrics = []byte("{}")
	}

	model, err := s.repo.GetModelByName(ctx, in.ModelName)
	if err != nil {
		return models.ModelVersion{}, err
	}

	// Cheap pre-check so a duplicate is rejected before we upload megabytes.
	// The UNIQUE constraint still catches a concurrent duplicate later.
	if _, err := s.repo.GetVersion(ctx, in.ModelName, in.Version); err == nil {
		return models.ModelVersion{}, fmt.Errorf("version %s/%s: %w", in.ModelName, in.Version, ErrConflict)
	} else if !errors.Is(err, ErrNotFound) {
		return models.ModelVersion{}, err
	}

	key := storage.ArtifactKey(in.ModelName, in.Version)

	// Hash and count bytes while streaming to MinIO, so we never buffer the
	// whole model in memory.
	hasher := sha256.New()
	counter := &countingReader{r: io.TeeReader(in.Artifact, hasher)}
	if err := s.store.UploadModelArtifact(ctx, key, counter, in.ArtifactSize); err != nil {
		return models.ModelVersion{}, fmt.Errorf("upload artifact: %w", err)
	}
	if counter.n == 0 {
		s.cleanup(ctx, key)
		return models.ModelVersion{}, fmt.Errorf("%w: model artifact is empty", ErrInvalid)
	}
	if err := s.store.UploadFeatureSchema(ctx, key, bytes.NewReader(in.FeatureSchema), int64(len(in.FeatureSchema))); err != nil {
		s.cleanup(ctx, key)
		return models.ModelVersion{}, fmt.Errorf("upload feature schema: %w", err)
	}

	v := models.ModelVersion{
		ModelID:        model.ID,
		ModelName:      model.Name,
		Version:        in.Version,
		Framework:      schema.Framework,
		ArtifactKey:    key,
		ArtifactSize:   counter.n,
		ChecksumSHA256: hex.EncodeToString(hasher.Sum(nil)),
		FeatureSchema:  in.FeatureSchema,
		Metrics:        metrics,
		BaselineStats:  in.BaselineStats,
		Stage:          models.StageRegistered,
	}
	if err := s.repo.CreateVersion(ctx, &v); err != nil {
		s.cleanup(ctx, key)
		return models.ModelVersion{}, err
	}

	s.log.InfoContext(ctx, "model version registered",
		"model", v.ModelName, "version", v.Version, "bytes", v.ArtifactSize, "sha256", v.ChecksumSHA256)
	return v, nil
}

func (s *Service) GetVersion(ctx context.Context, modelName, version string) (models.ModelVersion, error) {
	return s.repo.GetVersion(ctx, modelName, version)
}

func (s *Service) ListVersions(ctx context.Context, modelName string) ([]models.ModelVersion, error) {
	// Distinguish "model doesn't exist" (404) from "model has no versions" ([]).
	if _, err := s.repo.GetModelByName(ctx, modelName); err != nil {
		return nil, err
	}
	return s.repo.ListVersions(ctx, modelName)
}

// Promote moves a version to staging or production (or archives it).
func (s *Service) Promote(ctx context.Context, modelName, version string, stage models.Stage) (models.ModelVersion, error) {
	if !stage.Valid() || stage == models.StageRegistered {
		return models.ModelVersion{}, fmt.Errorf("%w: stage must be staging, production or archived", ErrInvalid)
	}
	v, err := s.repo.SetStage(ctx, modelName, version, stage)
	if err != nil {
		return models.ModelVersion{}, err
	}
	s.log.InfoContext(ctx, "model version promoted", "model", modelName, "version", version, "stage", stage)
	return v, nil
}

// DownloadArtifact returns the version metadata and a stream of its .onnx file.
func (s *Service) DownloadArtifact(ctx context.Context, modelName, version string) (models.ModelVersion, io.ReadCloser, error) {
	v, err := s.repo.GetVersion(ctx, modelName, version)
	if err != nil {
		return models.ModelVersion{}, nil, err
	}
	rc, err := s.store.DownloadModelArtifact(ctx, v.ArtifactKey)
	if errors.Is(err, storage.ErrNotFound) {
		return models.ModelVersion{}, nil, fmt.Errorf("artifact %s: %w", v.ArtifactKey, ErrNotFound)
	}
	return v, rc, err
}

// cleanup runs even if the request context was cancelled (client hung up),
// otherwise an aborted upload would leave orphaned objects behind.
func (s *Service) cleanup(ctx context.Context, key string) {
	if err := s.store.DeleteModelArtifact(context.WithoutCancel(ctx), key); err != nil {
		s.log.ErrorContext(ctx, "failed to clean up artifact", "key", key, "err", err)
	}
}

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}
