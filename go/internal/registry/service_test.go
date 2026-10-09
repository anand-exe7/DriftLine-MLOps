package registry

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"driftline/go/internal/models"
)

// ---- fakes ----------------------------------------------------------------

type fakeRepo struct {
	mu        sync.Mutex
	models    map[string]models.Model
	versions  map[string]models.ModelVersion // key "model/version"
	failWrite error                          // returned by CreateVersion when set
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{models: map[string]models.Model{}, versions: map[string]models.ModelVersion{}}
}

func (f *fakeRepo) CreateModel(_ context.Context, m *models.Model) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.models[m.Name]; ok {
		return fmt.Errorf("create model: %w", ErrConflict)
	}
	m.ID, m.CreatedAt, m.UpdatedAt = uuid.New(), time.Now(), time.Now()
	f.models[m.Name] = *m
	return nil
}

func (f *fakeRepo) GetModelByName(_ context.Context, name string) (models.Model, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.models[name]
	if !ok {
		return models.Model{}, fmt.Errorf("get model: %w", ErrNotFound)
	}
	return m, nil
}

func (f *fakeRepo) ListModels(context.Context) ([]models.Model, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []models.Model{}
	for _, m := range f.models {
		out = append(out, m)
	}
	return out, nil
}

func (f *fakeRepo) CreateVersion(_ context.Context, v *models.ModelVersion) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failWrite != nil {
		return f.failWrite
	}
	key := v.ModelName + "/" + v.Version
	if _, ok := f.versions[key]; ok {
		return fmt.Errorf("create version: %w", ErrConflict)
	}
	v.ID, v.CreatedAt = uuid.New(), time.Now()
	f.versions[key] = *v
	return nil
}

func (f *fakeRepo) GetVersion(_ context.Context, model, version string) (models.ModelVersion, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.versions[model+"/"+version]
	if !ok {
		return models.ModelVersion{}, fmt.Errorf("get version: %w", ErrNotFound)
	}
	return v, nil
}

func (f *fakeRepo) ListVersions(_ context.Context, model string) ([]models.ModelVersion, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []models.ModelVersion{}
	for _, v := range f.versions {
		if v.ModelName == model {
			out = append(out, v)
		}
	}
	return out, nil
}

func (f *fakeRepo) SetStage(_ context.Context, model, version string, stage models.Stage) (models.ModelVersion, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := model + "/" + version
	v, ok := f.versions[key]
	if !ok {
		return models.ModelVersion{}, fmt.Errorf("set stage: %w", ErrNotFound)
	}
	if stage == models.StageProduction {
		for k, other := range f.versions {
			if other.ModelName == model && other.Stage == models.StageProduction {
				other.Stage = models.StageArchived
				f.versions[k] = other
			}
		}
	}
	v.Stage = stage
	f.versions[key] = v
	return v, nil
}

type fakeStore struct {
	mu      sync.Mutex
	objects map[string][]byte
	deleted []string
}

func newFakeStore() *fakeStore { return &fakeStore{objects: map[string][]byte{}} }

func (s *fakeStore) put(key string, r io.Reader) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[key] = b
	return nil
}

func (s *fakeStore) UploadModelArtifact(_ context.Context, key string, r io.Reader, _ int64) error {
	return s.put(key+"/model.onnx", r)
}

func (s *fakeStore) UploadFeatureSchema(_ context.Context, key string, r io.Reader, _ int64) error {
	return s.put(key+"/feature_schema.json", r)
}

func (s *fakeStore) DownloadModelArtifact(_ context.Context, key string) (io.ReadCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.objects[key+"/model.onnx"]
	if !ok {
		return nil, errors.New("missing")
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (s *fakeStore) DeleteModelArtifact(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.objects, key+"/model.onnx")
	delete(s.objects, key+"/feature_schema.json")
	s.deleted = append(s.deleted, key)
	return nil
}

func newTestService(t *testing.T) (*Service, *fakeRepo, *fakeStore) {
	t.Helper()
	repo, store := newFakeRepo(), newFakeStore()
	svc := NewService(repo, store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, err := svc.CreateModel(context.Background(), CreateModelInput{Name: "loan_default_xgb"}); err != nil {
		t.Fatal(err)
	}
	return svc, repo, store
}

const validSchema = `{"framework":"xgboost","features":["Age","Income"]}`

func validInput(version string) RegisterVersionInput {
	return RegisterVersionInput{
		ModelName:     "loan_default_xgb",
		Version:       version,
		Artifact:      strings.NewReader("fake-onnx-bytes"),
		ArtifactSize:  -1,
		FeatureSchema: []byte(validSchema),
	}
}

// ---- tests ----------------------------------------------------------------

func TestCreateModelValidatesName(t *testing.T) {
	tests := []struct {
		name    string
		wantErr error
	}{
		{"loan_default", nil},
		{"model-v2.1", nil},
		{"", ErrInvalid},
		{"../escape", ErrInvalid},
		{"has space", ErrInvalid},
		{"-leading-dash", ErrInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewService(newFakeRepo(), newFakeStore(), slog.New(slog.NewTextHandler(io.Discard, nil)))
			_, err := svc.CreateModel(context.Background(), CreateModelInput{Name: tc.name})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestRegisterVersionStoresArtifactAndMetadata(t *testing.T) {
	svc, _, store := newTestService(t)

	v, err := svc.RegisterVersion(context.Background(), validInput("v1"))
	if err != nil {
		t.Fatal(err)
	}

	sum := sha256.Sum256([]byte("fake-onnx-bytes"))
	if v.ChecksumSHA256 != hex.EncodeToString(sum[:]) {
		t.Errorf("checksum = %s", v.ChecksumSHA256)
	}
	if v.ArtifactSize != int64(len("fake-onnx-bytes")) {
		t.Errorf("size = %d", v.ArtifactSize)
	}
	if v.ArtifactKey != "loan_default_xgb/v1" || v.Framework != "xgboost" || v.Stage != models.StageRegistered {
		t.Errorf("unexpected version: %+v", v)
	}
	if string(v.Metrics) != "{}" {
		t.Errorf("metrics default = %s", v.Metrics)
	}
	if _, ok := store.objects["loan_default_xgb/v1/model.onnx"]; !ok {
		t.Error("model.onnx not uploaded")
	}
	if string(store.objects["loan_default_xgb/v1/feature_schema.json"]) != validSchema {
		t.Error("feature_schema.json not uploaded")
	}
}

func TestRegisterVersionRejectsBadInput(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*RegisterVersionInput)
		wantErr error
	}{
		{"bad version", func(in *RegisterVersionInput) { in.Version = "v 1" }, ErrInvalid},
		{"no artifact", func(in *RegisterVersionInput) { in.Artifact = nil }, ErrInvalid},
		{"empty artifact", func(in *RegisterVersionInput) { in.Artifact = strings.NewReader("") }, ErrInvalid},
		{"schema not json", func(in *RegisterVersionInput) { in.FeatureSchema = []byte("nope") }, ErrInvalid},
		{"schema without features", func(in *RegisterVersionInput) { in.FeatureSchema = []byte(`{"features":[]}`) }, ErrInvalid},
		{"metrics not json", func(in *RegisterVersionInput) { in.Metrics = []byte("{") }, ErrInvalid},
		{"baseline not json", func(in *RegisterVersionInput) { in.BaselineStats = []byte("[") }, ErrInvalid},
		{"unknown model", func(in *RegisterVersionInput) { in.ModelName = "ghost" }, ErrNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, store := newTestService(t)
			in := validInput("v1")
			tc.mutate(&in)
			_, err := svc.RegisterVersion(context.Background(), in)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %v, want %v", err, tc.wantErr)
			}
			if len(store.objects) != 0 {
				t.Errorf("objects left behind: %v", store.objects)
			}
		})
	}
}

func TestRegisterVersionDuplicateIsConflict(t *testing.T) {
	svc, _, _ := newTestService(t)
	if _, err := svc.RegisterVersion(context.Background(), validInput("v1")); err != nil {
		t.Fatal(err)
	}
	_, err := svc.RegisterVersion(context.Background(), validInput("v1"))
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("got %v, want ErrConflict", err)
	}
}

func TestRegisterVersionCleansUpWhenDBWriteFails(t *testing.T) {
	svc, repo, store := newTestService(t)
	repo.failWrite = errors.New("db down")

	if _, err := svc.RegisterVersion(context.Background(), validInput("v1")); err == nil {
		t.Fatal("expected error")
	}
	if len(store.objects) != 0 || len(store.deleted) != 1 {
		t.Fatalf("artifacts not cleaned up: objects=%v deleted=%v", store.objects, store.deleted)
	}
}

func TestPromoteToProductionArchivesPrevious(t *testing.T) {
	svc, _, _ := newTestService(t)
	ctx := context.Background()
	for _, v := range []string{"v1", "v2"} {
		if _, err := svc.RegisterVersion(ctx, validInput(v)); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := svc.Promote(ctx, "loan_default_xgb", "v1", models.StageProduction); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Promote(ctx, "loan_default_xgb", "v2", models.StageProduction); err != nil {
		t.Fatal(err)
	}

	v1, _ := svc.GetVersion(ctx, "loan_default_xgb", "v1")
	v2, _ := svc.GetVersion(ctx, "loan_default_xgb", "v2")
	if v1.Stage != models.StageArchived || v2.Stage != models.StageProduction {
		t.Fatalf("v1=%s v2=%s", v1.Stage, v2.Stage)
	}
}

func TestPromoteRejectsBadStage(t *testing.T) {
	svc, _, _ := newTestService(t)
	for _, stage := range []models.Stage{"", "registered", "prod"} {
		if _, err := svc.Promote(context.Background(), "loan_default_xgb", "v1", stage); !errors.Is(err, ErrInvalid) {
			t.Errorf("stage %q: got %v, want ErrInvalid", stage, err)
		}
	}
}

func TestListVersionsUnknownModel(t *testing.T) {
	svc, _, _ := newTestService(t)
	if _, err := svc.ListVersions(context.Background(), "ghost"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}
