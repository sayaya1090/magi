#!/bin/bash
# Run a Terminal-Bench dataset against magi, and report per-task results, tokens and cost.
#
#   bench/harbor/run.sh [concurrency]
#
# One job over the whole dataset. The concurrency argument is the only knob most people need, and
# it is the one that has to be chosen rather than defaulted — see the warning below, because
# raising it costs something.
#
# Environment
# -----------
#   BASE_URL=...                MAGI_BASE_URL as the CONTAINER sees it (host.docker.internal, not
#                               localhost). Required unless the container can already reach one.
#   BINARY=/tmp/magi-serve      MAGI_BENCH_BINARY_PATH: a directory holding magi-arm64/magi-amd64
#   MODEL=...                   passed to harbor --model and on to magi verbatim
#   DATASET=terminal-bench/terminal-bench-2-1
#   BACKEND_PORTS="58411,58412" a pool of backend endpoints you started yourself, one per trial.
#                               Each trial claims one for its duration, which is what keeps the
#                               cost column exact above one concurrent trial (see below).
#   SPEND=state/spend.tsv       optional spend series from spend_poll.sh; without it, no cost column
#   JOB=<name>                  optional job name (default: <date>-tb21)
#   TIMEOUT_MULT=1.0            harbor's --agent-timeout-multiplier
#
# Concurrency and the cost column
# -------------------------------
# Per-task cost is attributed by differencing a backend's spend ledger across each trial's
# wall-clock window. Nothing on the wire says which trial a call belonged to — the task name never
# crosses an OpenAI-compatible endpoint — so that is exact only while ONE trial at a time talks to a
# given backend. Give BACKEND_PORTS as many endpoints as the concurrency and it stays exact;
# give it fewer (or none) and report.py marks the affected rows with "~" instead of passing a
# share-out off as a measurement.
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
MODEL=${MODEL:-}
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
[ -n "$MODEL" ] || { say "ABORT: set MODEL to the model name your backend serves"; exit 1; }

# Say the cost caveat once, up front, rather than leaving it to be discovered in the table. A pool
# smaller than the concurrency is not an error — the run is still valid, its cost column just stops
# being a measurement — so this warns and continues.
POOL=0
if [ -n "${BACKEND_PORTS:-}" ]; then
  POOL=$(echo "$BACKEND_PORTS" | tr ',' ' ' | wc -w | tr -d ' ')
fi
if [ "$CONC" -gt 1 ] && [ "$POOL" -lt "$CONC" ]; then
  say "NOTE: $CONC trials will share $([ "$POOL" -eq 0 ] && echo "one backend" || echo "$POOL backends")."
  say "      Per-task cost becomes apportioned (report.py marks those rows). Scores are unaffected."
fi

say "=== $JOB : $DATASET on $MODEL, $CONC at a time ==="
START=$(date +%s)

env PYTHONPATH="$REPO" \
  ${BASE_URL:+MAGI_BASE_URL="$BASE_URL"} \
  ${BACKEND_PORTS:+MAGI_BENCH_BACKEND_PORTS="$BACKEND_PORTS"} \
  MAGI_BENCH_BINARY_PATH="$BINARY" \
  harbor run \
    --agent bench.harbor.magi_agent:MagiAgent \
    --model "$MODEL" \
    --dataset "$DATASET" \
    --n-attempts 1 \
    --n-concurrent "$CONC" \
    --agent-timeout-multiplier "${TIMEOUT_MULT:-1.0}" \
    --job-name "$JOB" \
    >>"$LOG" 2>&1
RC=$?
END=$(date +%s)
say "=== $JOB finished (exit $RC) — $(( (END-START)/3600 ))h $(( ((END-START)%3600)/60 ))m ==="

python3 "$REPO/bench/harbor/report.py" --jobs-glob "jobs/$JOB" \
  ${SPEND:+--spend "$SPEND"} | tee -a "$LOG"
