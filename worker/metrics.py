from prometheus_client import CollectorRegistry, Counter, Gauge, Histogram


class WorkerMetrics:
    def __init__(self, registry: CollectorRegistry | None = None) -> None:
        self.registry = registry or CollectorRegistry()
        self.requests = Counter(
            "llm_platform_worker_requests_total",
            "Inference requests by final outcome.",
            ["worker", "outcome"],
            registry=self.registry,
        )
        self.admission_rejections = Counter(
            "llm_platform_worker_admission_rejections_total",
            "Requests rejected before admission to the worker queue.",
            ["worker", "reason"],
            registry=self.registry,
        )
        self.request_latency = Histogram(
            "llm_platform_worker_request_duration_seconds",
            "Worker-side request latency including queue wait.",
            ["worker"],
            buckets=(0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1),
            registry=self.registry,
        )
        self.batch_size = Histogram(
            "llm_platform_worker_batch_size",
            "Requests executed in each model batch.",
            ["worker"],
            buckets=(1, 2, 4, 8, 16, 32),
            registry=self.registry,
        )
        self.batch_execution = Histogram(
            "llm_platform_worker_batch_execution_seconds",
            "Fake model execution time per batch.",
            ["worker"],
            buckets=(0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5),
            registry=self.registry,
        )
        self.queue_wait = Histogram(
            "llm_platform_worker_queue_wait_seconds",
            "Time a request waits before batch execution.",
            ["worker"],
            buckets=(0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1),
            registry=self.registry,
        )
        self.queue_depth = Gauge(
            "llm_platform_worker_queue_depth",
            "Requests currently waiting for a batch.",
            ["worker"],
            registry=self.registry,
        )
        self.active_requests = Gauge(
            "llm_platform_worker_active_requests",
            "Requests currently executing in a model batch.",
            ["worker"],
            registry=self.registry,
        )
