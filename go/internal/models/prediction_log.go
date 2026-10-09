package models

import (
	"time"

	"github.com/google/uuid"
)

type PredictionLog struct {
	ID             int64              `json:"id"`
	RequestID      string             `json:"request_id"`
	ModelVersionID uuid.UUID          `json:"model_version_id"`
	DeploymentID   *uuid.UUID         `json:"deployment_id,omitempty"`
	Features       map[string]float64 `json:"features"`
	Prediction     *int32             `json:"prediction,omitempty"`
	Probabilities  []float64          `json:"probabilities"`
	LatencyMS      int64              `json:"latency_ms"`
	StatusCode     int32              `json:"status_code"`
	ErrorMessage   string             `json:"error_message,omitempty"`
	CreatedAt      time.Time          `json:"created_at"`
}
