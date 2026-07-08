package platform

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics groups the application-level Prometheus collectors scraped by Omni-Metrics.
type Metrics struct {
	Registry        *prometheus.Registry
	HTTPRequests    *prometheus.CounterVec
	HTTPDuration    *prometheus.HistogramVec
	IssuesCreated   *prometheus.CounterVec
	JobsProcessed   *prometheus.CounterVec
	WebhookAttempts *prometheus.CounterVec
}

// NewMetrics registers the collectors on a dedicated registry.
func NewMetrics() *Metrics {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector())
	factory := promauto.With(reg)

	return &Metrics{
		Registry: reg,
		HTTPRequests: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "bugtracker_http_requests_total",
			Help: "HTTP requests by route, method and status.",
		}, []string{"route", "method", "status"}),
		HTTPDuration: factory.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "bugtracker_http_request_duration_seconds",
			Help:    "HTTP request latency.",
			Buckets: prometheus.DefBuckets,
		}, []string{"route", "method"}),
		IssuesCreated: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "bugtracker_issues_created_total",
			Help: "Issues created by type and source.",
		}, []string{"type", "source"}),
		JobsProcessed: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "bugtracker_jobs_processed_total",
			Help: "Background jobs processed by kind and outcome.",
		}, []string{"kind", "outcome"}),
		WebhookAttempts: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "bugtracker_webhook_attempts_total",
			Help: "Outbound webhook delivery attempts by outcome.",
		}, []string{"outcome"}),
	}
}
