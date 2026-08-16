package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	inferencev1 "github.com/jiholee5217/distributed-llm-inference-platform/gen/inference/v1"
	"github.com/jiholee5217/distributed-llm-inference-platform/internal/controlplane"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc"
)

func main() {
	var (
		httpListen        = flag.String("http-listen", env("HTTP_LISTEN", ":8090"), "HTTP API and metrics listen address")
		grpcListen        = flag.String("grpc-listen", env("GRPC_LISTEN", ":8091"), "worker registry gRPC listen address")
		durableMode       = flag.String("durable-store", env("DURABLE_STORE", "raft"), "durable store: raft or memory")
		kvEndpoints       = flag.String("kv-endpoints", env("KV_ENDPOINTS", "http://127.0.0.1:8081,http://127.0.0.1:8082,http://127.0.0.1:8083,http://127.0.0.1:8084,http://127.0.0.1:8085"), "comma-separated Raft KV endpoints")
		heartbeatInterval = flag.Duration("heartbeat-interval", 500*time.Millisecond, "worker heartbeat interval")
		leaseTimeout      = flag.Duration("lease-timeout", 3*time.Second, "worker lease timeout")
		failureCooldown   = flag.Duration("failure-cooldown", time.Second, "temporary routing quarantine after worker RPC failure")
		attemptTimeout    = flag.Duration("attempt-timeout", 2*time.Second, "per-worker inference RPC timeout")
		maxAttempts       = flag.Int("max-attempts", 3, "maximum distinct workers attempted per request")
	)
	flag.Parse()
	if *maxAttempts < 1 {
		exit(errors.New("max-attempts must be positive"))
	}
	store, err := durableStore(*durableMode, *kvEndpoints)
	if err != nil {
		exit(err)
	}
	metricsRegistry := prometheus.NewRegistry()
	metrics := controlplane.NewMetrics(metricsRegistry)
	registry := controlplane.NewRegistry(*leaseTimeout, *failureCooldown)
	registryService := controlplane.NewRegistryService(registry, store, metrics, *heartbeatInterval, *leaseTimeout)
	invoker := controlplane.NewGRPCInvoker()
	defer invoker.Close()
	router := controlplane.NewRouter(registry, invoker, metrics, *maxAttempts, *attemptTimeout)
	api := controlplane.NewHTTPAPI(router, registry, registryService, metrics)

	grpcListener, err := net.Listen("tcp", *grpcListen)
	if err != nil {
		exit(err)
	}
	grpcServer := grpc.NewServer()
	inferencev1.RegisterWorkerRegistryServer(grpcServer, registryService)

	rootMux := http.NewServeMux()
	rootMux.Handle("/metrics", promhttp.HandlerFor(metricsRegistry, promhttp.HandlerOpts{}))
	rootMux.Handle("/", api.Handler())
	httpServer := &http.Server{Addr: *httpListen, Handler: rootMux, ReadHeaderTimeout: 5 * time.Second}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go registryService.RunLeaseReaper(ctx, *heartbeatInterval)
	go func() {
		slog.Info("worker registry listening", "address", *grpcListen)
		if err := grpcServer.Serve(grpcListener); err != nil {
			slog.Error("gRPC server stopped", "error", err)
			stop()
		}
	}()
	go func() {
		slog.Info("gateway listening", "address", *httpListen, "durable_store", *durableMode)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("HTTP server stopped", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	grpcServer.GracefulStop()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		slog.Error("HTTP shutdown failed", "error", err)
	}
}

func durableStore(mode, endpoints string) (controlplane.DurableStore, error) {
	switch mode {
	case "memory":
		return controlplane.NewMemoryStore(), nil
	case "raft":
		return controlplane.NewRaftStore(strings.Split(endpoints, ","), 3*time.Second)
	default:
		return nil, fmt.Errorf("unknown durable store %q", mode)
	}
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func exit(err error) {
	slog.Error("fatal error", "error", err)
	os.Exit(1)
}
