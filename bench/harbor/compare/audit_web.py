#!/usr/bin/env python3
"""Did any trial go and look up the answer -- over the wire, or from the box it was run in?

The dataset's tasks are public on GitHub, so a trial can find its own task page -- and does. Every
tool call magi makes is in the log, web calls included, so the check is mechanical rather than a
matter of trust. Scope matters: an audit that counts another run's calls is not this run's audit.
"""
import glob, os, re, sys

SCOPE = sys.argv[1] if len(sys.argv) > 1 else "jobs/*sonnet-one-*/*/agent/magi-stdout.txt"
# The dataset's own pages. A canary line inside the container is the task shipping its own file and
# is not a hit -- only a canary that arrived over the wire is.
# The dataset wears two names: the GitHub org is hyphenated (harbor-framework), the hub and the
# registry are not (harborframework.com). Matching only the first missed
# registry.harborframework.com/tasks/... entirely -- caught by hand on 2026-08-26, in a re-run of a
# task that had already been quarantined once for reaching.
DATASET = re.compile(r"(terminal-bench-2|harbor-?framework|tbench[^\s\"]*task)", re.I)
SOLUTION = re.compile(r"solution/solve|/solution/|solve\.sh", re.I)
CALL = re.compile(r'⚙ (websearch|webfetch) \{[^}]{0,240}')
SHELL = re.compile(r'⚙ bash \{"command": "[^"]{0,200}')
# The grader's own files. Every one of the 89 tasks ships tests/test_outputs.py and tests/test.sh,
# and some ship an answer alongside (correct_output.csv, weights_gtruth.pt). Reading one of those is
# reading the answer key, whether it arrived over the network or was already in the container.
# A project's own suite is not this: caffe's src/caffe/test/test_io.cpp and fix-code-vulnerability's
# /app/test/test_environ.py are the repository under repair, so the names below stay narrow.
# One task hands the grader to the agent on purpose: break-filter-js-from-html's Dockerfile has
# `COPY tests/test_outputs.py /app` and its instruction ends "You can run /app/test_outputs.py to
# verify." There the grader is the spec, not the answer, so reading it is doing as told.
BY_DESIGN = {"break-filter-js-from-html"}
GRADER = re.compile(r"test_outputs\.py|correct_output|expected_output|ground_?truth|_gtruth|"
                    r"/tests?/test\.sh|run-tests\.sh", re.I)

fetches = searches = 0
flagged = []
files = sorted(glob.glob(SCOPE))
for f in files:
    txt = open(f, errors="ignore").read()
    job = os.path.basename(os.path.dirname(os.path.dirname(os.path.dirname(f))))
    calls = CALL.findall(txt)
    fetches += txt.count("⚙ webfetch")
    searches += txt.count("⚙ websearch")
    hits = [c for c in re.findall(r'⚙ (?:websearch|webfetch) \{[^}]{0,240}', txt) if DATASET.search(c)]
    net = [c for c in SHELL.findall(txt) if re.search(r"curl|wget|git clone", c, re.I)
           and DATASET.search(c)]
    # Local reads count too: a grader file inside the container is one `read` away, and the web
    # audit alone would never see it.
    local = []
    for m in re.finditer(r'⚙ (?:read|grep|glob|list|bash|multiedit|edit) \{[^}]{0,240}', txt):
        if GRADER.search(m.group(0)):
            local.append(m.group(0))
    if any(t in job for t in BY_DESIGN):
        local = []          # the grader is shipped to this task on purpose
    if hits or net or local:
        flagged.append((job, hits + net + local))

print(f"감사 범위: {len(files)}개 런 · webfetch {fetches}회 · websearch {searches}회")
if not flagged:
    print("데이터셋의 과제·정답·채점기에 닿은 호출: 없음")
else:
    print(f"\n데이터셋의 과제·정답·채점기에 닿은 런 {len(flagged)}개 — 하나하나 읽으십시오:\n")
    for job, cs in flagged:
        worst = ("채점기 파일" if any(GRADER.search(c) for c in cs)
                 else "정답 파일" if any(SOLUTION.search(c) for c in cs) else "과제 페이지")
        print(f"  [{worst}] {job}")
        for c in cs[:3]:
            print(f"      {c[:150]}")
