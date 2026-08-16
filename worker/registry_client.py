from __future__ import annotations

import logging
import threading
import time

import grpc
from api.inference.v1 import inference_pb2, inference_pb2_grpc

from worker.service import InferenceService


class RegistryClient:
    def __init__(
        self,
        controller_endpoint: str,
        worker_id: str,
        generation: int,
        advertise_endpoint: str,
        model: str,
        model_version: str,
        max_concurrency: int,
        service: InferenceService,
    ) -> None:
        self.controller_endpoint = controller_endpoint
        self.worker_id = worker_id
        self.generation = generation
        self.advertise_endpoint = advertise_endpoint
        self.model = model
        self.model_version = model_version
        self.max_concurrency = max_concurrency
        self.service = service
        self._stop = threading.Event()
        self._thread = threading.Thread(target=self._run, name=f"registry-{worker_id}", daemon=True)

    def start(self) -> None:
        self._thread.start()

    def stop(self) -> None:
        self._stop.set()
        self._thread.join(timeout=5)

    def _run(self) -> None:
        backoff = 0.25
        with grpc.insecure_channel(self.controller_endpoint) as channel:
            client = inference_pb2_grpc.WorkerRegistryStub(channel)
            while not self._stop.is_set():
                try:
                    registration = client.Register(
                        inference_pb2.RegisterWorkerRequest(
                            worker_id=self.worker_id,
                            generation=self.generation,
                            endpoint=self.advertise_endpoint,
                            models=[
                                inference_pb2.ModelCapability(name=self.model, version=self.model_version)
                            ],
                            max_concurrency=self.max_concurrency,
                        ),
                        timeout=3,
                        wait_for_ready=True,
                    )
                    logging.info("registered worker %s generation %s", self.worker_id, self.generation)
                    backoff = 0.25
                    if self._heartbeat_loop(client, registration):
                        return
                except grpc.RpcError as error:
                    logging.warning("registration failed: %s", error)
                self._stop.wait(backoff)
                backoff = min(backoff * 2, 5)

    def _heartbeat_loop(self, client, registration) -> bool:
        interval = max(registration.heartbeat_interval_ms / 1000.0, 0.05)
        while not self._stop.wait(interval):
            active, queued = self.service.load()
            try:
                response = client.Heartbeat(
                    inference_pb2.HeartbeatRequest(
                        worker_id=self.worker_id,
                        generation=self.generation,
                        lease_id=registration.lease_id,
                        active_requests=active,
                        queue_depth=queued,
                        ready=True,
                    ),
                    timeout=max(interval, 1),
                )
            except grpc.RpcError as error:
                logging.warning("heartbeat failed: %s", error)
                return False
            if not response.accepted:
                logging.info("lease rejected; re-registering")
                return False
            if response.drain:
                logging.info("controller requested worker drain")
                self.service.set_draining()
                return True
        return True
