package controlplane

import (
	"errors"
	"math"
	"testing"
	"time"
)

func TestAcquireBalancesControllerReservations(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	registry := newRegistryWithClock(3*time.Second, time.Second, func() time.Time { return now })
	registerReady(t, registry, Registration{ID: "worker-a", Generation: 1, Endpoint: "a:50051", Models: []Model{{Name: "fake-llm", Version: "v1"}}, MaxConcurrency: 8})
	registerReady(t, registry, Registration{ID: "worker-b", Generation: 1, Endpoint: "b:50051", Models: []Model{{Name: "fake-llm", Version: "v1"}}, MaxConcurrency: 8})

	first, err := registry.Acquire("fake-llm", "v1", map[string]struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := registry.Acquire("fake-llm", "v1", map[string]struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != "worker-a" || second.ID != "worker-b" {
		t.Fatalf("expected deterministic balancing across a then b, got %s then %s", first.ID, second.ID)
	}
}

func TestAcquireUsesCapacityNormalizedLoad(t *testing.T) {
	now := time.Now()
	registry := newRegistryWithClock(3*time.Second, time.Second, func() time.Time { return now })
	leaseA := registerReady(t, registry, Registration{ID: "small", Generation: 1, Endpoint: "a:50051", Models: []Model{{Name: "fake-llm", Version: "v1"}}, MaxConcurrency: 2})
	leaseB := registerReady(t, registry, Registration{ID: "large", Generation: 1, Endpoint: "b:50051", Models: []Model{{Name: "fake-llm", Version: "v1"}}, MaxConcurrency: 10})
	if _, err := registry.Heartbeat(Heartbeat{ID: "small", Generation: 1, LeaseID: leaseA, ActiveRequests: 1, Ready: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Heartbeat(Heartbeat{ID: "large", Generation: 1, LeaseID: leaseB, ActiveRequests: 2, Ready: true}); err != nil {
		t.Fatal(err)
	}

	worker, err := registry.Acquire("fake-llm", "v1", map[string]struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	if worker.ID != "large" {
		t.Fatalf("expected lower normalized load worker, got %s", worker.ID)
	}
}

func TestAcquireLoadScoreDoesNotOverflow(t *testing.T) {
	registry := NewRegistry(3*time.Second, time.Second)
	leaseA := registerReady(t, registry, Registration{ID: "overloaded", Generation: 1, Endpoint: "a:50051", Models: []Model{{Name: "fake-llm", Version: "v1"}}, MaxConcurrency: 8})
	registerReady(t, registry, Registration{ID: "available", Generation: 1, Endpoint: "b:50051", Models: []Model{{Name: "fake-llm", Version: "v1"}}, MaxConcurrency: 8})
	if _, err := registry.Heartbeat(Heartbeat{ID: "overloaded", Generation: 1, LeaseID: leaseA, ActiveRequests: math.MaxUint32, QueueDepth: 1, Ready: true}); err != nil {
		t.Fatal(err)
	}

	worker, err := registry.Acquire("fake-llm", "v1", map[string]struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	if worker.ID != "available" {
		t.Fatalf("expected available worker despite uint32 load sum, got %s", worker.ID)
	}
}

func TestLeaseExpiryRequiresReregistration(t *testing.T) {
	now := time.Now()
	registry := newRegistryWithClock(time.Second, time.Second, func() time.Time { return now })
	lease := registerReady(t, registry, Registration{ID: "worker-a", Generation: 1, Endpoint: "a:50051", Models: []Model{{Name: "fake-llm", Version: "v1"}}, MaxConcurrency: 8})
	now = now.Add(2 * time.Second)
	expired := registry.ExpireStale()
	if len(expired) != 1 || expired[0].ID != "worker-a" {
		t.Fatalf("expected worker-a to expire, got %+v", expired)
	}
	if _, err := registry.Heartbeat(Heartbeat{ID: "worker-a", Generation: 1, LeaseID: lease, Ready: true}); !errors.Is(err, ErrLeaseRejected) {
		t.Fatalf("expected rejected expired lease, got %v", err)
	}
	if _, err := registry.Acquire("fake-llm", "v1", map[string]struct{}{}); !errors.Is(err, ErrNoWorker) {
		t.Fatalf("expected no eligible worker, got %v", err)
	}
}

func TestRejectsStaleGeneration(t *testing.T) {
	registry := NewRegistry(3*time.Second, time.Second)
	registration := Registration{ID: "worker-a", Generation: 2, Endpoint: "a:50051", Models: []Model{{Name: "fake-llm", Version: "v1"}}, MaxConcurrency: 8}
	if _, _, err := registry.Register(registration); err != nil {
		t.Fatal(err)
	}
	registration.Generation = 1
	if _, _, err := registry.Register(registration); !errors.Is(err, ErrStaleGeneration) {
		t.Fatalf("expected stale generation error, got %v", err)
	}
}

func registerReady(t *testing.T, registry *Registry, registration Registration) string {
	t.Helper()
	lease, _, err := registry.Register(registration)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Heartbeat(Heartbeat{ID: registration.ID, Generation: registration.Generation, LeaseID: lease, Ready: true}); err != nil {
		t.Fatal(err)
	}
	return lease
}
