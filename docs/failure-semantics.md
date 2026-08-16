# Failure and retry semantics

The platform provides bounded failover, not exactly-once inference. The same
request ID is preserved across attempts so logs and metrics can correlate work,
but a request ID alone cannot prove that a failed worker did not execute.

## Worker lifecycle

1. A worker registers its ID, generation, endpoint, model capabilities, and
   capacity. The controller commits that registration through Raft before
   issuing a lease.
2. Heartbeats refresh ephemeral liveness and load. They are not written through
   consensus.
3. A late worker becomes suspect implicitly: RPC failure places it in a short
   routing quarantine.
4. When its lease expires, the worker is removed from the ready set and an
   `unavailable` lifecycle transition is committed through Raft.
5. An expired lease cannot be revived by a heartbeat. The worker must register
   again; a stale generation is rejected.

## Request outcomes

| Failure point | Controller behavior | Execution guarantee |
| --- | --- | --- |
| No eligible worker before dispatch | Return HTTP 503 | Request did not reach a worker |
| Worker rejects before admission | Retry a different worker when budget permits | Normally one execution, but transport errors can obscure admission |
| Connection fails during execution | Retry only for configured gRPC status, deadline, and attempt budget | Original attempt may have executed; outcome can be unknown |
| End-to-end deadline expires | Return HTTP 504 | Queued work is cancelled when observed; executing work may finish |
| All distinct workers fail | Return HTTP 502/503 with request ID and attempt count | One or more attempts may have executed |

Retryable gRPC statuses are `Unavailable`, `ResourceExhausted`, and
`DeadlineExceeded`. Each attempt selects a distinct ready worker. The default
maximum is three attempts, and every attempt is bounded by both a per-worker
timeout and the original client deadline.

## Why this is not exactly once

Suppose a worker finishes generation, then loses its connection before the
response reaches the controller. The controller cannot distinguish that case
from a worker that failed before execution. Retrying can therefore duplicate
compute even though the client receives only one response.

Exactly-once claims would require durable admission/deduplication state and a
replayable result protocol. Token streaming would additionally require resume
positions or deterministic replay. Both are deliberately outside the current
unary fake-model milestone.

## Demonstrated behavior

The packaged failure demo stops one of three workers during a 32-user Locust
run. On the recorded environment, the controller made three transparent
retries, committed one lease-expiry transition through Raft, and completed
10,437 requests with zero client-visible failures. See the
[recorded results](results/2026-08-15-fake-model.md).
