package middleware

import (
	"context"
	"log/slog"
	"path"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/status"

	"driftline/go/internal/metrics"
)

// UnaryClientTimeout applies a default deadline to calls whose context has
// none, so a hung ml-service can never block a request goroutine forever.
func UnaryClientTimeout(d time.Duration) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if _, ok := ctx.Deadline(); !ok {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, d)
			defer cancel()
		}
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

// UnaryClientObserver records latency + outcome metrics for every call and
// logs failures. Interceptors are gRPC's middleware: they wrap the actual
// invocation the same way HTTP middleware wraps a handler.
func UnaryClientObserver(log *slog.Logger) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		start := time.Now()
		err := invoker(ctx, method, req, reply, cc, opts...)
		elapsed := time.Since(start)

		name := path.Base(method) // "/predict.PredictionService/Predict" -> "Predict"
		code := status.Code(err)
		metrics.GRPCClientCalls.WithLabelValues(name, code.String()).Inc()
		metrics.GRPCClientDuration.WithLabelValues(name).Observe(elapsed.Seconds())
		if err != nil {
			log.WarnContext(ctx, "grpc call failed", "method", name, "code", code.String(), "duration", elapsed, "err", err)
		}
		return err
	}
}
