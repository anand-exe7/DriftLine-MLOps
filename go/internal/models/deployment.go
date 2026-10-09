package models

import (
	"time"

	"github.com/google/uuid"
)

type DeploymentStatus string

const (
	DeploymentPending    DeploymentStatus = "pending"
	DeploymentRunning    DeploymentStatus = "running"
	DeploymentStopped    DeploymentStatus = "stopped"
	DeploymentFailed     DeploymentStatus = "failed"
	DeploymentRolledBack DeploymentStatus = "rolled_back"
)

type Deployment struct {
	ID             uuid.UUID        `json:"id"`
	ModelVersionID uuid.UUID        `json:"model_version_id"`
	Environment    string           `json:"environment"`
	Status         DeploymentStatus `json:"status"`
	TrafficPercent int              `json:"traffic_percent"`
	Endpoint       string           `json:"endpoint"`
	ContainerID    string           `json:"container_id"`
	StartedAt      *time.Time       `json:"started_at,omitempty"`
	StoppedAt      *time.Time       `json:"stopped_at,omitempty"`
	CreatedAt      time.Time        `json:"created_at"`
	UpdatedAt      time.Time        `json:"updated_at"`
}
