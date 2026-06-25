package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

var (
	HttpRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests processed, partitioned by status code and method.",
		},
		[]string{"method", "status", "path"},
	)

	HttpRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "Latency of HTTP requests in seconds.",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		},
		[]string{"method", "path"},
	)

	DocumentUploadsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "document_uploads_total",
			Help: "Total number of documents uploaded, partitioned by status.",
		},
		[]string{"status"},
	)

	BookletCompilationDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "booklet_compilation_duration_seconds",
			Help:    "Time taken to compile a booklet in seconds.",
			Buckets: []float64{0.5, 1, 2, 5, 10, 20, 30, 60, 120, 300},
		},
	)

	VectorSearchDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "vector_search_duration_seconds",
			Help:    "Time taken to execute a vector search in seconds.",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5},
		},
	)
)

func RegisterMetrics() {
	prometheus.MustRegister(HttpRequestsTotal)
	prometheus.MustRegister(HttpRequestDuration)
	prometheus.MustRegister(DocumentUploadsTotal)
	prometheus.MustRegister(BookletCompilationDuration)
	prometheus.MustRegister(VectorSearchDuration)
}
