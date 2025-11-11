.PHONY: all build test bench clean install lint fmt vet coverage help

# Binary name
BINARY_NAME=magpie
BINARY_PATH=./bin/$(BINARY_NAME)

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod
GOFMT=$(GOCMD) fmt
GOVET=$(GOCMD) vet

# Build flags
LDFLAGS=-ldflags "-s -w"

all: fmt vet test build ## Run all checks and build

build: ## Build the CLI binary
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p bin
	$(GOBUILD) $(LDFLAGS) -o $(BINARY_PATH) ./cmd/magpie
	@echo "✓ Built $(BINARY_PATH)"

test: ## Run tests
	@echo "Running tests..."
	$(GOTEST) -v -race -timeout 30s ./...

test-short: ## Run short tests only
	$(GOTEST) -v -short -race ./...

test-coverage: ## Run tests with coverage
	@echo "Running tests with coverage..."
	$(GOTEST) -v -race -coverprofile=coverage.out -covermode=atomic ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "✓ Coverage report: coverage.html"

bench: ## Run benchmarks
	@echo "Running benchmarks..."
	$(GOTEST) -bench=. -benchmem -run=^$$ ./...

bench-compare: ## Run benchmarks with comparison
	@echo "Running benchmarks with comparison..."
	$(GOTEST) -bench=. -benchmem -run=^$$ ./... | tee bench-new.txt

clean: ## Clean build artifacts
	@echo "Cleaning..."
	@rm -rf bin/
	@rm -f coverage.out coverage.html
	@rm -f *.magpie *.magpie.wal
	@$(GOCMD) clean
	@echo "✓ Cleaned"

install: ## Install the CLI binary
	@echo "Installing $(BINARY_NAME)..."
	$(GOCMD) install ./cmd/magpie
	@echo "✓ Installed to $(shell go env GOPATH)/bin/$(BINARY_NAME)"

lint: ## Run linter (requires golangci-lint)
	@which golangci-lint > /dev/null || (echo "golangci-lint not installed. Run: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest" && exit 1)
	golangci-lint run ./...

fmt: ## Format code
	@echo "Formatting code..."
	@$(GOFMT) ./...

vet: ## Run go vet
	@echo "Running go vet..."
	@$(GOVET) ./...

tidy: ## Tidy dependencies
	$(GOMOD) tidy
	$(GOMOD) verify

deps: ## Download dependencies
	$(GOMOD) download

upgrade: ## Upgrade dependencies
	$(GOGET) -u ./...
	$(GOMOD) tidy

run-examples: ## Run all examples
	@echo "Running basic example..."
	@cd examples/basic && $(GOCMD) run main.go
	@echo "\nRunning batch example..."
	@cd examples/batch && $(GOCMD) run main.go
	@echo "\nRunning metadata example..."
	@cd examples/metadata && $(GOCMD) run main.go

# Development helpers
dev: ## Run in development mode with auto-reload (requires air)
	@which air > /dev/null || (echo "air not installed. Run: go install github.com/air-verse/air@latest" && exit 1)
	air

watch-test: ## Watch and run tests on file changes (requires watchexec)
	@which watchexec > /dev/null || (echo "watchexec not installed. Visit: https://github.com/watchexec/watchexec" && exit 1)
	watchexec -e go -r -- make test

# Release helpers
release-dry-run: ## Test release process
	goreleaser release --snapshot --skip=publish --clean

release: ## Create a new release (requires goreleaser)
	@which goreleaser > /dev/null || (echo "goreleaser not installed. Visit: https://goreleaser.com/install" && exit 1)
	goreleaser release --clean

help: ## Show this help
	@echo "MagpieDB Makefile Commands:"
	@echo ""
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'
	@echo ""

# Default target
.DEFAULT_GOAL := help
