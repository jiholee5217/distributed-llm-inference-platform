from __future__ import annotations

import argparse
import logging
import os
import signal
import threading
import time
from concurrent import futures

import grpc
from api.inference.v1 import inference_pb2_grpc
from prometheus_client import start_http_server

from worker.batcher import DynamicBatcher
from worker.metrics import WorkerMetrics
from worker.registry_client import RegistryClient
from worker.service import InferenceService


def main() -> None:
    parser = argparse.ArgumentParser(description="Dynamic-batching fake LLM worker")
    parser.add_argument("--worker-id", default=os.getenv("WORKER_ID", "worker-1"))
    parser.add_argument("--listen", default=os.getenv("LISTEN_ADDRESS", "[::]:50051"))
    parser.add_argument("--advertise", default=os.getenv("ADVERTISE_ENDPOINT", "127.0.0.1:50051"))
    parser.add_argument("--controller", default=os.getenv("CONTROLLER_ENDPOINT", "127.0.0.1:8091"))
    parser.add_argument("--model", default=os.getenv("MODEL_NAME", "fake-llm"))
    parser.add_argument("--model-version", default=os.getenv("MODEL_VERSION", "v1"))
    parser.add_argument("--max-concurrency", type=int, default=int(os.getenv("MAX_CONCURRENCY", "32")))
    parser.add_argument("--max-batch-size", type=int, default=int(os.getenv("MAX_BATCH_SIZE", "8")))
    parser.add_argument("--max-queue-delay-ms", type=float, default=float(os.getenv("MAX_QUEUE_DELAY_MS", "5")))
    parser.add_argument("--fake-latency-ms", type=float, default=float(os.getenv("FAKE_LATENCY_MS", "20")))
    parser.add_argument("--metrics-port", type=int, default=int(os.getenv("METRICS_PORT", "9100")))
    parser.add_argument("--fail-every-n", type=int, default=int(os.getenv("FAIL_EVERY_N", "0")))
    parser.add_argument("--generation", type=int, default=int(os.getenv("WORKER_GENERATION", str(time.time_ns()))))
    args = parser.parse_args()

    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")
    metrics = WorkerMetrics()
    batcher = DynamicBatcher(
        worker_id=args.worker_id,
        model_version=args.model_version,
        max_batch_size=args.max_batch_size,
        max_queue_delay_ms=args.max_queue_delay_ms,
        fake_latency_ms=args.fake_latency_ms,
        metrics=metrics,
    )
    service = InferenceService(
        worker_id=args.worker_id,
        model=args.model,
        model_version=args.model_version,
        batcher=batcher,
        metrics=metrics,
        fail_every_n=args.fail_every_n,
    )
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=args.max_concurrency))
    inference_pb2_grpc.add_InferenceWorkerServicer_to_server(service, server)
    if server.add_insecure_port(args.listen) == 0:
        raise RuntimeError(f"could not bind inference server to {args.listen}")
    server.start()
    start_http_server(args.metrics_port, registry=metrics.registry)

    registry = RegistryClient(
        controller_endpoint=args.controller,
        worker_id=args.worker_id,
        generation=args.generation,
        advertise_endpoint=args.advertise,
        model=args.model,
        model_version=args.model_version,
        max_concurrency=args.max_concurrency,
        service=service,
    )
    registry.start()
    logging.info(
        "worker %s listening on %s with batch_size=%s queue_delay_ms=%s",
        args.worker_id,
        args.listen,
        args.max_batch_size,
        args.max_queue_delay_ms,
    )

    stopped = threading.Event()

    def stop(_signum, _frame) -> None:
        stopped.set()

    signal.signal(signal.SIGINT, stop)
    signal.signal(signal.SIGTERM, stop)
    stopped.wait()
    registry.stop()
    server.stop(grace=5).wait()
    batcher.close()


if __name__ == "__main__":
    main()
