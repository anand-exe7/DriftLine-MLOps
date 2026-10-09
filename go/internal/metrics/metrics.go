// Package metrics declares every Prometheus metric the control plane exports,
// in one place, so names and labels stay consistent across modules.
// Scraped at GET /metrics.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	HTTPRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "driftline_http_requests_total",
		Help: "HTTP requests by route pattern, method and status code.",
	}, []string{"route", "method", "status"})

	HTTPDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "driftline_http_request_duration_seconds",
		Help:    "HTTP request latency by route pattern.",
		Buckets: prometheus.DefBuckets,
	}, []string{"route", "method"})

	GRPCClientCalls = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "driftline_grpc_client_calls_total",
		Help: "Outgoing gRPC calls to ml-service by method and gRPC code.",
	}, []string{"method", "code"})

	GRPCClientDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "driftline_grpc_client_duration_seconds",
		Help:    "Round-trip latency of gRPC calls to ml-service.",
		Buckets: []float64{.0005, .001, .0025, .005, .01, .025, .05, .1, .25, .5, 1},
	}, []string{"method"})

	Predictions = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "driftline_predictions_total",
		Help: "Predictions served through the proxy, by model, version, routing role and outcome.",
	}, []string{"model", "version", "role", "outcome"})

	PredictionLogsDropped = promauto.NewCounter(prometheus.CounterOpts{
		Name: "driftline_prediction_logs_dropped_total",
		Help: "Prediction logs dropped because the async log buffer was full.",
	})

	PredictionLogsWritten = promauto.NewCounter(prometheus.CounterOpts{
		Name: "driftline_prediction_logs_written_total",
		Help: "Prediction logs persisted to Postgres.",
	})

	TrafficPercent = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "driftline_deployment_traffic_percent",
		Help: "Configured traffic share of each running deployment.",
	}, []string{"model", "version"})

	DriftPSI = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "driftline_drift_psi",
		Help: "Latest PSI per model version and feature (feature=\"_overall\" for the aggregate).",
	}, []string{"model", "version", "feature"})

	DriftChecks = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "driftline_drift_checks_total",
		Help: "Drift checks run, by result (drift, ok, skipped, error).",
	}, []string{"result"})

	Rollbacks = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "driftline_rollbacks_total",
		Help: "Rollbacks performed, by trigger (manual, drift, errors).",
	}, []string{"trigger"})
)
