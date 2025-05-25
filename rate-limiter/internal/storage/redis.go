package storage

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/distributed/ratelimiter/internal/config"
	"github.com/redis/go-redis/v9"
)

const (
	// Key prefixes for Redis
	prefixCounter   = "ratelimit:counter:"
	prefixWhitelist = "ratelimit:whitelist:"
	prefixLimit     = "ratelimit:limit:"
)

// RedisStorage implements the Storage interface using Redis
type RedisStorage struct {
	client          redis.UniversalClient
	config          *config.RedisConfig
	slidingWindowLua *redis.Script
	mutex           sync.Mutex
	failureCount    int
	lastFailure     time.Time
}

// NewRedisStorage creates a new Redis storage
func NewRedisStorage(cfg *config.RedisConfig) (*RedisStorage, error) {
	var client redis.UniversalClient
	
	if cfg.ClusterMode {
		// Configure Redis Cluster
		client = redis.NewClusterClient(&redis.ClusterOptions{
			Addrs:        cfg.Addresses,
			Password:     cfg.Password,
			PoolSize:     cfg.PoolSize,
			MinIdleConns: cfg.MinIdleConns,
			DialTimeout:  cfg.DialTimeout,
			ReadTimeout:  cfg.ReadTimeout,
			WriteTimeout: cfg.WriteTimeout,
			PoolTimeout:  cfg.PoolTimeout,
			MaxRetries:   cfg.MaxRetries,
		})
	} else {
		// Configure standalone Redis
		client = redis.NewClient(&redis.Options{
			Addr:         cfg.Addresses[0],
			Password:     cfg.Password,
			DB:           cfg.DB,
			PoolSize:     cfg.PoolSize,
			MinIdleConns: cfg.MinIdleConns,
			DialTimeout:  cfg.DialTimeout,
			ReadTimeout:  cfg.ReadTimeout,
			WriteTimeout: cfg.WriteTimeout,
			PoolTimeout:  cfg.PoolTimeout,
			MaxRetries:   cfg.MaxRetries,
		})
	}

	// Create sliding window counter Lua script
	slidingWindowLua := redis.NewScript(`
		local key = KEYS[1]
		local now = tonumber(ARGV[1])
		local window = tonumber(ARGV[2])
		local cutoff = now - window
		
		-- Remove timestamps older than the window
		redis.call('ZREMRANGEBYSCORE', key, 0, cutoff)
		
		-- Add current timestamp with score now
		redis.call('ZADD', key, now, now)
		
		-- Set key expiration to window + 1s for cleanup
		redis.call('PEXPIRE', key, window + 1000)
		
		-- Count events in current window
		return redis.call('ZCARD', key)
	`)

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return &RedisStorage{
		client:          client,
		config:          cfg,
		slidingWindowLua: slidingWindowLua,
		failureCount:    0,
		lastFailure:     time.Time{},
	}, nil
}

// Close closes the Redis connection
func (s *RedisStorage) Close() error {
	return s.client.Close()
}

// IncrementAndCount implements the sliding window counter algorithm
func (s *RedisStorage) IncrementAndCount(ctx context.Context, key string, window time.Duration) (int64, error) {
	// Check for context cancellation before making Redis call
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	default:
		// Continue with operation
	}

	now := time.Now().UnixNano() / int64(time.Millisecond)
	windowMs := window.Milliseconds()
	
	// Key format: ratelimit:counter:<key>
	redisKey := fmt.Sprintf("%s%s", prefixCounter, key)
	
	// Run the script with automatic retries
	result, err := s.slidingWindowLua.Run(ctx, s.client, []string{redisKey}, now, windowMs).Int64()
	if err != nil {
		return 0, fmt.Errorf("failed to execute Redis script: %w", err)
	}
	
	return result, nil
}

// IsWhitelisted checks if a key is in the whitelist
func (s *RedisStorage) IsWhitelisted(ctx context.Context, key string) (bool, error) {
	// Check for context cancellation before making Redis call
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	default:
		// Continue with operation
	}

	// Key format: ratelimit:whitelist:<key>
	redisKey := fmt.Sprintf("%s%s", prefixWhitelist, key)
	
	// Check if key exists in whitelist
	result, err := s.client.Exists(ctx, redisKey).Result()
	if err != nil {
		return false, fmt.Errorf("failed to check whitelist: %w", err)
	}
	
	return result > 0, nil
}

// UpdateWhitelist adds or removes a key from the whitelist
func (s *RedisStorage) UpdateWhitelist(ctx context.Context, key string, whitelisted bool) error {
	// Check for context cancellation before making Redis call
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		// Continue with operation
	}

	// Key format: ratelimit:whitelist:<key>
	redisKey := fmt.Sprintf("%s%s", prefixWhitelist, key)
	
	if whitelisted {
		// Add to whitelist with a TTL of 1 day (configurable in production)
		_, err := s.client.Set(ctx, redisKey, 1, 24*time.Hour).Result()
		if err != nil {
			return fmt.Errorf("failed to add to whitelist: %w", err)
		}
	} else {
		// Remove from whitelist
		_, err := s.client.Del(ctx, redisKey).Result()
		if err != nil {
			return fmt.Errorf("failed to remove from whitelist: %w", err)
		}
	}
	
	return nil
}

// SetLimit sets a custom limit for a key
func (s *RedisStorage) SetLimit(ctx context.Context, key string, window string, limit int) error {
	// Check for context cancellation before making Redis call
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		// Continue with operation
	}

	// Key format: ratelimit:limit:<key>:<window>
	redisKey := fmt.Sprintf("%s%s:%s", prefixLimit, key, window)
	
	// Set limit with a TTL of 1 day (configurable in production)
	_, err := s.client.Set(ctx, redisKey, limit, 24*time.Hour).Result()
	if err != nil {
		return fmt.Errorf("failed to set limit: %w", err)
	}
	
	return nil
}

// GetLimit gets the custom limit for a key if it exists
func (s *RedisStorage) GetLimit(ctx context.Context, key string, window string) (int, error) {
	// Check for context cancellation before making Redis call
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	default:
		// Continue with operation
	}

	// Key format: ratelimit:limit:<key>:<window>
	redisKey := fmt.Sprintf("%s%s:%s", prefixLimit, key, window)
	
	// Get limit
	result, err := s.client.Get(ctx, redisKey).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			// No custom limit set, not an error
			return 0, nil
		}
		return 0, fmt.Errorf("failed to get limit: %w", err)
	}
	
	// Parse limit
	limit, err := strconv.Atoi(result)
	if err != nil {
		return 0, fmt.Errorf("invalid limit value: %w", err)
	}
	
	return limit, nil
}

// BatchIncrementAndCount increments counters for multiple keys in a single pipeline
func (s *RedisStorage) BatchIncrementAndCount(ctx context.Context, keys []string, window time.Duration) (map[string]int64, error) {
	// Check for context cancellation before making Redis call
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		// Continue with operation
	}

	now := time.Now().UnixNano() / int64(time.Millisecond)
	windowMs := window.Milliseconds()
	
	// Use pipelining for efficiency
	pipe := s.client.Pipeline()
	
	// Track the order of commands to match results later
	cmdMap := make(map[string]*redis.Cmd, len(keys))
	
	// Add all keys to pipeline
	for _, key := range keys {
		redisKey := fmt.Sprintf("%s%s", prefixCounter, key)
		cmdMap[key] = s.slidingWindowLua.Run(ctx, pipe, []string{redisKey}, now, windowMs)
	}
	
	// Execute pipeline
	_, err := pipe.Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to execute Redis pipeline: %w", err)
	}
	
	// Process results
	results := make(map[string]int64, len(keys))
	for key, cmd := range cmdMap {
		count, err := cmd.Int64()
		if err != nil {
			return nil, fmt.Errorf("failed to get count for key %s: %w", key, err)
		}
		results[key] = count
	}
	
	return results, nil
}

// PingWithContext pings the Redis server to check health
func (s *RedisStorage) PingWithContext(ctx context.Context) error {
	return s.client.Ping(ctx).Err()
}

// FlushAll flushes all data (for testing only)
func (s *RedisStorage) FlushAll(ctx context.Context) error {
	return s.client.FlushAll(ctx).Err()
} 