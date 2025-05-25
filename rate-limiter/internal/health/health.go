package health

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/distributed/ratelimiter/internal/config"
	"github.com/distributed/ratelimiter/internal/logging"
	"github.com/distributed/ratelimiter/internal/metrics"
	"github.com/redis/go-redis/v9"
)

// ServiceHealth contains health status of a service component
type ServiceHealth struct {
	Status    string                 `json:"status"`
	Details   map[string]interface{} `json:"details,omitempty"`
	Error     string                 `json:"error,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
}

// SystemHealth contains the overall health status of the system
type SystemHealth struct {
	Status      string                    `json:"status"`
	Timestamp   time.Time                 `json:"timestamp"`
	Version     string                    `json:"version"`
	Services    map[string]ServiceHealth  `json:"services"`
	Memory      MemoryStats               `json:"memory"`
	CPU         CPUStats                  `json:"cpu"`
	Uptime      time.Duration             `json:"uptime"`
	Environment string                    `json:"environment"`
	Host        string                    `json:"host"`
}

// MemoryStats contains memory usage statistics
type MemoryStats struct {
	Alloc        uint64  `json:"alloc"`
	TotalAlloc   uint64  `json:"total_alloc"`
	Sys          uint64  `json:"sys"`
	HeapAlloc    uint64  `json:"heap_alloc"`
	HeapSys      uint64  `json:"heap_sys"`
	HeapInuse    uint64  `json:"heap_in_use"`
	StackInuse   uint64  `json:"stack_in_use"`
	NumGC        uint32  `json:"num_gc"`
	GCPauseTotal float64 `json:"gc_pause_total_ms"`
	GCPauseMean  float64 `json:"gc_pause_mean_ms"`
}

// CPUStats contains CPU usage statistics
type CPUStats struct {
	NumCPU       int     `json:"num_cpu"`
	NumGoroutine int     `json:"num_goroutine"`
	CGOCalls     int64   `json:"cgo_calls"`
	CPUUsage     float64 `json:"cpu_usage,omitempty"`
}

// HealthChecker checks the health of the system
type HealthChecker struct {
	config          *config.Config
	redisClient     redis.UniversalClient
	logger          *logging.Logger
	metrics         *metrics.Metrics
	serviceCheckers map[string]ServiceChecker
	startTime       time.Time
	version         string
	environment     string
	host            string
	mutex           sync.RWMutex
}

// ServiceChecker defines a function that checks a specific service's health
type ServiceChecker func(ctx context.Context) ServiceHealth

// NewHealthChecker creates a new health checker
func NewHealthChecker(
	cfg *config.Config,
	redisClient redis.UniversalClient,
	logger *logging.Logger,
	m *metrics.Metrics,
	version string,
	environment string,
	host string,
) *HealthChecker {
	return &HealthChecker{
		config:          cfg,
		redisClient:     redisClient,
		logger:          logger,
		metrics:         m,
		serviceCheckers: make(map[string]ServiceChecker),
		startTime:       time.Now(),
		version:         version,
		environment:     environment,
		host:            host,
	}
}

// RegisterServiceChecker registers a service checker
func (h *HealthChecker) RegisterServiceChecker(name string, checker ServiceChecker) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	h.serviceCheckers[name] = checker
}

// Check performs a health check
func (h *HealthChecker) Check(ctx context.Context) SystemHealth {
	now := time.Now()
	health := SystemHealth{
		Status:      "healthy",
		Timestamp:   now,
		Version:     h.version,
		Services:    make(map[string]ServiceHealth),
		Uptime:      now.Sub(h.startTime),
		Environment: h.environment,
		Host:        h.host,
	}

	// Check Redis health
	redisHealth := h.checkRedisHealth(ctx)
	health.Services["redis"] = redisHealth
	if redisHealth.Status != "healthy" {
		health.Status = "degraded"
	}

	// Check registered services
	h.mutex.RLock()
	for name, checker := range h.serviceCheckers {
		serviceHealth := checker(ctx)
		health.Services[name] = serviceHealth
		if serviceHealth.Status != "healthy" && health.Status != "unhealthy" {
			if serviceHealth.Status == "unhealthy" {
				health.Status = "unhealthy"
			} else {
				health.Status = "degraded"
			}
		}
	}
	h.mutex.RUnlock()

	// Get memory stats
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	health.Memory = MemoryStats{
		Alloc:        memStats.Alloc,
		TotalAlloc:   memStats.TotalAlloc,
		Sys:          memStats.Sys,
		HeapAlloc:    memStats.HeapAlloc,
		HeapSys:      memStats.HeapSys,
		HeapInuse:    memStats.HeapInuse,
		StackInuse:   memStats.StackInuse,
		NumGC:        memStats.NumGC,
		GCPauseTotal: float64(memStats.PauseTotalNs) / 1e6, // convert to ms
		GCPauseMean:  float64(memStats.PauseNs[(memStats.NumGC+255)%256]) / 1e6, // latest GC in ms
	}

	// Get CPU stats
	health.CPU = CPUStats{
		NumCPU:       runtime.NumCPU(),
		NumGoroutine: runtime.NumGoroutine(),
		CGOCalls:     runtime.NumCgoCall(),
	}

	// Update metrics
	if h.metrics != nil {
		// Memory metrics
		h.metrics.MemoryUsage.WithLabelValues("heap_alloc").Set(float64(memStats.HeapAlloc))
		h.metrics.MemoryUsage.WithLabelValues("heap_sys").Set(float64(memStats.HeapSys))
		h.metrics.MemoryUsage.WithLabelValues("stack_inuse").Set(float64(memStats.StackInuse))
		
		// Goroutine metrics
		h.metrics.GoRoutines.Set(float64(runtime.NumGoroutine()))
	}

	return health
}

// checkRedisHealth checks the health of Redis
func (h *HealthChecker) checkRedisHealth(ctx context.Context) ServiceHealth {
	ctxTimeout, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()

	health := ServiceHealth{
		Status:    "healthy",
		Timestamp: time.Now(),
		Details:   make(map[string]interface{}),
	}

	// Ping Redis
	start := time.Now()
	err := h.redisClient.Ping(ctxTimeout).Err()
	latency := time.Since(start)

	health.Details["latency_ms"] = float64(latency.Microseconds()) / 1000.0

	if err != nil {
		health.Status = "unhealthy"
		health.Error = fmt.Sprintf("failed to connect to Redis: %v", err)
		if h.logger != nil {
			h.logger.WithField("error", err.Error()).Error("Redis health check failed")
		}
		return health
	}

	// Get Redis info
	info, err := h.redisClient.Info(ctxTimeout).Result()
	if err == nil {
		health.Details["info"] = info
	}

	// Add Redis server details if available
	if h.config != nil {
		health.Details["addresses"] = h.config.Redis.Addresses
		health.Details["db"] = h.config.Redis.DB
	}

	return health
}

// Liveness performs a basic liveness check
func (h *HealthChecker) Liveness(ctx context.Context) bool {
	return true
}

// Readiness performs a readiness check
func (h *HealthChecker) Readiness(ctx context.Context) bool {
	ctxTimeout, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()

	// Check Redis
	err := h.redisClient.Ping(ctxTimeout).Err()
	if err != nil {
		if h.logger != nil {
			h.logger.WithField("error", err.Error()).Error("Redis readiness check failed")
		}
		return false
	}

	// Check all services
	h.mutex.RLock()
	defer h.mutex.RUnlock()

	for name, checker := range h.serviceCheckers {
		health := checker(ctxTimeout)
		if health.Status == "unhealthy" {
			if h.logger != nil {
				h.logger.WithFields(map[string]interface{}{
					"service": name,
					"error":   health.Error,
				}).Error("Service readiness check failed")
			}
			return false
		}
	}

	return true
}

// DefaultServiceHealthCheck provides a default health check for services
func DefaultServiceHealthCheck(name string, checkFn func(ctx context.Context) error) ServiceChecker {
	return func(ctx context.Context) ServiceHealth {
		start := time.Now()
		err := checkFn(ctx)
		latency := time.Since(start)

		health := ServiceHealth{
			Status:    "healthy",
			Timestamp: time.Now(),
			Details: map[string]interface{}{
				"latency_ms": float64(latency.Microseconds()) / 1000.0,
			},
		}

		if err != nil {
			health.Status = "unhealthy"
			health.Error = err.Error()
		}

		return health
	}
} 