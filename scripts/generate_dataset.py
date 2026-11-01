#!/usr/bin/env python3
"""
CS-30003 Phase 4A: Static Asset Dataset Generator
Creates 1,000 uniform 4,096-byte (4 KB) static assets in ./public/dataset/
for standardized empirical evaluations.
"""

import argparse
import os
import sys
import time


DEFAULT_TARGET_DIR = os.path.join(".", "public", "dataset")
DEFAULT_NUM_FILES = 1000
DEFAULT_FILE_SIZE = 4096  # exactly 4,096 bytes (4 KB)


def generate_dataset(target_dir: str = DEFAULT_TARGET_DIR, num_files: int = DEFAULT_NUM_FILES, file_size: int = DEFAULT_FILE_SIZE) -> None:
    target_dir = os.path.abspath(target_dir)
    os.makedirs(target_dir, exist_ok=True)

    print("==================================================")
    print("  CS-30003 Dataset Generator")
    print(f"  Target Directory: {target_dir}")
    print(f"  Total Files:      {num_files}")
    print(f"  File Size:        {file_size} bytes ({(file_size / 1024.0):.1f} KB)")
    print(f"  Target Footprint: {((num_files * file_size) / (1024.0 * 1024.0)):.2f} MB")
    print("==================================================")

    start_time = time.time()
    total_bytes = 0

    for i in range(1, num_files + 1):
        filename = f"file_{i:04d}.txt"
        filepath = os.path.join(target_dir, filename)

        # Header prefix identifying file number
        header = f"=== CS-30003 Catalog Asset {i:04d} ===\n".encode("utf-8")
        pad_needed = file_size - len(header)
        if pad_needed > 0:
            content = header + (b"A" * pad_needed)
        else:
            content = header[:file_size]

        with open(filepath, "wb") as f:
            f.write(content)

        total_bytes += len(content)

        if i % 100 == 0 or i == num_files:
            pct = (i / num_files) * 100.0
            print(f"  [{pct:5.1f}%] Generated {i}/{num_files} files ({total_bytes:,} bytes)")

    elapsed = time.time() - start_time
    total_mb = total_bytes / (1024.0 * 1024.0)
    print("--------------------------------------------------")
    print(f"[DATASET GENERATION COMPLETE] Created {num_files} files in {elapsed:.2f}s.")
    print(f"Total directory size: {total_bytes:,} bytes ({total_mb:.2f} MB)")
    print("==================================================")


def main():
    parser = argparse.ArgumentParser(description="Generate uniform static asset catalog for HTTP server benchmarks.")
    parser.add_argument("--dir", default=DEFAULT_TARGET_DIR, help=f"Target directory (default: {DEFAULT_TARGET_DIR})")
    parser.add_argument("--count", type=int, default=DEFAULT_NUM_FILES, help=f"Number of files to create (default: {DEFAULT_NUM_FILES})")
    parser.add_argument("--size", type=int, default=DEFAULT_FILE_SIZE, help=f"Exact file size in bytes (default: {DEFAULT_FILE_SIZE})")

    args = parser.parse_args()
    generate_dataset(args.dir, args.count, args.size)


if __name__ == "__main__":
    main()

