package service

import (
	"context"
	"testing"
	"time"

	"github.com/distributed/ratelimiter/internal/config"
	"github.com/distributed/ratelimiter/internal/storage"
)

// mockStorage is a mock implementation of the storage interface for testing
type mockStorage struct {
	counters   map[string]int64
	whitelist  map[string]bool
	limits     map[string]int
	shouldFail bool
}

func newMockStorage() *mockStorage {
	return &mockStorage{
		counters:   make(map[string]int64),
		whitelist:  make(map[string]bool),
		limits:     make(map[string]int),
		shouldFail: false,
	}
}

func (s *mockStorage) IncrementAndCount(ctx context.Context, key string, window time.Duration) (int64, error) {
	if s.shouldFail {
		return 0, errMockFailed
	}
	s.counters[key]++
	return s.counters[key], nil
}

func (s *mockStorage) IsWhitelisted(ctx context.Context, key string) (bool, error) {
	if s.shouldFail {
		return false, errMockFailed
	}
	return s.whitelist[key], nil
}

func (s *mockStorage) UpdateWhitelist(ctx context.Context, key string, whitelisted bool) error {
	if s.shouldFail {
		return errMockFailed
	}
	s.whitelist[key] = whitelisted
	return nil
}

func (s *mockStorage) SetLimit(ctx context.Context, key string, window string, limit int) error {
	if s.shouldFail {
		return errMockFailed
	}
	s.limits[key+":"+window] = limit
	return nil
}

func (s *mockStorage) GetLimit(ctx context.Context, key string, window string) (int, error) {
	if s.shouldFail {
		return 0, errMockFailed
	}
	return s.limits[key+":"+window], nil
}

var errMockFailed = storage.ErrStorageFailure

func TestRateLimiterService_CheckRateLimit(t *testing.T) {
	// Create mock storage
	mockStore := newMockStorage()
	
	// Create config with default limits
	cfg := config.RateLimiterConfig{
		DefaultLimits: map[string]config.Limit{
			"second": {
				Requests: 10,
				Window:   time.Second,
			},
			"minute": {
				Requests: 100,
				Window:   time.Minute,
			},
		},
	}
	
	// Create service
	service := NewRateLimiterService(mockStore, cfg)
	
	// Test cases
	tests := []struct {
		name       string
		key        string
		identifier string
		window     string
		setupMock  func()
		wantLimit  bool
		wantErr    bool
	}{
		{
			name:       "first request not limited",
			key:        "test",
			identifier: "user1",
			window:     "second",
			setupMock:  func() {},
			wantLimit:  false,
			wantErr:    false,
		},
		{
			name:       "whitelisted user not limited",
			key:        "test",
			identifier: "admin",
			window:     "second",
			setupMock: func() {
				mockStore.whitelist["test:admin"] = true
			},
			wantLimit: false,
			wantErr:   false,
		},
		{
			name:       "over limit",
			key:        "test",
			identifier: "user2",
			window:     "second",
			setupMock: func() {
				mockStore.counters["test:user2"] = 10
			},
			wantLimit: true,
			wantErr:   false,
		},
		{
			name:       "custom limit",
			key:        "test",
			identifier: "user3",
			window:     "second",
			setupMock: func() {
				mockStore.limits["test:user3:second"] = 5
				mockStore.counters["test:user3"] = 5
			},
			wantLimit: true,
			wantErr:   false,
		},
		{
			name:       "storage failure",
			key:        "test",
			identifier: "user4",
			window:     "second",
			setupMock: func() {
				mockStore.shouldFail = true
			},
			wantLimit: false,
			wantErr:   true,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset mock
			mockStore = newMockStorage()
			
			// Setup mock
			tt.setupMock()
			
			// Call service
			result, err := service.CheckRateLimit(context.Background(), tt.key, tt.identifier, tt.window)
			
			// Check error
			if (err != nil) != tt.wantErr {
				t.Errorf("CheckRateLimit() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			
			// Check result
			if err == nil && result.Limited != tt.wantLimit {
				t.Errorf("CheckRateLimit() limited = %v, want %v", result.Limited, tt.wantLimit)
			}
		})
	}
}

func TestRateLimiterService_CircuitBreaker(t *testing.T) {
	// Create mock storage
	mockStore := newMockStorage()
	
	// Create config
	cfg := config.RateLimiterConfig{
		DefaultLimits: map[string]config.Limit{
			"second": {
				Requests: 10,
				Window:   time.Second,
			},
		},
	}
	
	// Create service with lower threshold for testing
	service := NewRateLimiterService(mockStore, cfg)
	service.failureThreshold = 2
	service.resetTimeout = 50 * time.Millisecond
	
	// Make storage fail
	mockStore.shouldFail = true
	
	// First call should fail but not trip circuit breaker
	_, err1 := service.CheckRateLimit(context.Background(), "test", "user", "second")
	if err1 == nil {
		t.Error("Expected error on first call")
	}
	
	// Second call should fail and trip circuit breaker
	_, err2 := service.CheckRateLimit(context.Background(), "test", "user", "second")
	if err2 == nil {
		t.Error("Expected error on second call")
	}
	
	// Make storage work again
	mockStore.shouldFail = false
	
	// Third call should use circuit breaker and not call storage
	result, err3 := service.CheckRateLimit(context.Background(), "test", "user", "second")
	if err3 != nil {
		t.Errorf("Unexpected error with circuit breaker: %v", err3)
	}
	if result.Limited {
		t.Error("Expected not limited with circuit breaker")
	}
	
	// Wait for circuit breaker to reset
	time.Sleep(100 * time.Millisecond)
	
	// Fourth call should work normally
	result, err4 := service.CheckRateLimit(context.Background(), "test", "user", "second")
	if err4 != nil {
		t.Errorf("Unexpected error after circuit breaker reset: %v", err4)
	}
	if result.Limited {
		t.Error("Expected not limited after circuit breaker reset")
	}
} 