# ADR 0001: Split durable control state from ephemeral load state

- Status: Accepted for initial implementation
- Date: 2026-08-15

## Context

The platform needs worker registration, health detection, load-aware routing, and
recoverable deployment decisions. Its control-plane database is an existing
five-node Raft key-value store. A single Raft group provides an ordered,
strongly consistent history but serializes writes through its leader.

Heartbeat and load samples arrive frequently and become stale quickly. Persisting
every sample through consensus would add write load while giving the scheduler an
old view by the time the data is recovered.

## Decision

Persist worker identity, generation, declared capabilities, desired deployments,
routing configuration, and meaningful lifecycle transitions in the Raft KV store.

Keep latest heartbeat time, queue depth, active request count, and short-lived
load samples in controller memory. After a controller restart, workers must
re-register or heartbeat before becoming eligible for routing.

## Consequences

- The control plane can recover desired state and worker identity from Raft.
- The routing view is deliberately unavailable until fresh liveness arrives.
- Heartbeat volume does not directly consume Raft consensus throughput.
- Worker lifecycle transitions need generation checks to reject stale workers.
- A future highly available controller must define ownership of ephemeral state
  and reconciliation; this ADR does not solve controller leader election.
