package models

import (
	"time"

	"github.com/google/uuid"
)

// FeatureDrift is the per-feature result the drift engine computes.
type FeatureDrift struct {
	PSI    float64  `json:"psi"`
	KSStat *float64 `json:"ks_stat,omitempty"`
	KSP    *float64 `json:"ks_p,omitempty"`
}

// DriftReport is produced by the drift scheduler and consumed by the alert
// service. ModelName/Version are denormalised so notifiers can render a
// readable message without another DB lookup.
type DriftReport struct {
	ID             uuid.UUID               `json:"id"`
	ModelVersionID uuid.UUID               `json:"model_version_id"`
	ModelName      string                  `json:"model_name"`
	Version        string                  `json:"version"`
	WindowStart    time.Time               `json:"window_start"`
	WindowEnd      time.Time               `json:"window_end"`
	SampleSize     int                     `json:"sample_size"`
	OverallPSI     float64                 `json:"overall_psi"`
	DriftDetected  bool                    `json:"drift_detected"`
	FeatureScores  map[string]FeatureDrift `json:"feature_scores"`
	CreatedAt      time.Time               `json:"created_at"`
}
