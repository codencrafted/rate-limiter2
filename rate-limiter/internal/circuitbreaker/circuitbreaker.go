package circuitbreaker

import (
	"errors"
	"sync"
	"time"

	"github.com/distributed/ratelimiter/internal/metrics"
)

// State represents the state of the circuit breaker
type State int

const (
	// StateClosed means the circuit breaker is closed and requests are allowed
	StateClosed State = iota
	// StateOpen means the circuit breaker is open and requests are not allowed
	StateOpen
	// StateHalfOpen means the circuit breaker is testing if requests can be allowed
	StateHalfOpen
)

// String returns a string representation of the state
func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// Counts holds statistics for the circuit breaker
type Counts struct {
	Requests             uint64
	TotalSuccesses       uint64
	TotalFailures        uint64
	ConsecutiveSuccesses uint64
	ConsecutiveFailures  uint64
}

// Settings holds circuit breaker configuration
type Settings struct {
	Name          string
	MaxRequests   uint32
	Interval      time.Duration
	Timeout       time.Duration
	ReadyToTrip   func(counts Counts) bool
	OnStateChange func(name string, from State, to State)
}

// DefaultSettings returns default circuit breaker settings
func DefaultSettings() Settings {
	return Settings{
		Name:        "default",
		MaxRequests: 1,
		Interval:    0,
		Timeout:     60 * time.Second,
		ReadyToTrip: func(counts Counts) bool {
			return counts.ConsecutiveFailures > 5
		},
	}
}

// CircuitBreaker implements the circuit breaker pattern
type CircuitBreaker struct {
	name          string
	maxRequests   uint32
	interval      time.Duration
	timeout       time.Duration
	readyToTrip   func(counts Counts) bool
	onStateChange func(name string, from State, to State)
	metrics       *metrics.Metrics

	mutex       sync.RWMutex
	state       State
	generation  uint64
	counts      Counts
	expiry      time.Time
	lastStateChange time.Time
}

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(settings Settings, m *metrics.Metrics) *CircuitBreaker {
	cb := &CircuitBreaker{
		name:          settings.Name,
		maxRequests:   settings.MaxRequests,
		interval:      settings.Interval,
		timeout:       settings.Timeout,
		readyToTrip:   settings.ReadyToTrip,
		onStateChange: settings.OnStateChange,
		metrics:       m,
		state:         StateClosed,
		lastStateChange: time.Now(),
	}

	if cb.onStateChange == nil {
		cb.onStateChange = func(name string, from State, to State) {}
	}

	if cb.readyToTrip == nil {
		cb.readyToTrip = DefaultSettings().ReadyToTrip
	}

	// Record initial state in metrics
	if m != nil {
		m.RecordCircuitBreakerState(cb.name, false)
	}

	return cb
}

// Execute executes the given function if the circuit breaker is closed
func (cb *CircuitBreaker) Execute(req func() (interface{}, error)) (interface{}, error) {
	generation, err := cb.beforeRequest()
	if err != nil {
		return nil, err
	}

	result, err := req()

	cb.afterRequest(generation, err == nil)

	return result, err
}

// Name returns the name of the circuit breaker
func (cb *CircuitBreaker) Name() string {
	return cb.name
}

// State returns the current state of the circuit breaker
func (cb *CircuitBreaker) State() State {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()

	now := time.Now()
	state, _ := cb.currentState(now)
	return state
}

// Counts returns the current counts of the circuit breaker
func (cb *CircuitBreaker) Counts() Counts {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()

	return cb.counts
}

// beforeRequest is called before a request is executed
func (cb *CircuitBreaker) beforeRequest() (uint64, error) {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	now := time.Now()
	state, generation := cb.currentState(now)

	if state == StateOpen {
		return generation, errors.New("circuit breaker is open")
	}

	if state == StateHalfOpen && cb.counts.Requests >= uint64(cb.maxRequests) {
		return generation, errors.New("too many requests in half-open state")
	}

	cb.counts.Requests++
	return generation, nil
}

// afterRequest is called after a request is executed
func (cb *CircuitBreaker) afterRequest(generation uint64, success bool) {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	now := time.Now()
	state, currentGeneration := cb.currentState(now)

	// Ignore outdated generation
	if generation != currentGeneration {
		return
	}

	if success {
		cb.onSuccess(state, now)
	} else {
		cb.onFailure(state, now)
	}
}

// onSuccess handles successful requests
func (cb *CircuitBreaker) onSuccess(state State, now time.Time) {
	switch state {
	case StateClosed:
		cb.counts.TotalSuccesses++
		cb.counts.ConsecutiveSuccesses++
		cb.counts.ConsecutiveFailures = 0
		
		// Reset interval if needed
		if cb.interval > 0 {
			cb.expiry = now.Add(cb.interval)
		}
		
	case StateHalfOpen:
		cb.counts.TotalSuccesses++
		cb.counts.ConsecutiveSuccesses++
		cb.counts.ConsecutiveFailures = 0
		
		// If consecutive successes reach threshold, close the circuit
		if cb.counts.ConsecutiveSuccesses >= uint64(cb.maxRequests) {
			cb.setState(StateClosed, now)
		}
	}
}

// onFailure handles failed requests
func (cb *CircuitBreaker) onFailure(state State, now time.Time) {
	switch state {
	case StateClosed:
		cb.counts.TotalFailures++
		cb.counts.ConsecutiveFailures++
		cb.counts.ConsecutiveSuccesses = 0
		
		// Check if circuit should trip
		if cb.readyToTrip(cb.counts) {
			cb.setState(StateOpen, now)
		}
		
	case StateHalfOpen:
		cb.counts.TotalFailures++
		cb.counts.ConsecutiveFailures++
		cb.counts.ConsecutiveSuccesses = 0
		
		// If any request fails in half-open state, open the circuit again
		cb.setState(StateOpen, now)
	}
}

// setState changes the state of the circuit breaker
func (cb *CircuitBreaker) setState(state State, now time.Time) {
	if cb.state == state {
		return
	}

	oldState := cb.state
	
	// Update metrics
	if cb.metrics != nil {
		cb.metrics.RecordCircuitBreakerState(cb.name, state == StateOpen)
	}

	// Update state
	cb.state = state
	cb.lastStateChange = now
	
	// Reset counts
	cb.counts.Requests = 0
	cb.counts.ConsecutiveSuccesses = 0
	cb.counts.ConsecutiveFailures = 0
	
	// Set expiry for state
	if state == StateOpen {
		cb.expiry = now.Add(cb.timeout)
	}
	
	// Increment generation to invalidate pending requests
	cb.generation++
	
	// Notify state change
	cb.onStateChange(cb.name, oldState, state)
}

// currentState returns the current state and generation
func (cb *CircuitBreaker) currentState(now time.Time) (State, uint64) {
	switch cb.state {
	case StateClosed:
		// Check if interval has expired and reset counts if needed
		if cb.interval > 0 && now.After(cb.expiry) {
			cb.counts = Counts{}
			cb.expiry = now.Add(cb.interval)
		}
		
	case StateOpen:
		// Check if timeout has expired and transition to half-open
		if now.After(cb.expiry) {
			cb.setState(StateHalfOpen, now)
		}
	}
	
	return cb.state, cb.generation
}

// TimeSinceStateChange returns the time since the last state change
func (cb *CircuitBreaker) TimeSinceStateChange() time.Duration {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()
	
	return time.Since(cb.lastStateChange)
}

// Reset resets the circuit breaker to its initial closed state
func (cb *CircuitBreaker) Reset() {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()
	
	now := time.Now()
	if cb.state != StateClosed {
		cb.setState(StateClosed, now)
	}
	
	cb.counts = Counts{}
	cb.expiry = now.Add(cb.interval)
}

// ForceOpen forces the circuit breaker to open
func (cb *CircuitBreaker) ForceOpen() {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()
	
	if cb.state != StateOpen {
		cb.setState(StateOpen, time.Now())
	}
}

// ForceClose forces the circuit breaker to close
func (cb *CircuitBreaker) ForceClose() {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()
	
	if cb.state != StateClosed {
		cb.setState(StateClosed, time.Now())
	}
}

// IsOpen returns true if the circuit breaker is open
func (cb *CircuitBreaker) IsOpen() bool {
	return cb.State() == StateOpen
}

// IsClosed returns true if the circuit breaker is closed
func (cb *CircuitBreaker) IsClosed() bool {
	return cb.State() == StateClosed
}

// IsHalfOpen returns true if the circuit breaker is half-open
func (cb *CircuitBreaker) IsHalfOpen() bool {
	return cb.State() == StateHalfOpen
} 