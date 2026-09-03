package controlplane

import (
	"context"
	"testing"
	"time"

	inferencev1 "github.com/jiholee5217/distributed-llm-inference-platform/gen/inference/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type recordingInvoker struct {
	workers     []string
	attempts    []uint32
	failureCode codes.Code
}

func (i *recordingInvoker) Generate(_ context.Context, worker WorkerSnapshot, request *inferencev1.GenerateRequest) (*inferencev1.GenerateResponse, error) {
	i.workers = append(i.workers, worker.ID)
	i.attempts = append(i.attempts, request.GetAttempt())
	if worker.ID == "worker-a" {
		failureCode := i.failureCode
		if failureCode == codes.OK {
			failureCode = codes.Unavailable
		}
		return nil, status.Error(failureCode, "injected failure")
	}
	return &inferencev1.GenerateResponse{RequestId: request.GetRequestId(), WorkerId: worker.ID, Text: "FAKE: OK"}, nil
}

func TestRouterRetriesWorkerOverload(t *testing.T) {
	registry := NewRegistry(3*time.Second, time.Second)
	registerReady(t, registry, Registration{ID: "worker-a", Generation: 1, Endpoint: "a:50051", Models: []Model{{Name: "fake-llm", Version: "v1"}}, MaxConcurrency: 8})
	registerReady(t, registry, Registration{ID: "worker-b", Generation: 1, Endpoint: "b:50051", Models: []Model{{Name: "fake-llm", Version: "v1"}}, MaxConcurrency: 8})
	invoker := &recordingInvoker{failureCode: codes.ResourceExhausted}
	router := NewRouter(registry, invoker, nil, 3, time.Second)

	result, err := router.Generate(context.Background(), &inferencev1.GenerateRequest{
		RequestId: "request-overload", Model: "fake-llm", ModelVersion: "v1", Prompt: "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Attempts != 2 || result.Response.GetWorkerId() != "worker-b" {
		t.Fatalf("expected overload retry on worker-b, got %+v", result)
	}
}

func TestRouterRetriesDistinctWorker(t *testing.T) {
	registry := NewRegistry(3*time.Second, time.Second)
	registerReady(t, registry, Registration{ID: "worker-a", Generation: 1, Endpoint: "a:50051", Models: []Model{{Name: "fake-llm", Version: "v1"}}, MaxConcurrency: 8})
	registerReady(t, registry, Registration{ID: "worker-b", Generation: 1, Endpoint: "b:50051", Models: []Model{{Name: "fake-llm", Version: "v1"}}, MaxConcurrency: 8})
	invoker := &recordingInvoker{}
	router := NewRouter(registry, invoker, nil, 3, time.Second)

	result, err := router.Generate(context.Background(), &inferencev1.GenerateRequest{
		RequestId: "request-1", Model: "fake-llm", ModelVersion: "v1", Prompt: "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Attempts != 2 || result.Response.GetWorkerId() != "worker-b" {
		t.Fatalf("unexpected route result: %+v", result)
	}
	if len(invoker.workers) != 2 || invoker.workers[0] != "worker-a" || invoker.workers[1] != "worker-b" {
		t.Fatalf("expected distinct retry workers, got %v", invoker.workers)
	}
	if invoker.attempts[0] != 1 || invoker.attempts[1] != 2 {
		t.Fatalf("expected attempt numbers 1 and 2, got %v", invoker.attempts)
	}
}
