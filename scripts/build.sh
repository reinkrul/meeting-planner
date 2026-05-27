#!/usr/bin/env bash
# Rebuild the meeting-planner binary into .scratch/bin/.
set -euo pipefail
cd "$(dirname "$0")/.."
go build -o .scratch/bin/meeting-planner ./cmd/meeting-planner
echo "built .scratch/bin/meeting-planner"
