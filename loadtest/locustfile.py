import itertools

from locust import HttpUser, between, task


_requests = itertools.count()


class InferenceUser(HttpUser):
    wait_time = between(0.001, 0.01)

    @task
    def generate(self) -> None:
        sequence = next(_requests)
        with self.client.post(
            "/v1/generate",
            json={
                "request_id": f"locust-{sequence}",
                "model": "fake-llm",
                "model_version": "v1",
                "prompt": f"distributed inference request {sequence}",
                "max_new_tokens": 32,
                "timeout_ms": 3000,
            },
            name="POST /v1/generate",
            catch_response=True,
        ) as response:
            if response.status_code != 200:
                response.failure(f"HTTP {response.status_code}: {response.text[:200]}")
                return
            body = response.json()
            if not body.get("worker_id") or not body.get("text", "").startswith("FAKE:"):
                response.failure("response is missing worker or deterministic fake output")
