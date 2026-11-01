# ==============================================================================
# CS-30003: P1 HTTP/1.1 Origin Server and Forward Caching Proxy (Path A)
# Team CSE-34
# ==============================================================================

SHELL := /usr/bin/env bash
GO := go
PYTHON := python3

BIN_DIR := bin
ORIGIN_BIN := $(BIN_DIR)/origin
PROXY_BIN := $(BIN_DIR)/proxy

.PHONY: all build test test-integration dataset bench-exp1 bench-exp2 plot clean fmt vet help

all: build

help:
	@echo "Available build targets:"
	@echo "  make build             - Compile bin/origin and bin/proxy binaries"
	@echo "  make test              - Run all unit and integration tests with -race"
	@echo "  make test-integration  - Run root integration test suite (./tests/...)"
	@echo "  make dataset           - Generate 1,000 uniform 4 KB files in ./public/dataset"
	@echo "  make bench-exp1        - Run Experiment 1 (Concurrency Scaling Benchmark)"
	@echo "  make bench-exp2        - Run Experiment 2 (Zipfian Cache Evaluation)"
	@echo "  make plot              - Render publication-grade Matplotlib PNG figures"
	@echo "  make clean             - Remove compiled binaries and generated results"
	@echo "  make fmt               - Format Go code using go fmt"
	@echo "  make vet               - Analyze Go code using go vet"

build: $(ORIGIN_BIN) $(PROXY_BIN)

$(ORIGIN_BIN): cmd/origin/main.go internal/origin/*.go internal/origin/runtimes/*.go internal/http/*.go
	@mkdir -p $(BIN_DIR)
	@echo "[BUILD] Compiling Origin Server binary -> $(ORIGIN_BIN)..."
	$(GO) build -o $(ORIGIN_BIN) ./cmd/origin

$(PROXY_BIN): cmd/proxy/main.go internal/proxy/*.go internal/proxy/cache/*.go internal/http/*.go
	@mkdir -p $(BIN_DIR)
	@echo "[BUILD] Compiling Caching Proxy binary -> $(PROXY_BIN)..."
	$(GO) build -o $(PROXY_BIN) ./cmd/proxy

test:
	@echo "[TEST] Running all unit and integration tests across the codebase..."
	$(GO) test -v -race ./...

test-integration:
	@echo "[TEST] Running root integration tests (fragmentation and E2E proxy cache)..."
	$(GO) test -v ./tests/...

dataset:
	@echo "[DATASET] Generating 1,000 uniform 4 KB static files..."
	$(PYTHON) scripts/generate_dataset.py

bench-exp1: build dataset
	@echo "[BENCH] Executing Experiment 1: Concurrency Scaling & Tail Latency..."
	./scripts/run_concurrency_bench.sh

bench-exp2: build dataset
	@echo "[BENCH] Executing Experiment 2: Zipfian Cache Hit Ratio & Latency Offload..."
	./scripts/run_cache_bench.sh

plot:
	@echo "[PLOT] Rendering publication-quality benchmark charts..."
	$(PYTHON) scripts/plot_results.py

clean:
	@echo "[CLEAN] Removing compiled binaries and benchmark outputs..."
	rm -rf $(BIN_DIR)
	rm -f results/*.csv results/*.png
	@mkdir -p results
	@echo "[CLEAN] Done."

fmt:
	@echo "[FMT] Formatting Go files..."
	$(GO) fmt ./...

vet:
	@echo "[VET] Running static analysis..."
	$(GO) vet ./...

