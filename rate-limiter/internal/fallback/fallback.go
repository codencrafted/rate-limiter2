package fallback

import (
	"context"
	"sync"
	"time"

	"github.com/distributed/ratelimiter/internal/logging"
	"github.com/distributed/ratelimiter/internal/service"
)

// Strategy defines the interface for fallback strategies
type Strategy interface {
	Handle(ctx context.Context, key string, window string) (*service.LimitResult, error)
	Name() string
}

// LocalMemoryFallback implements a fallback strategy using local memory
type LocalMemoryFallback struct {
	limits      map[string]map[string]int64
	mutex       sync.RWMutex
	logger      *logging.Logger
	defaultRate int64
}

// NewLocalMemoryFallback creates a new local memory fallback strategy
func NewLocalMemoryFallback(logger *logging.Logger, defaultRate int64) *LocalMemoryFallback {
	return &LocalMemoryFallback{
		limits:      make(map[string]map[string]int64),
		logger:      logger,
		defaultRate: defaultRate,
	}
}

// Handle implements the fallback strategy
func (f *LocalMemoryFallback) Handle(ctx context.Context, key string, window string) (*service.LimitResult, error) {
	f.mutex.RLock()
	windowLimits, exists := f.limits[key]
	var limit int64
	if exists {
		limit = windowLimits[window]
	}
	f.mutex.RUnlock()

	// If no limit is set, use default
	if !exists || limit == 0 {
		limit = f.defaultRate
	}

	// Apply rate limiting using a very simple algorithm
	now := time.Now()
	result := &service.LimitResult{
		Limited:    false,
		Limit:      limit,
		Remaining:  limit - 1,
		ResetAfter: calculateResetTime(window),
	}

	if f.logger != nil {
		f.logger.WithField("strategy", f.Name()).Info("Applied fallback rate limiting")
	}

	return result, nil
}

// Name returns the name of the strategy
func (f *LocalMemoryFallback) Name() string {
	return "local_memory"
}

// SetLimit sets a custom limit for a key and window
func (f *LocalMemoryFallback) SetLimit(key string, window string, limit int64) {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	windowLimits, exists := f.limits[key]
	if !exists {
		windowLimits = make(map[string]int64)
		f.limits[key] = windowLimits
	}

	windowLimits[window] = limit
}

// AllowAllFallback implements a fallback strategy that allows all requests
type AllowAllFallback struct {
	logger *logging.Logger
}

// NewAllowAllFallback creates a new allow-all fallback strategy
func NewAllowAllFallback(logger *logging.Logger) *AllowAllFallback {
	return &AllowAllFallback{
		logger: logger,
	}
}

// Handle implements the fallback strategy
func (f *AllowAllFallback) Handle(ctx context.Context, key string, window string) (*service.LimitResult, error) {
	// Always allow the request
	result := &service.LimitResult{
		Limited:    false,
		Limit:      1000000,
		Remaining:  1000000,
		ResetAfter: calculateResetTime(window),
	}

	if f.logger != nil {
		f.logger.WithField("strategy", f.Name()).Info("Applied allow-all fallback")
	}

	return result, nil
}

// Name returns the name of the strategy
func (f *AllowAllFallback) Name() string {
	return "allow_all"
}

// DenyAllFallback implements a fallback strategy that denies all requests
type DenyAllFallback struct {
	logger *logging.Logger
}

// NewDenyAllFallback creates a new deny-all fallback strategy
func NewDenyAllFallback(logger *logging.Logger) *DenyAllFallback {
	return &DenyAllFallback{
		logger: logger,
	}
}

// Handle implements the fallback strategy
func (f *DenyAllFallback) Handle(ctx context.Context, key string, window string) (*service.LimitResult, error) {
	// Always deny the request
	result := &service.LimitResult{
		Limited:    true,
		Limit:      0,
		Remaining:  0,
		ResetAfter: calculateResetTime(window),
	}

	if f.logger != nil {
		f.logger.WithField("strategy", f.Name()).Info("Applied deny-all fallback")
	}

	return result, nil
}

// Name returns the name of the strategy
func (f *DenyAllFallback) Name() string {
	return "deny_all"
}

// PriorityFallback implements a fallback strategy that prioritizes certain keys
type PriorityFallback struct {
	priorities map[string]int
	mutex      sync.RWMutex
	logger     *logging.Logger
	inner      Strategy
}

// NewPriorityFallback creates a new priority fallback strategy
func NewPriorityFallback(logger *logging.Logger, inner Strategy) *PriorityFallback {
	return &PriorityFallback{
		priorities: make(map[string]int),
		logger:     logger,
		inner:      inner,
	}
}

// Handle implements the fallback strategy
func (f *PriorityFallback) Handle(ctx context.Context, key string, window string) (*service.LimitResult, error) {
	f.mutex.RLock()
	priority := f.priorities[key]
	f.mutex.RUnlock()

	// If priority is <= 0, deny the request
	if priority <= 0 {
		result := &service.LimitResult{
			Limited:    true,
			Limit:      0,
			Remaining:  0,
			ResetAfter: calculateResetTime(window),
		}

		if f.logger != nil {
			f.logger.WithFields(map[string]interface{}{
				"strategy": f.Name(),
				"key":      key,
				"priority": priority,
			}).Info("Denied request due to low priority")
		}

		return result, nil
	}

	// Otherwise, delegate to inner strategy
	result, err := f.inner.Handle(ctx, key, window)

	if f.logger != nil {
		f.logger.WithFields(map[string]interface{}{
			"strategy": f.Name(),
			"key":      key,
			"priority": priority,
			"limited":  result.Limited,
		}).Info("Applied priority-based fallback")
	}

	return result, err
}

// Name returns the name of the strategy
func (f *PriorityFallback) Name() string {
	return "priority"
}

// SetPriority sets the priority for a key
func (f *PriorityFallback) SetPriority(key string, priority int) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.priorities[key] = priority
}

// TimeWindowFallback implements a fallback strategy that varies based on time
type TimeWindowFallback struct {
	schedules map[string]map[string]int64
	mutex     sync.RWMutex
	logger    *logging.Logger
	inner     Strategy
}

// NewTimeWindowFallback creates a new time window fallback strategy
func NewTimeWindowFallback(logger *logging.Logger, inner Strategy) *TimeWindowFallback {
	return &TimeWindowFallback{
		schedules: make(map[string]map[string]int64),
		logger:    logger,
		inner:     inner,
	}
}

// Handle implements the fallback strategy
func (f *TimeWindowFallback) Handle(ctx context.Context, key string, window string) (*service.LimitResult, error) {
	now := time.Now()
	hour := now.Hour()

	f.mutex.RLock()
	windowSchedule, exists := f.schedules[key]
	var multiplier int64 = 1
	if exists {
		hourKey := formatHour(hour)
		if factor, ok := windowSchedule[hourKey]; ok {
			multiplier = factor
		}
	}
	f.mutex.RUnlock()

	// Delegate to inner strategy
	result, err := f.inner.Handle(ctx, key, window)
	if err != nil || result == nil {
		return result, err
	}

	// Apply time-based multiplier
	result.Limit *= multiplier
	result.Remaining *= multiplier

	if f.logger != nil {
		f.logger.WithFields(map[string]interface{}{
			"strategy":   f.Name(),
			"key":        key,
			"hour":       hour,
			"multiplier": multiplier,
			"limit":      result.Limit,
		}).Info("Applied time-window fallback")
	}

	return result, nil
}

// Name returns the name of the strategy
func (f *TimeWindowFallback) Name() string {
	return "time_window"
}

// SetSchedule sets the schedule for a key
func (f *TimeWindowFallback) SetSchedule(key string, hour int, multiplier int64) {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	windowSchedule, exists := f.schedules[key]
	if !exists {
		windowSchedule = make(map[string]int64)
		f.schedules[key] = windowSchedule
	}

	windowSchedule[formatHour(hour)] = multiplier
}

// Helper functions

// calculateResetTime returns a reset time based on the window
func calculateResetTime(window string) time.Duration {
	switch window {
	case "second":
		return time.Second
	case "minute":
		return time.Minute
	case "hour":
		return time.Hour
	case "day":
		return 24 * time.Hour
	default:
		return time.Minute
	}
}

// formatHour formats an hour (0-23) as a string key
func formatHour(hour int) string {
	if hour < 10 {
		return "0" + string(hour+'0')
	}
	return string('0'+hour/10) + string('0'+hour%10)
} 