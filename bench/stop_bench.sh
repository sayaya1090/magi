#!/bin/bash
# Stop a running bench chain, completely.
#
# Killing the chain script alone does NOT stop the bench: harbor is re-parented to init and keeps
# running, and its next act is to advance to the following task. Observed three times in a row
# (636ecde, 7d048e1, c25addc), each needing a manual `kill -9` after the fact — and a bench that is
# still running while a new one starts is two benches sharing one model endpoint, which invalidates
# both. So the exit condition here is not "I sent a signal", it is "nothing matches any more".
#
# Kill by PID, never by pattern. `pkill -f 'harbor run'` once matched a WATCHER whose own command
# line contained that string, and killed it instead of the bench (exit 144). This enumerates first,
# excludes itself and its own ancestors, prints what it will act on, and only then signals.
#
# Removing the container does not stop harbor — it makes harbor move on to the next task. Processes
# first, containers second.
#
# Usage:
#   bench/stop_bench.sh --dry-run     # list what would be killed, touch nothing
#   bench/stop_bench.sh               # stop it
set -u

DRY=0
[ "${1:-}" = "--dry-run" ] && DRY=1

SELF=$$
# Every ancestor of this script (the shell that launched it, and so on) — a match on one of those is
# this process tree, not the bench.
ancestors() {
  local p=$SELF
  while [ -n "$p" ] && [ "$p" != "0" ] && [ "$p" != "1" ]; do
    echo "$p"
    p=$(ps -o ppid= -p "$p" 2>/dev/null | tr -d ' ')
  done
}
EXCLUDE=" $(ancestors | tr '\n' ' ') "

# A bench process is the chain script, or the harbor run it launched. Matched on ARGV POSITION, not
# on "the line contains this string": a watcher that merely MENTIONS the chain path (a grep, a tail,
# this script's own invocation) has the same substrings and must never be a target. That is the
# failure that already happened once.
#
#   launcher  argv0 = /bin/bash   argv1 = /tmp/magi-serve/{chain_,phase}*.sh
#   harbor    argv0 = …/python    argv1 = …/harbor              argv2 = run
#
# Two launcher prefixes, not the whole directory: a chain script, and any waiter re-arming a later
# PHASE of one — missing a waiter leaves a bench that starts itself minutes after this reported
# "stopped clean". Named prefixes rather than *.sh because that directory also holds unrelated
# helpers, and a stop that reaches past the bench is the same class of mistake as a pattern kill.
bench_pids() {
  ps -eo pid,ppid,command | while read -r pid ppid a0 a1 a2 _rest; do
    case "$a0:$a1" in
      /bin/bash:/tmp/magi-serve/chain_*.sh|/bin/bash:/tmp/magi-serve/phase*.sh) ;;
      */python:*/harbor) [ "$a2" = "run" ] || continue ;;
      *) continue ;;
    esac
    case "$EXCLUDE" in *" $pid "*) continue ;; esac
    echo "$pid $ppid $a0 $a1 $a2"
  done
}

show() {
  local n=0
  while read -r pid ppid rest; do
    [ -z "${pid:-}" ] && continue
    printf '  pid=%-7s ppid=%-7s %s\n' "$pid" "$ppid" "$(echo "$rest" | cut -c1-90)"
    n=$((n + 1))
  done
  [ "$n" = 0 ] && echo "  (none)"
}

echo "== bench processes:"
bench_pids | show

if [ "$DRY" = 1 ]; then
  echo "== containers (would remove):"
  docker ps -a --format '  {{.Names}} — {{.Status}}' 2>/dev/null | grep -- '__env-' || echo "  (none)"
  echo "== dry run: nothing was signalled"
  exit 0
fi

# TERM first, parents included: a chain script that exits on its own leaves less to clean up.
for pid in $(bench_pids | awk '{print $1}'); do kill "$pid" 2>/dev/null; done
sleep 3

# Whatever survived was re-parented and will not stop on its own.
LEFT=$(bench_pids | awk '{print $1}')
if [ -n "$LEFT" ]; then
  echo "== survivors after TERM (re-parented) — sending KILL:"
  bench_pids | show
  for pid in $LEFT; do kill -9 "$pid" 2>/dev/null; done
  sleep 3
fi

echo "== remaining:"
bench_pids | show
if [ -n "$(bench_pids | awk '{print $1}')" ]; then
  echo "FATAL: a bench process is still running — do NOT start another bench" >&2
  exit 1
fi

# Only now: harbor is gone, so removing containers cannot advance a run.
for c in $(docker ps -a --format '{{.Names}}' 2>/dev/null | grep -- '__env-'); do
  echo "  removing container $c"
  docker rm -f "$c" >/dev/null 2>&1
done
echo "== stopped clean"
