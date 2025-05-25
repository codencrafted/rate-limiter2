# Distributed Rate Limiter

A high-performance, production-ready distributed rate limiter service built in Go, designed to handle 100k+ RPS with sub-millisecond latency.

## Features

- **High Performance**: Optimized for 100k+ RPS with sub-millisecond latency
- **Multi-Level Caching**: Local memory cache + Redis for distributed state
- **Security**: API key authentication with role-based access control
- **Observability**: Structured logging, Prometheus metrics, OpenTelemetry tracing
- **Reliability**: Circuit breaker with multiple fallback strategies
- **Kubernetes-Ready**: Production manifests with autoscaling, network policies, and more

## Quick Start

### Prerequisites

- Go 1.18+
- Redis
- Docker (optional)

### Installation

1. Clone the repository:

```bash
git clone https://github.com/distributed/ratelimiter.git
cd rate-limiter
```

2. Build the service:

```bash
go build -o ratelimiter cmd/server/main.go
```

3. Start Redis (if you don't have an instance running):

```bash
docker run -d -p 6379:6379 --name redis redis:latest
```

4. Run the service:

```bash
./ratelimiter
```

The service will start on port 8080 by default.

### Basic Usage

#### Check Rate Limit

```bash
curl -H "X-API-Key: admin-key" \
  "http://localhost:8080/v1/ratelimit/check?identifier=user123&window=minute"
```

#### Add to Whitelist

```bash
curl -X PUT -H "X-API-Key: admin-key" -H "Content-Type: application/json" \
  -d '{"key": "api", "identifier": "premium-user", "whitelisted": true}' \
  "http://localhost:8080/v1/ratelimit/whitelist"
```

#### Batch Check

```bash
curl -X POST -H "X-API-Key: admin-key" -H "Content-Type: application/json" \
  -d '{"identifiers": ["user1", "user2"], "window": "minute"}' \
  "http://localhost:8080/v1/ratelimit/batch"
```

### Configuration

Configuration can be provided via:

- YAML file (`config.yaml` in the working directory or `/etc/ratelimiter/`)
- Environment variables (prefixed with `RATELIMITER_`)

Example environment variables:

```bash
export RATELIMITER_REDIS_ADDRESSES=localhost:6379
export RATELIMITER_HTTP_ADDRESS=:8080
export RATELIMITER_API_KEYS=admin-key:admin,read-key:readonly
```

### Kubernetes Deployment

Deploy to Kubernetes using the provided manifests:

```bash
kubectl apply -f deployment/kubernetes/
```

## Monitoring

- **Health Checks**: `/health/live` and `/health/ready`
- **Metrics**: Prometheus metrics available at `/metrics`
- **Logs**: Structured JSON logs with correlation IDs

## Documentation

For complete documentation, see the `/docs` directory.

## License

MIT 