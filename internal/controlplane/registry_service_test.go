package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	inferencev1 "github.com/jiholee5217/distributed-llm-inference-platform/gen/inference/v1"
)

type failingStore struct{}

func (failingStore) Put(context.Context, string, string) error {
	return errors.New("store unavailable")
}

type recoverableStore struct {
	failuresRemaining atomic.Int32
	calls             atomic.Int32
	memory            *MemoryStore
}

func (s *recoverableStore) Put(ctx context.Context, key, value string) error {
	s.calls.Add(1)
	if s.failuresRemaining.Add(-1) >= 0 {
		return errors.New("temporary store outage")
	}
	return s.memory.Put(ctx, key, value)
}

func TestRegistrationPersistsBeforeAdmission(t *testing.T) {
	registry := NewRegistry(3*time.Second, time.Second)
	store := NewMemoryStore()
	service := NewRegistryService(registry, store, nil, 500*time.Millisecond, 3*time.Second)
	response, err := service.Register(context.Background(), &inferencev1.RegisterWorkerRequest{
		WorkerId: "worker-a", Generation: 1, Endpoint: "worker-a:50051", MaxConcurrency: 8,
		Models: []*inferencev1.ModelCapability{{Name: "fake-llm", Version: "v1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.GetLeaseId() == "" {
		t.Fatal("expected a lease ID")
	}
	stored, ok := store.Get(workerRegistrationKey("worker-a"))
	if !ok {
		t.Fatal("expected durable registration")
	}
	var value map[string]any
	if err := json.Unmarshal([]byte(stored), &value); err != nil {
		t.Fatal(err)
	}
	if value["status"] != "registered" || !strings.Contains(stored, "worker-a:50051") {
		t.Fatalf("unexpected durable value: %s", stored)
	}
}

func TestDrainDoesNotMutateRoutingStateWhenPersistenceFails(t *testing.T) {
	registry := NewRegistry(3*time.Second, time.Second)
	registerReady(t, registry, Registration{
		ID: "worker-a", Generation: 1, Endpoint: "worker-a:50051", MaxConcurrency: 8,
		Models: []Model{{Name: "fake-llm", Version: "v1"}},
	})
	service := NewRegistryService(registry, failingStore{}, nil, 500*time.Millisecond, 3*time.Second)
	if _, err := service.Drain(context.Background(), "worker-a"); err == nil {
		t.Fatal("expected persistence failure")
	}
	worker, ok := registry.Get("worker-a")
	if !ok || !worker.Ready || worker.Draining {
		t.Fatalf("worker routing state changed despite failed durable write: %+v", worker)
	}
}

func TestLeaseExpiryPersistenceRetriesAfterStoreRecovery(t *testing.T) {
	now := time.Now()
	registry := newRegistryWithClock(time.Second, time.Second, func() time.Time { return now })
	registerReady(t, registry, Registration{
		ID: "worker-a", Generation: 1, Endpoint: "worker-a:50051", MaxConcurrency: 8,
		Models: []Model{{Name: "fake-llm", Version: "v1"}},
	})
	now = now.Add(2 * time.Second)
	store := &recoverableStore{memory: NewMemoryStore()}
	store.failuresRemaining.Store(1)
	service := NewRegistryService(registry, store, nil, 500*time.Millisecond, time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go service.RunLeaseReaper(ctx, 5*time.Millisecond)

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if stored, ok := store.memory.Get(workerStatusKey("worker-a")); ok {
			if !strings.Contains(stored, `"status":"unavailable"`) {
				t.Fatalf("unexpected durable status: %s", stored)
			}
			if store.calls.Load() < 2 {
				t.Fatalf("expected a retry, got %d store call", store.calls.Load())
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("lease-expiry status was not persisted after the store recovered")
}
