package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type DurableStore interface {
	Put(ctx context.Context, key, value string) error
}

type RaftStore struct {
	endpoints []string
	client    *http.Client
	next      atomic.Uint64
}

func NewRaftStore(endpoints []string, timeout time.Duration) (*RaftStore, error) {
	cleaned := make([]string, 0, len(endpoints))
	for _, endpoint := range endpoints {
		endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
		parsed, err := url.ParseRequestURI(endpoint)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return nil, fmt.Errorf("invalid Raft endpoint %q", endpoint)
		}
		cleaned = append(cleaned, endpoint)
	}
	if len(cleaned) == 0 {
		return nil, errors.New("at least one Raft endpoint is required")
	}
	return &RaftStore{endpoints: cleaned, client: &http.Client{Timeout: timeout}}, nil
}

func (s *RaftStore) Put(ctx context.Context, key, value string) error {
	payload, err := json.Marshal(map[string]string{"value": value})
	if err != nil {
		return err
	}
	start := int(s.next.Add(1)-1) % len(s.endpoints)
	var failures []string
	for offset := range s.endpoints {
		endpoint := s.endpoints[(start+offset)%len(s.endpoints)] + "/v1/kv/" + url.PathEscape(key)
		request, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		request.Header.Set("Content-Type", "application/json")
		response, err := s.client.Do(request)
		if err != nil {
			failures = append(failures, err.Error())
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		response.Body.Close()
		if response.StatusCode == http.StatusCreated {
			return nil
		}
		failures = append(failures, fmt.Sprintf("%s returned %d: %s", endpoint, response.StatusCode, strings.TrimSpace(string(body))))
	}
	return fmt.Errorf("Raft write failed on every endpoint: %s", strings.Join(failures, "; "))
}

type MemoryStore struct {
	mu     sync.Mutex
	values map[string]string
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{values: make(map[string]string)}
}

func (s *MemoryStore) Put(_ context.Context, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[key] = value
	return nil
}

func (s *MemoryStore) Get(key string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.values[key]
	return value, ok
}
