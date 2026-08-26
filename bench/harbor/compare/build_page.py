#!/usr/bin/env python3
"""Rebuild the magi-vs-Claude-Code artifact from whatever has been measured so far.

Reads the job directories directly rather than a tally kept by hand, so a run that lands while
the page is being written is in the page. Two conventions matter and both are the leaderboard's:

  * a trial killed by quota is not a result -- it is dropped, and the task falls back to its
    newest run that was not, or goes unmeasured;
  * a trial killed by the wall clock is billed at zero, because the leaderboard leaves the cost
    column blank on its own timeout rows and the two columns have to mean the same thing.
"""
import json, os, re, glob, html, importlib, subprocess, sys

# The data (leaderboard tsv, quarantine ledger, templates) sits beside this script; the job
# directories it scans sit at the repository root. Holding the two apart is what makes the same
# page come out no matter where it is run from.
HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
ARCDIR = os.path.join(HERE, "..", "runs", "tb21-2026-08")
REPO = os.path.dirname(os.path.dirname(os.path.dirname(HERE)))
# Language is an argument. There is one set of numbers and only the sentences fork, so the two
# pages say the same measurement.
LANG = "ko"
_args = [a for a in sys.argv[1:] if not a.startswith("--")]
for a in sys.argv[1:]:
    if a.startswith("--lang="):
        LANG = a.split("=", 1)[1]
if LANG not in ("ko", "en"):
    sys.exit("--lang must be ko or en")
_t = importlib.import_module("text_" + LANG)
NOTES, WEBWHAT, REWARD_HACK, UI = _t.NOTES, _t.WEBWHAT, _t.REWARD_HACK, _t.UI
OUT = _args[0] if _args else os.path.join(HERE, "bench.%s.html" % LANG)
CONTAMINATED = {"NonZeroAgentExitCodeError", "NetworkConnectionError", "CancelledError"}



def read_runs():
    """Scan the job directories; fall back to the committed archives when there are none.

    The archives hold only adopted trials, so the quarantine and ADOPT rules have nothing left to
    act on and both paths land on the same page. jobs/ wins when it is there, so a re-run shows up.
    """
    runs = [r for r in _runs_from_jobs()]
    return runs or _runs_from_archives()

def _runs_from_archives():
    import tarfile
    runs = []
    man = os.path.join(ARCDIR, "manifest.json")
    if not os.path.exists(man):
        return runs
    for task, meta in json.load(open(man)).items():
        day, hhmm = meta["run"].rsplit("-", 1)
        tgz = os.path.join(ARCDIR, task + ".tar.gz")
        if not os.path.exists(tgz):
            continue
        with tarfile.open(tgz) as tf:
            def grab(suffix):
                for m in tf.getmembers():
                    if m.name.endswith(suffix):
                        f = tf.extractfile(m)
                        return f.read().decode("utf-8", "replace") if f else ""
                return ""
            try:
                r = json.loads(grab("/result.json"))
            except Exception:
                continue
            rw = ((r.get("verifier_result") or {}).get("rewards") or {}).get("reward")
            if rw is None:
                continue
            txt = grab("agent/magi-stdout.txt")
            runs.append(dict(task=task, day=day, hhmm=hhmm, ok=rw == 1.0,
                             exc=(r.get("exception_info") or {}).get("exception_type") or "",
                             unverified="landed UNVERIFIED" in txt,
                             calls=txt.count("\n⚙ "), cn=txt.count("⚖ council:")))
    return runs

def _runs_from_jobs():
    runs = []
    for rj in glob.glob(os.path.join(REPO, "jobs", "*-sonnet-one-*", "*", "result.json")):
        d = os.path.dirname(rj)
        m = re.match(r"(\d{4}-\d{2}-\d{2})-(\d{4})-sonnet-one-(.+)$", os.path.basename(os.path.dirname(d)))
        if not m:
            continue
        day, hhmm, task = m.groups()
        try:
            r = json.load(open(rj))
        except Exception:
            continue
        rw = ((r.get("verifier_result") or {}).get("rewards") or {}).get("reward")
        if rw is None:
            continue
        exc = (r.get("exception_info") or {}).get("exception_type") or ""
        s = os.path.join(d, "agent/magi-stdout.txt")
        txt = open(s, errors="ignore").read() if os.path.exists(s) else ""
        runs.append(dict(task=task, day=day, hhmm=hhmm, ok=rw == 1.0, exc=exc,
                         unverified="landed UNVERIFIED" in txt,
                         calls=txt.count("\n⚙ "), cn=txt.count("⚖ council:")))
    return runs

def ledger():
    """(task, hhmm) -> (usd, secs).

    Read the run logs when they are present, otherwise the frozen ledger.tsv. The logs are not in
    the repository, so rebuilding from a clone always takes the second path.
    """
    logs = sorted(glob.glob(os.path.join(REPO, "scratchpad", "*.log")))
    if logs:
        return _ledger_from_logs(logs)
    out = {}
    for ln in open(os.path.join(HERE, "ledger.tsv")):
        if ln.startswith("#") or not ln.strip():
            continue
        task, hhmm, usd, secs = ln.rstrip("\n").split("\t")
        out[(task, hhmm)] = (float(usd), int(secs))
    return out

def tokens():
    """(task, hhmm) -> [calls, in, out, cache_read, cache_write, usd] spent by that run.

    Same source as the ledger, one column wider. Absent from a clone, where the run logs are not
    committed -- the markdown emitter is a maintainer's tool, not part of rebuilding the page.
    """
    out, cur = {}, None
    for f in sorted(glob.glob(os.path.join(REPO, "scratchpad", "*.log"))):
        for ln in open(f, errors="ignore"):
            m = re.search(r"\[(\d\d):(\d\d):\d\d\] === sonnet .* (\S+) · loop ([0-9a-f]+)", ln)
            if m:
                cur = dict(hhmm=m.group(1) + m.group(2), task=m.group(3), before=None)
                continue
            m = re.search(r"ledger (before|after)\s*:"
                          r"\s*(?:calls in out cache_read cache_write usd =)?\s*([\d. ]+)$", ln)
            if m and cur:
                v = [float(x) for x in m.group(2).split()]
                if m.group(1) == "before":
                    cur["before"] = v
                elif cur["before"] and len(v) == len(cur["before"]):
                    out[(cur["task"], cur["hhmm"])] = [a - b for a, b in zip(v, cur["before"])]
                    cur = None
    return out

def _ledger_from_logs(logs):
    """(task, hhmm) -> (usd, secs), from the per-run logs the harness wrote as it went."""
    out, cur = {}, None
    for f in logs:
        for ln in open(f, errors="ignore"):
            m = re.search(r"\[(\d\d):(\d\d):\d\d\] === sonnet .* (\S+) · loop ([0-9a-f]+)", ln)
            if m:
                cur = dict(hhmm=m.group(1) + m.group(2), task=m.group(3), before=None, t0=None)
                continue
            m = re.search(r"\[(\d\d):(\d\d):(\d\d)\] ledger (before|after)\s*:"
                          r"\s*(?:calls in out cache_read cache_write usd =)?\s*([\d. ]+)$", ln)
            if m and cur:
                usd = float(m.group(5).split()[-1])
                sec = int(m.group(1)) * 3600 + int(m.group(2)) * 60 + int(m.group(3))
                if m.group(4) == "before":
                    cur["before"], cur["t0"] = usd, sec
                else:
                    if cur["before"] is not None:
                        d = sec - cur["t0"]
                        out[(cur["task"], cur["hhmm"])] = (round(usd - cur["before"], 4),
                                                          d + 86400 if d < 0 else d)
                    cur = None
    return out

def quarantined():
    """Trials whose web calls reached the dataset's own task or solution pages.

    A verdict reached after finding the answer is not evidence about the agent, so the trial is
    held out of every table and the task is queued again -- and it is the re-run that counts.
    The hold is on the one trial, not the task, so a clean re-run is picked up on its own.
    """
    out = {}
    f = os.path.join(HERE, "quarantine.tsv")
    if not os.path.exists(f):
        return out
    for ln in open(f):
        if ln.startswith("#") or not ln.strip():
            continue
        task, day, hhmm, why = ln.rstrip("\n").split("\t")
        out[(task, day, hhmm)] = why
    return out

# A task whose answer is a famous public repository can defeat the re-run rule outright: every
# attempt reaches it, so there is never a clean trial to adopt and the row would vanish. Naming the
# run the table uses keeps the row -- and the caveat beside it does the work the hold cannot.
# regex-chess: eight attempts on 2026-08-26 each fetched the answer file; 1310 is the last one that
# ran to completion, so it is what the table carries, with its limitation stated in WEBWHAT.
ADOPT = {"regex-chess": ("2026-08-26", "1310")}


def pick(runs, led):
    # A held trial takes everything before it with it. Falling back to an older run would put a
    # number in the table that the rule never adopted -- only a re-run, which is later, counts.
    held = quarantined()
    cut = {}
    for (task, day, hhmm) in held:
        k = day + hhmm
        cut[task] = max(cut.get(task, ""), k)
    runs = [r for r in runs
            if r["task"] in ADOPT or r["day"] + r["hhmm"] > cut.get(r["task"], "")]
    # A pinned task keeps exactly the named run, so neither a later abandoned attempt nor a hold
    # on an earlier one can move it.
    runs = [r for r in runs
            if r["task"] not in ADOPT or (r["day"], r["hhmm"]) == ADOPT[r["task"]]]
    by = {}
    for r in runs:
        by.setdefault(r["task"], []).append(r)
    sel = []
    for t, rs in by.items():
        rs.sort(key=lambda x: (x["day"], x["hhmm"]))
        ok = [r for r in rs if r["exc"] not in CONTAMINATED]
        if not ok:
            continue
        r = ok[-1]
        r["usd"], r["secs"] = led.get((t, r["hhmm"]), (None, None))
        if r["usd"] is None:
            continue
        sel.append(r)
    return sorted(sel, key=lambda r: r["task"])

# Every row links to that trial's own archive. The link points at the repository's copy rather
# than anything the page could hold itself, so it resolves once the commit is pushed.
RUNBASE = "https://github.com/sayaya1090/magi/raw/main/bench/harbor/runs/tb21-2026-08/"

def archive_sizes():
    """task -> "57 KB". Empty when the archive is not there yet."""
    out = {}
    for name in os.listdir(ARCDIR) if os.path.isdir(ARCDIR) else []:
        if name.endswith(".tar.gz"):
            n = os.path.getsize(os.path.join(ARCDIR, name))
            out[name[:-7]] = f"{n/1024:.0f} KB" if n < 1024 * 1024 else f"{n/1024/1024:.1f} MB"
    return out

# The docs carry the same numbers in prose. Emitting them from here rather than transcribing them
# is what stops the two drifting -- which they did once, leaving BENCHMARK.md quoting a pass rate
# from an earlier run for weeks.
def markdown(rows, tok):
    N = len(rows)
    passed = [r for r in rows if r["ok"]]
    to = [r for r in rows if not r["ok"] and r["exc"] == "AgentTimeoutError"]
    uv = [r for r in rows if not r["ok"] and r["unverified"] and r not in to]
    hacked = [r for r in rows if r["t"] in REWARD_HACK]
    wrong = [r for r in rows if not r["ok"] and r not in to and r not in uv and r not in hacked]
    g = lambda i: sum(tok.get(r["key"], [0] * 6)[i] for r in rows)
    secs = sum(r["secs"] for r in rows)
    out = ["| | |", "|---|---|",
           f"| pass rate | **{len(passed)} / {N} = {len(passed)/N*100:.1f}%** |",
           f"| of the {N-len(passed)} that did not pass | {len(to)} hit the agent timeout, "
           f"{len(wrong)} exited cleanly and wrong, {len(uv)} landed UNVERIFIED, "
           f"{len(hacked)} was rescored to zero |",
           f"| wall clock | {secs/3600:.1f} hours, {secs/N/60:.1f} min per task |",
           f"| model calls | {int(g(0)):,} total, {g(0)/N:.0f} per task |",
           f"| tool calls | {sum(r['calls'] for r in rows):,} total, "
           f"{sum(r['cn'] for r in rows)} council rounds |",
           f"| input tokens | {g(1)/1e6:.1f}M, of which {g(3)/1e6:.1f}M were cache reads |",
           f"| output tokens | {g(2)/1e6:.2f}M |", ""]
    out += ["| task | | calls | council | tokens in | cache read | tokens out |",
            "|---|---|---:|---:|---:|---:|---:|"]
    for r in sorted(rows, key=lambda x: x["t"]):
        v = tok.get(r["key"], [0] * 6)
        mark = "✅ PASS" if r["ok"] else ("⏱ TIME" if r["exc"] == "AgentTimeoutError"
                                         else ("♻ RESCORED" if r["t"] in REWARD_HACK else "❌ FAIL"))
        out.append(f"| `{r['t']}` | {mark} | {r['calls']} | {r['cn']} | "
                   f"{int(v[1]):,} | {int(v[3]):,} | {int(v[2]):,} |")
    return "\n".join(out)

def main():
    cc = {}
    for ln in open(os.path.join(HERE, "cc89.tsv")):
        t, p, n, usd, b = ln.rstrip("\n").split("\t")
        cc[t] = dict(p=int(p), n=int(n), usd=float(usd), b=int(b))
    sel = [r for r in pick(read_runs(), ledger()) if r["task"] in cc]
    rows = [dict(t=r["task"], ok=r["ok"] and r["task"] not in REWARD_HACK,
                hack=REWARD_HACK.get(r["task"], ""), calls=r["calls"], cn=r["cn"], usd=round(r["usd"], 2),
                 secs=r["secs"], exc=r["exc"], head="f4c94d00",
                 ccp=cc[r["task"]]["p"], ccn=cc[r["task"]]["n"],
                 ccusd=round(cc[r["task"]]["usd"] / cc[r["task"]]["n"], 2),
                 unverified=r["unverified"],
                 ccblank=cc[r["task"]]["b"]) for r in sel]

    N = len(rows)
    bill = lambda r: 0.0 if r["exc"] == "AgentTimeoutError" else r["usd"]
    mp = sum(1 for r in rows if r["ok"])
    mu, mdrop = sum(bill(r) for r in rows), sum(r["usd"] - bill(r) for r in rows)
    ccp = sum(r["ccp"] for r in rows); ccn = sum(r["ccn"] for r in rows)
    ccu = sum(r["ccusd"] for r in rows); ccb = sum(r["ccblank"] for r in rows)
    hrs = sum(r["secs"] for r in rows) / 3600

    def card(r, good=False):
        note = NOTES.get(r["t"])
        if not note and good:
            # A task only magi solved is not a failure, and must not borrow a failure's wording.
            note = (UI["only_pass"]
                    % (r["calls"], r["cn"], r["secs"] // 60, r["secs"] % 60, r["usd"]))
        if not note:
            if r.get("unverified"):
                why = UI["why_unverified"]
            elif r["exc"] == "AgentTimeoutError":
                why = UI["why_timeout"]
            else:
                why = UI["why_wrong"]
            note = UI["fail_tail"].format(why=why, calls=r["calls"], cn=r["cn"],
                                          mins=r["secs"] // 60, secs=r["secs"] % 60)
        style = ' style="border-left-color:var(--pass)"' if good else ''
        return ('<div class="fc"%s><div class="n">%s</div><div class="w">%s</div>'
                '<div class="cc">CC %d/%d</div></div>'
                % (style, html.escape(r["t"]), note, r["ccp"], r["ccn"]))

    # Name the held tasks rather than folding them into the total -- a reader cannot audit what
    # the table does not mention.
    held = sorted({t for (t, _, _) in quarantined()})
    waiting = sorted(t for t in held if t not in {r["t"] for r in rows})
    qline = ""
    if waiting:
        names = " · ".join("<code>%s</code>" % t for t in waiting)
        qline = UI["quarantine"] % names
    # magi has websearch/webfetch; the leaderboard's Claude Code has neither (sampled trials show
    # Bash/Read/Edit only, with curl going to localhost). That is a real difference in what the two
    # could do, so the tasks where magi actually used the web are named rather than left implicit.
    used = []
    for r in sel:
        g = glob.glob(os.path.join(REPO, "jobs",
                                   "%s-%s-sonnet-one-%s" % (r["day"], r["hhmm"], r["task"]),
                                   "*", "agent", "magi-stdout.txt"))
        if not g:
            continue
        txt = open(g[0], errors="ignore").read()
        n = txt.count("⚙ webfetch") + txt.count("⚙ websearch")
        if n:
            used.append((r["task"], n, cc[r["task"]]["p"], cc[r["task"]]["n"]))
    webline = ""
    if used:
        items = "".join(
            "<li><code>%s</code> — %s</li>" % (t, WEBWHAT.get(t, UI["webwhat_missing"]))
            for t, _, _, _ in sorted(used))
        ccw = json.load(open(os.path.join(HERE, "cc_web.json")))
        ccper = ccw["per"]
        both = sorted(t for t, _, _, _ in used if ccper.get(t))
        items = "".join(
            "<li><code>%s</code>%s — %s</li>" % (
                t, (UI["cc_also"] % ccper[t]) if ccper.get(t) else "",
                WEBWHAT.get(t, UI["webwhat_missing"]))
            for t, _, _, _ in sorted(used))
        webline = ((UI["web_head"] % (len(rows), len(used), len(both)))
                   + '<ul style="margin:8px 0 0;padding-left:18px">%s</ul>' % items
                   + UI["web_tail"])
    for r, src in zip(rows, sorted(sel, key=lambda x: x["task"])):
        r["key"] = (src["task"], src["hhmm"])
    if "--markdown" in sys.argv:
        print(markdown(rows, tokens()))
        return
    arc = archive_sizes()
    for r in rows:
        r["arc"] = arc.get(r["t"], UI["arc_missing"])
    fails = [r for r in rows if not r["ok"]]
    only = [r for r in rows if r["ok"] and r["ccp"] == 0]
    tpl = open(os.path.join(HERE, "page.%s.html" % LANG)).read()
    page = (tpl
      .replace("{{ROWS}}", json.dumps(rows, ensure_ascii=False, separators=(",", ":")))
      .replace("{{N}}", str(N))
      .replace("{{MPASS}}", str(mp)).replace("{{MPCT}}", f"{mp/N*100:.1f}")
      .replace("{{MUSD}}", f"{mu:,.2f}").replace("{{MDROP}}", f"{mdrop:,.2f}")
      .replace("{{NTO}}", str(sum(1 for r in rows if r["exc"] == "AgentTimeoutError")))
      .replace("{{CCPASS}}", str(ccp)).replace("{{CCN}}", str(ccn))
      .replace("{{CCPCT}}", f"{ccp/ccn*100:.1f}").replace("{{CCUSD}}", f"{ccu:,.2f}")
      .replace("{{CCBLANK}}", str(ccb))
      .replace("{{RATIO}}", f"{mu/ccu:.2f}" if ccu else "—")
      .replace("{{COVPCT}}", f"{N/89*100:.0f}").replace("{{HRS}}", f"{hrs:.1f}")
      .replace("{{FAILCARDS}}", "\n    ".join(card(r) for r in fails))
      .replace("{{NFAIL}}", str(len(fails)))
      .replace("{{ONLYCARDS}}", "\n    ".join(card(r, True) for r in only))
      .replace("{{NONLY}}", str(len(only)))
      .replace("{{QUARANTINE}}", qline)
      .replace("{{WEBTOOLS}}", webline)
      .replace("{{RUNBASE}}", RUNBASE)
      .replace("{{STAMP}}", subprocess.run(["date", "+%m-%d %H:%M"], capture_output=True,
                                           text=True).stdout.strip()))
    open(OUT, "w").write(page)
    print(f"{N} tasks · magi {mp}/{N} = {mp/N*100:.1f}% ${mu:.2f} "
          f"(timeouts {mdrop:.2f} excluded) · CC {ccp}/{ccn} = {ccp/ccn*100:.1f}% ${ccu:.2f} · {mu/ccu:.2f}×")


if __name__ == "__main__":
    main()
