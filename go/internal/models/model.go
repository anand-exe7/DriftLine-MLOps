// Package models holds plain structs that mirror the database tables. They
// carry no behaviour so any module can import them without import cycles.
package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type Model struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	TaskType    string    `json:"task_type"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Stage is the lifecycle position of a model version.
type Stage string

const (
	StageRegistered Stage = "registered"
	StageStaging    Stage = "staging"
	StageProduction Stage = "production"
	StageArchived   Stage = "archived"
)

func (s Stage) Valid() bool {
	switch s {
	case StageRegistered, StageStaging, StageProduction, StageArchived:
		return true
	}
	return false
}

type ModelVersion struct {
	ID             uuid.UUID       `json:"id"`
	ModelID        uuid.UUID       `json:"model_id"`
	ModelName      string          `json:"model_name"`
	Version        string          `json:"version"`
	Framework      string          `json:"framework"`
	ArtifactKey    string          `json:"artifact_key"`
	ArtifactSize   int64           `json:"artifact_size"`
	ChecksumSHA256 string          `json:"checksum_sha256"`
	FeatureSchema  json.RawMessage `json:"feature_schema"`
	Metrics        json.RawMessage `json:"metrics"`
	BaselineStats  json.RawMessage `json:"baseline_stats,omitempty"`
	Stage          Stage           `json:"stage"`
	CreatedAt      time.Time       `json:"created_at"`
	PromotedAt     *time.Time      `json:"promoted_at,omitempty"`
}
