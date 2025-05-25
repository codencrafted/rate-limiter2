package tracing

import (
	"context"
	"fmt"
	"net/http"

	"github.com/distributed/ratelimiter/internal/config"
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Tracer is a wrapper around OpenTelemetry tracer
type Tracer struct {
	tracer trace.Tracer
	config *config.TracingConfig
}

// NewTracer creates a new tracer
func NewTracer(cfg *config.TracingConfig) (*Tracer, error) {
	if !cfg.Enabled {
		return &Tracer{
			config: cfg,
		}, nil
	}

	// Create exporter
	conn, err := grpc.Dial(cfg.Endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC connection to collector: %w", err)
	}

	// Set up a trace exporter
	traceExporter, err := otlptracegrpc.New(context.Background(),
		otlptracegrpc.WithGRPCConn(conn),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create trace exporter: %w", err)
	}

	// Create resource
	res, err := resource.New(context.Background(),
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion("v1.0.0"),
			attribute.String("environment", "production"),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Create trace provider
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.TraceIDRatioBased(cfg.SamplingRate)),
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)

	// Set global propagator
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// Create tracer
	tracer := otel.Tracer("github.com/distributed/ratelimiter")

	return &Tracer{
		tracer: tracer,
		config: cfg,
	}, nil
}

// StartSpan starts a new span
func (t *Tracer) StartSpan(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	if t.tracer == nil || !t.config.Enabled {
		return ctx, trace.SpanFromContext(ctx)
	}
	return t.tracer.Start(ctx, name, opts...)
}

// SpanFromContext returns the current span from context
func (t *Tracer) SpanFromContext(ctx context.Context) trace.Span {
	return trace.SpanFromContext(ctx)
}

// AddEvent adds an event to the current span
func (t *Tracer) AddEvent(ctx context.Context, name string, attrs ...attribute.KeyValue) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	span.AddEvent(name, trace.WithAttributes(attrs...))
}

// SetAttributes sets attributes on the current span
func (t *Tracer) SetAttributes(ctx context.Context, attrs ...attribute.KeyValue) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	span.SetAttributes(attrs...)
}

// EndSpan ends the current span
func (t *Tracer) EndSpan(ctx context.Context) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	span.End()
}

// RecordError records an error on the current span
func (t *Tracer) RecordError(ctx context.Context, err error, opts ...trace.EventOption) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() || err == nil {
		return
	}
	span.RecordError(err, opts...)
}

// TraceMiddleware creates a middleware that adds tracing to requests
func TraceMiddleware(tracer *Tracer) gin.HandlerFunc {
	if tracer == nil || !tracer.config.Enabled {
		return func(c *gin.Context) {
			c.Next()
		}
	}

	return func(c *gin.Context) {
		// Extract context from request
		ctx := c.Request.Context()
		
		// Extract trace context from headers
		carrier := propagation.HeaderCarrier(c.Request.Header)
		ctx = otel.GetTextMapPropagator().Extract(ctx, carrier)
		
		// Start a new span
		spanName := fmt.Sprintf("%s %s", c.Request.Method, c.FullPath())
		ctx, span := tracer.StartSpan(ctx, spanName,
			trace.WithAttributes(
				semconv.HTTPMethod(c.Request.Method),
				semconv.HTTPURL(c.Request.URL.String()),
				semconv.HTTPUserAgent(c.Request.UserAgent()),
				semconv.HTTPClientIP(c.ClientIP()),
			),
			trace.WithSpanKind(trace.SpanKindServer),
		)
		defer span.End()
		
		// Update request context
		c.Request = c.Request.WithContext(ctx)
		
		// Process request
		c.Next()
		
		// Add response attributes
		span.SetAttributes(
			semconv.HTTPStatusCode(c.Writer.Status()),
			attribute.Int("http.response_size", c.Writer.Size()),
		)
		
		// Mark span as error if status code is 4xx or 5xx
		if c.Writer.Status() >= http.StatusBadRequest {
			span.SetStatus(trace.StatusCodeError, http.StatusText(c.Writer.Status()))
		} else {
			span.SetStatus(trace.StatusCodeOk, "")
		}
	}
}

// Shutdown gracefully shuts down the tracer
func (t *Tracer) Shutdown(ctx context.Context) error {
	if !t.config.Enabled {
		return nil
	}

	// Get trace provider
	tp, ok := otel.GetTracerProvider().(*sdktrace.TracerProvider)
	if !ok {
		return nil
	}

	return tp.Shutdown(ctx)
} 