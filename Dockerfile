# Multi-stage build

# Stage 1: Test and build
FROM golang:1.25.6-alpine3.21 AS builder

# Install build dependencies
RUN apk add --no-cache gcc musl-dev sqlite-dev && \
    apk update && apk upgrade

WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum* ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -o kvstore ./app && apk update && apk upgrade

# Stage 2: Runtime
FROM alpine:3.21

# Install runtime dependencies (wget for health checks)
RUN apk add --no-cache ca-certificates sqlite-libs wget

WORKDIR /root/

# Copy binary from builder
COPY --from=builder /app/kvstore .

# Create a non-root user for security
RUN addgroup -g 1000 kvstore && \
    adduser -D -u 1000 -G kvstore kvstore && \
    chown kvstore:kvstore /root/kvstore

USER kvstore

# Expose internal ports (docker-compose handles host port mapping)
# - 8001: RPC server
# - 9001: HTTP API
# - 6060: pprof debugging
EXPOSE 8001 9001 6060

ENTRYPOINT ["./kvstore"]
# Default command - override via docker-compose
CMD ["node", "-id", "1", "-config", "cluster.docker.json"]

