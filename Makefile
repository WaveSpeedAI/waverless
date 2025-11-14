.PHONY: build run test clean deps

build:
	go build -o waverless ./cmd

# Run service (new architecture)
run:
	go run ./cmd

# Run tests
test:
	go test -v ./...

# Clean build files
clean:
	rm -f waverless
	rm -rf output/

# Install dependencies
deps:
	go mod download
	go mod tidy

# Format code
fmt:
	go fmt ./...

# Code linting
lint:
	golangci-lint run

# Local development (using local configuration, new architecture)
dev:
	CONFIG_PATH=config/local.yaml go run ./cmd

# Docker build
docker-build:
	docker build -t waverless:latest .

# Docker run
docker-run:
	docker run -p 8080:8080 \
		-e REDIS_ADDR=host.docker.internal:6379 \
		waverless:latest
