package storage

import (
	"context"
	"time"
)

// Storage defines the interface for rate limiter storage
type Storage interface {
	// IncrementAndCount increments the counter for a key and returns the current count
	IncrementAndCount(ctx context.Context, key string, window time.Duration) (int64, error)
	
	// IsWhitelisted checks if a key is in the whitelist
	IsWhitelisted(ctx context.Context, key string) (bool, error)
	
	// UpdateWhitelist adds or removes a key from the whitelist
	UpdateWhitelist(ctx context.Context, key string, whitelisted bool) error
	
	// SetLimit sets a custom limit for a key
	SetLimit(ctx context.Context, key string, window string, limit int) error
	
	// GetLimit gets the custom limit for a key if it exists
	GetLimit(ctx context.Context, key string, window string) (int, error)
} 