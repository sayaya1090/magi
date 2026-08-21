#!/usr/bin/env python3
"""Per-task results, tokens and cost for a Terminal-Bench run, recomputed from the job tree.

Nothing here is hand-maintained. The table is derived from what the runs left on disk every time it
is asked, so a task that is re-run, cancelled or repeated cannot leave a stale row behind — the
failure mode a hand-appended ledger has. A later run of the same task supersedes an earlier one: a
re-run is a correction, not a second data point.

Four things per task, from three independent sources, on purpose:

  reward      the harness's own verdict, read from each job's aggregate
  turns       user prompts magi answered — usually one for a whole task
  calls       LLM requests the backend actually served for it
  tokens      magi's own accounting, summed from the trial's `turn.finished` usage
  usd         the backend's ledger, differenced across the trial's wall-clock window

tokens and usd answer different questions and are allowed to disagree; when they do, that
disagreement is itself the finding, which is why neither is derived from the other.

The usd column needs a backend that reports what it charges and a --spend series carrying those
totals over time. A bare OpenAI-compatible endpoint reports neither, and nothing that ships here
produces such a series; without one, cost reads "?" rather than 0. Zero is a claim; unknown is the
truth.

The series is a TSV, one sample per line, ascending:

    epoch  port  calls  in  out  cache_read  cache_write  usd

each field a RUNNING TOTAL for that backend (they only go up), sampled often enough that a trial's
window has a sample on each side of it — a few seconds. Anything that can read your backend's
own accounting can write it.

Usage:
    python3 bench/harbor/report.py --jobs-glob 'jobs/*sonnet89-b*'
    python3 bench/harbor/report.py --jobs-glob 'jobs/*' --markdown > results.md
"""
import argparse
import glob
import json
import os
from datetime import datetime


def epoch(ts):
    """ISO-8601 (with or without Z) to epoch seconds."""
    return datetime.fromisoformat(ts.replace("Z", "+00:00")).timestamp()


def load_spend(path):
    """The --spend samples, per backend port, ascending.

    Rows are `epoch port calls in out cache_read cache_write usd`. A run whose backends were one
    process has one port and one series; a run with a pool has one series each, and a trial's cost
    is ITS port's series differenced across ITS window — which is what makes the figure a
    measurement rather than a share-out.
    """
    by_port = {}
    if not path or not os.path.exists(path):
        return by_port
    for line in open(path):
        f = line.rstrip("\n").split("\t")
        if not f or f[0] == "epoch":
            continue
        if len(f) == 7:  # a series written before ports were recorded
            f = [f[0], "default"] + f[1:]
        if len(f) != 8:
            continue
        try:
            by_port.setdefault(f[1], []).append(
                (int(f[0]), float(f[2]), float(f[3]), float(f[4]),
                 float(f[5]), float(f[6]), float(f[7])))
        except ValueError:
            continue
    # A series that began before the port was recorded, on a run that turns out to have had exactly
    # one backend, is that backend's — splitting it into a nameless bucket would leave every trial
    # from that stretch costed at "unknown" for a bookkeeping reason rather than a real one. With
    # two or more named backends the nameless rows are genuinely ambiguous and are left alone.
    # Rejoin a series that was split by bookkeeping rather than by fact.
    #
    # Rows carry no port until the poller records one, so restarting a poller mid-run leaves the
    # same backend as a nameless stretch followed by a named one. Left apart, every trial in the
    # nameless stretch is costed "unknown" for no real reason.
    #
    # Continuity decides, not a guess: these ledgers are CUMULATIVE, so the named series is the same
    # ledger when it starts at or after the nameless one ends and its counters carry on from where
    # they left off. Exactly one candidate joins; several, or none, and the nameless rows are
    # genuinely ambiguous and stay that way.
    if "default" in by_port:
        tail = sorted(by_port["default"])[-1]
        cont = [k for k, rows in by_port.items()
                if k != "default" and rows
                and sorted(rows)[0][0] >= tail[0] and sorted(rows)[0][1] >= tail[1]]
        if len(cont) == 1:
            by_port[cont[0]].extend(by_port.pop("default"))
            # And "default" stays as an ALIAS for it. Trials from the nameless stretch recorded no
            # port either — dropping the key would merge the samples and then fail to look them up,
            # which is the same unknown by a longer road.
            by_port["default"] = by_port[cont[0]]
    for rows in by_port.values():
        rows.sort()
    return by_port


def sample_at(rows, t):
    """The last sample at or before t; None when the series does not cover it."""
    best = None
    for r in rows:
        if r[0] <= t:
            best = r
        else:
            break
    return best


def spend_between(rows, t0, t1, others=()):
    """What the backend billed while this trial ran, and whether any of it had to be shared.

    Exact while one trial at a time talks to a given backend. When two overlap on one backend there
    is nothing on the wire that says whose call was whose — the task name never crosses an
    OpenAI-compatible endpoint — so an overlapped interval is split equally among whoever was
    running. Unbiased across many intervals, exact whenever only one was.

    The "shared" flag is raised only when the splitting actually moved money: two trials back to
    back touch at a single sample, and flagging a whole row over a boundary interval that carried no
    calls would put a mark on nearly everything and teach the reader to ignore it.
    """
    shared_usd = 0.0
    keys = ("calls", "in", "out", "cache_read", "cache_write", "usd")
    tot = {k: 0.0 for k in keys}
    prev = sample_at(rows, t0)
    if not prev:
        return None, False
    seen = False
    for r in rows:
        if r[0] <= prev[0]:
            continue
        if r[0] > t1:
            break
        n = 1 + sum(1 for (a, b) in others if a < r[0] and b > prev[0])
        for i, k in enumerate(keys):
            share = (r[i + 1] - prev[i + 1]) / n
            tot[k] += share
            if n > 1 and k == "usd":
                shared_usd += share
        prev = r
        seen = True
    if not seen:
        return None, False
    return tot, shared_usd > 0.01 * max(tot["usd"], 1e-9)


def trial_facts(agent_dir):
    """Turns, tokens and the council's verdict, from the trial's own event log.

    A trial killed mid-turn — an agent timeout — never reaches turn.finished, and summing only that
    event reports the most expensive failures as costing ZERO tokens, which is worse than reporting
    nothing: a zero is a claim. Those fall back to the last context.usage, which carries cumulative
    output; cumulative input is not in that event, so input and cached come back unknown.
    """
    p = os.path.join(agent_dir, "magi-events.jsonl")
    tok = {"in": 0, "out": 0, "cached": 0}
    turns, finished, last_out, council = 0, False, 0, None
    if not os.path.exists(p):
        return tok, turns, council
    for line in open(p):
        try:
            d = json.loads(line)
        except Exception:
            continue
        ty = d.get("type")
        if ty == "prompt.submitted":
            turns += 1
        elif ty == "council.decided":
            # The tally, not just the word: "done 3-0" and "done 2-1" are different runs, and a
            # turn that ended with no council at all is a third thing again.
            data = d.get("data") or {}
            t = data.get("tally") or {}
            council = f"{data.get('decision', '?')} {int(t.get('done', 0))}-{int(t.get('continue', 0))}"
        elif ty == "context.usage":
            last_out = max(last_out, int((d.get("data") or {}).get("outTokens") or 0))
        elif ty == "turn.finished":
            finished = True
            u = (d.get("data") or {}).get("usage") or {}
            for k in tok:
                tok[k] += int(u.get(k) or 0)
    if not finished:
        tok = {"in": None, "out": last_out or None, "cached": None}
    return tok, turns, council


def collect(jobs_glob):
    out = {}
    for job in sorted(glob.glob(jobs_glob)):
        for trial in sorted(glob.glob(os.path.join(job, "*__*"))):
            name = os.path.basename(trial).split("__")[0]
            rp = os.path.join(trial, "result.json")
            if not os.path.exists(rp):
                continue  # still running: not a result, so not a row
            try:
                r = json.load(open(rp))
            except Exception:
                continue
            started, finished = r.get("started_at"), r.get("finished_at")
            if not (started and finished):
                continue
            tok, turns, council = trial_facts(os.path.join(trial, "agent"))
            portfile = os.path.join(trial, "agent", "backend-port.txt")
            row = {
                "task": name, "job": os.path.basename(job),
                "t0": epoch(started), "t1": epoch(finished),
                "mins": (epoch(finished) - epoch(started)) / 60,
                "tokens": tok, "turns": turns, "council": council,
                "port": open(portfile).read().strip() if os.path.exists(portfile) else "default",
                "timeout": os.path.exists(os.path.join(trial, "exception.txt")),
            }
            if name not in out or row["t0"] > out[name]["t0"]:
                out[name] = row
    return out


def rewards(jobs_glob):
    """reward per task, read from each job's own aggregate rather than re-derived."""
    got = {}
    for job in sorted(glob.glob(jobs_glob)):
        rp = os.path.join(job, "result.json")
        if not os.path.exists(rp):
            continue
        try:
            d = json.load(open(rp))
        except Exception:
            continue
        for ev in (d.get("stats") or {}).get("evals", {}).values():
            for value, trials in ((ev.get("reward_stats") or {}).get("reward") or {}).items():
                for t in trials:
                    got[t.split("__")[0]] = float(value)
    return got


def main():
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--jobs-glob", default="jobs/*", help="which job directories to read")
    ap.add_argument("--spend", default="", help="TSV of running spend totals; see the header (optional)")
    ap.add_argument("--tasks", default="", help="task list file; sets the denominator and the order")
    ap.add_argument("--demote", default="", help="file of task names whose PASS does not count "
                    "(one per line) — a pass produced under settings this run no longer uses")
    ap.add_argument("--markdown", action="store_true", help="emit a markdown table")
    ap.add_argument("--csv", action="store_true")
    a = ap.parse_args()

    rows, rew = collect(a.jobs_glob), rewards(a.jobs_glob)
    spend = load_spend(a.spend)
    if a.tasks and os.path.exists(a.tasks):
        order = [t.strip() for t in open(a.tasks) if t.strip()]
    else:
        order = sorted(rows)
    # A pass that only happened because of settings this run has since dropped is not a pass at the
    # settings the table claims. It is shown, marked, and NOT counted — hiding it would lose the
    # trial, and counting it would overstate the rate by exactly the help it got.
    demoted = set()
    if a.demote and os.path.exists(a.demote):
        demoted = {l.strip() for l in open(a.demote) if l.strip()}
    done = [t for t in order if t in rows]
    passed = [t for t in done if rew.get(t) == 1.0 and t not in demoted]

    tot = {"in": 0, "out": 0, "cached": 0, "usd": 0.0, "calls": 0.0, "turns": 0, "mins": 0.0}
    unknown_cost, any_shared, body = 0, False, []
    for t in done:
        r = rows[t]
        # Which series to difference. A trial records its backend only when it claimed one from a
        # pool; otherwise there was one backend and one series, and insisting on a name the trial
        # never had would report the whole run as costing nothing. With several series and no
        # recorded port there IS a real ambiguity, and then cost is left unknown rather than
        # guessed at.
        series = spend.get(r["port"])
        if not series:
            live = [(k, v) for k, v in spend.items() if v and v[-1][6] > v[0][6]]
            if len(live) == 1:
                series = live[0][1]
        others = [(o["t0"], o["t1"]) for k, o in rows.items() if k != t and o["port"] == r["port"]]
        s, shared = spend_between(series or [], r["t0"], r["t1"], others)
        usd = s["usd"] if s else None
        unknown_cost += usd is None
        any_shared = any_shared or shared
        tok = r["tokens"]
        for k in ("in", "out", "cached"):
            tot[k] += tok[k] or 0
        tot["turns"] += r["turns"]
        tot["calls"] += s["calls"] if s else 0
        tot["mins"] += r["mins"]
        tot["usd"] += usd or 0.0
        mark = "PASS" if rew.get(t) == 1.0 else ("TIME" if r["timeout"] else "FAIL")
        if mark == "PASS" and t in demoted:
            mark = "FAIL*"  # it passed; the settings that let it are gone, so the pass does not count
        body.append((t, mark,
                     r["mins"], r["turns"], s["calls"] if s else None, tok, usd, shared,
                     r["council"]))

    if a.csv:
        print("task,reward,minutes,turns,calls,in,out,cached,usd")
        for t, _m, mins, turns, calls, tok, usd, shared, _c in body:
            print(f"{t},{rew.get(t, '')},{mins:.0f},{turns},{'' if calls is None else f'{calls:.0f}'},"
                  f"{tok['in'] or ''},{tok['out'] or ''},{tok['cached'] or ''},"
                  f"{'' if usd is None else f'{usd:.3f}'}")
        return

    if a.markdown:
        print("| task | | min | turns | calls | in | cached | out | usd | council |")
        print("|---|---|---:|---:|---:|---:|---:|---:|---:|---|")
        for t, mark, mins, turns, calls, tok, usd, shared, council in body:
            n = lambda v: "?" if v is None else f"{v:,}"
            print(f"| {t} | {'**' + mark + '**' if mark != 'PASS' else mark} | {mins:.0f} | {turns} | "
                  f"{'?' if calls is None else f'{calls:.0f}'} | {n(tok['in'])} | {n(tok['cached'])} | "
                  f"{n(tok['out'])} | {'?' if usd is None else f'{usd:.2f}' + ('~' if shared else '')} | "
                  f"{council or '— none'} |")
        print(f"| **TOTAL {len(done)}/{len(order)}** | | {tot['mins']:.0f} | {tot['turns']} | "
              f"{tot['calls']:.0f} | {tot['in']:,} | {tot['cached']:,} | {tot['out']:,} | "
              f"{tot['usd']:.2f} | |")
    else:
        print(f"{'task':<34}{'':3}{'min':>5}{'turns':>6}{'calls':>7}{'in':>11}{'cached':>11}"
              f"{'out':>8}{'usd':>8}  council")
        print("-" * 100)
        for t, mark, mins, turns, calls, tok, usd, shared, council in body:
            n = lambda v, w: (f"{'?':>{w}}" if v is None else f"{v:>{w},}")
            print(f"{t:<34}{mark:>4}{mins:>5.0f}{turns:>6}"
                  f"{('?' if calls is None else f'{calls:.0f}'):>7}"
                  f"{n(tok['in'], 11)}{n(tok['cached'], 11)}{n(tok['out'], 8)}"
                  f"{'      ?' if usd is None else (f'{usd:>7.2f}' + ('~' if shared else ' '))}"
                  f"  {council or '— none'}")
        print("-" * 100)
        print(f"{'TOTAL ' + str(len(done)) + '/' + str(len(order)) + ' run':<34}{'':>4}"
              f"{tot['mins']:>5.0f}{tot['turns']:>6}{tot['calls']:>7.0f}{tot['in']:>11,}"
              f"{tot['cached']:>11,}{tot['out']:>8,}{tot['usd']:>8.2f}")

    rate = (100.0 * len(passed) / len(done)) if done else 0.0
    timeouts = sum(1 for r in body if r[1] == "TIME")
    # TIME is a label, not a third outcome. A trial killed at its own agent timeout did not do the
    # task, and the harness scores it zero — so it is a failure here too, and saying so on the line
    # that carries the rate stops a reader wondering whether the rate excluded them.
    print(f"\npass {len(passed)}/{len(done)} run = {rate:.1f}%"
          f"   ({len(order) - len(done)} of {len(order)} not yet run)")
    if timeouts:
        print(f"   {timeouts} of the {len(done) - len(passed)} failures are timeouts (TIME) — "
              f"counted as failures, not held aside")
    shown = sorted(t for t in demoted if rew.get(t) == 1.0 and t in rows)
    if shown:
        print(f"   FAIL* — passed, but under settings this run has dropped, so the pass does not "
              f"count: {', '.join(shown)}")
    if any_shared:
        print("~ apportioned: that trial shared a backend with another, and the ledger cannot say "
              "which call was whose — the interval was split equally among whoever was running.")
    if unknown_cost:
        print(f"? {unknown_cost} task(s) have no cost: no spend series covers their window, or the "
              "backend reports none.")
    if done:
        print(f"mean per task: {tot['usd'] / len(done):.2f} usd, {tot['in'] // len(done):,} in, "
              f"{tot['calls'] / len(done):.0f} calls")


if __name__ == "__main__":
    main()
