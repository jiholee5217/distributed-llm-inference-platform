from concurrent.futures import ThreadPoolExecutor
from threading import Barrier
from time import monotonic, sleep

import pytest
from api.inference.v1 import inference_pb2

from worker.batcher import BatcherClosedError, DynamicBatcher
from worker.metrics import WorkerMetrics


def make_batcher(**overrides) -> DynamicBatcher:
    options = {
        "worker_id": "worker-test",
        "model_version": "v1",
        "max_batch_size": 8,
        "max_queue_delay_ms": 20,
        "fake_latency_ms": 2,
        "metrics": WorkerMetrics(),
    }
    options.update(overrides)
    return DynamicBatcher(**options)


def request(sequence: int) -> inference_pb2.GenerateRequest:
    return inference_pb2.GenerateRequest(
        request_id=f"request-{sequence}",
        attempt=1,
        model="fake-llm",
        model_version="v1",
        prompt=f"hello worker {sequence}",
        max_new_tokens=16,
    )


def test_concurrent_requests_form_one_full_batch() -> None:
    batcher = make_batcher()
    barrier = Barrier(8)

    def submit(sequence: int):
        barrier.wait()
        return batcher.submit(request(sequence), timeout=1)

    with ThreadPoolExecutor(max_workers=8) as pool:
        responses = list(pool.map(submit, range(8)))

    completed, last_size = batcher.stats()
    batcher.close()
    assert completed == 1
    assert last_size == 8
    assert {response.request_id for response in responses} == {f"request-{index}" for index in range(8)}
    assert all(response.text.startswith("FAKE: HELLO WORKER") for response in responses)


def test_single_request_waits_for_queue_delay() -> None:
    batcher = make_batcher(max_queue_delay_ms=10, fake_latency_ms=0)
    started = monotonic()
    response = batcher.submit(request(1), timeout=1)
    elapsed = monotonic() - started
    batcher.close()
    assert response.worker_id == "worker-test"
    assert elapsed >= 0.008


def test_drain_rejects_new_work() -> None:
    batcher = make_batcher()
    batcher.drain()
    with pytest.raises(BatcherClosedError):
        batcher.submit(request(1), timeout=1)
    batcher.close()


def test_backlog_is_drained_without_extra_queue_delay() -> None:
    batcher = make_batcher(max_batch_size=4, max_queue_delay_ms=1, fake_latency_ms=40)
    with ThreadPoolExecutor(max_workers=9) as pool:
        first = pool.submit(batcher.submit, request(0), 1)
        deadline = monotonic() + 1
        while batcher.load()[0] == 0 and monotonic() < deadline:
            sleep(0.001)
        rest = [pool.submit(batcher.submit, request(index), 1) for index in range(1, 9)]
        first.result()
        for future in rest:
            future.result()

    completed, last_size = batcher.stats()
    batcher.close()
    assert completed == 3
    assert last_size == 4
