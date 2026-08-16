# Roadmap

Each milestone should end with tests, a runnable demo, and an honest update to
the project claims.

## Milestone 0 — contracts and boundaries

- [x] Define component responsibilities and data ownership
- [x] Separate durable Raft state from ephemeral heartbeat/load data
- [x] Add an initial versioned protobuf contract
- [x] Review, generate, and compile Go and Python bindings

Exit condition: the request path and failure assumptions can be explained before
implementation begins.

## Milestone 1 — single-worker vertical slice

- [x] Implement the Go gateway/controller
- [x] Implement the Python gRPC worker
- [x] Implement deterministic fake inference
- [x] Propagate request IDs, deadlines, and structured errors
- [x] Run the vertical slice with Docker Compose

Exit condition: one client request crosses Go to Python over gRPC and returns a
tested deterministic result.

## Milestone 2 — worker lifecycle and routing

- [x] Register worker identity, generation, model, and capacity through Raft
- [x] Accept periodic heartbeat/load reports
- [x] Expire unhealthy workers using leases
- [x] Route across at least three workers using capacity-normalized load
- [x] Rebuild controller liveness from worker re-registration after restart

Exit condition: tests demonstrate join, healthy routing, overload avoidance,
failure exclusion, and re-registration.

## Milestone 3 — dynamic batching

- [x] Queue compatible requests per model/version
- [x] Implement maximum batch size and queue delay
- [ ] Propagate cancellation while queued
- [x] Measure batch size, queue wait, execution latency, and throughput

Exit condition: a reproducible experiment shows the latency/throughput tradeoff.

## Milestone 4 — failure recovery

- [x] Define retryable gRPC status codes
- [x] Add bounded retry budgets and attempt numbers
- [x] Test worker failure before admission and during execution
- [x] Add fault-injection scripts
- [x] Document non-execution, duplicate-compute, and unknown-outcome cases

Exit condition: failure tests match the documented request semantics.

## Milestone 5 — observability and load testing

- [x] Export Prometheus metrics from the controller and inference workers
- [x] Add Grafana dashboards for the golden signals and batching
- [x] Add Locust workloads
- [x] Record hardware, model, workload, and configuration with results

Exit condition: dashboards explain a load test and identify an injected failure.

## Milestone 6 — rolling deployments

- [ ] Store desired model versions and rollout generations in Raft
- [x] Add readiness and manual draining
- [ ] Implement progressive replacement and rollback
- [ ] Demonstrate uninterrupted traffic during a rollout
- [ ] Port the proven Docker topology to Kubernetes

Exit condition: a tested rollout preserves availability and can be rolled back.
