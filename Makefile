GOCACHE ?= /tmp/llm-platform-go-cache
PYTHON ?= .venv/bin/python

.PHONY: generate test test-go test-python format verify

generate:
	./scripts/generate-proto.sh

test: test-go test-python

test-go:
	GOCACHE=$(GOCACHE) go test -race ./...

test-python:
	PYTHONPATH=worker/generated:. $(PYTHON) -m pytest worker/tests

format:
	gofmt -w cmd internal

verify: test
	GOCACHE=$(GOCACHE) go vet ./...
	GOCACHE=$(GOCACHE) go build -o /tmp/llm-platform-controller ./cmd/controller
	test -z "$$(gofmt -l cmd internal)"
	$(PYTHON) -m json.tool deploy/grafana/dashboards/platform.json >/dev/null
	docker compose config --quiet
	git diff --check
