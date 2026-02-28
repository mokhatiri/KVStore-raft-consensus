# Multi-stage build
# Stage 1: Test and build
FROM golang:1.25.5-alpine AS builder

# Install build dependencies
RUN apk add --no-cache gcc musl-dev sqlite-dev

WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum* ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Run tests with coverage
RUN go test -v -coverprofile=coverage.out ./...

# Build the application
RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -o kvstore ./app

# Stage 2: Runtime
FROM alpine:latest

# Install runtime dependencies
RUN apk add --no-cache ca-certificates sqlite-libs

WORKDIR /root/

# Copy binary from builder
COPY --from=builder /app/kvstore .
COPY --from=builder /app/cluster.json .

# Create a non-root user for security
RUN addgroup -g 1000 kvstore && \
    adduser -D -u 1000 -G kvstore kvstore

USER kvstore

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD test -f /proc/self/cgroup || exit 1

EXPOSE 8080 8090 50051

ENTRYPOINT ["./kvstore"]
CMD ["node", "-id", "1", "-config", "cluster.json"]
