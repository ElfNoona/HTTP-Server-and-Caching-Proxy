#!/usr/bin/env python3
"""
CS-30003 Phase 4B: Standalone Zipfian Workload Trace Generator
Generates a sequence of request paths sampled from a Zipfian distribution:
w_k = 1 / (k ^ alpha)
"""

import argparse
import math
import os
import random
import sys


def generate_zipf_trace(
    alpha: float = 0.8,
    num_files: int = 1000,
    requests: int = 10000,
    output_path: str = "./public/dataset/zipf_trace.txt",
    seed: int = 42,
) -> None:
    random.seed(seed)
    os.makedirs(os.path.dirname(os.path.abspath(output_path)), exist_ok=True)

    print("==================================================")
    print("  CS-30003 Zipfian Trace Generator")
    print(f"  Catalog Size (N): {num_files} files")
    print(f"  Skew Factor (α): {alpha}")
    print(f"  Random Seed:      {seed}")
    print(f"  Requests Count:   {requests:,}")
    print(f"  Output Trace:     {output_path}")
    print("==================================================")

    # Precompute population and weights
    population = [f"/dataset/file_{k:04d}.txt" for k in range(1, num_files + 1)]
    weights = [1.0 / (k ** alpha) for k in range(1, num_files + 1)]

    samples = random.choices(population, weights=weights, k=requests)

    with open(output_path, "w", encoding="utf-8") as f:
        for path in samples:
            f.write(f"{path}\n")

    print(f"[SUCCESS] Wrote {len(samples):,} Zipf-distributed request paths to {output_path}")


def main():
    parser = argparse.ArgumentParser(description="Generate Zipfian workload trace file.")
    parser.add_argument("--alpha", type=float, default=0.8, help="Zipfian skew factor alpha (default: 0.8)")
    parser.add_argument("--num-files", type=int, default=1000, help="Total number of catalog files (default: 1000)")
    parser.add_argument("--requests", type=int, default=10000, help="Number of request samples to generate (default: 10000)")
    parser.add_argument("--output", default="./public/dataset/zipf_trace.txt", help="Output trace file path (default: ./public/dataset/zipf_trace.txt)")
    parser.add_argument("--seed", type=int, default=42, help="Random seed for reproducibility (default: 42)")

    args = parser.parse_args()
    generate_zipf_trace(
        alpha=args.alpha,
        num_files=args.num_files,
        requests=args.requests,
        output_path=args.output,
        seed=args.seed,
    )


if __name__ == "__main__":
    main()