# Project tour for reviewers

This page is the shortest path through the engineering story. It distinguishes
the target architecture from the evidence currently present in the repository.

## The problem

Serving one model from one process is straightforward. Coordinating several model
workers becomes a distributed-systems problem when workers have different load,
miss heartbeats, restart with stale identity, drain during deployments, or fail
after accepting a request.

This project asks the controller to make those decisions with two very different
types of information:

- durable facts that require one agreed order, stored in a five-node Raft KV cluster
- live observations that change too quickly to justify a consensus write

The boundary between those two categories is the central design decision.

## Five-minute walkthrough

1. Start with the [diagram gallery](diagrams.md) for the complete system shape.
2. Read [ADR 0001](adr/0001-control-plane-state.md) for the Raft-versus-memory
   decision and its consequences.
3. Inspect the [protobuf contract](../api/inference/v1/inference.proto) for
   request IDs, attempt numbers, worker generations, leases, readiness, and load.
4. Use the [architecture notes](architecture.md) to review deadlines, batching,
   retries, and controller recovery.
5. Review the [recorded results](results/2026-08-15-fake-model.md) and
   [failure semantics](failure-semantics.md) for demonstrated behavior and limits.
6. Check the [roadmap](roadmap.md) to see what is implemented and the testable
   exit condition for every upcoming milestone.

## Engineering decisions worth discussing

| Decision | Reason | Tradeoff |
| --- | --- | --- |
| Store worker generations and lifecycle transitions in Raft | Controllers need one recoverable ordering | Every durable transition pays consensus latency |
| Keep heartbeats and load samples in memory | They are frequent and quickly stale | A restarted controller must wait for fresh data |
| Put batching in Python workers | Workers know model/runtime compatibility | Global queue optimization is intentionally deferred |
| Bound each worker queue by request count | Saturation fails fast instead of growing memory use and queue latency | Real models still need token- and GPU-memory-aware limits |
| Start with unary inference | Failure and retry semantics are easier to define | Token streaming requires a later resume/deduplication design |
| Begin with a deterministic fake model | Makes scheduling and fault tests cheap and repeatable | It does not demonstrate real model performance |
| Use request IDs plus attempt numbers | Correlates retries and metrics across services | Exactly-once inference is not implied |

## Evidence ledger

Architecture is only the hypothesis. Each feature must produce evidence:

| Claim | Current evidence |
| --- | --- |
| Workers recover from failure | Unit retry test plus live process-kill and packaged Docker failure experiments |
| Routing is load-aware | Deterministic capacity-normalized scheduling tests and balanced three-worker load runs |
| Dynamic batching improves throughput | Controlled batch-size-one versus batch-size-eight Locust comparison with p50/p95/p99 |
| Overload is bounded and retryable | Python queue-saturation and gRPC status tests plus a Go `ResourceExhausted` retry test |
| The system is observable | Prometheus scrape validation and a provisioned Grafana dashboard for routing and batching |
| Lifecycle state is strongly consistent | Live registration and lease-expiry values read back through the five-node Raft API |
| Rolling deployments preserve availability | Not demonstrated; remains Milestone 6 |
| Controller replicas coordinate safely | Not implemented; the controller remains single-instance |

## Current status

Milestones 1-5 are implemented around a deterministic fake model: Go routing,
Raft-backed registration/lifecycle state, Python dynamic batching, bounded
worker admission, leases, retries, metrics, dashboards, Locust workloads, and
worker fault injection. The controlled run measured 6.73x throughput from
batching; the packaged worker-kill run completed 10,437 requests without a
client-visible failure.

Real model execution, streaming, immediate queued-request cancellation,
controller high availability, rolling deployments, authenticated transport, and
Kubernetes have not been implemented. Claims remain scoped to the recorded
fake-model environment.

## Interview discussion prompts

- Why would writing every heartbeat to Raft be both expensive and misleading?
- What happens if a worker finishes inference but its response is lost?
- How should request deadlines interact with batching delay and retries?
- Which load signal produces stable routing without causing oscillation?
- How does a worker generation prevent a restarted stale process from reclaiming
  an old lease?
- What ownership mechanism is needed before running multiple active controllers?
- When would this design need sharded or Multi-Raft control-plane storage?
