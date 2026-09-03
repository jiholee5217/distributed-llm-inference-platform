from concurrent.futures import ThreadPoolExecutor
from threading import Event
from time import monotonic, sleep

import grpc
import pytest
from api.inference.v1 import inference_pb2
from prometheus_client import generate_latest

from worker.batcher import DynamicBatcher
from worker.metrics import WorkerMetrics
from worker.service import InferenceService


class _AbortedRequest(RuntimeError):
    def __init__(self, code: grpc.StatusCode, details: str) -> None:
        super().__init__(details)
        self.code = code


class _Context:
    def time_remaining(self) -> float:
        return 1.0

    def abort(self, code: grpc.StatusCode, details: str) -> None:
        raise _AbortedRequest(code, details)


def _request(sequence: int) -> inference_pb2.GenerateRequest:
    return inference_pb2.GenerateRequest(
        request_id=f"request-{sequence}",
        attempt=1,
        model="fake-llm",
        model_version="v1",
        prompt="hello worker",
        max_new_tokens=16,
    )


def test_full_queue_returns_retryable_resource_exhausted(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    execution_started = Event()
    release_execution = Event()

    def block_execution(_delay: float) -> None:
        execution_started.set()
        release_execution.wait(timeout=1)

    monkeypatch.setattr("worker.batcher.time.sleep", block_execution)
    metrics = WorkerMetrics()
    batcher = DynamicBatcher(
        worker_id="worker-test",
        model_version="v1",
        max_batch_size=1,
        max_queue_depth=1,
        max_queue_delay_ms=0,
        fake_latency_ms=1,
        metrics=metrics,
    )
    service = InferenceService(
        worker_id="worker-test",
        model="fake-llm",
        model_version="v1",
        batcher=batcher,
        metrics=metrics,
    )

    try:
        with ThreadPoolExecutor(max_workers=2) as pool:
            active = pool.submit(batcher.submit, _request(1), 1)
            assert execution_started.wait(timeout=1)

            queued = pool.submit(batcher.submit, _request(2), 1)
            deadline = monotonic() + 1
            while batcher.load()[1] == 0 and monotonic() < deadline:
                sleep(0.001)
            assert batcher.load()[1] == 1

            with pytest.raises(_AbortedRequest) as aborted:
                service.Generate(_request(3), _Context())
            assert aborted.value.code is grpc.StatusCode.RESOURCE_EXHAUSTED

            release_execution.set()
            active.result()
            queued.result()
    finally:
        batcher.close()

    exported = generate_latest(metrics.registry)
    assert (
        b'llm_platform_worker_admission_rejections_total{reason="queue_full",worker="worker-test"} 1.0'
        in exported
    )
    assert (
        b'llm_platform_worker_requests_total{outcome="overloaded",worker="worker-test"} 1.0'
        in exported
    )
