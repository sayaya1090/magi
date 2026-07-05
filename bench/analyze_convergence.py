#!/usr/bin/env python3
r"""Convergence metrics for magi harbor bench runs.

reward alone hides HOW a trial ended. This reads each trial's result.json +
agent/magi-stdout.txt and reports, per run (arm):

  - solved            reward > 0
  - timeout           hit the wall clock (AgentTimeoutError / exec >= ~wall)
  - clean-finish      the agent stopped on its own (no timeout, no exception)
  - clean & correct   of the clean finishes, how many were right  ← convergence quality
  - false-completion  declared "task complete" in stdout but reward == 0  ← honesty
  - no-oracle         container lacked the tool to self-verify (e.g. python3 missing)
  - exec_s / writes   medians, for the surviving (non-timeout) finishes

Usage:
    python3 bench/analyze_convergence.py runs/2026-07-04d__refine-on runs/2026-07-04d__refine-off
    python3 bench/analyze_convergence.py --per-task runs/2026-07-04d__refine-on
"""

import argparse
import datetime
import glob
import json
import os
import re
import statistics
import sys

# A trial is judged a wall-clock timeout if harbor flagged AgentTimeoutError, or the
# agent ran to within this margin of a 900s wall (some trials record the exception on
# one side only). Kept as a fraction so it scales if the wall changes.
WALL_SEC = 900.0
WALL_MARGIN = 0.97  # exec >= 873s counts as "at the wall" even without the exception

# stdout markers (see any agent/magi-stdout.txt). Kept literal so they match the real
# emitter, not a paraphrase.
WRITE = "⚙ write"   # ⚙ write
READ = "⚙ read"     # ⚙ read
BASH = "⚙ bash"     # ⚙ bash
LOOP_GUARD = "Loop guard"
COMPLETE_RE = re.compile(r"task (?:has been|is) complet|successfully completed", re.I)
# Signals the container could not run the agent's own verification (regex-log had no
# python3, so the agent guessed blind). Extend as new no-oracle signatures show up.
NO_ORACLE_RE = re.compile(r"python3?: not found|No python found|command not found", re.I)


def _iso(s):
    return datetime.datetime.fromisoformat(s.replace("Z", "+00:00"))


def _dur(block):
    if not block or not block.get("started_at") or not block.get("finished_at"):
        return None
    return (_iso(block["finished_at"]) - _iso(block["started_at"])).total_seconds()


def load_trial(result_json):
    """Parse one trial dir into a flat dict of facts read from its logs."""
    d = json.load(open(result_json))
    trial_dir = os.path.dirname(result_json)
    name = d.get("task_name", "?").split("/")[-1]
    reward = (d.get("verifier_result") or {}).get("rewards", {}).get("reward")
    exc = d.get("exception_info") or {}
    exc_type = exc.get("exception_type", "")
    exec_s = _dur(d.get("agent_execution"))

    timeout = exc_type == "AgentTimeoutError" or (
        exec_s is not None and exec_s >= WALL_SEC * WALL_MARGIN
    )
    # "clean finish" = the agent chose to stop (no wall-clock kill, no crash).
    clean = not timeout and not exc_type

    stdout_path = os.path.join(trial_dir, "agent", "magi-stdout.txt")
    if not os.path.isfile(stdout_path):
        hits = glob.glob(os.path.join(trial_dir, "**", "magi-stdout.txt"), recursive=True)
        stdout_path = hits[0] if hits else None
    writes = reads = bashes = guards = completes = 0
    no_oracle = False
    if stdout_path:
        with open(stdout_path, encoding="utf-8", errors="replace") as f:
            for line in f:
                if line.startswith(WRITE):
                    writes += 1
                elif line.startswith(READ):
                    reads += 1
                elif line.startswith(BASH):
                    bashes += 1
                if LOOP_GUARD in line:
                    guards += 1
                if COMPLETE_RE.search(line):
                    completes += 1
                if NO_ORACLE_RE.search(line):
                    no_oracle = True

    solved = bool(reward and reward > 0)
    return {
        "task": name,
        "reward": reward,
        "solved": solved,
        "exec_s": exec_s,
        "timeout": timeout,
        "clean": clean,
        "clean_correct": clean and solved,
        # declared done but graded wrong = a false completion (honesty signal).
        "false_completion": (completes > 0) and not solved,
        "no_oracle": no_oracle,
        "writes": writes,
        "reads": reads,
        "bashes": bashes,
        "guards": guards,
        "completes": completes,
    }


def load_run(run_dir):
    trials = [load_trial(rj) for rj in sorted(glob.glob(f"{run_dir}/**/result.json", recursive=True))
              if os.path.basename(os.path.dirname(rj)) != os.path.basename(run_dir)]
    # keep only the leaf trial result.json (has a task_name), not job-level rollups
    return [t for t in trials if t["task"] != "?"]


def _med(xs):
    xs = [x for x in xs if x is not None]
    return round(statistics.median(xs)) if xs else None


def summarize(run_dir):
    trials = load_run(run_dir)
    n = len(trials)
    if not n:
        return None
    clean = [t for t in trials if t["clean"]]
    return {
        "run": os.path.basename(run_dir.rstrip("/")),
        "n": n,
        "solved": sum(t["solved"] for t in trials),
        "timeout": sum(t["timeout"] for t in trials),
        "clean_finish": len(clean),
        "clean_correct": sum(t["clean_correct"] for t in trials),
        "false_completion": sum(t["false_completion"] for t in trials),
        "no_oracle": sum(t["no_oracle"] for t in trials),
        "med_exec_clean": _med([t["exec_s"] for t in clean]),
        "med_writes_clean": _med([t["writes"] for t in clean]),
        "trials": trials,
    }


def pct(a, b):
    return f"{100*a/b:.0f}%" if b else "  -"


def print_summary(s):
    n = s["n"]
    print(f"\n===== {s['run']}  (n={n}) =====")
    print(f"  solved            {s['solved']:>3}/{n}  ({pct(s['solved'], n)})")
    print(f"  timeout (wall)    {s['timeout']:>3}/{n}  ({pct(s['timeout'], n)})")
    print(f"  clean-finish      {s['clean_finish']:>3}/{n}  ({pct(s['clean_finish'], n)})")
    cf = s["clean_finish"]
    print(f"  ├ clean & correct {s['clean_correct']:>3}/{cf}  ({pct(s['clean_correct'], cf)})   <- convergence quality")
    print(f"  false-completion  {s['false_completion']:>3}/{n}  ({pct(s['false_completion'], n)})   <- honesty")
    print(f"  no-oracle tasks   {s['no_oracle']:>3}/{n}")
    print(f"  median exec (clean)   {s['med_exec_clean']}s")
    print(f"  median writes (clean) {s['med_writes_clean']}")


def print_per_task(s):
    print(f"\n----- {s['run']} per-task -----")
    hdr = f"{'task':28} {'rew':>4} {'exec':>5} {'end':>7} {'wr':>3} {'rd':>3} {'gd':>3} {'cmp':>3} {'oracle':>6}"
    print(hdr)
    for t in sorted(s["trials"], key=lambda x: -(x["exec_s"] or 0)):
        end = "timeout" if t["timeout"] else ("clean" if t["clean"] else "err")
        oracle = "MISSING" if t["no_oracle"] else ""
        print(f"{t['task']:28} {str(t['reward']):>4} {str(round(t['exec_s']) if t['exec_s'] else '-'):>5} "
              f"{end:>7} {t['writes']:>3} {t['reads']:>3} {t['guards']:>3} {t['completes']:>3} {oracle:>6}")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("runs", nargs="+", help="run (arm) directories holding trial subdirs")
    ap.add_argument("--per-task", action="store_true", help="also print each trial's row")
    a = ap.parse_args()
    summaries = []
    for r in a.runs:
        s = summarize(r)
        if not s:
            print(f"[skip] no trials under {r}", file=sys.stderr)
            continue
        summaries.append(s)
        print_summary(s)
        if a.per_task:
            print_per_task(s)
    if len(summaries) == 2:
        x, y = summaries
        print(f"\n===== {x['run']}  vs  {y['run']} =====")
        for k, label in [("solved", "solved"), ("clean_finish", "clean-finish"),
                         ("clean_correct", "clean&correct"), ("false_completion", "false-compl"),
                         ("timeout", "timeout")]:
            print(f"  {label:14} {x[k]:>3}  vs {y[k]:>3}")


if __name__ == "__main__":
    main()
