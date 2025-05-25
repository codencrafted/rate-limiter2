package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/distributed/ratelimiter/internal/config"
	"github.com/distributed/ratelimiter/internal/handler"
	"github.com/distributed/ratelimiter/internal/metrics"
	"github.com/distributed/ratelimiter/internal/service"
	"github.com/distributed/ratelimiter/internal/storage"
	
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Initialize Redis client
	redisClient := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Address,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})

	// Ping Redis to check connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	if _, err := redisClient.Ping(ctx).Result(); err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}
	
	// Initialize metrics
	metricsCollector := metrics.NewMetrics()
	
	// Initialize storage
	store := storage.NewRedisStorage(redisClient)
	
	// Initialize rate limiter service
	rateLimiterService := service.NewRateLimiterService(store, cfg.RateLimiter)
	
	// Start HTTP server
	router := gin.Default()
	
	// Add Prometheus metrics endpoint
	router.GET("/metrics", gin.WrapH(promhttp.Handler()))
	
	httpHandler := handler.NewHTTPHandler(rateLimiterService)
	httpHandler.RegisterRoutes(router)
	
	httpServer := &http.Server{
		Addr:    cfg.HTTP.Address,
		Handler: router,
	}
	
	// Start gRPC server
	grpcServer := grpc.NewServer()
	grpcHandler := handler.NewGRPCHandler(rateLimiterService)
	grpcHandler.RegisterServer(grpcServer)
	
	lis, err := net.Listen("tcp", cfg.GRPC.Address)
	if err != nil {
		log.Fatalf("Failed to listen for gRPC: %v", err)
	}
	
	// Start servers in goroutines
	go func() {
		log.Printf("Starting HTTP server on %s", cfg.HTTP.Address)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()
	
	go func() {
		log.Printf("Starting gRPC server on %s", cfg.GRPC.Address)
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("gRPC server error: %v", err)
		}
	}()
	
	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	
	log.Println("Shutting down server...")
	
	// Shutdown HTTP server
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	if err := httpServer.Shutdown(ctx); err != nil {
		log.Fatalf("HTTP server forced to shutdown: %v", err)
	}
	
	// Stop gRPC server
	grpcServer.GracefulStop()
	
	log.Println("Server exiting")
} 