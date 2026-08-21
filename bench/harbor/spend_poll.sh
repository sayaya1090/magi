#!/bin/bash
# Sample a backend's spend ledger, with a timestamp and the port it belongs to, so a run can be
# costed PER TASK afterwards. Optional: without it, report.py leaves the cost column unknown.
#
# Why sampling, and why the port matters
# --------------------------------------
# A backend plugin that meters itself keeps one running total per process — calls, input, output,
# cache read/write, and a dollar figure when it knows the price — in
# <config>/plugin-data/<plugin>.json. Nothing in it says which task a call belonged to, and nothing
# can: it serves an OpenAI-compatible endpoint and the task name never crosses that wire. The
# containers are isolated; the backend is not.
#
# What makes attribution exact is that a trial holds a backend to itself for its duration — trivially
# so when trials run one at a time, and by claim when bench/harbor/magi_agent.py takes a port from
# MAGI_BENCH_BACKEND_PORTS. Then every call on that port between the trial's started_at and its
# finished_at is that trial's, and report.py differences that port's series. The only error is the
# sampling period at each edge.
#
# Rows: epoch, port, calls, in, out, cache_read, cache_write, usd.
#
# Usage:  LEDGERS="/tmp/mgc /tmp/mgc2" bench/harbor/spend_poll.sh [interval_seconds] &
set -u
REPO=${REPO:-$(cd "$(dirname "$0")/../.." && pwd)}
OUT=${OUT:-$REPO/bench/harbor/state/spend.tsv}
INTERVAL=${1:-5}
PLUGIN=${PLUGIN:?name the plugin whose ledger to sample, e.g. PLUGIN=mybackend}
LEDGERS=${LEDGERS:-}

if [ -z "$LEDGERS" ]; then
  echo "set LEDGERS to the config dirs whose ledgers to sample, e.g. LEDGERS=\"/tmp/mgc\"" >&2
  exit 1
fi
mkdir -p "$(dirname "$OUT")"

while true; do
  for d in $LEDGERS; do
    f="$d/plugin-data/$PLUGIN.json"
    [ -r "$f" ] || continue
    python3 - "$f" >>"$OUT" 2>/dev/null <<'PY'
import json, sys, time
try:
    d = json.load(open(sys.argv[1]))
except Exception:
    raise SystemExit(0)          # a half-written file is skipped, not guessed at
print("%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s" % (
    int(time.time()), next((d[k] for k in ("port", "shim_port") if k in d), "default"),
    d.get("spend_calls", 0), d.get("spend_in", 0), d.get("spend_out", 0),
    d.get("spend_cache_read", 0), d.get("spend_cache_write", 0), d.get("spend_usd", 0)))
PY
  done
  sleep "$INTERVAL"
done
