# Build Stage
FROM golang:1.21-alpine AS builder

# Install dependencies
RUN apk --no-cache add ca-certificates git

WORKDIR /app

# Copy go.mod and go.sum first to leverage Docker cache
COPY go.mod go.sum* ./
RUN go mod download

# Copy the rest of the source code
COPY . .

# Build the application with optimizations
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o /app/ratelimiter ./cmd/server

# Final Stage
FROM alpine:3.18

# Add ca-certificates for HTTPS
RUN apk --no-cache add ca-certificates tzdata && \
    update-ca-certificates

# Create non-root user
RUN adduser -D -H -h /app appuser
USER appuser

WORKDIR /app

# Copy the binary from the builder stage
COPY --from=builder /app/ratelimiter .
# Copy config files
COPY --from=builder /app/config.yaml ./config.yaml

# Expose HTTP and gRPC ports
EXPOSE 8080 9090

# Set environment variables
ENV GIN_MODE=release

# Run the application
CMD ["./ratelimiter"] 