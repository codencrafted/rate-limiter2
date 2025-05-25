package integration

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/distributed/ratelimiter/internal/config"
	"github.com/distributed/ratelimiter/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// TestRedisStorage tests the Redis storage implementation
func TestRedisStorage(t *testing.T) {
	// Skip if in CI environment with -short flag
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create Redis container
	ctx := context.Background()
	container, port := setupRedisContainer(t, ctx)
	defer container.Terminate(ctx)

	// Create Redis config
	cfg := &config.RedisConfig{
		Addresses:    []string{fmt.Sprintf("localhost:%s", port)},
		Password:     "",
		DB:           0,
		PoolSize:     10,
		MinIdleConns: 2,
		MaxRetries:   3,
		DialTimeout:  200 * time.Millisecond,
		ReadTimeout:  500 * time.Millisecond,
		WriteTimeout: 500 * time.Millisecond,
		PoolTimeout:  1 * time.Second,
		ClusterMode:  false,
	}

	// Create storage
	redisStorage, err := storage.NewRedisStorage(cfg)
	require.NoError(t, err, "Failed to create Redis storage")
	defer redisStorage.Close()

	// Test IncrementAndCount
	t.Run("IncrementAndCount", func(t *testing.T) {
		key := "test:key1"
		window := 1 * time.Minute

		// First increment
		count, err := redisStorage.IncrementAndCount(ctx, key, window)
		require.NoError(t, err, "Failed to increment and count")
		assert.Equal(t, int64(1), count, "First increment should return 1")

		// Second increment
		count, err = redisStorage.IncrementAndCount(ctx, key, window)
		require.NoError(t, err, "Failed to increment and count")
		assert.Equal(t, int64(2), count, "Second increment should return 2")

		// Third increment
		count, err = redisStorage.IncrementAndCount(ctx, key, window)
		require.NoError(t, err, "Failed to increment and count")
		assert.Equal(t, int64(3), count, "Third increment should return 3")
	})

	// Test whitelist
	t.Run("Whitelist", func(t *testing.T) {
		key := "test:key2"

		// Check not whitelisted initially
		whitelisted, err := redisStorage.IsWhitelisted(ctx, key)
		require.NoError(t, err, "Failed to check whitelist")
		assert.False(t, whitelisted, "Key should not be whitelisted initially")

		// Add to whitelist
		err = redisStorage.UpdateWhitelist(ctx, key, true)
		require.NoError(t, err, "Failed to update whitelist")

		// Check whitelisted
		whitelisted, err = redisStorage.IsWhitelisted(ctx, key)
		require.NoError(t, err, "Failed to check whitelist")
		assert.True(t, whitelisted, "Key should be whitelisted")

		// Remove from whitelist
		err = redisStorage.UpdateWhitelist(ctx, key, false)
		require.NoError(t, err, "Failed to update whitelist")

		// Check not whitelisted again
		whitelisted, err = redisStorage.IsWhitelisted(ctx, key)
		require.NoError(t, err, "Failed to check whitelist")
		assert.False(t, whitelisted, "Key should not be whitelisted after removal")
	})

	// Test custom limits
	t.Run("CustomLimits", func(t *testing.T) {
		key := "test:key3"
		window := "minute"
		limit := 42

		// Check no custom limit initially
		customLimit, err := redisStorage.GetLimit(ctx, key, window)
		require.NoError(t, err, "Failed to get limit")
		assert.Equal(t, 0, customLimit, "No custom limit should be set initially")

		// Set custom limit
		err = redisStorage.SetLimit(ctx, key, window, limit)
		require.NoError(t, err, "Failed to set limit")

		// Check custom limit
		customLimit, err = redisStorage.GetLimit(ctx, key, window)
		require.NoError(t, err, "Failed to get limit")
		assert.Equal(t, limit, customLimit, "Custom limit should be set")
	})

	// Test batch operations
	t.Run("BatchOperations", func(t *testing.T) {
		keys := []string{"test:batch1", "test:batch2", "test:batch3"}
		window := 1 * time.Minute

		// Batch increment
		results, err := redisStorage.BatchIncrementAndCount(ctx, keys, window)
		require.NoError(t, err, "Failed to batch increment and count")
		assert.Len(t, results, len(keys), "Should return results for all keys")

		// Check each key has count 1
		for _, key := range keys {
			count, ok := results[key]
			assert.True(t, ok, "Missing result for key: %s", key)
			assert.Equal(t, int64(1), count, "First increment should return 1 for key: %s", key)
		}

		// Batch increment again
		results, err = redisStorage.BatchIncrementAndCount(ctx, keys, window)
		require.NoError(t, err, "Failed to batch increment and count")

		// Check each key has count 2
		for _, key := range keys {
			count, ok := results[key]
			assert.True(t, ok, "Missing result for key: %s", key)
			assert.Equal(t, int64(2), count, "Second increment should return 2 for key: %s", key)
		}
	})

	// Test ping
	t.Run("Ping", func(t *testing.T) {
		err := redisStorage.PingWithContext(ctx)
		require.NoError(t, err, "Failed to ping Redis")
	})
}

// setupRedisContainer creates a Redis container for testing
func setupRedisContainer(t *testing.T, ctx context.Context) (testcontainers.Container, string) {
	req := testcontainers.ContainerRequest{
		Image:        "redis:latest",
		ExposedPorts: []string{"6379/tcp"},
		WaitingFor:   wait.ForLog("Ready to accept connections"),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err, "Failed to start Redis container")

	// Get mapped port
	mappedPort, err := container.MappedPort(ctx, "6379")
	require.NoError(t, err, "Failed to get mapped port")

	return container, mappedPort.Port()
}

// TestRedisFailover tests Redis failover scenarios
func TestRedisFailover(t *testing.T) {
	// Skip if in CI environment with -short flag
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create Redis container
	ctx := context.Background()
	container, port := setupRedisContainer(t, ctx)

	// Create Redis config
	cfg := &config.RedisConfig{
		Addresses:    []string{fmt.Sprintf("localhost:%s", port)},
		Password:     "",
		DB:           0,
		PoolSize:     10,
		MinIdleConns: 2,
		MaxRetries:   3,
		DialTimeout:  200 * time.Millisecond,
		ReadTimeout:  500 * time.Millisecond,
		WriteTimeout: 500 * time.Millisecond,
		PoolTimeout:  1 * time.Second,
		ClusterMode:  false,
	}

	// Create storage
	redisStorage, err := storage.NewRedisStorage(cfg)
	require.NoError(t, err, "Failed to create Redis storage")

	// Test normal operation
	key := "test:failover"
	window := 1 * time.Minute

	count, err := redisStorage.IncrementAndCount(ctx, key, window)
	require.NoError(t, err, "Failed to increment and count")
	assert.Equal(t, int64(1), count, "First increment should return 1")

	// Stop Redis container to simulate failure
	err = container.Stop(ctx, nil)
	require.NoError(t, err, "Failed to stop Redis container")

	// Test operation after failure
	_, err = redisStorage.IncrementAndCount(ctx, key, window)
	require.Error(t, err, "Should fail when Redis is down")

	// Start Redis container again
	err = container.Start(ctx)
	require.NoError(t, err, "Failed to start Redis container")
	defer container.Terminate(ctx)

	// Wait for Redis to be ready
	time.Sleep(2 * time.Second)

	// Test operation after recovery
	count, err = redisStorage.IncrementAndCount(ctx, key, window)
	require.NoError(t, err, "Should work after Redis is back up")
	assert.Equal(t, int64(1), count, "Counter should reset after Redis restart")
}

// TestHighConcurrency tests Redis storage under high concurrency
func TestHighConcurrency(t *testing.T) {
	// Skip if in CI environment with -short flag
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create Redis container
	ctx := context.Background()
	container, port := setupRedisContainer(t, ctx)
	defer container.Terminate(ctx)

	// Create Redis config
	cfg := &config.RedisConfig{
		Addresses:    []string{fmt.Sprintf("localhost:%s", port)},
		Password:     "",
		DB:           0,
		PoolSize:     50, // Higher pool size for concurrency
		MinIdleConns: 10,
		MaxRetries:   3,
		DialTimeout:  200 * time.Millisecond,
		ReadTimeout:  500 * time.Millisecond,
		WriteTimeout: 500 * time.Millisecond,
		PoolTimeout:  1 * time.Second,
		ClusterMode:  false,
	}

	// Create storage
	redisStorage, err := storage.NewRedisStorage(cfg)
	require.NoError(t, err, "Failed to create Redis storage")
	defer redisStorage.Close()

	// Test high concurrency
	key := "test:concurrency"
	window := 1 * time.Minute
	goroutines := 100
	iterations := 10

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_, err := redisStorage.IncrementAndCount(ctx, key, window)
				if err != nil {
					t.Logf("Goroutine %d, iteration %d: %v", id, j, err)
				}
				// Add small random sleep to simulate real workload
				time.Sleep(time.Duration(rand.Intn(10)) * time.Millisecond)
			}
		}(i)
	}

	wg.Wait()

	// Check final count
	count, err := redisStorage.IncrementAndCount(ctx, key, window)
	require.NoError(t, err, "Failed to get final count")
	assert.Equal(t, int64(goroutines*iterations+1), count, "Final count should match total operations")
} 