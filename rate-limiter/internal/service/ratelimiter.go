package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/distributed/ratelimiter/internal/config"
	"github.com/distributed/ratelimiter/internal/storage"
)

// LimitResult represents the result of a rate limit check
type LimitResult struct {
	Limited      bool
	CurrentCount int64
	Limit        int
	Remaining    int
	ResetAfter   time.Duration
	Window       string
}

// RateLimiterService implements the rate limiting service
type RateLimiterService struct {
	store storage.Storage
	config config.RateLimiterConfig
	
	// Circuit breaker state
	circuitBreaker  bool
	failureCount    int
	failureThreshold int
	lastFailure     time.Time
	resetTimeout    time.Duration
	mutex           sync.RWMutex
}

// NewRateLimiterService creates a new rate limiter service
func NewRateLimiterService(store storage.Storage, config config.RateLimiterConfig) *RateLimiterService {
	return &RateLimiterService{
		store:  store,
		config: config,
		
		// Circuit breaker configuration
		circuitBreaker:  false,
		failureCount:    0,
		failureThreshold: 5,
		resetTimeout:    10 * time.Second,
	}
}

// CheckRateLimit checks if a request should be rate limited
func (s *RateLimiterService) CheckRateLimit(ctx context.Context, key string, identifier string, windowName string) (*LimitResult, error) {
	// Construct the full key for rate limiting
	fullKey := fmt.Sprintf("%s:%s", key, identifier)
	
	// Check if circuit breaker is open
	if s.isCircuitBreakerOpen() {
		// Fall back to local token bucket or other fallback mechanism
		// For simplicity, we'll just allow the request in this example
		return &LimitResult{
			Limited:      false,
			CurrentCount: 0,
			Limit:        0,
			Remaining:    0,
			ResetAfter:   0,
			Window:       windowName,
		}, nil
	}
	
	// Check whitelist
	whitelisted, err := s.store.IsWhitelisted(ctx, fullKey)
	if err != nil {
		s.incrementFailure()
		return nil, fmt.Errorf("failed to check whitelist: %w", err)
	}
	
	if whitelisted {
		return &LimitResult{
			Limited:      false,
			CurrentCount: 0,
			Limit:        0,
			Remaining:    0,
			ResetAfter:   0,
			Window:       windowName,
		}, nil
	}
	
	// Get default limit for the window
	defaultLimit, ok := s.config.DefaultLimits[windowName]
	if !ok {
		return nil, fmt.Errorf("invalid window name: %s", windowName)
	}
	
	limit := defaultLimit.Requests
	window := defaultLimit.Window
	
	// Check for custom limit
	customLimit, err := s.store.GetLimit(ctx, fullKey, windowName)
	if err != nil {
		s.incrementFailure()
		return nil, fmt.Errorf("failed to get custom limit: %w", err)
	}
	
	if customLimit > 0 {
		limit = customLimit
	}
	
	// Increment and get current count
	count, err := s.store.IncrementAndCount(ctx, fullKey, window)
	if err != nil {
		s.incrementFailure()
		return nil, fmt.Errorf("failed to increment counter: %w", err)
	}
	
	// Reset failure count on successful Redis operations
	s.resetFailure()
	
	// Calculate remaining and reset time
	remaining := limit - int(count)
	if remaining < 0 {
		remaining = 0
	}
	
	// Return result
	return &LimitResult{
		Limited:      count > int64(limit),
		CurrentCount: count,
		Limit:        limit,
		Remaining:    remaining,
		ResetAfter:   window,
		Window:       windowName,
	}, nil
}

// UpdateWhitelist adds or removes a key from the whitelist
func (s *RateLimiterService) UpdateWhitelist(ctx context.Context, key string, identifier string, whitelisted bool) error {
	// Construct the full key for whitelisting
	fullKey := fmt.Sprintf("%s:%s", key, identifier)
	
	if s.isCircuitBreakerOpen() {
		return fmt.Errorf("service unavailable: circuit breaker open")
	}
	
	err := s.store.UpdateWhitelist(ctx, fullKey, whitelisted)
	if err != nil {
		s.incrementFailure()
		return fmt.Errorf("failed to update whitelist: %w", err)
	}
	
	s.resetFailure()
	return nil
}

// SetLimit sets a custom limit for a key
func (s *RateLimiterService) SetLimit(ctx context.Context, key string, identifier string, windowName string, limit int) error {
	// Construct the full key for limit setting
	fullKey := fmt.Sprintf("%s:%s", key, identifier)
	
	if s.isCircuitBreakerOpen() {
		return fmt.Errorf("service unavailable: circuit breaker open")
	}
	
	err := s.store.SetLimit(ctx, fullKey, windowName, limit)
	if err != nil {
		s.incrementFailure()
		return fmt.Errorf("failed to set limit: %w", err)
	}
	
	s.resetFailure()
	return nil
}

// Circuit breaker methods

// isCircuitBreakerOpen checks if the circuit breaker is open
func (s *RateLimiterService) isCircuitBreakerOpen() bool {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	
	// Check if we should try to reset the circuit breaker
	if s.circuitBreaker && time.Since(s.lastFailure) > s.resetTimeout {
		s.mutex.RUnlock()
		s.mutex.Lock()
		defer s.mutex.Unlock()
		
		// Double-check after acquiring write lock
		if s.circuitBreaker && time.Since(s.lastFailure) > s.resetTimeout {
			s.circuitBreaker = false
			s.failureCount = 0
		}
		
		return s.circuitBreaker
	}
	
	return s.circuitBreaker
}

// incrementFailure increments the failure counter and opens the circuit breaker if threshold is reached
func (s *RateLimiterService) incrementFailure() {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	
	s.failureCount++
	s.lastFailure = time.Now()
	
	if s.failureCount >= s.failureThreshold {
		s.circuitBreaker = true
	}
}

// resetFailure resets the failure counter
func (s *RateLimiterService) resetFailure() {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	
	s.failureCount = 0
	
	// If circuit breaker is open, keep it open until timeout
	if !s.circuitBreaker {
		s.lastFailure = time.Time{}
	}
} 