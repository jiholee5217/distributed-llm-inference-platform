#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$repo_root"

tool_bin=${TOOL_BIN:-$repo_root/.tools/bin}
python_bin=${PYTHON_BIN:-$repo_root/.venv/bin/python}

mkdir -p worker/generated
PATH="$tool_bin:$PATH" "$python_bin" -m grpc_tools.protoc \
  -I. \
  --go_out=. \
  --go_opt=module=github.com/jiholee5217/distributed-llm-inference-platform \
  --go-grpc_out=. \
  --go-grpc_opt=module=github.com/jiholee5217/distributed-llm-inference-platform \
  --python_out=worker/generated \
  --grpc_python_out=worker/generated \
  api/inference/v1/inference.proto
