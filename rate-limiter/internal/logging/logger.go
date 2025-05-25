package logging

import (
	"context"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/distributed/ratelimiter/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ContextKey is a type for context keys
type ContextKey string

// Context keys
const (
	CorrelationIDKey ContextKey = "correlation_id"
	RequestIDKey     ContextKey = "request_id"
	UserIDKey        ContextKey = "user_id"
)

// Logger wraps the slog.Logger with additional functionality
type Logger struct {
	*slog.Logger
	level slog.Level
}

// LoggerOptions contains options for creating a new logger
type LoggerOptions struct {
	Level      string
	Format     string
	Output     string
	TimeFormat string
	AddCaller  bool
}

// NewLogger creates a new logger
func NewLogger(cfg *config.LoggingConfig) *Logger {
	// Determine log level
	var level slog.Level
	switch cfg.Level {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	// Determine output writer
	var output io.Writer
	switch cfg.Output {
	case "stdout":
		output = os.Stdout
	case "stderr":
		output = os.Stderr
	default:
		// Default to stdout
		output = os.Stdout
	}

	// Create handler based on format
	var handler slog.Handler
	switch cfg.Format {
	case "json":
		handler = slog.NewJSONHandler(output, &slog.HandlerOptions{
			Level:     level,
			AddSource: cfg.EnableCaller,
		})
	case "text":
		handler = slog.NewTextHandler(output, &slog.HandlerOptions{
			Level:     level,
			AddSource: cfg.EnableCaller,
		})
	default:
		// Default to JSON for structured logging
		handler = slog.NewJSONHandler(output, &slog.HandlerOptions{
			Level:     level,
			AddSource: cfg.EnableCaller,
		})
	}

	// Create logger
	logger := slog.New(handler)

	return &Logger{
		Logger: logger,
		level:  level,
	}
}

// WithContext returns a logger with context values added as attributes
func (l *Logger) WithContext(ctx context.Context) *Logger {
	logger := l.Logger

	// Add correlation ID if present
	if correlationID, ok := ctx.Value(CorrelationIDKey).(string); ok && correlationID != "" {
		logger = logger.With("correlation_id", correlationID)
	}

	// Add request ID if present
	if requestID, ok := ctx.Value(RequestIDKey).(string); ok && requestID != "" {
		logger = logger.With("request_id", requestID)
	}

	// Add user ID if present
	if userID, ok := ctx.Value(UserIDKey).(string); ok && userID != "" {
		logger = logger.With("user_id", userID)
	}

	return &Logger{
		Logger: logger,
		level:  l.level,
	}
}

// WithField adds a single field to the logger
func (l *Logger) WithField(key string, value interface{}) *Logger {
	return &Logger{
		Logger: l.Logger.With(key, value),
		level:  l.level,
	}
}

// WithFields adds multiple fields to the logger
func (l *Logger) WithFields(fields map[string]interface{}) *Logger {
	logger := l.Logger
	for k, v := range fields {
		logger = logger.With(k, v)
	}
	return &Logger{
		Logger: logger,
		level:  l.level,
	}
}

// DebugEnabled returns true if debug logging is enabled
func (l *Logger) DebugEnabled() bool {
	return l.level <= slog.LevelDebug
}

// InfoEnabled returns true if info logging is enabled
func (l *Logger) InfoEnabled() bool {
	return l.level <= slog.LevelInfo
}

// WarnEnabled returns true if warn logging is enabled
func (l *Logger) WarnEnabled() bool {
	return l.level <= slog.LevelWarn
}

// ErrorEnabled returns true if error logging is enabled
func (l *Logger) ErrorEnabled() bool {
	return l.level <= slog.LevelError
}

// LoggingMiddleware creates a middleware that adds logging to requests
func LoggingMiddleware(logger *Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		
		// Generate correlation ID if not present
		correlationID := c.GetHeader("X-Correlation-ID")
		if correlationID == "" {
			correlationID = uuid.New().String()
			c.Header("X-Correlation-ID", correlationID)
		}
		
		// Add correlation ID to context
		ctx := context.WithValue(c.Request.Context(), CorrelationIDKey, correlationID)
		c.Request = c.Request.WithContext(ctx)
		
		// Store correlation ID in Gin context for easy access
		c.Set(string(CorrelationIDKey), correlationID)
		
		// Add request ID
		requestID := uuid.New().String()
		ctx = context.WithValue(ctx, RequestIDKey, requestID)
		c.Request = c.Request.WithContext(ctx)
		c.Set(string(RequestIDKey), requestID)
		
		// Add user ID if authenticated
		if userID, exists := c.Get("user_id"); exists {
			ctx = context.WithValue(ctx, UserIDKey, userID)
			c.Request = c.Request.WithContext(ctx)
		}
		
		// Process request
		c.Next()
		
		// Log request details
		duration := time.Since(start)
		path := c.Request.URL.Path
		raw := c.Request.URL.RawQuery
		if raw != "" {
			path = path + "?" + raw
		}
		
		// Get request logger with context
		reqLogger := logger.WithContext(c.Request.Context())
		
		// Log based on status code
		status := c.Writer.Status()
		fields := map[string]interface{}{
			"status":     status,
			"method":     c.Request.Method,
			"path":       path,
			"ip":         c.ClientIP(),
			"user_agent": c.Request.UserAgent(),
			"latency_ms": float64(duration.Microseconds()) / 1000.0,
			"size":       c.Writer.Size(),
		}
		
		if status >= 500 {
			reqLogger.WithFields(fields).Error("Server error")
		} else if status >= 400 {
			reqLogger.WithFields(fields).Warn("Client error")
		} else {
			reqLogger.WithFields(fields).Info("Request completed")
		}
	}
}

// CorrelationIDMiddleware creates a middleware that adds correlation IDs to requests
func CorrelationIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		correlationID := c.GetHeader("X-Correlation-ID")
		if correlationID == "" {
			correlationID = uuid.New().String()
			c.Header("X-Correlation-ID", correlationID)
		}
		
		ctx := context.WithValue(c.Request.Context(), CorrelationIDKey, correlationID)
		c.Request = c.Request.WithContext(ctx)
		c.Set(string(CorrelationIDKey), correlationID)
		
		c.Next()
	}
} 