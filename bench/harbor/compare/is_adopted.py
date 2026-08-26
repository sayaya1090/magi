#!/usr/bin/env python3
"""Exit 0 if the named task has a run the tables adopt, 1 otherwise.

A quarantined trial leaves a result.json behind and is still not an answer, so "the file exists"
is the wrong question for a worker deciding what is left to run.
"""
import os, sys
sys.path.insert(0, os.path.join(os.environ.get("REPO", "."), "scratchpad"))
import build_bench_page as B

task = sys.argv[1]
adopted = {r["task"] for r in B.pick(B.read_runs(), B.ledger())}
raise SystemExit(0 if task in adopted else 1)
