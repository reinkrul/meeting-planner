#!/usr/bin/env bash
# Stop any running meeting-planner instance(s).
set -euo pipefail
cd "$(dirname "$0")/.."
if pkill -f "meeting-planner -config" 2>/dev/null; then
  echo "stopped"
else
  echo "no instances running"
fi
sleep 0.3
