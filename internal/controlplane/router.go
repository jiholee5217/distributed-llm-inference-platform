package controlplane

import (
	"context"
	"errors"
	"fmt"
	"time"

	inferencev1 "github.com/jiholee5217/distributed-llm-inference-platform/gen/inference/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type WorkerInvoker interface {
	Generate(ctx context.Context, worker WorkerSnapshot, request *inferencev1.GenerateRequest) (*inferencev1.GenerateResponse, error)
}

type RouteResult struct {
	Response *inferencev1.GenerateResponse
	Attempts int
}

type Router struct {
	registry       *Registry
	invoker        WorkerInvoker
	metrics        *Metrics
	maxAttempts    int
	attemptTimeout time.Duration
}

func NewRouter(registry *Registry, invoker WorkerInvoker, metrics *Metrics, maxAttempts int, attemptTimeout time.Duration) *Router {
	return &Router{registry: registry, invoker: invoker, metrics: metrics, maxAttempts: maxAttempts, attemptTimeout: attemptTimeout}
}

func (r *Router) Generate(ctx context.Context, request *inferencev1.GenerateRequest) (RouteResult, error) {
	excluded := make(map[string]struct{})
	var lastErr error
	for attempt := 1; attempt <= r.maxAttempts; attempt++ {
		worker, err := r.registry.Acquire(request.GetModel(), request.GetModelVersion(), excluded)
		if err != nil {
			if lastErr != nil {
				return RouteResult{Attempts: attempt - 1}, fmt.Errorf("%w after worker failure: %v", ErrNoWorker, lastErr)
			}
			return RouteResult{Attempts: attempt - 1}, err
		}
		excluded[worker.ID] = struct{}{}
		request.Attempt = uint32(attempt)
		attemptCtx, cancel := context.WithTimeout(ctx, r.attemptTimeout)
		response, callErr := r.invoker.Generate(attemptCtx, worker, request)
		cancel()
		r.registry.Release(worker.ID)
		if callErr == nil {
			if r.metrics != nil {
				r.metrics.RouteAttempts.WithLabelValues(worker.ID, "success").Inc()
			}
			return RouteResult{Response: response, Attempts: attempt}, nil
		}
		lastErr = callErr
		code := status.Code(callErr)
		if r.metrics != nil {
			r.metrics.RouteAttempts.WithLabelValues(worker.ID, code.String()).Inc()
		}
		if !retryable(code) || ctx.Err() != nil || attempt == r.maxAttempts {
			return RouteResult{Attempts: attempt}, callErr
		}
		r.registry.ReportFailure(worker.ID)
		if r.metrics != nil {
			r.metrics.Retries.WithLabelValues(code.String()).Inc()
		}
	}
	return RouteResult{Attempts: r.maxAttempts}, lastErr
}

func retryable(code codes.Code) bool {
	return code == codes.Unavailable || code == codes.ResourceExhausted || code == codes.DeadlineExceeded
}

func IsNoWorker(err error) bool { return errors.Is(err, ErrNoWorker) }
