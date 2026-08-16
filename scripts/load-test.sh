#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$repo_root"

users=${USERS:-32}
spawn_rate=${SPAWN_RATE:-32}
run_time=${RUN_TIME:-15s}

docker compose --profile load run --rm locust \
  --headless \
  --users "$users" \
  --spawn-rate "$spawn_rate" \
  --run-time "$run_time" \
  --host http://controller:8090
