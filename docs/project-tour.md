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
5. Check the [roadmap](roadmap.md) to see what is implemented and the testable
   exit condition for every upcoming milestone.

## Engineering decisions worth discussing

| Decision | Reason | Tradeoff |
| --- | --- | --- |
| Store deployment intent and worker generations in Raft | Controllers need one recoverable ordering | Every durable transition pays consensus latency |
| Keep heartbeats and load samples in memory | They are frequent and quickly stale | A restarted controller must wait for fresh data |
| Put batching in Python workers | Workers know model/runtime compatibility | Global queue optimization is intentionally deferred |
| Start with unary inference | Failure and retry semantics are easier to define | Token streaming requires a later resume/deduplication design |
| Begin with a deterministic fake model | Makes scheduling and fault tests cheap and repeatable | It does not demonstrate real model performance |
| Use request IDs plus attempt numbers | Correlates retries and metrics across services | Exactly-once inference is not implied |

## Evidence plan

Architecture is only the hypothesis. Each feature must produce evidence:

| Claim | Required evidence before claiming it |
| --- | --- |
| Workers recover from failure | Automated heartbeat-expiry, exclusion, re-registration, and in-flight request tests |
| Routing is load-aware | Deterministic scheduler tests plus a skewed-load experiment |
| Dynamic batching improves throughput | Reproducible batch-size/queue-delay experiments with p50, p95, and p99 latency |
| Rolling deployments preserve availability | Fault-injected rollout and rollback demo with request-success metrics |
| The system is observable | Dashboards connecting queue depth, batch size, latency, retries, and worker health |
| The control plane survives restart | Test that reloads Raft state and requires fresh liveness before routing |

## Current status

Phase 0 contains architecture boundaries, a control-plane state decision, a
milestone plan, and an initial protobuf contract. Services, generated clients,
Docker topology, tests, dashboards, and benchmarks have not been implemented yet.

That status is intentional: repository claims should advance only when the
corresponding demonstration or automated test lands.

## Interview discussion prompts

- Why would writing every heartbeat to Raft be both expensive and misleading?
- What happens if a worker finishes inference but its response is lost?
- How should request deadlines interact with batching delay and retries?
- Which load signal produces stable routing without causing oscillation?
- How does a worker generation prevent a restarted stale process from reclaiming
  an old lease?
- What ownership mechanism is needed before running multiple active controllers?
- When would this design need sharded or Multi-Raft control-plane storage?
