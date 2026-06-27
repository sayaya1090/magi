#!/usr/bin/env bash
# swebench_spotcheck.sh — run magi on ONE SWE-bench-style instance and emit the
# candidate patch (git diff), so we can eyeball whether the scaffold produces a
# sane fix BEFORE a full evaluation. It does NOT run the repo's tests (that needs
# the SWE-bench Docker harness) — it validates the agent/patch-extraction path.
#
# Usage:
#   scripts/swebench_spotcheck.sh \
#     --repo https://github.com/psf/requests --commit <base_sha> \
#     --problem "<issue text>"  (or --problem @issue.txt, or piped on stdin) \
#     [--instance-id requests__requests-1234] [--out predictions.jsonl]
#
# Model/auth (same env magi uses):
#   MAGI_BASE_URL  (default http://localhost:11434/v1)
#   MAGI_MODEL     (default qwen3-coder:30b)
#   MAGI_API_KEY   (for hosted endpoints, e.g. Gemini)
#
# Hold these constant across magi and the tool you compare against (e.g. an orchestrator).
set -euo pipefail

repo="" commit="" problem="" instance_id="" out=""
while [ $# -gt 0 ]; do
  case "$1" in
    --repo) repo="$2"; shift 2;;
    --commit) commit="$2"; shift 2;;
    --problem) problem="$2"; shift 2;;
    --instance-id) instance_id="$2"; shift 2;;
    --out) out="$2"; shift 2;;
    *) echo "unknown arg: $1" >&2; exit 2;;
  esac
done

[ -n "$repo" ] || { echo "--repo required" >&2; exit 2; }
[ -n "$commit" ] || { echo "--commit required" >&2; exit 2; }

# Problem text: literal, @file, or stdin.
case "$problem" in
  "") problem="$(cat)";;            # read from stdin
  @*) problem="$(cat "${problem#@}")";;
esac
[ -n "$problem" ] || { echo "empty problem statement" >&2; exit 2; }

root="$(cd "$(dirname "$0")/.." && pwd)"
bin="$root/magi"
[ -x "$bin" ] || { echo "building magi…" >&2; (cd "$root" && go build -o "$bin" ./cmd/magi); }

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
echo "cloning $repo @ $commit …" >&2
git clone --quiet "$repo" "$work/repo"
git -C "$work/repo" checkout --quiet "$commit"

# The instruction wraps the raw issue with SWE-bench-appropriate guardrails.
prompt="Resolve the following issue in THIS repository. Make the SMALLEST source-code change that fixes it; do NOT modify or add tests, and do not touch unrelated code. When done, stop.

--- ISSUE ---
$problem"

echo "running magi (model=${MAGI_MODEL:-qwen3-coder:30b}) …" >&2
( cd "$work/repo" && printf '%s' "$prompt" | "$bin" -p - \
    -base-url "${MAGI_BASE_URL:-http://localhost:11434/v1}" \
    -model "${MAGI_MODEL:-qwen3-coder:30b}" \
    --permission allow >/dev/null 2>"$work/agent.err" ) || {
  echo "magi run failed; tail of stderr:" >&2; tail -5 "$work/agent.err" >&2; exit 1; }

# Extract the patch, including any new files (staged), excluding test changes is
# left to inspection — we print the full diff for eyeballing.
git -C "$work/repo" add -A
patch="$(git -C "$work/repo" diff --cached)"

echo "===== CANDIDATE PATCH =====" >&2
printf '%s\n' "$patch"

if [ -n "$out" ] && [ -n "$instance_id" ]; then
  # SWE-bench predictions.jsonl line (patch passed via file to avoid arg/stdin clashes).
  printf '%s' "$patch" > "$work/patch.diff"
  python3 - "$instance_id" "${MAGI_MODEL:-qwen3-coder:30b}" "$out" "$work/patch.diff" <<'PY'
import json, sys
instance_id, model, out, pf = sys.argv[1:5]
with open(pf) as f:
    patch = f.read()
with open(out, "a") as f:
    f.write(json.dumps({"instance_id": instance_id,
                        "model_name_or_path": model,
                        "model_patch": patch}) + "\n")
PY
  echo "appended prediction for $instance_id -> $out" >&2
fi
