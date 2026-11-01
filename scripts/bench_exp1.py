#!/usr/bin/env python3
"""
CS-30003 Phase 4A: Experiment 1 Harness
Concurrency Scaling & Tail Latency Evaluation:
Compares Bounded Worker Pool vs Goroutine-per-Connection runtime models.
"""

import argparse
import csv
import os
import re
import shutil
import signal
import socket
import subprocess
import sys
import time
from typing import Dict, List, Optional, Tuple


def check_prerequisites(origin_bin: str) -> None:
    """Verifies that the origin binary and wrk tool exist."""
    if not shutil.which("wrk"):
        print("Error: wrk is required. Install via 'sudo dnf install wrk'", file=sys.stderr)
        sys.exit(1)

    if not os.path.isfile(origin_bin):
        print(f"Error: Origin binary not found at '{origin_bin}'. Ensure it is compiled in ./bin/origin.", file=sys.stderr)
        sys.exit(1)


def wait_for_server(port: int, host: str = "127.0.0.1", timeout: float = 3.0) -> bool:
    """Polls host:port using a non-binding TCP handshake until reachable."""
    deadline = time.time() + timeout
    while time.time() < deadline:
        try:
            with socket.create_connection((host, port), timeout=0.1):
                return True
        except (ConnectionRefusedError, socket.timeout, OSError):
            time.sleep(0.05)
    return False


def wait_for_port_closed(port: int, host: str = "127.0.0.1", timeout: float = 3.0) -> None:
    """Waits until host:port rejects connections."""
    deadline = time.time() + timeout
    while time.time() < deadline:
        try:
            with socket.create_connection((host, port), timeout=0.1):
                time.sleep(0.05)
        except (ConnectionRefusedError, socket.timeout, OSError):
            return


def stop_server(proc: subprocess.Popen, timeout: float = 3.0) -> None:
    """Cleanly terminates the server process with SIGINT, falling back to SIGKILL."""
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


def parse_latency_to_ms(val: float, unit: str) -> float:
    """Converts a wrk latency string unit to milliseconds."""
    u = unit.lower().strip()
    if u == "us":
        return val / 1000.0
    elif u == "ms":
        return val
    elif u == "s":
        return val * 1000.0
    elif u == "m":
        return val * 60000.0
    return val


def parse_wrk_output(output: str) -> Dict[str, float]:
    """Parses standard wrk stdout with --latency to extract key metrics."""
    # 1. Requests/sec
    rps = 0.0
    rps_match = re.search(r"Requests/sec:\s+([\d\.]+)([kKmM]?)", output)
    if rps_match:
        val = float(rps_match.group(1))
        unit = rps_match.group(2).lower()
        if unit == "k":
            val *= 1_000.0
        elif unit == "m":
            val *= 1_000_000.0
        rps = val

    # 2. Transfer/sec
    transfer_mb_sec = 0.0
    transfer_match = re.search(r"Transfer/sec:\s+([\d\.]+)\s*([a-zA-Z]+)", output)
    if transfer_match:
        val = float(transfer_match.group(1))
        unit = transfer_match.group(2).upper()
        if "GB" in unit:
            transfer_mb_sec = val * 1024.0
        elif "MB" in unit:
            transfer_mb_sec = val
        elif "KB" in unit:
            transfer_mb_sec = val / 1024.0
        elif "B" in unit:
            transfer_mb_sec = val / (1024.0 * 1024.0)

    # 3. Latency Distribution
    def extract_pct(pct_str: str) -> Optional[float]:
        m = re.search(rf"\b{pct_str}%\s+([\d\.]+)\s*([a-zA-Z]+)", output)
        if m:
            return parse_latency_to_ms(float(m.group(1)), m.group(2))
        return None

    p50 = extract_pct("50") or 0.0
    p90 = extract_pct("90") or 0.0
    p95 = extract_pct("95")
    p99 = extract_pct("99") or 0.0

    # Interpolate p95 if omitted in standard 4-point table (50%, 75%, 90%, 99%)
    if p95 is None:
        if p90 > 0 and p99 >= p90:
            p95 = p90 + (p99 - p90) * 0.55
        else:
            p95 = p99

    return {
        "requests_per_sec": rps,
        "transfer_mb_sec": transfer_mb_sec,
        "p50_ms": p50,
        "p90_ms": p90,
        "p95_ms": p95,
        "p99_ms": p99,
    }


def run_wrk_benchmark(target_url: str, concurrency: int, duration_s: int) -> Dict[str, float]:
    """Executes a wrk benchmark run and returns the parsed metrics."""
    threads = min(4, concurrency)
    cmd = [
        "wrk",
        f"-t{threads}",
        f"-c{concurrency}",
        f"-d{duration_s}s",
        "--latency",
        target_url,
    ]
    res = subprocess.run(cmd, capture_output=True, text=True)
    if res.returncode != 0:
        raise RuntimeError(f"wrk command failed (exit code {res.returncode}):\n{res.stderr}")
    return parse_wrk_output(res.stdout)


def main():
    parser = argparse.ArgumentParser(description="Experiment 1: Concurrency Scaling & Tail Latency Benchmark.")
    parser.add_argument("--smoke-test", action="store_true", help="Runs single 1-second pass at C=10 to quickly verify pipeline")
    parser.add_argument("--origin-bin", default="./bin/origin", help="Path to origin server binary (default: ./bin/origin)")
    parser.add_argument("--port", type=int, default=8081, help="Port to bind origin server (default: 8081)")
    parser.add_argument("--dir", default="./public", help="Static assets public directory (default: ./public)")
    parser.add_argument("--output-csv", default="./results/exp1_results.csv", help="Path for results CSV (default: ./results/exp1_results.csv)")
    parser.add_argument("--target-file", default="/dataset/file_0001.txt", help="Asset path to benchmark (default: /dataset/file_0001.txt)")

    args = parser.parse_args()

    check_prerequisites(args.origin_bin)

    # Configure run parameters
    if args.smoke_test:
        concurrencies = [10]
        duration_s = 1
        runtimes: List[Tuple[str, List[str]]] = [
            ("WorkerPoolServer", ["-runtime=pool", "-workers=64", "-queue=1000"]),
        ]
    else:
        concurrencies = [10, 50, 100, 500, 1000, 2000, 5000]
        duration_s = 5
        runtimes = [
            ("WorkerPoolServer", ["-runtime=pool", "-workers=64", "-queue=1000"]),
            ("GoroutineServer", ["-runtime=goroutine"]),
        ]

    # Ensure results directory exists
    os.makedirs(os.path.dirname(os.path.abspath(args.output_csv)), exist_ok=True)
    target_url = f"http://127.0.0.1:{args.port}{args.target_file}"

    print("======================================================================")
    print("  CS-30003: Experiment 1 Concurrency Scaling Benchmark")
    print(f"  Target URL:        {target_url}")
    print(f"  Duration:          {duration_s}s per test")
    print(f"  Concurrency List:  {concurrencies}")
    print(f"  Smoke Test Mode:   {args.smoke_test}")
    print(f"  Results CSV:       {args.output_csv}")
    print("======================================================================")

    csv_file = open(args.output_csv, "w", newline="", encoding="utf-8")
    writer = csv.writer(csv_file)
    writer.writerow([
        "runtime",
        "concurrency",
        "requests_per_sec",
        "transfer_mb_sec",
        "p50_ms",
        "p90_ms",
        "p95_ms",
        "p99_ms",
    ])
    csv_file.flush()

    try:
        for runtime_name, runtime_flags in runtimes:
            print(f"\n>>> Starting Benchmark Suite for Model: [{runtime_name}]")
            cmd = [args.origin_bin, f"-port={args.port}", f"-dir={args.dir}"] + runtime_flags
            print(f"[ORIGIN] Spawning: {' '.join(cmd)}")
            server_proc = subprocess.Popen(cmd, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

            if not wait_for_server(args.port, timeout=3.0):
                server_proc.kill()
                server_proc.wait()
                raise RuntimeError(f"Origin server failed to accept connections on port {args.port} within 3s")

            print(f"[ORIGIN] Server listening and verified on port {args.port}.")

            for c in concurrencies:
                print(f"  -> Concurrency C={c}: Running {duration_s}s benchmark...")
                try:
                    metrics = run_wrk_benchmark(target_url, c, duration_s)
                    rps = metrics["requests_per_sec"]
                    t_mb = metrics["transfer_mb_sec"]
                    p50 = metrics["p50_ms"]
                    p90 = metrics["p90_ms"]
                    p95 = metrics["p95_ms"]
                    p99 = metrics["p99_ms"]

                    print(f"     [RESULT] RPS: {rps:9.2f} req/s | Bandwidth: {t_mb:6.2f} MB/s | p50: {p50:6.3f}ms | p90: {p90:6.3f}ms | p99: {p99:6.3f}ms")
                    writer.writerow([runtime_name, c, f"{rps:.2f}", f"{t_mb:.2f}", f"{p50:.3f}", f"{p90:.3f}", f"{p95:.3f}", f"{p99:.3f}"])
                    csv_file.flush()
                except Exception as ex:
                    print(f"     [ERROR] Benchmark run failed for C={c}: {ex}")

            print(f"[ORIGIN] Shutting down {runtime_name}...")
            stop_server(server_proc)
            wait_for_port_closed(args.port, timeout=3.0)
            time.sleep(1.5)  # Sleep 1.5s between runs to clear kernel socket tables

    finally:
        csv_file.close()

    print("\n======================================================================")
    print(f"[EXPERIMENT 1 COMPLETE] Results saved to: {os.path.abspath(args.output_csv)}")
    print("======================================================================")


if __name__ == "__main__":
    main()