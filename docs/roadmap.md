# Roadmap

Each milestone should end with tests, a runnable demo, and an honest update to
the project claims.

## Milestone 0 — contracts and boundaries

- [x] Define component responsibilities and data ownership
- [x] Separate durable Raft state from ephemeral heartbeat/load data
- [x] Add an initial versioned protobuf contract
- [ ] Review and revise the contract before generating code

Exit condition: the request path and failure assumptions can be explained before
implementation begins.

## Milestone 1 — single-worker vertical slice

- [ ] Scaffold Go gateway/controller
- [ ] Scaffold Python gRPC worker
- [ ] Implement deterministic fake inference
- [ ] Propagate request IDs, deadlines, errors, and cancellation
- [ ] Run the vertical slice with Docker Compose

Exit condition: one client request crosses Go to Python over gRPC and returns a
tested deterministic result.

## Milestone 2 — worker lifecycle and routing

- [ ] Register worker identity, generation, model, and capacity
- [ ] Accept periodic heartbeat/load reports
- [ ] Expire unhealthy workers using leases
- [ ] Route across at least three workers using a documented load score
- [ ] Rebuild controller liveness after restart

Exit condition: tests demonstrate join, healthy routing, overload avoidance,
failure exclusion, and re-registration.

## Milestone 3 — dynamic batching

- [ ] Queue compatible requests per model/version
- [ ] Implement maximum batch size and queue delay
- [ ] Propagate cancellation while queued
- [ ] Measure batch size, queue wait, execution latency, and throughput

Exit condition: a reproducible experiment shows the latency/throughput tradeoff.

## Milestone 4 — failure recovery

- [ ] Define retryable gRPC status codes
- [ ] Add bounded retry budgets and attempt numbers
- [ ] Test worker failure before admission and during execution
- [ ] Add fault-injection scripts
- [ ] Document at-most-once, at-least-once, and unknown-outcome cases

Exit condition: failure tests match the documented request semantics.

## Milestone 5 — observability and load testing

- [ ] Export Prometheus metrics from every service
- [ ] Add Grafana dashboards for the golden signals and batching
- [ ] Add k6 or Locust workloads
- [ ] Record hardware, model, workload, and configuration with results

Exit condition: dashboards explain a load test and identify an injected failure.

## Milestone 6 — rolling deployments

- [ ] Store desired model versions and rollout generations in Raft
- [ ] Add readiness and draining
- [ ] Implement progressive replacement and rollback
- [ ] Demonstrate uninterrupted traffic during a rollout
- [ ] Port the proven Docker topology to Kubernetes

Exit condition: a tested rollout preserves availability and can be rolled back.
