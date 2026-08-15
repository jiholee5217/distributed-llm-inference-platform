# Architecture

## System boundary

The first version is a control-plane and scheduling project, not a new model
runtime. A Python worker may initially execute a deterministic fake model. That
keeps tests fast and makes routing, batching, timeouts, and recovery observable
without GPU availability becoming a prerequisite.

## Request path

1. The gateway validates a request, assigns a stable request ID, and establishes
   an end-to-end deadline.
2. The controller filters workers by model, version, readiness, and heartbeat
   freshness.
3. A load-aware policy ranks eligible workers using queue depth, active requests,
   and reported capacity.
4. The controller calls the selected worker over gRPC and propagates the deadline
   and cancellation signal.
5. The worker queues the request, forms a batch within configured size and delay
   limits, runs inference, and returns a response.
6. Each service records latency and failure metrics using the same request ID.

## Control-plane state

### Durable and strongly consistent

The Raft KV store will hold state that must survive controller restarts or be
observed in one agreed order:

- worker identity and generation
- model/version capabilities declared at registration
- desired deployment state and rollout generation
- worker lifecycle transitions such as registered, draining, and removed
- routing policy configuration
- request metadata only where an idempotency design requires it

Keys will use explicit namespaces, for example:

```text
/workers/{worker_id}/registration
/deployments/{deployment_id}/desired
/deployments/{deployment_id}/status
/config/routing
```

### Ephemeral and high-frequency

The controller keeps heartbeat timestamps, queue depth, active request count,
and short-lived load samples in memory. A controller restart rebuilds this view
from fresh worker registrations and heartbeats. Only meaningful lifecycle
transitions are written through Raft.

This split is necessary because the existing KV system is one Raft group: its
leader serializes writes. Writing every heartbeat would consume consensus
capacity without adding a useful durability guarantee.

## Dynamic batching

Batching belongs in the Python worker because it owns the model runtime and can
decide which requests are compatible. The initial policy will expose two knobs:

- `max_batch_size`: upper bound on requests in one execution
- `max_queue_delay_ms`: upper bound on how long the oldest request waits for a batch

Metrics must show the latency/throughput tradeoff: observed batch size, queue
wait, execution time, and end-to-end latency.

## Failure semantics

- A worker becomes ineligible after its heartbeat lease expires.
- In-flight unary requests may be retried only when the controller can establish
  that no response was delivered and the retry budget/deadline permits it.
- Streaming retries require a separate resume or deduplication protocol and are
  intentionally postponed.
- Each retry uses the same request ID and emits an attempt number.
- Draining workers remain alive for admitted work but receive no new requests.
- A controller restart reconstructs durable desired state from Raft and waits for
  workers to re-establish ephemeral liveness.

## Initial deployment topology

Docker Compose will run:

- five existing Raft KV nodes
- one Go gateway/controller process initially
- two or more Python workers
- Prometheus
- Grafana

Controller high availability is a later milestone. Before adding replicas, the
project must define leader election or ownership of reconciliation work so two
controllers do not issue conflicting actions.
