package controlplane

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sort"
	"sync"
	"time"
)

var (
	ErrNoWorker        = errors.New("no eligible worker")
	ErrWorkerNotFound  = errors.New("worker not found")
	ErrStaleGeneration = errors.New("worker generation is stale")
	ErrLeaseRejected   = errors.New("worker lease is not valid")
)

type Model struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type Registration struct {
	ID             string  `json:"worker_id"`
	Generation     uint64  `json:"generation"`
	Endpoint       string  `json:"endpoint"`
	Models         []Model `json:"models"`
	MaxConcurrency uint32  `json:"max_concurrency"`
}

type Heartbeat struct {
	ID             string
	Generation     uint64
	LeaseID        string
	ActiveRequests uint32
	QueueDepth     uint32
	Ready          bool
}

type WorkerSnapshot struct {
	ID               string    `json:"worker_id"`
	Generation       uint64    `json:"generation"`
	Endpoint         string    `json:"endpoint"`
	Models           []Model   `json:"models"`
	MaxConcurrency   uint32    `json:"max_concurrency"`
	ActiveRequests   uint32    `json:"active_requests"`
	QueueDepth       uint32    `json:"queue_depth"`
	Reservations     uint32    `json:"controller_reservations"`
	Ready            bool      `json:"ready"`
	Draining         bool      `json:"draining"`
	LeaseExpired     bool      `json:"lease_expired"`
	LastHeartbeat    time.Time `json:"last_heartbeat"`
	LeaseExpiresAt   time.Time `json:"lease_expires_at"`
	QuarantinedUntil time.Time `json:"quarantined_until,omitempty"`
}

type workerRecord struct {
	registration Registration
	leaseID      string
	active       uint32
	queueDepth   uint32
	reservations uint32
	ready        bool
	draining     bool
	leaseExpired bool
	lastBeat     time.Time
	expiresAt    time.Time
	quarantined  time.Time
}

type Registry struct {
	mu           sync.Mutex
	workers      map[string]*workerRecord
	leaseTimeout time.Duration
	cooldown     time.Duration
	now          func() time.Time
}

func NewRegistry(leaseTimeout, cooldown time.Duration) *Registry {
	return newRegistryWithClock(leaseTimeout, cooldown, time.Now)
}

func newRegistryWithClock(leaseTimeout, cooldown time.Duration, now func() time.Time) *Registry {
	return &Registry{
		workers:      make(map[string]*workerRecord),
		leaseTimeout: leaseTimeout,
		cooldown:     cooldown,
		now:          now,
	}
}

func (r *Registry) CanRegister(registration Registration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.canRegisterLocked(registration)
}

func (r *Registry) canRegisterLocked(registration Registration) error {
	existing, ok := r.workers[registration.ID]
	if !ok {
		return nil
	}
	if registration.Generation < existing.registration.Generation {
		return ErrStaleGeneration
	}
	if registration.Generation == existing.registration.Generation && registration.Endpoint != existing.registration.Endpoint {
		return errors.New("same worker generation cannot change endpoint")
	}
	return nil
}

func (r *Registry) Register(registration Registration) (leaseID string, snapshot WorkerSnapshot, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.canRegisterLocked(registration); err != nil {
		return "", WorkerSnapshot{}, err
	}
	leaseID, err = randomID()
	if err != nil {
		return "", WorkerSnapshot{}, err
	}
	now := r.now()
	record := &workerRecord{
		registration: registration,
		leaseID:      leaseID,
		ready:        false,
		lastBeat:     now,
		expiresAt:    now.Add(r.leaseTimeout),
	}
	r.workers[registration.ID] = record
	return leaseID, snapshotOf(record), nil
}

func (r *Registry) Heartbeat(heartbeat Heartbeat) (drain bool, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.workers[heartbeat.ID]
	if !ok || record.registration.Generation != heartbeat.Generation || record.leaseID != heartbeat.LeaseID || record.leaseExpired {
		return false, ErrLeaseRejected
	}
	now := r.now()
	record.active = heartbeat.ActiveRequests
	record.queueDepth = heartbeat.QueueDepth
	record.ready = heartbeat.Ready && !record.draining
	record.lastBeat = now
	record.expiresAt = now.Add(r.leaseTimeout)
	return record.draining, nil
}

func (r *Registry) Acquire(model, version string, excluded map[string]struct{}) (WorkerSnapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	eligible := make([]*workerRecord, 0, len(r.workers))
	for _, record := range r.workers {
		if _, skip := excluded[record.registration.ID]; skip {
			continue
		}
		if !record.ready || record.draining || record.leaseExpired || !record.quarantined.IsZero() && now.Before(record.quarantined) {
			continue
		}
		if !now.Before(record.expiresAt) || !supports(record.registration.Models, model, version) {
			continue
		}
		eligible = append(eligible, record)
	}
	if len(eligible) == 0 {
		return WorkerSnapshot{}, ErrNoWorker
	}
	sort.Slice(eligible, func(i, j int) bool {
		left, right := eligible[i], eligible[j]
		leftLoad := uint64(left.active) + uint64(left.queueDepth) + uint64(left.reservations)
		rightLoad := uint64(right.active) + uint64(right.queueDepth) + uint64(right.reservations)
		leftCapacity := uint64(max(left.registration.MaxConcurrency, 1))
		rightCapacity := uint64(max(right.registration.MaxConcurrency, 1))
		if leftLoad*rightCapacity != rightLoad*leftCapacity {
			return leftLoad*rightCapacity < rightLoad*leftCapacity
		}
		return left.registration.ID < right.registration.ID
	})
	chosen := eligible[0]
	chosen.reservations++
	return snapshotOf(chosen), nil
}

func (r *Registry) Release(workerID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if record, ok := r.workers[workerID]; ok && record.reservations > 0 {
		record.reservations--
	}
}

func (r *Registry) ReportFailure(workerID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if record, ok := r.workers[workerID]; ok {
		record.quarantined = r.now().Add(r.cooldown)
	}
}

func (r *Registry) Drain(workerID string) (WorkerSnapshot, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.workers[workerID]
	if !ok {
		return WorkerSnapshot{}, false
	}
	record.draining = true
	record.ready = false
	return snapshotOf(record), true
}

func (r *Registry) ExpireStale() []WorkerSnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	expired := make([]WorkerSnapshot, 0)
	for _, record := range r.workers {
		if record.leaseExpired || now.Before(record.expiresAt) {
			continue
		}
		record.leaseExpired = true
		record.ready = false
		expired = append(expired, snapshotOf(record))
	}
	return expired
}

func (r *Registry) List() []WorkerSnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	workers := make([]WorkerSnapshot, 0, len(r.workers))
	for _, record := range r.workers {
		workers = append(workers, snapshotOf(record))
	}
	sort.Slice(workers, func(i, j int) bool { return workers[i].ID < workers[j].ID })
	return workers
}

func (r *Registry) Get(workerID string) (WorkerSnapshot, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.workers[workerID]
	if !ok {
		return WorkerSnapshot{}, false
	}
	return snapshotOf(record), true
}

func snapshotOf(record *workerRecord) WorkerSnapshot {
	models := append([]Model(nil), record.registration.Models...)
	return WorkerSnapshot{
		ID:               record.registration.ID,
		Generation:       record.registration.Generation,
		Endpoint:         record.registration.Endpoint,
		Models:           models,
		MaxConcurrency:   record.registration.MaxConcurrency,
		ActiveRequests:   record.active,
		QueueDepth:       record.queueDepth,
		Reservations:     record.reservations,
		Ready:            record.ready,
		Draining:         record.draining,
		LeaseExpired:     record.leaseExpired,
		LastHeartbeat:    record.lastBeat,
		LeaseExpiresAt:   record.expiresAt,
		QuarantinedUntil: record.quarantined,
	}
}

func supports(models []Model, name, version string) bool {
	for _, model := range models {
		if model.Name == name && model.Version == version {
			return true
		}
	}
	return false
}

func randomID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
