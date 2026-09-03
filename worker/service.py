from __future__ import annotations

import threading
import time
from concurrent.futures import TimeoutError

import grpc
from api.inference.v1 import inference_pb2, inference_pb2_grpc

from worker.batcher import BatcherClosedError, BatcherFullError, DynamicBatcher
from worker.metrics import WorkerMetrics


class InferenceService(inference_pb2_grpc.InferenceWorkerServicer):
    def __init__(
        self,
        worker_id: str,
        model: str,
        model_version: str,
        batcher: DynamicBatcher,
        metrics: WorkerMetrics,
        fail_every_n: int = 0,
    ) -> None:
        self.worker_id = worker_id
        self.model = model
        self.model_version = model_version
        self.batcher = batcher
        self.metrics = metrics
        self.fail_every_n = fail_every_n
        self._lock = threading.Lock()
        self._requests_seen = 0
        self._draining = False

    def Generate(self, request, context):  # noqa: N802 - gRPC generated method name
        started = time.monotonic()
        outcome = "success"
        try:
            if not request.request_id or not request.prompt:
                outcome = "invalid"
                context.abort(grpc.StatusCode.INVALID_ARGUMENT, "request_id and prompt are required")
            if request.model != self.model or request.model_version != self.model_version:
                outcome = "wrong_model"
                context.abort(grpc.StatusCode.FAILED_PRECONDITION, "model or version is not loaded")
            with self._lock:
                if self._draining:
                    outcome = "draining"
                    context.abort(grpc.StatusCode.UNAVAILABLE, "worker is draining")
                self._requests_seen += 1
                should_fail = self.fail_every_n > 0 and self._requests_seen % self.fail_every_n == 0
            if should_fail:
                outcome = "injected_failure"
                context.abort(grpc.StatusCode.UNAVAILABLE, "injected worker failure")
            timeout = context.time_remaining()
            if timeout is not None and timeout <= 0:
                outcome = "deadline"
                context.abort(grpc.StatusCode.DEADLINE_EXCEEDED, "request deadline elapsed")
            try:
                return self.batcher.submit(request, timeout)
            except TimeoutError:
                outcome = "deadline"
                context.abort(grpc.StatusCode.DEADLINE_EXCEEDED, "deadline elapsed while queued")
            except BatcherClosedError as error:
                outcome = "draining"
                context.abort(grpc.StatusCode.UNAVAILABLE, str(error))
            except BatcherFullError as error:
                outcome = "overloaded"
                context.abort(grpc.StatusCode.RESOURCE_EXHAUSTED, str(error))
        finally:
            self.metrics.requests.labels(self.worker_id, outcome).inc()
            self.metrics.request_latency.labels(self.worker_id).observe(time.monotonic() - started)

    def set_draining(self) -> None:
        with self._lock:
            self._draining = True
        self.batcher.drain()

    def load(self) -> tuple[int, int]:
        return self.batcher.load()
