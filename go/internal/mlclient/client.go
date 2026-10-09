// Package mlclient is the Go side of the PredictionService gRPC contract:
// a thin, typed wrapper over the generated stub with interceptors applied.
package mlclient

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"driftline/go/internal/middleware"
	pb "driftline/proto/predict"
)

// Client is what the rest of the control plane depends on; tests use fakes.
type Client interface {
	Predict(ctx context.Context, model, version string, features map[string]float64, requestID string) (*pb.PredictionResponse, error)
	LoadModel(ctx context.Context, model, version string) (*pb.LoadModelResponse, error)
	UnloadModel(ctx context.Context, model, version string) (*pb.LoadModelResponse, error)
	Health(ctx context.Context) (*pb.HealthResponse, error)
}

type GRPC struct {
	conn *grpc.ClientConn
	stub pb.PredictionServiceClient
}

// Dial creates a client. grpc.NewClient connects lazily, so this succeeds even
// if ml-service is still starting; the first RPC waits for the connection.
func Dial(addr string, log *slog.Logger) (*GRPC, error) {
	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()), // in-cluster traffic
		grpc.WithChainUnaryInterceptor(
			middleware.UnaryClientTimeout(5*time.Second),
			middleware.UnaryClientObserver(log),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("mlclient: dial %s: %w", addr, err)
	}
	return &GRPC{conn: conn, stub: pb.NewPredictionServiceClient(conn)}, nil
}

func (c *GRPC) Close() error { return c.conn.Close() }

func (c *GRPC) Predict(ctx context.Context, model, version string, features map[string]float64, requestID string) (*pb.PredictionResponse, error) {
	return c.stub.Predict(ctx, &pb.PredictionRequest{
		ModelName: model, ModelVersion: version, Features: features, RequestId: requestID,
	})
}

func (c *GRPC) LoadModel(ctx context.Context, model, version string) (*pb.LoadModelResponse, error) {
	// Loading may pull from MinIO and build the session: allow longer than a predict.
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	return c.stub.LoadModel(ctx, &pb.ModelRef{ModelName: model, ModelVersion: version})
}

func (c *GRPC) UnloadModel(ctx context.Context, model, version string) (*pb.LoadModelResponse, error) {
	return c.stub.UnloadModel(ctx, &pb.ModelRef{ModelName: model, ModelVersion: version})
}

func (c *GRPC) Health(ctx context.Context) (*pb.HealthResponse, error) {
	return c.stub.HealthCheck(ctx, &pb.HealthRequest{})
}
