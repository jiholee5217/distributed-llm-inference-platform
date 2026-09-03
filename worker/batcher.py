from __future__ import annotations

import queue
import threading
import time
from concurrent.futures import Future, TimeoutError
from dataclasses import dataclass

from api.inference.v1 import inference_pb2

from worker.metrics import WorkerMetrics


class BatcherClosedError(RuntimeError):
    pass


class BatcherFullError(RuntimeError):
    pass


@dataclass
class _WorkItem:
    request: inference_pb2.GenerateRequest
    future: Future[inference_pb2.GenerateResponse]
    enqueued_at: float


class DynamicBatcher:
    """One model queue that executes on size or oldest-request delay."""

    def __init__(
        self,
        worker_id: str,
        model_version: str,
        max_batch_size: int,
        max_queue_depth: int,
        max_queue_delay_ms: float,
        fake_latency_ms: float,
        metrics: WorkerMetrics,
    ) -> None:
        if max_batch_size < 1:
            raise ValueError("max_batch_size must be positive")
        if max_queue_depth < 1:
            raise ValueError("max_queue_depth must be positive")
        if max_queue_delay_ms < 0 or fake_latency_ms < 0:
            raise ValueError("delays must not be negative")
        self.worker_id = worker_id
        self.model_version = model_version
        self.max_batch_size = max_batch_size
        self.max_queue_depth = max_queue_depth
        self.max_queue_delay = max_queue_delay_ms / 1000.0
        self.fake_latency = fake_latency_ms / 1000.0
        self.metrics = metrics
        self._queue: queue.Queue[_WorkItem | None] = queue.Queue(maxsize=max_queue_depth)
        self._lock = threading.Lock()
        self._active_requests = 0
        self._completed_batches = 0
        self._last_batch_size = 0
        self._accepting = True
        self._thread = threading.Thread(target=self._run, name=f"batcher-{worker_id}", daemon=True)
        self._thread.start()

    def submit(
        self, request: inference_pb2.GenerateRequest, timeout: float | None
    ) -> inference_pb2.GenerateResponse:
        with self._lock:
            if not self._accepting:
                raise BatcherClosedError("worker is draining")
            future: Future[inference_pb2.GenerateResponse] = Future()
            # Enqueue while holding the acceptance lock. This makes drain/close
            # linearizable with submit and prevents a shutdown sentinel from
            # overtaking a request that was already admitted.
            try:
                self._queue.put_nowait(
                    _WorkItem(request=request, future=future, enqueued_at=time.monotonic())
                )
            except queue.Full as error:
                self.metrics.admission_rejections.labels(self.worker_id, "queue_full").inc()
                raise BatcherFullError(
                    f"worker queue reached its {self.max_queue_depth}-request limit"
                ) from error
        self.metrics.queue_depth.labels(self.worker_id).set(self._queue.qsize())
        try:
            return future.result(timeout=timeout)
        except TimeoutError:
            future.cancel()
            raise

    def load(self) -> tuple[int, int]:
        with self._lock:
            active = self._active_requests
        return active, self._queue.qsize()

    def stats(self) -> tuple[int, int]:
        with self._lock:
            return self._completed_batches, self._last_batch_size

    def drain(self) -> None:
        with self._lock:
            self._accepting = False

    def close(self, timeout: float = 5.0) -> None:
        self.drain()
        self._queue.put(None)
        self._thread.join(timeout)

    def _run(self) -> None:
        while True:
            first = self._queue.get()
            if first is None:
                return
            batch = [first]
            deadline = first.enqueued_at + self.max_queue_delay
            while len(batch) < self.max_batch_size:
                try:
                    # A request that already exceeded its queue-delay budget must
                    # execute now, but an existing backlog can still join its
                    # batch without adding latency.
                    item = self._queue.get_nowait()
                except queue.Empty:
                    remaining = deadline - time.monotonic()
                    if remaining <= 0:
                        break
                    try:
                        item = self._queue.get(timeout=remaining)
                    except queue.Empty:
                        break
                if item is None:
                    self._queue.put(None)
                    break
                batch.append(item)
            live_batch = [item for item in batch if not item.future.cancelled()]
            self.metrics.queue_depth.labels(self.worker_id).set(self._queue.qsize())
            if not live_batch:
                continue
            started = time.monotonic()
            with self._lock:
                self._active_requests += len(live_batch)
                self.metrics.active_requests.labels(self.worker_id).set(self._active_requests)
            self.metrics.batch_size.labels(self.worker_id).observe(len(live_batch))
            for item in live_batch:
                self.metrics.queue_wait.labels(self.worker_id).observe(started - item.enqueued_at)
            try:
                time.sleep(self.fake_latency)
                for item in live_batch:
                    if not item.future.cancelled():
                        item.future.set_result(self._infer(item.request))
            except BaseException as error:
                for item in live_batch:
                    if not item.future.cancelled():
                        item.future.set_exception(error)
            finally:
                self.metrics.batch_execution.labels(self.worker_id).observe(time.monotonic() - started)
                with self._lock:
                    self._active_requests -= len(live_batch)
                    self._completed_batches += 1
                    self._last_batch_size = len(live_batch)
                    self.metrics.active_requests.labels(self.worker_id).set(self._active_requests)

    def _infer(self, request: inference_pb2.GenerateRequest) -> inference_pb2.GenerateResponse:
        input_tokens = request.prompt.split()
        generated = ["FAKE:"] + [token.upper() for token in input_tokens]
        generated = generated[: max(1, request.max_new_tokens)]
        return inference_pb2.GenerateResponse(
            request_id=request.request_id,
            worker_id=self.worker_id,
            model_version=self.model_version,
            text=" ".join(generated),
            input_tokens=len(input_tokens),
            output_tokens=len(generated),
        )
