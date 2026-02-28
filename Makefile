.PHONY: help build test lint clean docker-build docker-up docker-down sonar

help:
	@echo "Available targets:"
	@echo "  make build           - Build the application"
	@echo "  make test            - Run tests"
	@echo "  make test-coverage   - Run tests with coverage report"
	@echo "  make lint            - Run linters"
	@echo "  make clean           - Clean build artifacts"
	@echo "  make docker-build    - Build Docker image"
	@echo "  make docker-up       - Start containers with docker-compose"
	@echo "  make docker-down     - Stop containers"
	@echo "  make docker-logs     - View Docker logs"
	@echo "  make sonar           - Run SonarQube analysis"
	@echo "  make run             - Run the application locally"

build:
	CGO_ENABLED=1 go build -o kvstore ./app

test:
	go test -v -race ./...

test-coverage:
	go test -v -race -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

lint:
	@which golangci-lint > /dev/null || (echo "Installing golangci-lint..." && go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
	golangci-lint run ./... --timeout=5m

clean:
	go clean
	rm -f kvstore coverage.out coverage.html

docker-build:
	docker build -t kvstore:latest .

docker-up:
	docker-compose up -d

docker-down:
	docker-compose down

docker-logs:
	docker-compose logs -f

docker-clean:
	docker-compose down -v

run: build
	./kvstore node -id 1 -config cluster.json

all: clean lint test build
