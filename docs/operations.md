# Running and observing the platform

## Start the complete topology

Requirements: Docker with Compose and approximately 6 GB of available memory.

```bash
docker compose up --detach --build
```

This starts five pinned Raft KV nodes, one Go controller/gateway, three Python
workers, Prometheus, and Grafana. The KV build context is pinned to commit
`8ad3ef6fdfe28cbc78d8ec7ebd44394adf68fba6` of the sibling project. To build
from a local checkout instead:

```bash
KV_STORE_CONTEXT=../distributed-kv-store docker compose up --detach --build
```

Workers retry registration while the Raft cluster elects a leader. Confirm all
three are ready:

```bash
curl -s http://127.0.0.1:8090/v1/workers
```

Each worker admits at most `MAX_QUEUE_DEPTH` waiting requests (16 in the Docker
topology), and gRPC limits total concurrent RPCs to `MAX_CONCURRENCY` (32).
Requests beyond either bound receive a retryable `ResourceExhausted` response
instead of allowing memory use and queue latency to grow indefinitely.

## Send inference

```bash
curl -sS -X POST http://127.0.0.1:8090/v1/generate \
  -H 'Content-Type: application/json' \
  -d '{
    "request_id": "demo-1",
    "model": "fake-llm",
    "model_version": "v1",
    "prompt": "distributed inference works",
    "max_new_tokens": 32,
    "timeout_ms": 3000
  }'
```

The fake model returns deterministic uppercase text. Its fixed batch latency is
useful for repeatable scheduler and batching tests; it is not an LLM-quality or
GPU-performance benchmark.

## Observe the system

| Surface | URL | Purpose |
| --- | --- | --- |
| Gateway health | <http://127.0.0.1:8090/healthz> | Controller process and known-worker count |
| Worker state | <http://127.0.0.1:8090/v1/workers> | Generations, leases, load, readiness, and drain state |
| Controller metrics | <http://127.0.0.1:8090/metrics> | Requests, latency, routing, retries, and lifecycle events |
| Prometheus | <http://127.0.0.1:9090> | Metric queries and scrape status |
| Grafana | <http://127.0.0.1:3000> | Provisioned distributed-inference dashboard |

The dashboard includes per-worker queue depth and admission-rejection rate. A
nonzero rejection rate means the controller had to retry work that a saturated
worker declined before execution.

## Run load and failure experiments

```bash
./scripts/load-test.sh
```

Override the default workload without editing files:

```bash
USERS=64 SPAWN_RATE=64 RUN_TIME=30s ./scripts/load-test.sh
```

Run a sustained workload, stop `worker-1`, and print recovery evidence:

```bash
./scripts/failure-demo.sh
```

The failure script intentionally leaves `worker-1` stopped. Restore it with:

```bash
docker compose start worker-1
```

## Stop safely

```bash
docker compose down
```

This preserves Raft volumes. Use `docker compose down --volumes` only when you
intentionally want to erase the platform's persisted control-plane state.
