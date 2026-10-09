package models

import (
	"time"

	"github.com/google/uuid"
)

type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

type Alert struct {
	ID             uuid.UUID  `json:"id"`
	ModelVersionID *uuid.UUID `json:"model_version_id,omitempty"`
	DriftReportID  *uuid.UUID `json:"drift_report_id,omitempty"`
	Severity       Severity   `json:"severity"`
	Channel        string     `json:"channel"`
	Message        string     `json:"message"`
	Status         string     `json:"status"` // "sent" | "failed"
	Error          string     `json:"error,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}
