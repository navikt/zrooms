# Makefile for zrooms application

# Variables
BINARY_NAME=zrooms
BINARY_PATH=bin/$(BINARY_NAME)
GO=go
PKG_LIST=$(shell $(GO) list ./... | grep -v /vendor/)
GOFLAGS=-ldflags="-s -w"

.PHONY: all build clean test lint vet fmt run help

all: clean lint test build

# Build the application
zrooms:
	$(GO) build -o $(BINARY_PATH) cmd/zrooms/*.go

# Build with additional flags
build: 
	$(GO) build $(GOFLAGS) -o $(BINARY_PATH) cmd/zrooms/*.go

# Clean build artifacts
clean:
	$(GO) clean
	rm -f $(BINARY_PATH)
	rm -rf ./dist

# Run tests
test:
	$(GO) test -v $(PKG_LIST)

# Run test with coverage
test-coverage:
	$(GO) test -v -cover $(PKG_LIST)
	$(GO) test -coverprofile=coverage.out $(PKG_LIST)
	$(GO) tool cover -html=coverage.out -o coverage.html

# Lint code
lint:
	golint -set_exit_status $(PKG_LIST)

# Run go vet
vet:
	$(GO) vet $(PKG_LIST)

# Format code
fmt:
	$(GO) fmt $(PKG_LIST)

# Run application
run:
	$(GO) run cmd/zrooms/main.go

# Docker build
docker-build:
	docker build -t zrooms:latest .

# Docker run
docker-run:
	docker run -p 8080:8080 zrooms:latest

# Show help
help:
	@echo "make - Build the application"
	@echo "make build - Build with optimization flags"
	@echo "make clean - Clean build artifacts"
	@echo "make test - Run tests"
	@echo "make test-coverage - Run tests with coverage"
	@echo "make lint - Lint the code"
	@echo "make vet - Run go vet"
	@echo "make fmt - Format the code"
	@echo "make run - Run the application"
	@echo "make docker-build - Build Docker image"
	@echo "make docker-run - Run Docker container"

