package handlers

import (
	"net/http"
	"strconv"
	"time"

	"booklet/metrics"

	"github.com/prometheus/client_golang/prometheus"
)

type statusWriter struct {
	http.ResponseWriter
	statusCode int
}

// InstrumentHandler wraps http.HandlerFunc to export Prometheus metrics
func InstrumentHandler(path string, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		
		sw := &statusWriter{ResponseWriter: w, statusCode: http.StatusOK}
		
		handler(sw, r)
		
		duration := time.Since(start).Seconds()
		statusStr := strconv.Itoa(sw.statusCode)
		
		metrics.HttpRequestsTotal.With(prometheus.Labels{
			"method": r.Method,
			"status": statusStr,
			"path":   path,
		}).Inc()
		
		metrics.HttpRequestDuration.With(prometheus.Labels{
			"method": r.Method,
			"path":   path,
		}).Observe(duration)
	}
}

func (sw *statusWriter) WriteHeader(statusCode int) {
	sw.statusCode = statusCode
	sw.ResponseWriter.WriteHeader(statusCode)
}
