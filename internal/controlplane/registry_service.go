package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"regexp"
	"sync"
	"time"

	inferencev1 "github.com/jiholee5217/distributed-llm-inference-platform/gen/inference/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var workerIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

type RegistryService struct {
	inferencev1.UnimplementedWorkerRegistryServer
	registry          *Registry
	store             DurableStore
	metrics           *Metrics
	heartbeatInterval time.Duration
	leaseTimeout      time.Duration
	registerMu        sync.Mutex
}

func NewRegistryService(registry *Registry, store DurableStore, metrics *Metrics, heartbeatInterval, leaseTimeout time.Duration) *RegistryService {
	return &RegistryService{
		registry: registry, store: store, metrics: metrics,
		heartbeatInterval: heartbeatInterval, leaseTimeout: leaseTimeout,
	}
}

func (s *RegistryService) Register(ctx context.Context, request *inferencev1.RegisterWorkerRequest) (*inferencev1.RegisterWorkerResponse, error) {
	registration, err := registrationFromProto(request)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	s.registerMu.Lock()
	defer s.registerMu.Unlock()
	if err := s.registry.CanRegister(registration); err != nil {
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}
	durable := struct {
		Registration Registration `json:"registration"`
		Status       string       `json:"status"`
		UpdatedAt    time.Time    `json:"updated_at"`
	}{Registration: registration, Status: "registered", UpdatedAt: time.Now().UTC()}
	encoded, err := json.Marshal(durable)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if err := s.store.Put(ctx, workerRegistrationKey(registration.ID), string(encoded)); err != nil {
		return nil, status.Errorf(codes.Unavailable, "persist worker registration: %v", err)
	}
	leaseID, _, err := s.registry.Register(registration)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if s.metrics != nil {
		s.metrics.WorkerEvents.WithLabelValues("registered").Inc()
		s.metrics.Workers.Set(float64(len(s.registry.List())))
	}
	return &inferencev1.RegisterWorkerResponse{
		LeaseId:             leaseID,
		HeartbeatIntervalMs: uint32(s.heartbeatInterval.Milliseconds()),
		LeaseTimeoutMs:      uint32(s.leaseTimeout.Milliseconds()),
	}, nil
}

func (s *RegistryService) Heartbeat(_ context.Context, request *inferencev1.HeartbeatRequest) (*inferencev1.HeartbeatResponse, error) {
	drain, err := s.registry.Heartbeat(Heartbeat{
		ID: request.GetWorkerId(), Generation: request.GetGeneration(), LeaseID: request.GetLeaseId(),
		ActiveRequests: request.GetActiveRequests(), QueueDepth: request.GetQueueDepth(), Ready: request.GetReady(),
	})
	if err != nil {
		return &inferencev1.HeartbeatResponse{Accepted: false}, nil
	}
	return &inferencev1.HeartbeatResponse{Accepted: true, Drain: drain}, nil
}

func (s *RegistryService) RunLeaseReaper(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	pending := make(map[string]WorkerSnapshot)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, worker := range s.registry.ExpireStale() {
				pending[worker.ID] = worker
				if s.metrics != nil {
					s.metrics.WorkerEvents.WithLabelValues("lease_expired").Inc()
				}
			}
			for workerID, worker := range pending {
				updateCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
				err := s.persistUnavailable(updateCtx, worker)
				cancel()
				if err == nil {
					delete(pending, workerID)
				}
			}
		}
	}
}

func (s *RegistryService) persistUnavailable(ctx context.Context, worker WorkerSnapshot) error {
	value, err := json.Marshal(map[string]any{
		"worker_id": worker.ID, "generation": worker.Generation,
		"status": "unavailable", "updated_at": time.Now().UTC(),
	})
	if err != nil {
		return err
	}
	return s.store.Put(ctx, workerStatusKey(worker.ID), string(value))
}

func (s *RegistryService) Drain(ctx context.Context, workerID string) (WorkerSnapshot, error) {
	s.registerMu.Lock()
	defer s.registerMu.Unlock()
	worker, ok := s.registry.Get(workerID)
	if !ok {
		return WorkerSnapshot{}, ErrWorkerNotFound
	}
	value, _ := json.Marshal(map[string]any{
		"worker_id": worker.ID, "generation": worker.Generation,
		"status": "draining", "updated_at": time.Now().UTC(),
	})
	if err := s.store.Put(ctx, workerStatusKey(workerID), string(value)); err != nil {
		return WorkerSnapshot{}, fmt.Errorf("persist drain status: %w", err)
	}
	worker, ok = s.registry.Drain(workerID)
	if !ok {
		return WorkerSnapshot{}, ErrWorkerNotFound
	}
	if s.metrics != nil {
		s.metrics.WorkerEvents.WithLabelValues("draining").Inc()
	}
	return worker, nil
}

func registrationFromProto(request *inferencev1.RegisterWorkerRequest) (Registration, error) {
	if !workerIDPattern.MatchString(request.GetWorkerId()) {
		return Registration{}, errors.New("worker_id must be 1-128 letters, digits, dots, underscores, or hyphens")
	}
	if request.GetGeneration() == 0 {
		return Registration{}, errors.New("generation must be positive")
	}
	if _, _, err := net.SplitHostPort(request.GetEndpoint()); err != nil {
		return Registration{}, errors.New("endpoint must be host:port")
	}
	if request.GetMaxConcurrency() == 0 {
		return Registration{}, errors.New("max_concurrency must be positive")
	}
	if len(request.GetModels()) == 0 {
		return Registration{}, errors.New("at least one model capability is required")
	}
	models := make([]Model, 0, len(request.GetModels()))
	for _, model := range request.GetModels() {
		if model.GetName() == "" || model.GetVersion() == "" {
			return Registration{}, errors.New("model name and version are required")
		}
		models = append(models, Model{Name: model.GetName(), Version: model.GetVersion()})
	}
	return Registration{
		ID: request.GetWorkerId(), Generation: request.GetGeneration(), Endpoint: request.GetEndpoint(),
		Models: models, MaxConcurrency: request.GetMaxConcurrency(),
	}, nil
}

func workerRegistrationKey(workerID string) string { return "workers." + workerID + ".registration" }
func workerStatusKey(workerID string) string       { return "workers." + workerID + ".status" }
