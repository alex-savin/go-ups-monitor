# Makefile for UPS Monitoring Service

.PHONY: help build test clean docker-build docker-run docker-stop fmt vet lint deps all

# Default target
help: ## Show this help message
	@echo "Available commands:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-15s %s\n", $$1, $$2}'

build: ## Build the Go application
	go build -o bin/go-ups-monitor ./cmd/go-ups-monitor

test: ## Run Go tests
	go test -v ./...

test-coverage: ## Run tests with coverage
	go test -race -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -html=coverage.out -o coverage.html

clean: ## Clean build artifacts
	rm -rf bin/ coverage.out coverage.html

docker-build: ## Build Docker image locally
	docker build -t go-ups:latest .

docker-run: ## Run Docker container locally
	docker run -p 8080:8080 --name go-ups-container go-ups:latest

docker-stop: ## Stop and remove Docker container
	docker stop go-ups-container || true
	docker rm go-ups-container || true

fmt: ## Format Go code
	go fmt ./...

vet: ## Run go vet
	go vet ./...

lint: ## Run golangci-lint (if installed)
	golangci-lint run

deps: ## Download and tidy Go modules
	go mod download
	go mod tidy

all: clean deps fmt vet test build ## Run full build pipeline