#!/usr/bin/env python3
"""
CS-30003 Phase 4A: Experiment 2 Harness
Zipfian Cache Hit Ratio & Latency Offload Evaluation:
Evaluates LRU forward caching proxy performance across varying cache capacities.
"""

import argparse
import csv
import http.client
import math
import os
import random
import signal
import socket
import subprocess
import sys
import time
from typing import Dict, List, Tuple


WORKING_SET_BYTES = 4_096_000  # 1,000 files * 4,096 bytes

ALL_CAPACITIES: List[Tuple[int, int]] = [
    (5, 204_800),
    (10, 409_600),
    (20, 819_200),
    (30, 1_228_800),
    (50, 2_048_000),
]


def generate_zipf_workload(num_requests: int = 10000, num_files: int = 1000, alpha: float = 0.8, seed: int = 42) -> List[str]:
    """Generates a reproducible sequence of file requests following a Zipfian distribution."""
    random.seed(seed)
    population = [f"file_{k:04d}.txt" for k in range(1, num_files + 1)]
    weights = [1.0 / (k ** alpha) for k in range(1, num_files + 1)]
    return random.choices(population, weights=weights, k=num_requests)


def wait_for_server(port: int, host: str = "127.0.0.1", timeout: float = 5.0) -> bool:
    """Verifies server readiness using non-binding TCP connect probe."""
    deadline = time.time() + timeout
    while time.time() < deadline:
        try:
            with socket.create_connection((host, port), timeout=0.1):
                return True
        except (ConnectionRefusedError, socket.timeout, OSError):
            time.sleep(0.05)
    return False


def wait_for_port_closed(port: int, host: str = "127.0.0.1", timeout: float = 3.0) -> None:
    """Waits until port is no longer accepting connections."""
    deadline = time.time() + timeout
    while time.time() < deadline:
        try:
            with socket.create_connection((host, port), timeout=0.1):
                time.sleep(0.05)
        except (ConnectionRefusedError, socket.timeout, OSError):
            return


def stop_process(proc: subprocess.Popen, timeout: float = 3.0) -> None:
    """Gracefully terminates a subprocess using SIGINT with SIGKILL fallback."""
    if proc.poll() is None:
        try:
            proc.send_signal(signal.SIGINT)
            proc.wait(timeout=timeout)
        except (subprocess.TimeoutExpired, OSError):
            proc.kill()
            try:
                proc.wait(timeout=2.0)
            except (subprocess.TimeoutExpired, OSError):
                pass


def run_benchmark_run(
    proxy_port: int,
    origin_port: int,
    request_sequence: List[str],
) -> Dict[str, float]:
    """Sends requests sequentially over persistent HTTP/1.1 connections and records latencies and cache status."""
    hits = 0
    misses = 0
    hit_latencies_ms: List[float] = []
    miss_latencies_ms: List[float] = []

    conn = http.client.HTTPConnection("127.0.0.1", proxy_port, timeout=5.0)

    for i, filename in enumerate(request_sequence):
        req_path = f"http://127.0.0.1:{origin_port}/dataset/{filename}"
        headers = {
            "Host": f"127.0.0.1:{origin_port}",
            "Connection": "keep-alive",
        }

        # Attempt request with automatic keep-alive reconnection on transient drops
        max_retries = 3
        success = False
        t_ms = 0.0
        x_cache = "MISS"

        for _ in range(max_retries):
            try:
                t0 = time.perf_counter()
                conn.request("GET", req_path, headers=headers)
                resp = conn.getresponse()
                _ = resp.read()  # Drain body to allow connection reuse
                t1 = time.perf_counter()
                t_ms = (t1 - t0) * 1000.0

                cache_hdr = resp.getheader("X-Cache") or resp.getheader("x-cache") or ""
                x_cache = "HIT" if "HIT" in cache_hdr.upper() else "MISS"
                success = True
                break
            except (http.client.RemoteDisconnected, http.client.CannotSendRequest, OSError):
                try:
                    conn.close()
                except Exception:
                    pass
                conn = http.client.HTTPConnection("127.0.0.1", proxy_port, timeout=5.0)

        if not success:
            continue

        if x_cache == "HIT":
            hits += 1
            hit_latencies_ms.append(t_ms)
        else:
            misses += 1
            miss_latencies_ms.append(t_ms)

    try:
        conn.close()
    except Exception:
        pass

    total = hits + misses
    hit_ratio_pct = (hits / total * 100.0) if total > 0 else 0.0
    avg_hit_ms = (sum(hit_latencies_ms) / len(hit_latencies_ms)) if hit_latencies_ms else 0.0
    avg_miss_ms = (sum(miss_latencies_ms) / len(miss_latencies_ms)) if miss_latencies_ms else 0.0

    latency_reduction_pct = 0.0
    if avg_miss_ms > 0 and avg_hit_ms > 0:
        latency_reduction_pct = max(0.0, ((avg_miss_ms - avg_hit_ms) / avg_miss_ms) * 100.0)

    return {
        "total_requests": total,
        "hits": hits,
        "misses": misses,
        "hit_ratio_pct": hit_ratio_pct,
        "avg_hit_ms": avg_hit_ms,
        "avg_miss_ms": avg_miss_ms,
        "latency_reduction_pct": latency_reduction_pct,
    }


def main():
    parser = argparse.ArgumentParser(description="Experiment 2: Zipfian Cache Evaluation Harness.")
    parser.add_argument("--smoke-test", action="store_true", help="Runs 100 requests on 1 cache size to verify pipeline")
    parser.add_argument("--origin-bin", default="./bin/origin", help="Path to origin binary (default: ./bin/origin)")
    parser.add_argument("--proxy-bin", default="./bin/proxy", help="Path to proxy binary (default: ./bin/proxy)")
    parser.add_argument("--origin-port", type=int, default=8082, help="Origin server port (default: 8082)")
    parser.add_argument("--proxy-port", type=int, default=9092, help="Proxy server port (default: 9092)")
    parser.add_argument("--dir", default="./public", help="Static assets public directory (default: ./public)")
    parser.add_argument("--output-csv", default="./results/exp2_results.csv", help="Results CSV path (default: ./results/exp2_results.csv)")
    parser.add_argument("--alpha", type=float, default=0.8, help="Zipfian skew factor alpha (default: 0.8)")
    parser.add_argument("--seed", type=int, default=42, help="Random seed for reproducible trace (default: 42)")

    args = parser.parse_args()

    if not os.path.isfile(args.origin_bin):
        print(f"Error: Origin binary not found at '{args.origin_bin}'", file=sys.stderr)
        sys.exit(1)
    if not os.path.isfile(args.proxy_bin):
        print(f"Error: Proxy binary not found at '{args.proxy_bin}'", file=sys.stderr)
        sys.exit(1)

    if args.smoke_test:
        capacities = [(10, 409_600)]
        num_requests = 100
    else:
        capacities = ALL_CAPACITIES
        num_requests = 10000

    os.makedirs(os.path.dirname(os.path.abspath(args.output_csv)), exist_ok=True)

    print("======================================================================")
    print("  CS-30003: Experiment 2 Zipfian Cache Evaluation")
    print(f"  Origin Port:       {args.origin_port}")
    print(f"  Proxy Port:        {args.proxy_port}")
    print(f"  Zipf Parameters:   N=1000 files, alpha={args.alpha}, seed={args.seed}")
    print(f"  Requests per Run:  {num_requests:,}")
    print(f"  Capacities:        {[f'{pct}% ({bytes_:,} B)' for pct, bytes_ in capacities]}")
    print(f"  Smoke Test Mode:   {args.smoke_test}")
    print(f"  Results CSV:       {args.output_csv}")
    print("======================================================================")

    print("[TRACE] Precomputing Zipfian workload trace...")
    workload = generate_zipf_workload(num_requests=num_requests, num_files=1000, alpha=args.alpha, seed=args.seed)
    print(f"[TRACE] Generated {len(workload)} requests.")

    csv_file = open(args.output_csv, "w", newline="", encoding="utf-8")
    writer = csv.writer(csv_file)
    writer.writerow([
        "cache_capacity_pct",
        "cache_bytes",
        "total_requests",
        "hits",
        "misses",
        "hit_ratio_pct",
        "avg_hit_ms",
        "avg_miss_ms",
        "latency_reduction_pct",
    ])
    csv_file.flush()

    try:
        for cap_pct, cap_bytes in capacities:
            print(f"\n>>> Testing Cache Capacity: {cap_pct}% ({cap_bytes:,} bytes)")

            # 1. Spawn Origin Server
            origin_cmd = [args.origin_bin, f"-port={args.origin_port}", f"-dir={args.dir}"]
            print(f"  [ORIGIN] Spawning on port {args.origin_port}...")
            p_origin = subprocess.Popen(origin_cmd, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

            if not wait_for_server(args.origin_port, timeout=5.0):
                stop_process(p_origin)
                raise RuntimeError(f"Origin server failed to listen on port {args.origin_port}")

            # 2. Spawn Forward Caching Proxy
            proxy_cmd = [
                args.proxy_bin,
                f"-port={args.proxy_port}",
                f"-upstream=127.0.0.1:{args.origin_port}",
                f"-cache-size={cap_bytes}",
            ]
            print(f"  [PROXY]  Spawning on port {args.proxy_port} (upstream 127.0.0.1:{args.origin_port})...")
            p_proxy = subprocess.Popen(proxy_cmd, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

            if not wait_for_server(args.proxy_port, timeout=5.0):
                stop_process(p_proxy)
                stop_process(p_origin)
                raise RuntimeError(f"Proxy server failed to listen on port {args.proxy_port}")

            # 3. Execute Workload
            print(f"  -> Dispatching {len(workload):,} requests...")
            t_start = time.time()
            metrics = run_benchmark_run(args.proxy_port, args.origin_port, workload)
            elapsed = time.time() - t_start

            # 4. Display Results
            tot = metrics["total_requests"]
            hits = metrics["hits"]
            misses = metrics["misses"]
            hr = metrics["hit_ratio_pct"]
            hit_lat = metrics["avg_hit_ms"]
            miss_lat = metrics["avg_miss_ms"]
            red = metrics["latency_reduction_pct"]

            print(f"     [RESULT] Requests: {tot} | Hits: {hits} | Misses: {misses} | Hit Ratio: {hr:.2f}%")
            print(f"     [LATENCY] Avg Hit: {hit_lat:.3f}ms | Avg Miss: {miss_lat:.3f}ms | Latency Reduction: {red:.2f}% | Elapsed: {elapsed:.2f}s")

            writer.writerow([
                cap_pct,
                cap_bytes,
                tot,
                hits,
                misses,
                f"{hr:.2f}",
                f"{hit_lat:.3f}",
                f"{miss_lat:.3f}",
                f"{red:.2f}",
            ])
            csv_file.flush()

            # 5. Clean Teardown
            print("  [TEARDOWN] Stopping Proxy and Origin...")
            stop_process(p_proxy)
            stop_process(p_origin)
            wait_for_port_closed(args.proxy_port, timeout=3.0)
            wait_for_port_closed(args.origin_port, timeout=3.0)
            time.sleep(1.5)  # Clean TCP state table cooldown

    finally:
        csv_file.close()

    print("\n======================================================================")
    print(f"[EXPERIMENT 2 COMPLETE] Results saved to: {os.path.abspath(args.output_csv)}")
    print("======================================================================")


if __name__ == "__main__":
    main()