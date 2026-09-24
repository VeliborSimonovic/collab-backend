#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
set -a; source .env; set +a
make wasm
go run ./cmd/collabd -mem -addr :8080
