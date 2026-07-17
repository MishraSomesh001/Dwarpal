package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// RequestsTotal counts every proxied request, sliced by provider, model, and outcome.
	// In Grafana you can query: aegis_requests_total{provider="gemini", status="success"}
	RequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "aegis_requests_total",
		Help: "Total number of requests proxied by Aegis gateway",
	}, []string{"provider", "model", "status"}) // status = "success" | "error"

	// FailoverTotal counts how many times Aegis triggered an automatic failover.
	FailoverTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "aegis_failover_total",
		Help: "Total number of automatic failover events",
	}, []string{"from_provider", "to_model"})

	// CircuitBreakerOpen is a gauge: 1.0 = OPEN (broken), 0.0 = CLOSED/HALF_OPEN (healthy).
	// This lets Grafana alert when a circuit trips.
	CircuitBreakerOpen = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "aegis_circuit_breaker_open",
		Help: "1 if the circuit breaker is OPEN for a provider, 0 otherwise",
	}, []string{"provider"})

	// RequestDuration tracks how long each upstream LLM call takes in seconds.
	// Buckets are tuned for LLM latency which ranges from 100ms to 30s.
	RequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "aegis_request_duration_seconds",
		Help:    "Latency histogram of proxied LLM requests",
		Buckets: []float64{0.1, 0.5, 1.0, 2.0, 5.0, 10.0, 30.0},
	}, []string{"provider"})
)
