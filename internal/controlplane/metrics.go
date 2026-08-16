package controlplane

import "github.com/prometheus/client_golang/prometheus"

type Metrics struct {
	Requests       *prometheus.CounterVec
	RequestLatency *prometheus.HistogramVec
	RouteAttempts  *prometheus.CounterVec
	Retries        *prometheus.CounterVec
	WorkerEvents   *prometheus.CounterVec
	Workers        prometheus.Gauge
}

func NewMetrics(registerer prometheus.Registerer) *Metrics {
	metrics := &Metrics{
		Requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "llm_platform", Subsystem: "gateway", Name: "requests_total",
			Help: "Client inference requests by final outcome.",
		}, []string{"outcome"}),
		RequestLatency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "llm_platform", Subsystem: "gateway", Name: "request_duration_seconds",
			Help:    "End-to-end inference request latency.",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
		}, []string{"outcome"}),
		RouteAttempts: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "llm_platform", Subsystem: "controller", Name: "route_attempts_total",
			Help: "Worker RPC attempts by worker and outcome.",
		}, []string{"worker", "outcome"}),
		Retries: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "llm_platform", Subsystem: "controller", Name: "retries_total",
			Help: "Retry attempts by triggering gRPC status.",
		}, []string{"reason"}),
		WorkerEvents: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "llm_platform", Subsystem: "controller", Name: "worker_events_total",
			Help: "Worker lifecycle events.",
		}, []string{"event"}),
		Workers: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "llm_platform", Subsystem: "controller", Name: "workers_registered",
			Help: "Workers currently known to this controller.",
		}),
	}
	registerer.MustRegister(metrics.Requests, metrics.RequestLatency, metrics.RouteAttempts, metrics.Retries, metrics.WorkerEvents, metrics.Workers)
	return metrics
}
