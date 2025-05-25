package handler

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/distributed/ratelimiter/internal/service"
	"github.com/gin-gonic/gin"
)

// HTTPHandler handles HTTP requests
type HTTPHandler struct {
	rateLimiter *service.RateLimiterService
}

// NewHTTPHandler creates a new HTTP handler
func NewHTTPHandler(rateLimiter *service.RateLimiterService) *HTTPHandler {
	return &HTTPHandler{
		rateLimiter: rateLimiter,
	}
}

// RegisterRoutes registers HTTP routes
func (h *HTTPHandler) RegisterRoutes(router *gin.Engine) {
	// Health check
	router.GET("/health", h.healthCheck)
	
	// Rate limiting API
	v1 := router.Group("/api/v1")
	{
		// Check rate limit
		v1.GET("/check", h.checkRateLimit)
		
		// Management endpoints
		management := v1.Group("/management")
		{
			// Update whitelist
			management.POST("/whitelist", h.updateWhitelist)
			
			// Set custom limit
			management.POST("/limit", h.setLimit)
		}
	}
	
	// Rate limiting middleware for protected routes
	protected := router.Group("/protected")
	protected.Use(h.rateLimitMiddleware())
	{
		protected.GET("/resource", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{
				"message": "Rate limited resource accessed successfully",
			})
		})
	}
}

// healthCheck handles health check requests
func (h *HTTPHandler) healthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "healthy",
		"time":   time.Now().Format(time.RFC3339),
	})
}

// checkRateLimit handles rate limit check requests
func (h *HTTPHandler) checkRateLimit(c *gin.Context) {
	key := c.Query("key")
	if key == "" {
		key = "default"
	}
	
	identifier := c.Query("identifier")
	if identifier == "" {
		identifier = c.ClientIP()
	}
	
	window := c.Query("window")
	if window == "" {
		window = "minute"
	}
	
	ctx, cancel := context.WithTimeout(c.Request.Context(), 500*time.Millisecond)
	defer cancel()
	
	result, err := h.rateLimiter.CheckRateLimit(ctx, key, identifier, window)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}
	
	c.JSON(http.StatusOK, gin.H{
		"limited":       result.Limited,
		"current_count": result.CurrentCount,
		"limit":         result.Limit,
		"remaining":     result.Remaining,
		"reset_after":   result.ResetAfter.Seconds(),
		"window":        result.Window,
	})
}

// updateWhitelist handles whitelist update requests
func (h *HTTPHandler) updateWhitelist(c *gin.Context) {
	var request struct {
		Key        string `json:"key" binding:"required"`
		Identifier string `json:"identifier" binding:"required"`
		Whitelisted bool   `json:"whitelisted" binding:"required"`
	}
	
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}
	
	ctx, cancel := context.WithTimeout(c.Request.Context(), 500*time.Millisecond)
	defer cancel()
	
	err := h.rateLimiter.UpdateWhitelist(ctx, request.Key, request.Identifier, request.Whitelisted)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}
	
	c.JSON(http.StatusOK, gin.H{
		"success": true,
	})
}

// setLimit handles limit setting requests
func (h *HTTPHandler) setLimit(c *gin.Context) {
	var request struct {
		Key        string `json:"key" binding:"required"`
		Identifier string `json:"identifier" binding:"required"`
		Window     string `json:"window" binding:"required"`
		Limit      int    `json:"limit" binding:"required"`
	}
	
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}
	
	ctx, cancel := context.WithTimeout(c.Request.Context(), 500*time.Millisecond)
	defer cancel()
	
	err := h.rateLimiter.SetLimit(ctx, request.Key, request.Identifier, request.Window, request.Limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}
	
	c.JSON(http.StatusOK, gin.H{
		"success": true,
	})
}

// rateLimitMiddleware creates a middleware for rate limiting
func (h *HTTPHandler) rateLimitMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Use IP address as identifier by default
		identifier := c.ClientIP()
		
		// Use API key from header if available
		apiKey := c.GetHeader("X-API-Key")
		if apiKey != "" {
			identifier = apiKey
		}
		
		// Check rate limit for minute window
		ctx, cancel := context.WithTimeout(c.Request.Context(), 100*time.Millisecond)
		defer cancel()
		
		result, err := h.rateLimiter.CheckRateLimit(ctx, "api", identifier, "minute")
		if err != nil {
			// Allow request if rate limiter fails
			c.Next()
			return
		}
		
		// Set rate limit headers
		c.Header("X-RateLimit-Limit", strconv.Itoa(result.Limit))
		c.Header("X-RateLimit-Remaining", strconv.Itoa(result.Remaining))
		c.Header("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(result.ResetAfter).Unix(), 10))
		
		if result.Limited {
			c.Header("Retry-After", strconv.FormatInt(int64(result.ResetAfter.Seconds()), 10))
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "Rate limit exceeded",
				"limit": result.Limit,
				"reset": result.ResetAfter.Seconds(),
			})
			return
		}
		
		c.Next()
	}
} 