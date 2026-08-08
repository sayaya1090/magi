#!/bin/bash
# A/B: MAGI_RESTRAINT off vs on, 14 tasks x 3 attempts, both arms, serial.
#
# The arms differ in ONE environment variable and nothing else — same binary, same tasks, same
# attempt count, same model, same timeouts. The clause returns the empty string when off, so the
# control arm's system prompt is byte-identical to what shipped.
#
# Serial by design: the model is served locally on this machine and will not batch, so concurrency
# would only queue. Arm B starts only after arm A has fully finished — two benches sharing one
# endpoint invalidates both, which this tree has already paid for.
set -u

REPO=/Users/sayaya/IdeaProjects/magi
cd "$REPO" || exit 1
SHA=$(git rev-parse --short HEAD)
STAMP=$(date +%Y-%m-%d)
LOG="$REPO/scratchpad/ab_restraint_${STAMP}.log"

TASKS=(
  # short, and short because they RUN and finish — the three that were short because they
  # threw before magi started (openssl-selfsigned-cert, pytorch-model-recovery,
  # merge-diff-arc-agi-task) are excluded: a task with no surface for a prompt clause to act on
  # pins both arms at zero and inflates the sample without adding information.
  schemelike-metacircular-eval regex-log mteb-leaderboard caffe-cifar-10 fix-git
  path-tracing headless-terminal kv-store-grpc cancel-async-tasks sqlite-with-gcov build-pmars
  # and three long ones on purpose. The clause is about deliberating before the first edit; the
  # runs where that could matter most are the ones that wander, and a sample of only quick tasks
  # would measure the intervention where it has the least room to act.
  large-scale-text-editing cobol-modernization extract-elf
)

INCLUDE=()
for t in "${TASKS[@]}"; do INCLUDE+=(-i "terminal-bench/$t"); done

say() { echo "[$(date +%H:%M:%S)] $*" | tee -a "$LOG"; }

run_arm() {
  local arm="$1" val="$2"
  say "=== ARM $arm : MAGI_RESTRAINT=$val ==="
  PYTHONPATH="$REPO" \
  MAGI_BASE_URL=http://host.docker.internal:11434/v1 \
  MAGI_BENCH_BINARY_PATH=/tmp/magi-serve \
  MAGI_RESTRAINT="$val" \
  harbor run \
    --agent bench.harbor.magi_agent:MagiAgent \
    --model qwen3-coder-next:latest \
    --dataset terminal-bench/terminal-bench-2-1 \
    "${INCLUDE[@]}" \
    --n-attempts 3 \
    --n-concurrent 1 \
    --agent-timeout-multiplier 3.0 \
    --job-name "${STAMP}-restraint-${arm}-${SHA}" \
    >>"$LOG" 2>&1
  say "=== ARM $arm finished (exit $?) ==="
}

say "A/B start — HEAD $SHA, ${#TASKS[@]} tasks x 3 attempts x 2 arms"
run_arm off 0
say "--- pausing 60s so the endpoint settles between arms ---"
sleep 60
run_arm on 1
say "A/B done"
