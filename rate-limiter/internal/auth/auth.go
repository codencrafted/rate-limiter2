package auth

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/distributed/ratelimiter/internal/config"
	"github.com/distributed/ratelimiter/internal/service"
	"github.com/gin-gonic/gin"
)

// Role represents a user role for authorization
type Role string

// Available roles
const (
	RoleAdmin    Role = "admin"
	RoleUser     Role = "user"
	RoleReadOnly Role = "readonly"
)

// APIKey represents an API key with metadata
type APIKey struct {
	Key      string
	Role     Role
	IsActive bool
	LastUsed time.Time
}

// APIKeyStore manages API keys
type APIKeyStore struct {
	keys  map[string]APIKey
	mutex sync.RWMutex
}

// NewAPIKeyStore creates a new API key store
func NewAPIKeyStore(cfg *config.Config) *APIKeyStore {
	store := &APIKeyStore{
		keys: make(map[string]APIKey),
	}

	// Initialize with configured API keys
	for _, keyConfig := range cfg.Auth.APIKeys {
		store.AddKey(keyConfig.Key, Role(keyConfig.Role), keyConfig.IsActive)
	}

	return store
}

// AddKey adds a new API key
func (s *APIKeyStore) AddKey(key string, role Role, isActive bool) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.keys[key] = APIKey{
		Key:      key,
		Role:     role,
		IsActive: isActive,
		LastUsed: time.Time{},
	}
}

// GetKey retrieves an API key
func (s *APIKeyStore) GetKey(key string) (APIKey, bool) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	apiKey, exists := s.keys[key]
	if exists && apiKey.IsActive {
		// Update last used time
		s.mutex.RUnlock()
		s.mutex.Lock()
		apiKey.LastUsed = time.Now()
		s.keys[key] = apiKey
		s.mutex.Unlock()
		s.mutex.RLock()
		return apiKey, true
	}
	return APIKey{}, false
}

// DeleteKey removes an API key
func (s *APIKeyStore) DeleteKey(key string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	delete(s.keys, key)
}

// Common errors
var (
	ErrMissingAPIKey     = errors.New("missing API key")
	ErrInvalidAPIKey     = errors.New("invalid API key")
	ErrInsufficientPerms = errors.New("insufficient permissions")
)

// APIKeyAuthMiddleware creates middleware for API key authentication
func APIKeyAuthMiddleware(keyStore *APIKeyStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get API key from header
		apiKey := c.GetHeader("X-API-Key")
		if apiKey == "" {
			// Try query parameter
			apiKey = c.Query("api_key")
		}

		if apiKey == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": ErrMissingAPIKey.Error(),
			})
			return
		}

		// Validate API key
		key, exists := keyStore.GetKey(apiKey)
		if !exists {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": ErrInvalidAPIKey.Error(),
			})
			return
		}

		// Store API key info in context
		c.Set("api_key", key.Key)
		c.Set("role", key.Role)

		c.Next()
	}
}

// RoleAuthMiddleware creates middleware for role-based authorization
func RoleAuthMiddleware(requiredRoles ...Role) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get role from context
		roleValue, exists := c.Get("role")
		if !exists {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "authorization required",
			})
			return
		}

		role := roleValue.(Role)

		// Check if user has required role
		for _, requiredRole := range requiredRoles {
			// Admin role has access to everything
			if role == RoleAdmin || role == requiredRole {
				c.Next()
				return
			}
		}

		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"error": ErrInsufficientPerms.Error(),
		})
	}
}

// SecurityHeadersMiddleware adds security headers to responses
func SecurityHeadersMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Set security headers
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-XSS-Protection", "1; mode=block")
		c.Header("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		c.Header("Content-Security-Policy", "default-src 'self'")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		
		c.Next()
	}
}

// CORSSecurityMiddleware creates a secure CORS middleware
func CORSSecurityMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Key")
		c.Header("Access-Control-Max-Age", "86400")

		// Handle preflight requests
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

// RateLimitMiddleware creates a rate limiting middleware
func RateLimitMiddleware(limiter *service.RateLimiterService) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Extract identifier (IP or API key)
		identifier := c.ClientIP()
		apiKey, exists := c.Get("api_key")
		if exists {
			identifier = apiKey.(string)
		}

		// Apply rate limit
		ctx := c.Request.Context()
		result, err := limiter.CheckRateLimit(ctx, "auth", identifier, "minute")
		if err != nil {
			// On error, allow the request but log the issue
			c.Next()
			return
		}

		// Add rate limit headers
		c.Header("X-RateLimit-Limit", string(result.Limit))
		c.Header("X-RateLimit-Remaining", string(result.Remaining))
		c.Header("X-RateLimit-Reset", string(int(time.Now().Add(result.ResetAfter).Unix())))

		if result.Limited {
			c.Header("Retry-After", string(int(result.ResetAfter.Seconds())))
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "rate limit exceeded",
				"limit": result.Limit,
				"reset": result.ResetAfter.Seconds(),
			})
			return
		}

		c.Next()
	}
}

// BasicAuthMiddleware creates a basic authentication middleware for testing
func BasicAuthMiddleware(username, password string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get Authorization header
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.Header("WWW-Authenticate", "Basic realm=\"API\"")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "authentication required",
			})
			return
		}

		// Check if it's Basic auth
		if !strings.HasPrefix(authHeader, "Basic ") {
			c.Header("WWW-Authenticate", "Basic realm=\"API\"")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "invalid authentication method",
			})
			return
		}

		// Decode and verify
		auth := strings.TrimPrefix(authHeader, "Basic ")
		providedUsername, providedPassword, ok := parseBasicAuth(auth)
		if !ok {
			c.Header("WWW-Authenticate", "Basic realm=\"API\"")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "invalid authentication format",
			})
			return
		}

		// Constant-time comparison to prevent timing attacks
		if subtle.ConstantTimeCompare([]byte(providedUsername), []byte(username)) != 1 ||
			subtle.ConstantTimeCompare([]byte(providedPassword), []byte(password)) != 1 {
			c.Header("WWW-Authenticate", "Basic realm=\"API\"")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "invalid credentials",
			})
			return
		}

		c.Next()
	}
}

// parseBasicAuth parses an HTTP Basic Authentication string
func parseBasicAuth(auth string) (username, password string, ok bool) {
	// Implementation of basic auth parsing (simplified)
	// In a real implementation, you'd use the built-in parseBasicAuth from net/http
	parts := strings.SplitN(auth, ":", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
} 