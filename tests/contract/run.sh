#!/usr/bin/env bash
# Roda todos os testes de contrato em ordem.
set -euo pipefail
cd "$(dirname "$0")"
for t in test_*.sh; do
  echo "=== $t ==="
  "./$t"
done
echo "=== todos os testes de contrato passaram ==="
