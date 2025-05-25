package handler

import (
	"context"
	"time"

	pb "github.com/distributed/ratelimiter/api/proto"
	"github.com/distributed/ratelimiter/internal/service"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GRPCHandler handles gRPC requests
type GRPCHandler struct {
	pb.UnimplementedRateLimiterServiceServer
	rateLimiter *service.RateLimiterService
}

// NewGRPCHandler creates a new gRPC handler
func NewGRPCHandler(rateLimiter *service.RateLimiterService) *GRPCHandler {
	return &GRPCHandler{
		rateLimiter: rateLimiter,
	}
}

// RegisterServer registers the gRPC server
func (h *GRPCHandler) RegisterServer(server *grpc.Server) {
	pb.RegisterRateLimiterServiceServer(server, h)
}

// CheckRateLimit implements the CheckRateLimit RPC
func (h *GRPCHandler) CheckRateLimit(ctx context.Context, req *pb.CheckRateLimitRequest) (*pb.CheckRateLimitResponse, error) {
	// Set default values if not provided
	key := req.Key
	if key == "" {
		key = "default"
	}
	
	identifier := req.Identifier
	if identifier == "" {
		return nil, status.Error(codes.InvalidArgument, "identifier is required")
	}
	
	window := req.Window
	if window == "" {
		window = "minute"
	}
	
	// Add timeout
	ctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	
	result, err := h.rateLimiter.CheckRateLimit(ctx, key, identifier, window)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to check rate limit: %v", err)
	}
	
	return &pb.CheckRateLimitResponse{
		Limited:      result.Limited,
		CurrentCount: result.CurrentCount,
		Limit:        int32(result.Limit),
		Remaining:    int32(result.Remaining),
		ResetAfter:   result.ResetAfter.Seconds(),
		Window:       result.Window,
	}, nil
}

// UpdateWhitelist implements the UpdateWhitelist RPC
func (h *GRPCHandler) UpdateWhitelist(ctx context.Context, req *pb.UpdateWhitelistRequest) (*pb.UpdateWhitelistResponse, error) {
	// Validate request
	if req.Key == "" {
		return nil, status.Error(codes.InvalidArgument, "key is required")
	}
	
	if req.Identifier == "" {
		return nil, status.Error(codes.InvalidArgument, "identifier is required")
	}
	
	// Add timeout
	ctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	
	err := h.rateLimiter.UpdateWhitelist(ctx, req.Key, req.Identifier, req.Whitelisted)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to update whitelist: %v", err)
	}
	
	return &pb.UpdateWhitelistResponse{
		Success: true,
	}, nil
}

// SetLimit implements the SetLimit RPC
func (h *GRPCHandler) SetLimit(ctx context.Context, req *pb.SetLimitRequest) (*pb.SetLimitResponse, error) {
	// Validate request
	if req.Key == "" {
		return nil, status.Error(codes.InvalidArgument, "key is required")
	}
	
	if req.Identifier == "" {
		return nil, status.Error(codes.InvalidArgument, "identifier is required")
	}
	
	if req.Window == "" {
		return nil, status.Error(codes.InvalidArgument, "window is required")
	}
	
	if req.Limit <= 0 {
		return nil, status.Error(codes.InvalidArgument, "limit must be positive")
	}
	
	// Add timeout
	ctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	
	err := h.rateLimiter.SetLimit(ctx, req.Key, req.Identifier, req.Window, int(req.Limit))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to set limit: %v", err)
	}
	
	return &pb.SetLimitResponse{
		Success: true,
	}, nil
} 