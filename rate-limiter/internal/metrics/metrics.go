package metrics

import (
	"context"
	"net/http"
	"time"

	"github.com/distributed/ratelimiter/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds all Prometheus metrics
type Metrics struct {
	// Request metrics
	RequestsTotal         *prometheus.CounterVec
	RequestsLimited       *prometheus.CounterVec
	RequestDuration       *prometheus.HistogramVec
	RequestsInFlight      prometheus.Gauge
	ResponseSize          *prometheus.HistogramVec
	RequestErrors         *prometheus.CounterVec
	
	// Rate limiter metrics
	RateLimitChecks       *prometheus.CounterVec
	RateLimitHits         *prometheus.CounterVec
	RateLimitCurrentValue *prometheus.GaugeVec
	RateLimitDuration     *prometheus.HistogramVec
	
	// Storage metrics
	StorageOperations     *prometheus.CounterVec
	StorageErrors         *prometheus.CounterVec
	StorageDuration       *prometheus.HistogramVec
	
	// Cache metrics
	CacheHits             *prometheus.CounterVec
	CacheMisses           *prometheus.CounterVec
	CacheSize             *prometheus.GaugeVec
	CacheOperations       *prometheus.CounterVec
	CacheDuration         *prometheus.HistogramVec
	
	// Circuit breaker metrics
	CircuitBreakerOpen    *prometheus.GaugeVec
	CircuitBreakerTrips   *prometheus.CounterVec
	
	// System metrics
	GoRoutines            prometheus.Gauge
	GCDuration            prometheus.Histogram
	MemoryUsage           *prometheus.GaugeVec
}

// NewMetrics creates a new metrics collector
func NewMetrics(cfg *config.MetricsConfig) *Metrics {
	if !cfg.Enabled {
		return nil
	}
	
	// Register and return metrics
	m := &Metrics{
		// Request metrics
		RequestsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "ratelimiter_http_requests_total",
				Help: "Total number of HTTP requests",
			},
			[]string{"method", "path", "status"},
		),
		RequestsLimited: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "ratelimiter_http_requests_limited_total",
				Help: "Total number of rate-limited HTTP requests",
			},
			[]string{"method", "path"},
		),
		RequestDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "ratelimiter_http_request_duration_seconds",
				Help:    "HTTP request duration in seconds",
				Buckets: []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5, 10},
			},
			[]string{"method", "path", "status"},
		),
		RequestsInFlight: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "ratelimiter_http_requests_in_flight",
				Help: "Current number of HTTP requests being processed",
			},
		),
		ResponseSize: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "ratelimiter_http_response_size_bytes",
				Help:    "HTTP response size in bytes",
				Buckets: prometheus.ExponentialBuckets(100, 10, 8),
			},
			[]string{"method", "path", "status"},
		),
		RequestErrors: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "ratelimiter_http_request_errors_total",
				Help: "Total number of HTTP request errors",
			},
			[]string{"method", "path", "error_type"},
		),
		
		// Rate limiter metrics
		RateLimitChecks: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "ratelimiter_checks_total",
				Help: "Total number of rate limit checks",
			},
			[]string{"key", "window"},
		),
		RateLimitHits: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "ratelimiter_hits_total",
				Help: "Total number of rate limit hits (requests that were limited)",
			},
			[]string{"key", "window"},
		),
		RateLimitCurrentValue: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "ratelimiter_current_value",
				Help: "Current value of the rate limit counter",
			},
			[]string{"key", "window"},
		),
		RateLimitDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "ratelimiter_check_duration_seconds",
				Help:    "Duration of rate limit checks in seconds",
				Buckets: []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05, 0.1},
			},
			[]string{"key", "window"},
		),
		
		// Storage metrics
		StorageOperations: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "ratelimiter_storage_operations_total",
				Help: "Total number of storage operations",
			},
			[]string{"operation", "storage_type"},
		),
		StorageErrors: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "ratelimiter_storage_errors_total",
				Help: "Total number of storage errors",
			},
			[]string{"operation", "storage_type", "error_type"},
		),
		StorageDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "ratelimiter_storage_operation_duration_seconds",
				Help:    "Duration of storage operations in seconds",
				Buckets: []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05, 0.1},
			},
			[]string{"operation", "storage_type"},
		),
		
		// Cache metrics
		CacheHits: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "ratelimiter_cache_hits_total",
				Help: "Total number of cache hits",
			},
			[]string{"cache_type"},
		),
		CacheMisses: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "ratelimiter_cache_misses_total",
				Help: "Total number of cache misses",
			},
			[]string{"cache_type"},
		),
		CacheSize: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "ratelimiter_cache_size",
				Help: "Current size of the cache",
			},
			[]string{"cache_type"},
		),
		CacheOperations: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "ratelimiter_cache_operations_total",
				Help: "Total number of cache operations",
			},
			[]string{"operation", "cache_type"},
		),
		CacheDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "ratelimiter_cache_operation_duration_seconds",
				Help:    "Duration of cache operations in seconds",
				Buckets: []float64{0.0001, 0.0005, 0.001, 0.005, 0.01},
			},
			[]string{"operation", "cache_type"},
		),
		
		// Circuit breaker metrics
		CircuitBreakerOpen: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "ratelimiter_circuit_breaker_open",
				Help: "Whether the circuit breaker is open (1) or closed (0)",
			},
			[]string{"service"},
		),
		CircuitBreakerTrips: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "ratelimiter_circuit_breaker_trips_total",
				Help: "Total number of circuit breaker trips",
			},
			[]string{"service"},
		),
		
		// System metrics
		GoRoutines: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "ratelimiter_goroutines",
				Help: "Current number of goroutines",
			},
		),
		GCDuration: promauto.NewHistogram(
			prometheus.HistogramOpts{
				Name:    "ratelimiter_gc_duration_seconds",
				Help:    "GC duration in seconds",
				Buckets: prometheus.ExponentialBuckets(0.0001, 2, 10),
			},
		),
		MemoryUsage: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "ratelimiter_memory_usage_bytes",
				Help: "Memory usage in bytes",
			},
			[]string{"type"},
		),
	}

	// Register Go collector if enabled
	if cfg.EnableGoStats {
		prometheus.DefaultRegisterer.MustRegister(prometheus.NewGoCollector())
	}

	return m
}

// RecordHTTPRequest records metrics for HTTP requests
func (m *Metrics) RecordHTTPRequest(method, path string, status int, duration time.Duration, size int) {
	if m == nil {
		return
	}
	
	statusStr := http.StatusText(status)
	if statusStr == "" {
		statusStr = "unknown"
	}
	
	m.RequestsTotal.WithLabelValues(method, path, statusStr).Inc()
	m.RequestDuration.WithLabelValues(method, path, statusStr).Observe(duration.Seconds())
	m.ResponseSize.WithLabelValues(method, path, statusStr).Observe(float64(size))
}

// RecordRateLimitCheck records metrics for rate limit checks
func (m *Metrics) RecordRateLimitCheck(key, window string, limited bool, current int64, duration time.Duration) {
	if m == nil {
		return
	}
	
	m.RateLimitChecks.WithLabelValues(key, window).Inc()
	m.RateLimitDuration.WithLabelValues(key, window).Observe(duration.Seconds())
	m.RateLimitCurrentValue.WithLabelValues(key, window).Set(float64(current))
	
	if limited {
		m.RateLimitHits.WithLabelValues(key, window).Inc()
	}
}

// RecordStorageOperation records metrics for storage operations
func (m *Metrics) RecordStorageOperation(operation, storageType string, duration time.Duration, err error) {
	if m == nil {
		return
	}
	
	m.StorageOperations.WithLabelValues(operation, storageType).Inc()
	m.StorageDuration.WithLabelValues(operation, storageType).Observe(duration.Seconds())
	
	if err != nil {
		m.StorageErrors.WithLabelValues(operation, storageType, err.Error()).Inc()
	}
}

// RecordCacheOperation records metrics for cache operations
func (m *Metrics) RecordCacheOperation(operation, cacheType string, duration time.Duration, hit bool) {
	if m == nil {
		return
	}
	
	m.CacheOperations.WithLabelValues(operation, cacheType).Inc()
	m.CacheDuration.WithLabelValues(operation, cacheType).Observe(duration.Seconds())
	
	if hit {
		m.CacheHits.WithLabelValues(cacheType).Inc()
	} else {
		m.CacheMisses.WithLabelValues(cacheType).Inc()
	}
}

// RecordCacheSize records the current cache size
func (m *Metrics) RecordCacheSize(cacheType string, size int) {
	if m == nil {
		return
	}
	
	m.CacheSize.WithLabelValues(cacheType).Set(float64(size))
}

// RecordCircuitBreakerState records the circuit breaker state
func (m *Metrics) RecordCircuitBreakerState(service string, open bool) {
	if m == nil {
		return
	}
	
	state := 0.0
	if open {
		state = 1.0
		m.CircuitBreakerTrips.WithLabelValues(service).Inc()
	}
	
	m.CircuitBreakerOpen.WithLabelValues(service).Set(state)
}

// MetricsMiddleware creates a middleware that records request metrics
func MetricsMiddleware(metrics *Metrics) gin.HandlerFunc {
	return func(c *gin.Context) {
		if metrics == nil {
			c.Next()
			return
		}
		
		// Track request start time
		start := time.Now()
		
		// Track requests in flight
		metrics.RequestsInFlight.Inc()
		defer metrics.RequestsInFlight.Dec()
		
		// Process request
		c.Next()
		
		// Record request metrics
		duration := time.Since(start)
		status := c.Writer.Status()
		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}
		
		metrics.RecordHTTPRequest(c.Request.Method, path, status, duration, c.Writer.Size())
		
		// Record if request was rate limited
		if status == http.StatusTooManyRequests {
			metrics.RequestsLimited.WithLabelValues(c.Request.Method, path).Inc()
		}
	}
}

// PrometheusHandler returns an HTTP handler for Prometheus metrics
func PrometheusHandler() http.Handler {
	return promhttp.Handler()
}

// InstrumentHandler instruments an HTTP handler with metrics
func InstrumentHandler(handler http.Handler, name string) http.Handler {
	return promhttp.InstrumentHandlerDuration(
		prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "ratelimiter_handler_duration_seconds",
				Help:    "HTTP handler duration in seconds",
				Buckets: []float64{0.001, 0.01, 0.1, 0.5, 1, 5, 10},
			},
			[]string{"handler"},
		).MustCurryWith(prometheus.Labels{"handler": name}),
		handler,
	)
} 