# Fake-model routing, batching, and failure results

Date: 2026-08-15

These measurements characterize the distributed runtime and deterministic fake
model. They are not claims about transformer inference, GPU throughput, token
streaming, or production hardware.

## Environment

- MacBook Pro with Apple M1 Pro, 10 CPU cores, and 16 GB memory
- macOS 26.5.2
- Go 1.26.5 and Python 3.13.7
- five-node Raft KV cluster at commit `8ad3ef6fdfe28cbc78d8ec7ebd44394adf68fba6`
- one Go controller and three Python gRPC workers
- 20 ms deterministic fake-model latency per executed batch
- 5 ms maximum queue delay, worker concurrency 32
- Locust 2.46.3 with 32 users, spawn rate 32 users/second, duration 15 seconds

## Controlled dynamic-batching comparison

Both comparison runs used the same host processes, workload, live Dockerized
Raft cluster, three workers, and model latency. Only `max_batch_size` changed.

| Configuration | Requests | Failures | Throughput | Average | p50 | p95 | p99 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Batch size 1 | 1,508 | 0 | 101.98 req/s | 304 ms | 270 ms | 400 ms | 410 ms |
| Dynamic batch up to 8 | 10,129 | 0 | 686.37 req/s | 40 ms | 41 ms | 48 ms | 60 ms |

The dynamic-batching run executed 10,146 observed worker requests in 1,904
batches, for a mean batch size of 5.33. Under this saturated fake-model workload,
batching delivered **6.73x throughput** and reduced p95 latency by **88%**. The
latency decrease occurs because batch-size-one workers accumulate long queues
under 32-user load; it does not imply batching reduces latency at every load.

## Worker-failure experiment

The packaged Docker experiment ran the full topology and stopped `worker-1`
five seconds into the same 15-second, 32-user workload.

| Requests | Failures | Throughput | Average | p50 | p95 | p99 | Maximum |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 10,437 | 0 | 704.59 req/s | 38 ms | 37 ms | 55 ms | 59 ms | 70 ms |

The controller recorded three `Unavailable` retries, expired the stopped
worker's lease, excluded it from routing, and committed the worker's
`unavailable` lifecycle transition through the Raft KV cluster. Two workers
continued serving the workload with no client-visible failures.

This experiment demonstrates crash-stop worker recovery for the tested unary
fake-model workload. It does not prove exactly-once execution: an attempt whose
response is lost may have completed before a retry. See
[failure semantics](../failure-semantics.md).

## Reproduce

```bash
docker compose up --detach --build
./scripts/load-test.sh
./scripts/failure-demo.sh
```

Raw Locust CSV files from exploratory runs were not committed because they embed
machine-local timestamps and are easy to regenerate. The table above records
the exact configuration and terminal summaries used for the claims.
