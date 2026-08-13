#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."

PYTHONDONTWRITEBYTECODE=1 python3 tests/contract/test_routing_pair_selection.py
