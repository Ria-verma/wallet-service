package obs

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Metrics struct {
	registry *prometheus.Registry

	HTTPRequests *prometheus.CounterVec
	HTTPDuration *prometheus.HistogramVec

	TransfersApplied    prometheus.Counter
	TransfersRejected   *prometheus.CounterVec
	IdempotentReplays   prometheus.Counter
	GetOrCreateRaceLost prometheus.Counter
	AuthFailures        prometheus.Counter
}

func NewMetrics() *Metrics {
	m := &Metrics{
		registry: prometheus.NewRegistry(),
		HTTPRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "HTTP requests by route, method and status code.",
		}, []string{"route", "method", "code"}),
		HTTPDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request latency by route (p99 via histogram_quantile).",
			Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5},
		}, []string{"route"}),
		TransfersApplied: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "transfers_applied_total",
			Help: "Transfers that moved money.",
		}),
		TransfersRejected: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "transfers_rejected_total",
			Help: "Transfers rejected, by reason.",
		}, []string{"reason"}),
		IdempotentReplays: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "idempotent_replays_total",
			Help: "Retried transfers answered from the stored outcome.",
		}),
		GetOrCreateRaceLost: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "get_or_create_race_lost_total",
			Help: "Wallet creations that lost the concurrent insert race.",
		}),
		AuthFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "auth_failures_total",
			Help: "Requests rejected for missing/invalid credentials.",
		}),
	}
	m.registry.MustRegister(
		m.HTTPRequests, m.HTTPDuration,
		m.TransfersApplied, m.TransfersRejected, m.IdempotentReplays,
		m.GetOrCreateRaceLost, m.AuthFailures,
	)
	return m
}

func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}
