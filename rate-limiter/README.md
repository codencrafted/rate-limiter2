# Distributed Rate Limiter Service

A high-performance, production-ready distributed rate limiter service implemented in Go.

## Features

- **Sliding Window Counter Algorithm**: Accurate rate limiting with Redis
- **Multiple Rate Limiting Strategies**: Support for per-user, per-IP, and per-API-key limiting
- **Configurable Time Windows**: Limit by second, minute, hour, or day
- **High Performance**: Sub-millisecond response times
- **Distributed Architecture**: Scales horizontally with Redis cluster
- **Multi-Protocol Support**: HTTP and gRPC APIs
- **Circuit Breaker**: Fallback mechanisms for Redis failures
- **Observability**: Prometheus metrics and Grafana dashboards
- **Dynamic Configuration**: Update rate limits without redeployment
- **Whitelist Support**: Bypass rate limits for trusted clients

## Prerequisites

- Go 1.16+
- Docker and Docker Compose
- Redis
- Kubernetes (for production deployment)

## Quick Start

### Local Development

1. Clone the repository:
   ```
   git clone https://github.com/yourusername/rate-limiter.git
   cd rate-limiter
   ```

2. Start the service with Docker Compose:
   ```
   docker-compose up
   ```

3. Test the service:
   ```
   curl http://localhost:8080/health
   ```

### Using the HTTP API

#### Check Rate Limit

```bash
curl "http://localhost:8080/api/v1/check?key=user&identifier=user123&window=minute"
```

Response:
```json
{
  "limited": false,
  "current_count": 1,
  "limit": 100,
  "remaining": 99,
  "reset_after": 60,
  "window": "minute"
}
```

#### Update Whitelist

```bash
curl -X POST http://localhost:8080/api/v1/management/whitelist \
  -H "Content-Type: application/json" \
  -d '{"key": "user", "identifier": "admin123", "whitelisted": true}'
```

#### Set Custom Limit

```bash
curl -X POST http://localhost:8080/api/v1/management/limit \
  -H "Content-Type: application/json" \
  -d '{"key": "api", "identifier": "customer1", "window": "minute", "limit": 200}'
```

### Using the gRPC API

You can use any gRPC client to interact with the service on port 9090.

## Production Deployment

### Kubernetes

Deploy to Kubernetes:

```bash
# Set environment variables
export DOCKER_REGISTRY=your-registry
export VERSION=1.0.0

# Apply Kubernetes manifests
kubectl apply -f deployment/kubernetes/deployment.yaml
```

### Configuration

The service can be configured via:

1. Configuration file (`config.yaml`)
2. Environment variables
3. API calls (for dynamic configuration)

## Architecture

The service follows a clean architecture pattern:

- **Handlers**: HTTP and gRPC interfaces
- **Service**: Core business logic
- **Storage**: Redis-based implementation
- **Config**: Configuration management

## Performance

- Optimized for high throughput (100k+ requests/second)
- Redis pipelining for minimal latency
- Atomic operations to prevent race conditions
- Circuit breaker pattern for resilience

## Monitoring

The service exposes Prometheus metrics at `/metrics` and includes a Grafana dashboard for visualization.

## License

MIT 