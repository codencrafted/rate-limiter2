package cache

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/distributed/ratelimiter/internal/config"
	"github.com/distributed/ratelimiter/internal/service"
)

// Entry represents a cache entry with expiration
type Entry struct {
	Result      *service.LimitResult
	Expiry      time.Time
	LastUpdated time.Time
}

// Cache defines the interface for a caching layer
type Cache interface {
	Get(ctx context.Context, key string) (*service.LimitResult, bool)
	Set(ctx context.Context, key string, result *service.LimitResult, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
	Clear(ctx context.Context) error
}

// LocalCache implements an in-memory cache
type LocalCache struct {
	entries     map[string]*Entry
	mutex       sync.RWMutex
	maxSize     int
	defaultTTL  time.Duration
	resultPool  *sync.Pool
	janitorStop chan struct{}
}

// NewLocalCache creates a new local cache
func NewLocalCache(cfg *config.RateLimiterConfig) *LocalCache {
	if !cfg.LocalCacheEnabled {
		return nil
	}

	cache := &LocalCache{
		entries:     make(map[string]*Entry, cfg.LocalCacheSize),
		maxSize:     cfg.LocalCacheSize,
		defaultTTL:  cfg.LocalCacheTTL,
		janitorStop: make(chan struct{}),
		resultPool: &sync.Pool{
			New: func() interface{} {
				return &service.LimitResult{}
			},
		},
	}

	// Start janitor to clean expired entries
	go cache.janitor()

	return cache
}

// Get retrieves an entry from the cache
func (c *LocalCache) Get(ctx context.Context, key string) (*service.LimitResult, bool) {
	// Check context cancellation
	select {
	case <-ctx.Done():
		return nil, false
	default:
		// Continue
	}

	c.mutex.RLock()
	entry, found := c.entries[key]
	c.mutex.RUnlock()

	// Check if entry exists and is not expired
	if !found {
		return nil, false
	}

	if time.Now().After(entry.Expiry) {
		// Asynchronously remove expired entry
		go c.Delete(context.Background(), key)
		return nil, false
	}

	// Return a copy of the result to avoid race conditions
	result := c.resultPool.Get().(*service.LimitResult)
	*result = *entry.Result
	return result, true
}

// Set adds or updates an entry in the cache
func (c *LocalCache) Set(ctx context.Context, key string, result *service.LimitResult, ttl time.Duration) error {
	// Check context cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		// Continue
	}

	// Use default TTL if not specified
	if ttl <= 0 {
		ttl = c.defaultTTL
	}

	// Create entry
	entry := &Entry{
		Result:      result,
		Expiry:      time.Now().Add(ttl),
		LastUpdated: time.Now(),
	}

	// Add to cache
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// Check if we need to evict entries due to size limit
	if len(c.entries) >= c.maxSize && c.entries[key] == nil {
		// Simple eviction: remove the oldest entry
		var oldestKey string
		var oldestTime time.Time = time.Now()

		for k, e := range c.entries {
			if e.LastUpdated.Before(oldestTime) {
				oldestKey = k
				oldestTime = e.LastUpdated
			}
		}

		if oldestKey != "" {
			delete(c.entries, oldestKey)
		}
	}

	c.entries[key] = entry
	return nil
}

// Delete removes an entry from the cache
func (c *LocalCache) Delete(ctx context.Context, key string) error {
	// Check context cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		// Continue
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	if entry, exists := c.entries[key]; exists {
		// Return the entry to the pool if it exists
		c.resultPool.Put(entry.Result)
		delete(c.entries, key)
	}

	return nil
}

// Clear removes all entries from the cache
func (c *LocalCache) Clear(ctx context.Context) error {
	// Check context cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		// Continue
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	// Return all results to the pool
	for _, entry := range c.entries {
		c.resultPool.Put(entry.Result)
	}

	c.entries = make(map[string]*Entry, c.maxSize)
	return nil
}

// Close stops the janitor goroutine
func (c *LocalCache) Close() error {
	close(c.janitorStop)
	return nil
}

// janitor periodically cleans expired entries
func (c *LocalCache) janitor() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.cleanExpired()
		case <-c.janitorStop:
			return
		}
	}
}

// cleanExpired removes all expired entries
func (c *LocalCache) cleanExpired() {
	now := time.Now()
	keysToDelete := make([]string, 0)

	// Find expired keys
	c.mutex.RLock()
	for k, entry := range c.entries {
		if now.After(entry.Expiry) {
			keysToDelete = append(keysToDelete, k)
		}
	}
	c.mutex.RUnlock()

	// Delete expired keys
	if len(keysToDelete) > 0 {
		c.mutex.Lock()
		for _, k := range keysToDelete {
			if entry, exists := c.entries[k]; exists && now.After(entry.Expiry) {
				// Return the entry to the pool
				c.resultPool.Put(entry.Result)
				delete(c.entries, k)
			}
		}
		c.mutex.Unlock()
	}
}

// MultiLevelCache implements a cache with local and Redis layers
type MultiLevelCache struct {
	local       *LocalCache
	remote      Cache
	hitCounter  uint64
	missCounter uint64
	mutex       sync.RWMutex
}

// NewMultiLevelCache creates a new multi-level cache
func NewMultiLevelCache(local *LocalCache, remote Cache) *MultiLevelCache {
	return &MultiLevelCache{
		local:  local,
		remote: remote,
	}
}

// Get retrieves an entry from the cache, trying local first then remote
func (c *MultiLevelCache) Get(ctx context.Context, key string) (*service.LimitResult, bool) {
	// Try local cache first
	if c.local != nil {
		if result, found := c.local.Get(ctx, key); found {
			c.mutex.Lock()
			c.hitCounter++
			c.mutex.Unlock()
			return result, true
		}
	}

	// Try remote cache
	if c.remote != nil {
		if result, found := c.remote.Get(ctx, key); found {
			// Update local cache
			if c.local != nil {
				_ = c.local.Set(ctx, key, result, 0)
			}
			c.mutex.Lock()
			c.hitCounter++
			c.mutex.Unlock()
			return result, true
		}
	}

	c.mutex.Lock()
	c.missCounter++
	c.mutex.Unlock()
	return nil, false
}

// Set adds or updates an entry in both local and remote caches
func (c *MultiLevelCache) Set(ctx context.Context, key string, result *service.LimitResult, ttl time.Duration) error {
	var errs []error

	// Update local cache
	if c.local != nil {
		if err := c.local.Set(ctx, key, result, ttl); err != nil {
			errs = append(errs, err)
		}
	}

	// Update remote cache
	if c.remote != nil {
		if err := c.remote.Set(ctx, key, result, ttl); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return errors.New("failed to set cache entry in one or more cache layers")
	}
	return nil
}

// Delete removes an entry from both local and remote caches
func (c *MultiLevelCache) Delete(ctx context.Context, key string) error {
	var errs []error

	// Delete from local cache
	if c.local != nil {
		if err := c.local.Delete(ctx, key); err != nil {
			errs = append(errs, err)
		}
	}

	// Delete from remote cache
	if c.remote != nil {
		if err := c.remote.Delete(ctx, key); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return errors.New("failed to delete cache entry from one or more cache layers")
	}
	return nil
}

// Clear removes all entries from both local and remote caches
func (c *MultiLevelCache) Clear(ctx context.Context) error {
	var errs []error

	// Clear local cache
	if c.local != nil {
		if err := c.local.Clear(ctx); err != nil {
			errs = append(errs, err)
		}
	}

	// Clear remote cache
	if c.remote != nil {
		if err := c.remote.Clear(ctx); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return errors.New("failed to clear one or more cache layers")
	}
	return nil
}

// GetHitRate returns the cache hit rate
func (c *MultiLevelCache) GetHitRate() float64 {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	total := c.hitCounter + c.missCounter
	if total == 0 {
		return 0
	}
	return float64(c.hitCounter) / float64(total)
}

// Close closes the cache
func (c *MultiLevelCache) Close() error {
	var errs []error

	if c.local != nil {
		if err := c.local.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return errors.New("failed to close one or more cache layers")
	}
	return nil
} 