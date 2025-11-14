FROM --platform=linux/amd64 golang:1.24-alpine AS builder

WORKDIR /app

# Install dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o waverless ./cmd

# Runtime stage
FROM --platform=linux/amd64 alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy binary from builder stage
COPY --from=builder /app/waverless .

# Copy configuration files (specs.yaml and templates)
COPY --from=builder /app/config ./config

# Expose port
EXPOSE 8080

# Run
CMD ["./waverless"]
