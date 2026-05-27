#!/usr/bin/env bash
# Run all Go tests.
set -euo pipefail
cd "$(dirname "$0")/.."
go test ./...
