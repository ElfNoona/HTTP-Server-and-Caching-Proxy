# Academic Integrity & AI Assistance Disclosure (AI-USE.md)

**Course**: CS-30003  
**Project**: P1 HTTP/1.1 Origin Server and Caching Proxy (Path A)  
**Team**: CSE-34  
**Repository**: [github.com/ElfNoona/HTTP-Server-and-Caching-Proxy](https://github.com/ElfNoona/HTTP-Server-and-Caching-Proxy)

---

## 1. Executive Summary & Integrity Statement

In adherence to the academic integrity policies of CS-30003, this document provides a comprehensive, transparent, and rigorous disclosure of all Artificial Intelligence (AI) assistance utilized throughout the design, implementation, testing, and evaluation phases of this assignment. 

The human engineering team maintained full architectural authority, designed all component interfaces and data structures, and conducted rigorous manual verification across all subsystems. AI assistance was strictly constrained to automated test authoring, evaluation harness generation, syntax rectification, boilerplate generation, and documentation drafting.

---

## 2. Tools & Models Utilized

* **Platform**: Google AntiGravity IDE
* **Underlying Model**: Google Gemini 3.8 Flash (High reasoning effort)
* **Mode of Interaction**: Pair-programming, test-suite synthesis, and synthetic benchmark harness generation.

---

## 3. Disclosed Scope of AI Assistance

The table below delineates the precise division of labor across all repository deliverables:

| Subsystem / Deliverable | Primary Author | AI Contribution & Scope | Human Oversight & Verification |
| :--- | :--- | :--- | :--- |
| **Protocol Engine (`internal/http`)** | Human / AI | Syntax correction, boilerplate generation, accumulator edge-case bug fixing, chunked framing logic refinement. | Designed state-machine framing, sliding-window buffer compaction math, and RFC 9112 limit validations. |
| **Unit Tests (`internal/http/*_test.go`)** | AI | Authoring unit and fuzzing tests for request parsing, chunked encoding/decoding, and buffer accumulation. | Reviewed test assertions, verified edge case coverage (414, 431, 505 errors), and verified pipe closing. |
| **Static Dispatcher (`internal/origin/static.go`)** | Human / AI | Boilerplate lookup table generation and MIME mapping. | Designed CWE-22 path traversal jail prefix checks and canonicalization logic (`filepath.Clean`). |
| **Origin Concurrency (`internal/origin/runtimes/`)** | Human / AI | Worker pool channel dispatching logic and lifecycle boilerplate. | Formulated $M/M/c/K$ queue architecture, graceful shutdown propagation, and keep-alive timeout semantics. |
| **Origin Unit Tests (`internal/origin/*_test.go`)** | AI | Authored unit tests for static dispatching, MIME resolution, worker pool backpressure, and keep-alive loops. | Verified test isolation, socket teardown, and non-blocking concurrency execution. |
| **LRU Cache Engine (`internal/proxy/cache/`)** | Human / AI | Doubly-linked list (`container/list`) operations, hash map bookkeeping, and byte size accounting. | Designed $O(1)$ eviction invariants, mutex granularity (`sync.RWMutex`), and conditional ETag/Last-Modified revalidation semantics. |
| **Cache Unit Tests (`internal/proxy/cache/*_test.go`)** | AI | Authored tests for LRU capacity bounds, thread-safe access, and TTL freshness computation. | Reviewed eviction ordering assertions and verified race-detector passes (`go test -race`). |
| **Proxy Core & Pool (`internal/proxy/`)** | Human / AI | Hop-by-hop header blocklist lookup, request rewriting helpers, and socket pooling mechanics. | Designed upstream keep-alive reuse, socket health checks before acquisition, and bidirectional byte streaming. |
| **Proxy Unit Tests (`internal/proxy/*_test.go`)** | AI | Authored end-to-end integration tests validating header stripping, pool acquisition/release, and cache hit/miss flows. | Validated upstream target isolation and client teardown handling. |
| **System Tests (`tests/`)** | AI | Authored `tests/fragmentation_test.go` (arbitrary TCP chunks over `net.Pipe`) and `tests/proxy_cache_test.go` (full E2E proxy lifecycle). | Inspected assertions, verified zero body on 304, and confirmed clean listener cleanup via `t.Cleanup()`. |
| **Evaluation Suite (`scripts/`)** | AI | Authored `generate_dataset.py`, `bench_exp1.py`, `bench_exp2_zipf.py`, `generate_zipf.py`, and `plot_results.py`. | Defined experimental hypotheses, Zipfian parameters ($\alpha=0.8, N=1000, seed=42$), and validated output CSV schemas. |
| **Build & Docs (`Makefile`, `README.md`)** | AI | Synthesized standardized targets, ASCII architecture diagrams, and quickstart documentation. | Validated all make targets (`all`, `build`, `test`, `bench-exp1`, `bench-exp2`, `plot`, `clean`). |

---

## 4. Human Architectural Oversight & Critical Interventions

Throughout development, human team members actively guided, constrained, and corrected the assistant's output to maintain rigorous systems engineering standards:

1. **Architectural Pivoting (Epoll Syscalls $\to$ Go Netpoller)**:
   * *Initial Plan*: An initial experimental file (`internal/origin/runtimes/epoll_linux.go`) attempted raw Linux syscalls (`epoll_create1`, `epoll_ctl`, `epoll_wait`).
   * *Human Intervention*: The human team realized that raw syscalls bypassed Go's native asynchronous Netpoller and runtime scheduler, introducing fragile cross-platform constraints. The human lead mandated a strict architectural pivot to swappable, idiomatic Goroutine-based concurrency models (`pool.go` vs `goroutine.go`), successfully deprecating the epoll prototype.
2. **Core Codebase Writing & Logic Rectification**:
   * While the core logic of the framing engine and concurrency handlers was primarily handwritten, the AI agent assisted with syntax corrections, boilerplate reduction, and diagnosing logical faults (such as sliding-window slice reslicing and keep-alive socket deadlines).
3. **Port Collisions & Socket Lifecycle Fixes**:
   * During early automated benchmark testing, rapid back-to-back subprocess restarts triggered `listen tcp :8080: bind: address already in use`. The human team identified that tests were colliding on TCP `TIME_WAIT` states and that socket-probing functions were attempting to `bind()` to test ports.
   * The team enforced non-binding TCP connect probes (`socket.create_connection`) and assigned dedicated ports across experiments (8081 for Exp 1, 8082/9092 for Exp 2).
4. **Data Race and Memory Safety Verification**:
   * Every package and integration test was executed through the Go race detector (`go test -race ./...`) under human supervision to ensure that mutex locking in `LRUCache`, channel synchronization in `WorkerPool`, and socket handoffs in `ConnectionPool` were strictly race-free.

---

## 5. Team Member Attestation

We hereby attest that:
* The core systems design, mathematical formulations, and engineering principles are our own work.
* All AI assistance has been fully disclosed above.
* No unauthorized or undeclared third-party HTTP libraries (such as `net/http` or external networking frameworks) were introduced into production packages.



