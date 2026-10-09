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

type DeploymentRole string

const (
	RolePrimary DeploymentRole = "primary"
	RoleCanary  DeploymentRole = "canary"
)

type Deployment struct {
	ID             uuid.UUID        `json:"id"`
	ModelVersionID uuid.UUID        `json:"model_version_id"`
	ModelName      string           `json:"model_name"` // joined from models
	Version        string           `json:"version"`    // joined from model_versions
	Environment    string           `json:"environment"`
	Role           DeploymentRole   `json:"role"`
	Status         DeploymentStatus `json:"status"`
	StatusReason   string           `json:"status_reason,omitempty"`
	TrafficPercent int              `json:"traffic_percent"`
	Endpoint       string           `json:"endpoint"`
	ContainerID    string           `json:"container_id,omitempty"`
	StartedAt      *time.Time       `json:"started_at,omitempty"`
	StoppedAt      *time.Time       `json:"stopped_at,omitempty"`
	CreatedAt      time.Time        `json:"created_at"`
	UpdatedAt      time.Time        `json:"updated_at"`
}
