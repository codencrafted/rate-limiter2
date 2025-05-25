.PHONY: build test clean proto docker run load-test

# Build the application
build:
	go build -o bin/ratelimiter ./cmd/server

# Run tests
test:
	go test -v ./...

# Clean build artifacts
clean:
	rm -rf bin/

# Generate protobuf code
proto:
	protoc --go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		api/proto/ratelimiter.proto

# Build Docker image
docker:
	docker build -t ratelimiter:latest -f docker/Dockerfile .

# Run with Docker Compose
run:
	docker-compose up

# Run load test
load-test:
	go run scripts/load_test.go --concurrency=10 --requests=1000 --rps=100

# Show help
help:
	@echo "Available targets:"
	@echo "  build      - Build the application"
	@echo "  test       - Run tests"
	@echo "  clean      - Clean build artifacts"
	@echo "  proto      - Generate protobuf code"
	@echo "  docker     - Build Docker image"
	@echo "  run        - Run with Docker Compose"
	@echo "  load-test  - Run load test"
	@echo "  help       - Show this help" 