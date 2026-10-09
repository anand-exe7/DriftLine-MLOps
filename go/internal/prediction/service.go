// Package prediction is the public prediction proxy: it picks a model version
// (canary routing), calls ml-service over gRPC, and logs every request so the
// drift engine has live data to compare against the training baseline.
package prediction

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"driftline/go/internal/metrics"
	"driftline/go/internal/mlclient"
	"driftline/go/internal/models"
)

var (
	ErrNoRoute     = errors.New("no deployed or production version for this model")
	ErrInvalid     = errors.New("invalid input")
	ErrUnavailable = errors.New("ml-service unavailable")
)

type Request struct {
	Features   map[string]float64 `json:"features"`
	RequestID  string             `json:"request_id,omitempty"`
	RoutingKey string             `json:"routing_key,omitempty"` // optional: sticky canary assignment
}

type Result struct {
	RequestID     string     `json:"request_id"`
	Model         string     `json:"model"`
	Version       string     `json:"version"`
	Role          Role       `json:"role"`
	DeploymentID  *uuid.UUID `json:"deployment_id,omitempty"`
	Prediction    int32      `json:"prediction"`
	Probabilities []float32  `json:"probabilities"`
	InferenceMS   int64      `json:"inference_ms"` // measured by ml-service
	TotalMS       float64    `json:"total_ms"`     // measured here, includes the gRPC hop
}

type Service struct {
	router *Router
	ml     mlclient.Client
	sink   LogSink
	log    *slog.Logger
}

func NewService(router *Router, ml mlclient.Client, sink LogSink, log *slog.Logger) *Service {
	return &Service{router: router, ml: ml, sink: sink, log: log}
}

func (s *Service) Router() *Router { return s.router }

func (s *Service) Predict(ctx context.Context, model string, req Request) (Result, error) {
	if len(req.Features) == 0 {
		return Result{}, fmt.Errorf("%w: features must not be empty", ErrInvalid)
	}
	if req.RequestID == "" {
		req.RequestID = uuid.NewString()
	}

	routes, err := s.router.Routes(ctx, model)
	if err != nil {
		return Result{}, err
	}
	route, ok := s.router.Pick(model, req.RoutingKey, routes)
	if !ok {
		return Result{}, fmt.Errorf("model %s: %w", model, ErrNoRoute)
	}

	start := time.Now()
	resp, err := s.ml.Predict(ctx, route.ModelName, route.Version, req.Features, req.RequestID)
	total := time.Since(start)

	entry := models.PredictionLog{
		RequestID:      req.RequestID,
		ModelVersionID: route.VersionID,
		DeploymentID:   route.DeploymentID,
		Features:       req.Features,
		Probabilities:  []float64{},
		CreatedAt:      time.Now().UTC(),
	}

	if err != nil {
		// Transport failure: ml-service down, deadline exceeded, ...
		entry.StatusCode = int32(status.Code(err))
		entry.ErrorMessage = err.Error()
		s.sink.Enqueue(entry)
		metrics.Predictions.WithLabelValues(model, route.Version, string(route.Role), "unavailable").Inc()
		if status.Code(err) == codes.DeadlineExceeded {
			return Result{}, fmt.Errorf("%w: timed out", ErrUnavailable)
		}
		return Result{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}

	entry.StatusCode = resp.StatusCode
	entry.ErrorMessage = resp.ErrorMessage
	entry.LatencyMS = resp.LatencyMs
	if resp.StatusCode == 0 {
		p := resp.Prediction
		entry.Prediction = &p
		for _, v := range resp.Probabilities {
			entry.Probabilities = append(entry.Probabilities, float64(v))
		}
	}
	s.sink.Enqueue(entry)

	if resp.StatusCode != 0 {
		metrics.Predictions.WithLabelValues(model, route.Version, string(route.Role), "rejected").Inc()
		if codes.Code(resp.StatusCode) == codes.InvalidArgument {
			return Result{}, fmt.Errorf("%w: %s", ErrInvalid, resp.ErrorMessage)
		}
		return Result{}, fmt.Errorf("%w: %s", ErrUnavailable, resp.ErrorMessage)
	}
	metrics.Predictions.WithLabelValues(model, route.Version, string(route.Role), "ok").Inc()

	return Result{
		RequestID:     req.RequestID,
		Model:         model,
		Version:       route.Version,
		Role:          route.Role,
		DeploymentID:  route.DeploymentID,
		Prediction:    resp.Prediction,
		Probabilities: resp.Probabilities,
		InferenceMS:   resp.LatencyMs,
		TotalMS:       float64(total.Microseconds()) / 1000,
	}, nil
}
