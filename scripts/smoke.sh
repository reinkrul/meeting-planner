#!/usr/bin/env bash
# Smoke-test a running meeting-planner instance. Pulls the booking link and
# public base from the server log, then exercises the key endpoints.
#
# Usage: scripts/smoke.sh [config-name]      (default: hostnet)
set -euo pipefail
cd "$(dirname "$0")/.."

NAME="${1:-hostnet}"
LOG=".scratch/server-$NAME.log"
[[ -f "$LOG" ]] || { echo "no log at $LOG — start it with: scripts/run.sh $NAME"; exit 1; }

CAP=$(grep "Booking link:" "$LOG" | tail -1 | awk '{print $NF}')
BASE=$(grep "Public base:" "$LOG" | tail -1 | awk '{print $NF}')
[[ -n "$CAP" ]] || { echo "no booking link found in $LOG"; exit 1; }

echo "instance:     $NAME"
echo "public base:  $BASE"
echo "booking link: $CAP"
echo

pass=0; fail=0
check() { # desc expected url [extra curl args...]
  local desc="$1" exp="$2" url="$3"; shift 3
  local code
  code=$(curl -sS -o /dev/null -w "%{http_code}" "$@" "$url" 2>/dev/null || echo "000")
  if [[ "$code" == "$exp" ]]; then
    printf "  PASS  %-22s %s\n" "$desc" "$code"; pass=$((pass+1))
  else
    printf "  FAIL  %-22s got %s want %s\n" "$desc" "$code" "$exp"; fail=$((fail+1))
  fi
}

FROM=$(date -u +"%Y-%m-%dT00:00:00Z")
TO=$(date -u -v+3d +"%Y-%m-%dT00:00:00Z" 2>/dev/null || date -u -d "+3 days" +"%Y-%m-%dT00:00:00Z")

check "healthz"      200 "$BASE/healthz"
check "booking form" 200 "$CAP/"
check "freebusy"     200 "$CAP/freebusy?from=$FROM&to=$TO"
check "slots"        200 "$CAP/slots" -X POST \
  --data-urlencode "initiator_name=Smoke" \
  --data-urlencode "initiator_email=smoke@example.com" \
  --data-urlencode "duration_minutes=30"

echo
echo "--- slots response summary ---"
BODY=$(curl -sS -X POST "$CAP/slots" \
  --data-urlencode "initiator_name=Smoke" \
  --data-urlencode "initiator_email=smoke@example.com" \
  --data-urlencode "duration_minutes=30" 2>/dev/null || true)
SLOTS=$(printf '%s' "$BODY" | grep -c 'name="slot_start"' || true)
DAYS=$(printf '%s' "$BODY" | grep -c '<details class="day"' || true)
WARNS=$(printf '%s' "$BODY" | grep -oE 'class="warn">[^<]*' | sed 's/class="warn">//' || true)
echo "  days offered:  $DAYS"
echo "  slots offered: $SLOTS"
if [[ -n "$WARNS" ]]; then
  echo "  warnings:"
  printf '%s\n' "$WARNS" | sed 's/^/    - /'
fi
if printf '%s' "$BODY" | grep -q "No times match"; then
  echo "  (no matching slots in horizon)"
fi

echo
echo "pass=$pass fail=$fail"
[[ "$fail" -eq 0 ]]
