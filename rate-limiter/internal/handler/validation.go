package handler

import (
	"errors"
	"net/http"
	"regexp"

	"github.com/gin-gonic/gin"
)

// ValidationError represents a validation error
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// Common validation errors
var (
	ErrInvalidIdentifier = errors.New("identifier must be between 1 and 100 characters and contain only alphanumeric, dash, and underscore characters")
	ErrInvalidRateLimit  = errors.New("rate limit must be between 1 and 1,000,000")
	ErrInvalidTimeWindow = errors.New("window must be one of: second, minute, hour, day")
)

// ValidTimeWindows defines the allowed time windows
var ValidTimeWindows = map[string]bool{
	"second": true,
	"minute": true,
	"hour":   true,
	"day":    true,
}

// ValidateIdentifier validates an identifier
func ValidateIdentifier(identifier string) error {
	if len(identifier) < 1 || len(identifier) > 100 {
		return ErrInvalidIdentifier
	}

	// Check for valid characters (alphanumeric, dash, underscore)
	match, _ := regexp.MatchString("^[a-zA-Z0-9_-]+$", identifier)
	if !match {
		return ErrInvalidIdentifier
	}

	return nil
}

// ValidateRateLimit validates a rate limit value
func ValidateRateLimit(limit int) error {
	if limit < 1 || limit > 1000000 {
		return ErrInvalidRateLimit
	}
	return nil
}

// ValidateTimeWindow validates a time window
func ValidateTimeWindow(window string) error {
	if !ValidTimeWindows[window] {
		return ErrInvalidTimeWindow
	}
	return nil
}

// SanitizeInput sanitizes input to prevent injection attacks
func SanitizeInput(input string) string {
	// Remove any control characters
	reg := regexp.MustCompile(`[[:cntrl:]]`)
	return reg.ReplaceAllString(input, "")
}

// ValidationMiddleware creates a middleware that validates common request parameters
func ValidationMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get common parameters
		identifier := c.Query("identifier")
		if identifier != "" {
			sanitizedIdentifier := SanitizeInput(identifier)
			if err := ValidateIdentifier(sanitizedIdentifier); err != nil {
				c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
					"error": err.Error(),
				})
				return
			}
			// Replace with sanitized value
			c.Set("sanitized_identifier", sanitizedIdentifier)
		}

		window := c.Query("window")
		if window != "" {
			if err := ValidateTimeWindow(window); err != nil {
				c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
					"error": err.Error(),
				})
				return
			}
		}

		c.Next()
	}
}

// ValidateRequestBody validates a request body against a schema
func ValidateRequestBody(c *gin.Context, obj interface{}) bool {
	if err := c.ShouldBindJSON(obj); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid request body: " + err.Error(),
		})
		return false
	}
	return true
}

// RateLimitRequest represents a rate limit request with validation
type RateLimitRequest struct {
	Key        string `json:"key" binding:"required,min=1,max=100"`
	Identifier string `json:"identifier" binding:"required,min=1,max=100"`
	Window     string `json:"window" binding:"required,oneof=second minute hour day"`
	Limit      int    `json:"limit" binding:"required,min=1,max=1000000"`
}

// WhitelistRequest represents a whitelist update request with validation
type WhitelistRequest struct {
	Key        string `json:"key" binding:"required,min=1,max=100"`
	Identifier string `json:"identifier" binding:"required,min=1,max=100"`
	Whitelisted bool   `json:"whitelisted" binding:"required"`
} 