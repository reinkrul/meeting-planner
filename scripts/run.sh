#!/usr/bin/env bash
# Build + (re)start a meeting-planner instance in the background.
#
# Usage:
#   scripts/run.sh                # defaults to "dev"
#   scripts/run.sh dev | dev2 | caldav | dev-google
#   scripts/run.sh path/to/config.yaml
#
# Side effects:
#   - Stops any previously-running instance.
#   - Sources .scratch/env.sh if present (for GOOGLE_APP_PASSWORD etc).
#   - Logs to .scratch/server.log.
set -euo pipefail
cd "$(dirname "$0")/.."

# Rebuild so we're always running latest code.
go build -o .scratch/bin/meeting-planner ./cmd/meeting-planner

CONFIG="${1:-dev}"
case "$CONFIG" in
  /*|./*|*.yaml|*.yml)
    CONFIG_FILE="$CONFIG" ;;
  *)
    # Prefer your local override; fall back to the committed example.
    CONFIG_FILE="configs/config.$CONFIG.yaml"
    if [[ ! -f "$CONFIG_FILE" ]]; then
      CONFIG_FILE="configs/config.$CONFIG.example.yaml"
    fi
    ;;
esac

if [[ ! -f "$CONFIG_FILE" ]]; then
  echo "config not found: tried configs/config.$CONFIG.yaml and configs/config.$CONFIG.example.yaml" >&2
  exit 2
fi

# Stop only previous instance(s) using the same config file, so multiple configs
# can run concurrently (e.g. main + dev2 for federation testing).
pkill -f "meeting-planner -config $CONFIG_FILE" 2>/dev/null || true
sleep 0.3

# Source secrets file if present.
if [[ -f .scratch/env.sh ]]; then
  # shellcheck disable=SC1091
  source .scratch/env.sh
fi

# Per-config log file so logs.sh stays useful when multiple instances run.
case "$CONFIG" in
  /*|./*|*.yaml|*.yml) LOG_NAME=$(basename "$CONFIG_FILE" .yaml) ;;
  *)                   LOG_NAME="$CONFIG" ;;
esac
LOG=".scratch/server-$LOG_NAME.log"
mkdir -p .scratch
nohup ./.scratch/bin/meeting-planner -config "$CONFIG_FILE" serve > "$LOG" 2>&1 &
PID=$!
echo "started pid=$PID config=$CONFIG_FILE log=$LOG"

# Give it a moment, then show the startup banner or surface the failure.
sleep 1
if ! kill -0 "$PID" 2>/dev/null; then
  echo "--- server failed to start ---"
  cat "$LOG"
  exit 1
fi
tail -n 20 "$LOG"
