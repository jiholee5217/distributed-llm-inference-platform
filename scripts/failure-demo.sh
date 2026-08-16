#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$repo_root"

docker compose up --detach --build

for attempt in $(seq 1 60); do
  ready=$(curl -fsS http://127.0.0.1:8090/v1/workers 2>/dev/null || true)
  if [[ $(grep -o '"ready":true' <<<"$ready" | wc -l | tr -d ' ') == "3" ]]; then
    break
  fi
  if [[ "$attempt" == "60" ]]; then
    echo "workers did not become ready" >&2
    exit 1
  fi
  sleep 1
done

docker compose --profile load run --rm locust \
  --headless --users 32 --spawn-rate 32 --run-time 15s \
  --host http://controller:8090 &
load_pid=$!

sleep 5
docker compose stop worker-1
wait "$load_pid"

echo "Worker state after injected failure:"
curl -fsS http://127.0.0.1:8090/v1/workers
echo
echo "Retry and lease metrics:"
curl -fsS http://127.0.0.1:8090/metrics | grep -E 'retries_total|lease_expired'
