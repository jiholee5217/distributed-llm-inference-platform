package controlplane

import (
	"context"
	"sync"

	inferencev1 "github.com/jiholee5217/distributed-llm-inference-platform/gen/inference/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type GRPCInvoker struct {
	mu          sync.Mutex
	connections map[string]*grpc.ClientConn
}

func NewGRPCInvoker() *GRPCInvoker {
	return &GRPCInvoker{connections: make(map[string]*grpc.ClientConn)}
}

func (i *GRPCInvoker) Generate(ctx context.Context, worker WorkerSnapshot, request *inferencev1.GenerateRequest) (*inferencev1.GenerateResponse, error) {
	connection, err := i.connection(worker.Endpoint)
	if err != nil {
		return nil, err
	}
	return inferencev1.NewInferenceWorkerClient(connection).Generate(ctx, request)
}

func (i *GRPCInvoker) connection(endpoint string) (*grpc.ClientConn, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if connection, ok := i.connections[endpoint]; ok {
		return connection, nil
	}
	connection, err := grpc.NewClient(endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	i.connections[endpoint] = connection
	return connection, nil
}

func (i *GRPCInvoker) Close() error {
	i.mu.Lock()
	defer i.mu.Unlock()
	var firstErr error
	for endpoint, connection := range i.connections {
		if err := connection.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		delete(i.connections, endpoint)
	}
	return firstErr
}
