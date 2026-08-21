# Benchmarking magi on Terminal-Bench 2.1

What this measures, how to run it yourself, and what the numbers are.

[Terminal-Bench](https://www.tbench.ai) gives an agent a real terminal in a Docker container and a
task, then scores whether the task got done. It is a whole-agent benchmark: the loop, the tools,
the recovery and the model are all under test together, and a run says nothing about which of them
produced the result. That is why the tables below carry cost and call counts beside the verdict —
two agents that both pass are not equivalent if one spent five times as much getting there.

## What is under test

- **The loop**: magi's planner, tools, council and recovery, from a pinned binary.
- **The backend**: whatever `MAGI_BASE_URL` serves. The dollar column exists only when the backend
  meters itself and you sample that ledger (`spend_poll.sh`); a bare endpoint reports no cost and
  the column reads `?`.
- **The dataset**: `terminal-bench/terminal-bench-2-1`, 89 tasks.

## Prerequisites

- **Docker**, running, with enough room for the concurrency you ask for. Terminal-Bench launches a
  container per trial and they share what the VM has; `docker info` tells you what that is.
- **[Harbor](https://www.tbench.ai)**, the Terminal-Bench runner (`harbor run`).
- **magi binaries for the container** — `magi-arm64` and/or `magi-amd64` in one directory. The
  adapter uploads them into each container and installs them there; nothing is built inside.
- **A backend the containers can reach.** It runs on the HOST or anywhere else reachable; the
  containers only ever speak HTTP to it, which is why the images need nothing added to them. Give
  `BASE_URL` the address as the CONTAINER sees it (`host.docker.internal`, not `localhost`).

Check it answers before starting an eighty-nine task run, not after:

```sh
curl -s "$BASE_URL/models" | head -c 200
```

## Running it

```sh
# The magi binaries the container will run (magi-arm64 / magi-amd64).
export BINARY=/tmp/magi-serve
export BASE_URL=http://host.docker.internal:11434/v1

# All 89 tasks. The argument is how many run concurrently.
MODEL=qwen3-coder:30b bench/harbor/run.sh 1
```

For a cost column, sample the backend's spend ledger throughout the run and hand the series to the
report:

```sh
LEDGERS="/path/to/backend-config-dir" PLUGIN=<plugin> bench/harbor/spend_poll.sh 5 &
SPEND=bench/harbor/state/spend.tsv MODEL=... bench/harbor/run.sh 1
```

The run prints the table when it finishes. Ask for it again at any time:

```sh
python3 bench/harbor/report.py --jobs-glob 'jobs/*tb21*'
python3 bench/harbor/report.py --jobs-glob 'jobs/*tb21*' --markdown   # for a document
```

## Reading the table

| column | what it is |
|---|---|
| `min` | wall clock for the trial, container setup and verification included |
| `turns` | user prompts magi answered — a whole task is usually **one** |
| `calls` | LLM requests the backend actually served for it, which is the number that scales |
| `in` / `cached` / `out` | magi's own token accounting, from the trial's `turn.finished` |
| `usd` | the backend's ledger, differenced across the trial's window; `~` means it was shared |
| `council` | the finish gate's tally, `done 3-0`. `— none` means the turn never declared finished |

`turns` and `calls` differ by more than an order of magnitude and both are real: one task, one
prompt, forty model calls inside it. A trial killed by the agent timeout never reaches
`turn.finished`, so its input reads `?` rather than `0` — a zero would be a claim.

## The one thing that will bite you

**Concurrency can move the score, and only downward.** Trials share the Docker VM's CPU and memory.
On a 7-CPU, 8 GB VM, four trials get 1.75 cores and 2 GB each — and a task that compiles or trains
something then times out for a reason that has nothing to do with the agent. The distortion runs one
way: starvation can turn a pass into a timeout and never the reverse. Check `docker info` before
raising it, and compare a score only against runs given the same room.

**Concurrency also costs you the cost column.** Attribution works by differencing a backend's ledger
across a trial's window, and nothing on the wire says which trial a call came from — the task name
never crosses an OpenAI-compatible endpoint. One trial at a time, that difference has only one
candidate and the figure is exact. Several at once and it is a share-out, which `report.py` marks
with `~` rather than passing off as a measurement.

> The exactness is recoverable if you happen to run **several metered backends**, one per trial:
> `BACKEND_PORTS=58411,58412,…` makes each trial claim one endpoint for its duration (a lock file
> per port; the claimed one is written to the trial's `agent/backend-port.txt`, which is the series
> `report.py` differences). One backend serving everything — the ordinary case — cannot be split
> this way, and the `~` is the honest answer.

## Isolation, and how cheating was ruled out

A benchmark whose tasks are public on GitHub has two ways to be wrong: a trial can learn from an
earlier trial, or the agent can look the answer up. Neither is prevented by hoping.

**Nothing carries between trials.** Each task gets a fresh container from its own image. Exactly one
thing crosses in from the host — the magi binary, uploaded and `install`ed, with no network used so
that in-container sabotage of curl/wget cannot fail the install. No host directory is mounted.
`MAGI_DATA_DIR` points inside the container, and the adapter sets no experience, memory or skills
directory at all, so magi's store lives and dies with the container. Nothing about one task is in
the prompt of the next: every trial opens a new session and renders its history from scratch.

**The containers do have network access.** Many tasks need it: package installs, source downloads,
dataset fetches. So answer-lookup is possible in principle and has to be checked rather than assumed
away. Every tool call magi makes is in the event log, web calls included, which makes the check
mechanical:

```sh
set -- jobs/*tb21*/*__*/agent/magi-stdout.txt   # scope it to the run you are auditing
grep -ohE '⚙ (websearch|webfetch)' "$@" | sort | uniq -c
grep -ohE '⚙ (websearch|webfetch) \{[^}]{0,200}' "$@"        # read every one of them
grep -ohE '⚙ bash \{"command": "[^"]{0,160}' "$@" | grep -iE 'curl|wget|git clone'
grep -oih 'terminal-bench' "$@" | wc -l
```

Scope matters: an unscoped glob sweeps every job the repo has ever collected, and an audit that
counts other runs' calls is not an audit of this one.

The audit for the run below is not written yet — it belongs after the last task, not partway
through, because a check over twenty tasks says nothing about the sixty-nine that follow. When the
run completes, this paragraph will carry the counts those commands return, every web call quoted
and read, and the same statement for the delegated subagents' own session stores, which do not
appear in the parent transcript and would make a parent-only check a partial one.

**The backend's own settings apply identically to every trial.** Reasoning effort and the like are
properties of the endpoint, not of any one task, so they are a property of the RUN — stated with the
results below rather than left to be inferred from the numbers.

## Results

magi on `claude-sonnet-5`, reasoning effort HIGH (a setting of the backend, not pinned by a magi
flag), `MAGI_COLLAPSE_REPEATS=0`.

**The run is still going, and this section is deliberately empty rather than partial.** A pass rate
over part of a dataset is not a smaller version of the real one — it is a different number, and
publishing it invites the comparison it cannot support. The table lands here when all 89 tasks have
a result, generated by:

```sh
python3 bench/harbor/report.py --jobs-glob 'jobs/*tb21*' --markdown
```

| | |
|---|---|
| pass rate | _pending_ |
| total cost | _pending_ |
| mean per task | _pending_ |
| concurrency | _pending_ |

For comparison, the same loop on a local `qwen3.8:27b-mlx` over a 14-task subset scored 9/14 at
every reasoning effort that was tried.
