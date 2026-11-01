# High-Performance HTTP/1.1 Origin Server and Forward Caching Proxy

[![Go Version](https://img.shields.io/badge/Go-1.22%2B-blue.svg)](https://golang.org)
[![Course](https://img.shields.io/badge/Course-CS--30003-orange.svg)](#)
[![RFC Compliance](https://img.shields.io/badge/RFC-9110%20%7C%209111%20%7C%209112-success.svg)](https://www.rfc-editor.org/)
[![License](https://img.shields.io/badge/License-MIT-lightgrey.svg)](LICENSE)

A Layer-4 raw-socket HTTP/1.1 origin server and concurrent forward caching proxy implemented strictly from first principles in Go without importing or referencing `net/http`. Features swappable concurrency architectures (Bounded Worker Pool vs. Goroutine-per-Connection), sliding-window TCP stream framing, persistent upstream connection pooling, and an RFC 9111 compliant concurrent in-memory LRU cache engine.

---

## Team Information (Team CSE-34)

| Name | Roll Number | Role | Core Deliverables |
| :--- | :--- | :--- | :--- |
| **Arijit Sanyal** | 24051243 | **Origin Engine Lead** | Protocol framing, sliding-window buffer accumulator, chunked parser, status error mappings |
| **Soumyadeep Saha** | 25057074 | **Systems Lead** | Swappable concurrency runtimes (`pool.go`, `goroutine.go`), sandboxed static dispatcher (CWE-22) |
| **Arhan kumar Hazra** | 24156124 | **Proxy Lead** | Hop-by-hop header sanitizer, upstream persistent connection pool, proxy request rewriter |
| **Kuldeep Kumar** | 24156145 | **Caching & Evaluation Lead** | Thread-safe LRU cache engine ($O(1)$ eviction), RFC conditional validator, evaluation harness |

---

## 1. System Overview & Architectural Invariants

### 1.1 Strict Layer-4 Transport Boundary (Zero `net/http`)
Production packages contain **zero imports of `net/http`**. All network communication operates directly over operating system TCP sockets via `net.TCPListener`, `net.TCPConn`, `net.Conn`, and `net.DialTCP`. All protocol-level constructs—request-line tokenization, case-insensitive header normalization, sliding-window stream compaction, chunked encoding/decoding, and serialization—are written from scratch.

### 1.2 End-to-End System Topology

```
+-------------------+                    +-------------------------+                    +-----------------------+
| Downstream Client | <--- Raw TCP ----> |  Forward Caching Proxy  | <--- Raw TCP ----> |  HTTP/1.1 Origin      |
| (wrk / curl / py) |    Port 9090       |  (In-Memory LRU Cache)  |    Port 8080       |  (Static Dispatcher)  |
+-------------------+                    +-------------------------+                    +-----------------------+
                                                       |                                            |
                                            +---------------------+                      +---------------------+
                                            | Upstream Conn Pool  |                      | Concurrency Runtime |
                                            | (Persistent Sockets)|                      |  - Worker Pool      |
                                            +---------------------+                      |  - Goroutine Engine |
                                                                                         +---------------------+
```

### 1.3 Dual Origin Concurrency Models
1. **Bounded Worker Pool (`-runtime=pool`)**:
   * Fixed worker goroutines ($N = 64$) dequeue accepted connections from a bounded channel ($K = 1000$).
   * Mitigates unbounded thread explosion and OS context-switching overhead under massive connection spikes.
2. **Goroutine-per-Connection (`-runtime=goroutine`)**:
   * Spawns a dedicated lightweight Go routine per accepted TCP connection, delegating I/O multiplexing to Go's runtime Netpoller.
   * Serves as an empirical baseline to analyze Go runtime scheduling behavior under high concurrency.

### 1.4 RFC Compliance & Protocol Invariants
* **RFC 9112 (HTTP/1.1 Protocol Framing)**:
  * Persistent connection semantics with sequential keep-alive pipelining.
  * Idle connection read deadlines (`DefaultKeepAliveTimeout = 5s`) to prevent socket exhaustion.
  * Strict length limit enforcement: URIs $> 2048$ B $\to$ `414 URI Too Long`; Headers $> 8192$ B $\to$ `431 Request Header Fields Too Large`.
  * Absolute-form URIs (`http://host:port/path`) and relative URIs (`/path`) supported across origin and proxy.
* **RFC 9110 (HTTP Semantics & Hop-by-Hop Sanitization)**:
  * Strips hop-by-hop headers (`Connection`, `Keep-Alive`, `Proxy-Authenticate`, `Proxy-Authorization`, `TE`, `Trailers`, `Transfer-Encoding`, `Upgrade`) before proxying upstream.
* **RFC 9111 (HTTP Caching & Conditional Revalidation)**:
  * Concurrent, thread-safe in-memory LRU cache ($O(1)$ operations via hash map + doubly linked list).
  * Strict memory byte tracking prevents heap overflow.
  * Automatic strong ETag generation (`SHA-256`) and `Last-Modified` validation.
  * Conditional revalidation: Requests bearing `If-None-Match` or `If-Modified-Since` are served `304 Not Modified` with zero body bytes on cache hits.

---

## 2. Directory Structure

```
.
├── cmd/
│   ├── origin/main.go            # Origin server CLI entrypoint
│   └── proxy/main.go             # Forward caching proxy CLI entrypoint
├── internal/
│   ├── http/                     # Core Protocol Framing & Stream Parser
│   │   ├── accumulator.go        # Sliding-window stream buffer with reslicing math
│   │   ├── chunked.go            # Streaming chunked transfer encoder/decoder
│   │   ├── request.go            # Request parsing state machine & threshold enforcement
│   │   ├── response.go           # Response serializer & header management
│   │   ├── status.go             # RFC status constants & StatusError mappings
│   │   └── http_test.go          # Protocol framing unit & fuzzing test suite
│   ├── origin/                   # Origin Server Architecture
│   │   ├── runtimes/
│   │   │   ├── pool.go           # Bounded Worker Pool runtime (N=64, Q=1000)
│   │   │   └── goroutine.go      # Goroutine-per-connection runtime
│   │   ├── server.go             # Lifecycle manager & persistent connection handler
│   │   └── static.go             # Sandboxed static file dispatcher (CWE-22 safe)
│   └── proxy/                    # Forward Caching Proxy Architecture
│       ├── cache/
│       │   ├── entry.go          # CacheEntry structure with byte size accounting
│       │   ├── lru.go            # Thread-safe O(1) concurrent LRU cache engine
│       │   └── validator.go      # RFC 9111 Cache-Control & 304 conditional evaluator
│       ├── pool.go               # Upstream persistent TCP connection pool
│       ├── proxy.go              # Proxy engine & downstream client orchestrator
│       └── rewriter.go           # RFC 9110 hop-by-hop header sanitizer & URL parser
├── public/                       # Static file document root
│   ├── dataset/                  # 1,000 uniform 4 KB synthetic catalog files
│   └── index.html                # Default landing document
├── results/                      # Evaluation outputs, CSV tables, and PNG charts
├── scripts/                      # Evaluation & Automation Pipeline
│   ├── bench_exp1.py             # Exp 1: Concurrency scaling & tail latency harness
│   ├── bench_exp2_zipf.py        # Exp 2: Zipfian cache evaluation harness
│   ├── generate_dataset.py       # Catalog generator (1,000 files, exactly 4,096 bytes)
│   ├── generate_zipf.py          # Standalone Zipf trace generator (alpha=0.8)
│   ├── plot_results.py           # Publication-quality Matplotlib visualizer (300 DPI)
│   ├── run_concurrency_bench.sh  # Bash runner for Experiment 1
│   └── run_cache_bench.sh        # Bash runner for Experiment 2
├── tests/                        # Root-level integration test suite
│   ├── fragmentation_test.go     # Stream fragmentation & chunked assembly tests
│   └── proxy_cache_test.go       # End-to-end proxy cache hit/miss/304 tests
├── AI-USE.md                     # Academic integrity disclosure & AI usage documentation
├── Makefile                      # Standard developer build & test orchestration
└── README.md                     # Project documentation
```

---

## 3. Quickstart & Verification Guide

### 3.1 Prerequisites
* **Go**: Version 1.22 or higher
* **Python**: Version 3.8+ (for synthetic workload and evaluation harness)
* **wrk**: Modern HTTP benchmarking tool (`sudo dnf install wrk` or `sudo apt install wrk`)
* **matplotlib**: Python plotting library (`pip install matplotlib`)

### 3.2 Building the Binaries
Compile both standalone binaries (`bin/origin` and `bin/proxy`) via the Makefile:
```bash
make build
```

Verify test suite execution with the Go race detector enabled:
```bash
make test
```

### 3.3 Running the Origin Server
Start the origin server serving static assets from `./public`:
```bash
# Model 1: Bounded Worker Pool (default)
./bin/origin -port=8080 -dir=./public -runtime=pool -workers=64 -queue=1000

# Model 2: Goroutine-per-Connection
./bin/origin -port=8080 -dir=./public -runtime=goroutine
```

### 3.4 Running the Forward Caching Proxy
In a separate terminal, launch the caching proxy pointing to the upstream origin:
```bash
./bin/proxy -port=9090 -upstream=127.0.0.1:8080 -cache-size=10485760
```

---

## 4. Manual Verification via `curl`

Execute the following commands to verify end-to-end functionality:

### 1. Static Asset Retrieval
Fetch an asset directly from the origin server:
```bash
curl -i http://127.0.0.1:8080/index.html
```
*Expected*: `HTTP/1.1 200 OK`, valid `Content-Type`, `Content-Length`, and `Last-Modified` headers.

### 2. Persistent Keep-Alive Verification
Send two sequential requests over a single TCP connection:
```bash
curl -iv http://127.0.0.1:8080/index.html http://127.0.0.1:8080/index.html
```
*Expected*: Both requests reuse the same underlying socket without connection drops.

### 3. Proxy Cache Miss (`X-Cache: MISS`)
Issue an initial request through the caching proxy:
```bash
curl -i http://127.0.0.1:9090/index.html -H "Host: 127.0.0.1:8080"
```
*Expected*: `HTTP/1.1 200 OK`, payload returned, and response header `X-Cache: MISS`.

### 4. Proxy Cache Hit (`X-Cache: HIT`)
Repeat the same request through the proxy:
```bash
curl -i http://127.0.0.1:9090/index.html -H "Host: 127.0.0.1:8080"
```
*Expected*: `HTTP/1.1 200 OK`, response header `X-Cache: HIT`, served entirely from memory without an upstream network round-trip.

### 5. Conditional 304 Revalidation (`If-None-Match`)
Supply the `ETag` extracted from the previous response:
```bash
ETAG=$(curl -sI http://127.0.0.1:9090/index.html -H "Host: 127.0.0.1:8080" | grep -i ETag | awk '{print $2}' | tr -d '\r')
curl -i http://127.0.0.1:9090/index.html -H "Host: 127.0.0.1:8080" -H "If-None-Match: $ETAG"
```
*Expected*: `HTTP/1.1 304 Not Modified` with **zero body bytes**.

---

## 5. Benchmarking & Empirical Evaluation

The evaluation harness implements the experimental blueprint defined in our CS-30003 Project Proposal.

### 5.1 Dataset Generation
Generates 1,000 uniform 4,096-byte (4 KB) static assets in `./public/dataset/`:
```bash
make dataset
```

### 5.2 Experiment 1: Concurrency Scaling & Tail Latency
Evaluates `WorkerPoolServer` ($N=64, Q=1000$) versus `GoroutineServer` across concurrency tiers:
$$C \in [10, 50, 100, 500, 1000, 2000, 5000]$$

Executes 5-second sweeps via `wrk`, recording throughput (req/s), transfer rate (MB/s), and tail latency percentiles ($p50, p90, p95, p99$):
```bash
make bench-exp1
```
*(For rapid pipeline validation, pass `--smoke-test`: `./scripts/run_concurrency_bench.sh --smoke-test`)*

### 5.3 Experiment 2: Cache Capacity vs. Hit Ratio (Zipf Workload)
Evaluates LRU cache effectiveness under a synthetic Zipfian request trace ($N = 1000$, $\alpha = 0.8$, seed = 42, 10,000 requests) across five working set cache capacities:
* **5%**: 204,800 bytes
* **10%**: 409,600 bytes
* **20%**: 819,200 bytes
* **30%**: 1,228,800 bytes
* **50%**: 2,048,000 bytes

Measures Cache Hit Ratio (%), Average Hit Latency, Average Miss Latency, and Latency Reduction (%):
```bash
make bench-exp2
```
*(For rapid pipeline validation, pass `--smoke-test`: `./scripts/run_cache_bench.sh --smoke-test`)*

### 5.4 Visualization Pipeline
Renders 300-DPI publication-grade plots into `./results/`:
```bash
make plot
```
*Generated Figures*:
1. `results/exp1_throughput_vs_concurrency.png`: Throughput comparison curves on logarithmic X-scale.
2. `results/exp1_tail_latency_cdf.png`: Percentile latency scaling comparison ($p50, p90, p95, p99$).
3. `results/exp2_cache_hit_ratio.png`: Dual-axis plot of Working Set Capacity vs. Hit Ratio (%) and Latency Reduction (%).

---

## 6. Development & Build Automation Commands

| Command | Description |
| :--- | :--- |
| `make all` | Default target; builds both `bin/origin` and `bin/proxy` |
| `make build` | Compiles binaries to `./bin/` |
| `make test` | Executes all unit and integration tests with the race detector (`-race`) |
| `make test-integration` | Executes root integration tests (`tests/fragmentation_test.go`, `tests/proxy_cache_test.go`) |
| `make dataset` | Populates `./public/dataset/` with 1,000 uniform 4 KB static files |
| `make bench-exp1` | Executes Experiment 1 benchmark sweep via `wrk` |
| `make bench-exp2` | Executes Experiment 2 Zipfian cache evaluation |
| `make plot` | Renders high-resolution PNG charts from benchmark CSV results |
| `make clean` | Removes compiled binaries and benchmark outputs |
| `make fmt` | Formats Go source files via `go fmt` |
| `make vet` | Runs Go static code analysis via `go vet` |

---

## 7. License & Acknowledgements
This project is developed as part of **CS-30003** at the Department of Computer Science and Engineering. Licensed under the MIT License.

