#!/usr/bin/env bash
# Show the last N lines of a server log.
#
# Usage:
#   scripts/logs.sh              # most recently modified server log, last 50 lines
#   scripts/logs.sh hostnet      # the log for `scripts/run.sh hostnet`
#   scripts/logs.sh hostnet 100  # last 100 lines of the same
#   scripts/logs.sh -- 100       # most recent log, last 100 lines
set -euo pipefail
cd "$(dirname "$0")/.."

CONFIG="${1:-}"
N="${2:-50}"

if [[ -z "$CONFIG" || "$CONFIG" == "--" ]]; then
  # Find the most recently modified server-*.log
  shopt -s nullglob
  LOGS=(.scratch/server-*.log)
  if [[ ${#LOGS[@]} -eq 0 ]]; then
    echo "no logs yet under .scratch/server-*.log"
    exit 0
  fi
  LOG=$(ls -t "${LOGS[@]}" | head -n 1)
else
  LOG=".scratch/server-$CONFIG.log"
  if [[ ! -f "$LOG" ]]; then
    echo "no log at $LOG (have you run scripts/run.sh $CONFIG ?)"
    exit 1
  fi
fi

echo "=== $LOG ==="
tail -n "$N" "$LOG"
