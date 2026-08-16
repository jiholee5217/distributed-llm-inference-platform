package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	inferencev1 "github.com/jiholee5217/distributed-llm-inference-platform/gen/inference/v1"
)

const maxBodyBytes = 1 << 20

type HTTPAPI struct {
	router          *Router
	registry        *Registry
	registryService *RegistryService
	metrics         *Metrics
}

func NewHTTPAPI(router *Router, registry *Registry, registryService *RegistryService, metrics *Metrics) *HTTPAPI {
	return &HTTPAPI{router: router, registry: registry, registryService: registryService, metrics: metrics}
}

func (a *HTTPAPI) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", a.health)
	mux.HandleFunc("POST /v1/generate", a.generate)
	mux.HandleFunc("GET /v1/workers", a.workers)
	mux.HandleFunc("POST /v1/workers/{worker_id}/drain", a.drain)
	return mux
}

type generateInput struct {
	RequestID    string `json:"request_id"`
	Model        string `json:"model"`
	ModelVersion string `json:"model_version"`
	Prompt       string `json:"prompt"`
	MaxNewTokens uint32 `json:"max_new_tokens"`
	TimeoutMS    uint32 `json:"timeout_ms"`
}

func (a *HTTPAPI) generate(writer http.ResponseWriter, request *http.Request) {
	started := time.Now()
	outcome := "success"
	defer func() {
		if a.metrics != nil {
			a.metrics.Requests.WithLabelValues(outcome).Inc()
			a.metrics.RequestLatency.WithLabelValues(outcome).Observe(time.Since(started).Seconds())
		}
	}()
	var input generateInput
	if err := decodeJSON(writer, request, &input); err != nil {
		outcome = "invalid"
		writeError(writer, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(input.Prompt) == "" || len(input.Prompt) > 16*1024 {
		outcome = "invalid"
		writeError(writer, http.StatusBadRequest, errors.New("prompt must contain 1-16384 bytes"))
		return
	}
	if input.Model == "" {
		input.Model = "fake-llm"
	}
	if input.ModelVersion == "" {
		input.ModelVersion = "v1"
	}
	if input.MaxNewTokens == 0 {
		input.MaxNewTokens = 64
	}
	if input.TimeoutMS == 0 {
		input.TimeoutMS = 5000
	}
	if input.TimeoutMS < 100 || input.TimeoutMS > 30000 {
		outcome = "invalid"
		writeError(writer, http.StatusBadRequest, errors.New("timeout_ms must be between 100 and 30000"))
		return
	}
	requestID := input.RequestID
	if requestID == "" {
		requestID = request.Header.Get("X-Request-ID")
	}
	if requestID == "" {
		var err error
		requestID, err = randomID()
		if err != nil {
			outcome = "internal"
			writeError(writer, http.StatusInternalServerError, err)
			return
		}
	}
	ctx, cancel := context.WithTimeout(request.Context(), time.Duration(input.TimeoutMS)*time.Millisecond)
	defer cancel()
	result, err := a.router.Generate(ctx, &inferencev1.GenerateRequest{
		RequestId: requestID, Model: input.Model, ModelVersion: input.ModelVersion,
		Prompt: input.Prompt, MaxNewTokens: input.MaxNewTokens,
	})
	if err != nil {
		statusCode := http.StatusBadGateway
		outcome = "worker_error"
		if IsNoWorker(err) {
			statusCode = http.StatusServiceUnavailable
			outcome = "no_worker"
		} else if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			statusCode = http.StatusGatewayTimeout
			outcome = "deadline"
		}
		writeJSON(writer, statusCode, map[string]any{"error": err.Error(), "request_id": requestID, "attempts": result.Attempts})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"request_id": result.Response.GetRequestId(), "worker_id": result.Response.GetWorkerId(),
		"model_version": result.Response.GetModelVersion(), "text": result.Response.GetText(),
		"input_tokens": result.Response.GetInputTokens(), "output_tokens": result.Response.GetOutputTokens(),
		"attempts": result.Attempts,
	})
}

func (a *HTTPAPI) health(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]any{"status": "ok", "known_workers": len(a.registry.List())})
}

func (a *HTTPAPI) workers(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]any{"workers": a.registry.List()})
}

func (a *HTTPAPI) drain(writer http.ResponseWriter, request *http.Request) {
	worker, err := a.registryService.Drain(request.Context(), request.PathValue("worker_id"))
	if err != nil {
		statusCode := http.StatusInternalServerError
		if errors.Is(err, ErrWorkerNotFound) {
			statusCode = http.StatusNotFound
		}
		writeError(writer, statusCode, err)
		return
	}
	writeJSON(writer, http.StatusOK, worker)
}

func decodeJSON(writer http.ResponseWriter, request *http.Request, destination any) error {
	request.Body = http.MaxBytesReader(writer, request.Body, maxBodyBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("request body must contain one JSON object")
	}
	return nil
}

func writeError(writer http.ResponseWriter, statusCode int, err error) {
	writeJSON(writer, statusCode, map[string]string{"error": err.Error()})
}

func writeJSON(writer http.ResponseWriter, statusCode int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(statusCode)
	_ = json.NewEncoder(writer).Encode(value)
}
