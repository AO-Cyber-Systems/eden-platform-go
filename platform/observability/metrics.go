package observability

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds all Prometheus metrics for the platform server.
type Metrics struct {
	Registry *prometheus.Registry

	RequestsTotal    *prometheus.CounterVec
	RequestDuration  *prometheus.HistogramVec
	ActiveConnections prometheus.Gauge
}

// NewMetrics creates a new Metrics instance with pre-defined server metrics
// registered to a dedicated prometheus.Registry.
func NewMetrics() *Metrics {
	reg := prometheus.NewRegistry()

	// Also register default Go runtime and process collectors.
	reg.MustRegister(prometheus.NewGoCollector())
	reg.MustRegister(prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))

	m := &Metrics{
		Registry: reg,
		RequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "http_requests_total",
				Help: "Total number of HTTP requests by method, path, and status.",
			},
			[]string{"method", "path", "status"},
		),
		RequestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "http_request_duration_seconds",
				Help:    "Histogram of HTTP request durations in seconds.",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"method", "path"},
		),
		ActiveConnections: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "active_connections",
				Help: "Number of active connections being processed.",
			},
		),
	}

	reg.MustRegister(m.RequestsTotal)
	reg.MustRegister(m.RequestDuration)
	reg.MustRegister(m.ActiveConnections)

	return m
}

// MetricsHandler returns an HTTP handler that serves Prometheus metrics.
func (m *Metrics) MetricsHandler() http.Handler {
	return promhttp.HandlerFor(m.Registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})
}
