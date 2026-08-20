#!/bin/bash
# Run a Terminal-Bench dataset against magi, and report per-task results, tokens and cost.
#
#   bench/harbor/run.sh [concurrency]
#
# One job over the whole dataset. The concurrency argument is the only knob most people need, and
# it is the one that has to be chosen rather than defaulted — see the two warnings below, because
# raising it costs something in both directions.
#
# Environment
# -----------
#   BASE_URL=...                MAGI_BASE_URL as the CONTAINER sees it (host.docker.internal, not
#                               localhost). Required unless the container can already reach one.
#   BINARY=/tmp/magi-serve      MAGI_BENCH_BINARY_PATH: a directory holding magi-arm64/magi-amd64
#   MODEL=sonnet                passed to harbor --model and on to magi verbatim
#   DATASET=terminal-bench/terminal-bench-2-1
#   SHIM=claudecode             start one backend shim PER TRIAL and route to them (see below).
#                               With this, BASE_URL, SHIM_PORTS and the spend poller are all set up
#                               and torn down for you, and per-task cost stays exact at any
#                               concurrency. Without it, bring your own BASE_URL.
#   SHIM_BIN=magi               the magi binary that hosts those shims (this machine's, not the
#                               container's)
#   SHIM_PORT_BASE=58411        first port of the pool
#   SHIM_PORTS="58411,58412"    a pool you started yourself, if you would rather not use SHIM=
#   SPEND=state/spend.tsv       optional spend series from spend_poll.sh; without it, no cost column
#   JOB=<name>                  optional job name (default: <date>-tb21)
#   TIMEOUT_MULT=3.0            harbor's --agent-timeout-multiplier
#
# Concurrency and the cost column
# -------------------------------
# Per-task cost is attributed by differencing the backend's spend ledger across each trial's
# wall-clock window. Nothing on the wire says which trial a call belonged to — the task name never
# crosses an OpenAI-compatible endpoint — so that is exact only while ONE trial at a time talks to a
# given backend.
#
# Which is why SHIM= sizes the pool from the concurrency argument rather than taking it as a second
# setting: the two cannot be set to disagree, and cost stays exact however many trials you run. A
# pool you bring yourself CAN be smaller (SHIM_PORTS), and then report.py marks the affected rows
# with "~" instead of passing a share-out off as a measurement.
#
# ⚠ Concurrency and the SCORE
# ---------------------------
# Trials share the Docker VM's CPU and memory, and that distortion runs one way only: a task that
# would have passed can time out because it was starved, and none passes because of it. Check
# `docker info` first — on a 7-CPU, 8 GB VM, four trials get 1.75 cores and 2 GB each, which is not
# enough for the tasks that compile or train something. A reported score is only comparable to
# another score run with the same room.
set -u

REPO=${REPO:-$(cd "$(dirname "$0")/../.." && pwd)}
cd "$REPO" || exit 1

CONC=${1:-${CONC:-1}}
MODEL=${MODEL:-sonnet}
DATASET=${DATASET:-terminal-bench/terminal-bench-2-1}
BINARY=${BINARY:-/tmp/magi-serve}
STATE=${STATE:-$REPO/bench/harbor/state}
JOB=${JOB:-$(date '+%Y-%m-%d')-tb21}
LOG="$STATE/run.log"
say() { echo "[$(date '+%Y-%m-%d %H:%M:%S')] $*" | tee -a "$LOG"; }

mkdir -p "$STATE"

case "$CONC" in
  ''|*[!0-9]*) say "ABORT: concurrency must be a number, got '$CONC'"; exit 1 ;;
esac

# SHIM=<plugin>: one backend per trial, started here.
#
# A CLI-backed shim serves one call at a time — magi.serve holds the plugin lock across the whole
# handler, model turn included — so a single shim is both the throughput ceiling and the reason
# per-task cost stops being exact above one trial. Both go away by running as many as there are
# trials, and there is no reason to make somebody do that by hand.
#
# Each gets its OWN config dir. Two magi instances sharing one rewrite each other's base_url and
# hot-reload into localhost — measured, and it looks like a backend that answers 404 for a model it
# was serving a second ago.
SHIM_PIDS=""
cleanup() {
  [ -n "${POLL_PID:-}" ] && kill "$POLL_PID" 2>/dev/null
  for pid in $SHIM_PIDS; do kill "$pid" 2>/dev/null; done
  [ -n "$SHIM_PIDS" ] && say "stopped the shim pool"
  return 0
}
trap cleanup EXIT INT TERM

if [ -n "${SHIM:-}" ]; then
  SHIM_BIN=${SHIM_BIN:-magi}
  command -v "$SHIM_BIN" >/dev/null || { say "ABORT: SHIM_BIN '$SHIM_BIN' is not on PATH"; exit 1; }
  BASE=${SHIM_PORT_BASE:-58411}
  PORTS="" ; LEDGERS=""
  i=0
  while [ "$i" -lt "$CONC" ]; do
    port=$((BASE + i))
    dir="$STATE/shim/$port"
    mkdir -p "$dir"
    # base_url/model are the daemon's OWN backend, which it never uses: this process exists only to
    # serve the shim. The plugin table is what matters.
    cat > "$dir/config.toml" <<EOF
# Started by bench/harbor/run.sh — one shim of a pool of $CONC. Its own directory on purpose.
base_url = "http://localhost:11434/v1"
model = "unused"

[plugins.$SHIM]
enabled = true
model = "$MODEL"
port = $port
EOF
    env MAGI_CONFIG_DIR="$dir" MAGI_DATA_DIR="$dir" nohup "$SHIM_BIN" -daemon >>"$dir/daemon.log" 2>&1 &
    SHIM_PIDS="$SHIM_PIDS $!"
    PORTS="${PORTS:+$PORTS,}$port"
    LEDGERS="${LEDGERS:+$LEDGERS }$dir"
    i=$((i + 1))
  done

  say "starting $CONC $SHIM shim(s) on ${PORTS}"
  # Wait for every port, and refuse to run rather than schedule N guaranteed failures that each burn
  # their full agent timeout.
  waited=0
  while [ "$waited" -lt 90 ]; do
    up=0
    for p in $(echo "$PORTS" | tr ',' ' '); do
      lsof -nP -iTCP:"$p" -sTCP:LISTEN >/dev/null 2>&1 && up=$((up + 1))
    done
    [ "$up" -eq "$CONC" ] && break
    sleep 3; waited=$((waited + 3))
  done
  if [ "$up" -ne "$CONC" ]; then
    say "ABORT: only $up of $CONC shims came up — see $STATE/shim/*/daemon.log"
    exit 1
  fi
  SHIM_PORTS="$PORTS"
  BASE_URL=${BASE_URL:-http://host.docker.internal:$BASE/v1}
  SPEND=${SPEND:-$STATE/spend.tsv}
  LEDGERS="$LEDGERS" OUT="$SPEND" PLUGIN="$SHIM"     nohup "$REPO/bench/harbor/spend_poll.sh" 5 >/dev/null 2>&1 &
  POLL_PID=$!
  sleep 6   # one sample must land BEFORE the first trial, or its window has no left edge
  say "pool ready; per-task cost will be exact (one backend per trial)"
fi

# Say the cost caveat once, up front, rather than leaving it to be discovered in the table. A pool
# smaller than the concurrency is not an error — the run is still valid, its cost column just stops
# being a measurement — so this warns and continues.
POOL=0
if [ -n "${SHIM_PORTS:-}" ]; then
  POOL=$(echo "$SHIM_PORTS" | tr ',' ' ' | wc -w | tr -d ' ')
fi
if [ "$CONC" -gt 1 ] && [ "$POOL" -lt "$CONC" ]; then
  say "NOTE: $CONC trials will share $([ "$POOL" -eq 0 ] && echo "one backend" || echo "$POOL backends")."
  say "      Per-task cost becomes apportioned (report.py marks those rows). Scores are unaffected."
fi

say "=== $JOB : $DATASET on $MODEL, $CONC at a time ==="
START=$(date +%s)

env PYTHONPATH="$REPO" \
  ${BASE_URL:+MAGI_BASE_URL="$BASE_URL"} \
  ${SHIM_PORTS:+MAGI_BENCH_SHIM_PORTS="$SHIM_PORTS"} \
  MAGI_BENCH_BINARY_PATH="$BINARY" \
  harbor run \
    --agent bench.harbor.magi_agent:MagiAgent \
    --model "$MODEL" \
    --dataset "$DATASET" \
    --n-attempts 1 \
    --n-concurrent "$CONC" \
    --agent-timeout-multiplier "${TIMEOUT_MULT:-3.0}" \
    --job-name "$JOB" \
    >>"$LOG" 2>&1
RC=$?
END=$(date +%s)
say "=== $JOB finished (exit $RC) — $(( (END-START)/3600 ))h $(( ((END-START)%3600)/60 ))m ==="

python3 "$REPO/bench/harbor/report.py" --jobs-glob "jobs/$JOB" \
  ${SPEND:+--spend "$SPEND"} | tee -a "$LOG"
