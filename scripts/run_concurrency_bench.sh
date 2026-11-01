#!/usr/bin/env bash
set -euo pipefail

# Elevate file descriptor limits for high concurrency benchmark sweeps
ulimit -n 65535 2>/dev/null || true

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"

cd "$REPO_ROOT"
python3 scripts/bench_exp1.py "$@"