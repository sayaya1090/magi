# Benchmarking magi on Terminal-Bench 2.1

What this measures, how to run it yourself, and what the numbers are.

[Terminal-Bench](https://www.tbench.ai) gives an agent a real terminal in a Docker container and a
task, then scores whether the task got done. It is a whole-agent benchmark: the loop, the tools,
the recovery and the model are all under test together, and a run says nothing about which of them
produced the result. That is why the tables below carry token and call counts beside the verdict —
two agents that both pass are not equivalent if one took five times the work to get there.

## What is under test

- **The loop**: magi's planner, tools, council and recovery, from a pinned binary.
- **The backend**: whatever `MAGI_BASE_URL` serves. The dollar column exists only when the backend
  reports what it charges and you feed those totals to the report (`--spend`); a bare endpoint
  reports no cost and the column reads `?`.
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

`host.docker.internal` resolves **inside** a container, not on the host, and on Docker Desktop
only — on plain Linux Docker the container reaches the host at the bridge address (`172.17.0.1`) or
whatever `--add-host` you have arranged. So check the backend from the host at its host-side address
before starting an eighty-nine task run, not after:

```sh
curl -s http://localhost:11434/v1/models | head -c 200   # same backend, host's own view
```

## Running it

```sh
# The magi binaries the container will run (magi-arm64 / magi-amd64).
export BINARY=/tmp/magi-serve
export BASE_URL=http://host.docker.internal:11434/v1

# All 89 tasks. The argument is how many run concurrently.
MODEL=qwen3-coder:30b bench/harbor/run.sh 1
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
| `usd` | the backend's own ledger, differenced across the trial's window; `?` unless you supply one (below), `~` if it was shared. It is whatever your backend reports, not a figure derived from a price list — see the note under Results |
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

**Where a dollar figure comes from at all.** magi does not meter your backend and cannot: an
OpenAI-compatible endpoint reports what it charges only if it chooses to. So the column is fed from
a side channel you provide — `SPEND=<file> bench/harbor/run.sh`, a TSV sampled throughout the run,
one line per sample, each field a running total for that backend:

```
epoch  port  calls  in  out  cache_read  cache_write  usd
```

Sample it every few seconds, so every trial's window has a sample on each side of it. Anything that
can read your backend's own accounting can write those lines; **nothing that ships here does**, so
for most readers the column reads `?` — and the run is still perfectly valid, because the pass rate,
turns, calls and minutes do not depend on it. The dollar figures in the results below came from a
backend that kept its own ledger.

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

**A trial that went looking for the answer is re-run on its own.** The check above is not
decorative: a trial can and does find the benchmark's own task pages on GitHub, and once it has, its
verdict is not evidence about the agent. So any trial whose web calls reached the dataset's task or
solution pages is quarantined — the result stands in no table on its own, the task is queued again
after the last of the eighty-nine, and **the re-run is the result that counts**. Which tasks those
were is named with the results rather than folded silently into the total, because a reader cannot
audit what a table does not mention.

**The audit of the run below.** Across the 89 trials the agent made 41 web fetches and 18 web
searches, and every one was read. Two of them reached the dataset's own pages on GitHub:
`mteb-leaderboard` fetched that task's README, and `qemu-alpine-ssh` fetched an issue thread while
diagnosing a translation fault. Neither is a solution file and neither changed a verdict — the
first passes, the second fails for the reason named above.

Two earlier trials did reach solution files, which is why the quarantine rule exists rather than
being hypothetical. `extract-elf` fetched a `solution/solve.sh` and `regex-chess` decoded a
solution blob from a third-party mirror of the dataset. Both were re-run, and **neither re-run made
any such fetch**: `extract-elf` passed again in fewer calls than the quarantined attempt (20 against
29), and `regex-chess` passed unaided. The re-runs are the results in the table.

The check is complete rather than partial, and that is measurable: this run made **zero** `spawn`,
`meeting` and `hand_off` calls, so no delegated subagent kept a session store of its own and the
parent transcripts are the entire record of what the agent did.

**The backend's own settings apply identically to every trial.** Reasoning effort and the like are
properties of the endpoint, not of any one task, so they are a property of the RUN — stated with the
results below rather than left to be inferred from the numbers.

## Results

magi on `claude-sonnet-5`, reasoning effort HIGH (a setting of the backend, not pinned by a magi
flag), `MAGI_COLLAPSE_REPEATS=0`, one attempt per task, one or two trials at a time.

| | |
|---|---|
| pass rate | **65 / 89 = 73.0%** |
| of the 24 that did not pass | 11 failed a verifier, 13 hit the agent timeout |
| wall clock | 21.2 hours, 14.3 min per task |
| model calls | 3,081 total, 35 per task |
| input tokens | 85.5M, of which 64.8M were cache reads |
| output tokens | 3.3M |

Regenerate it with:

```sh
python3 bench/harbor/report.py --jobs-glob 'jobs/*tb21*' --markdown
```

**No dollar figure is published here, and that is deliberate.** The backend kept its own cost
accounting and magi recorded it, but those numbers do not reconcile with the model's published
per-token rates — they run well above the highest listed rate for the same token counts. A figure
we cannot derive from the tokens and a price list is not a measurement anyone can check, so the
tokens are reported and the dollars are not. Cost the run against your own backend's rates if you
need a number.

**Two things about this run that a reader should weigh.** It is ONE attempt per task, where the
published leaderboard entries are five, so its pass rate carries a much wider error bar than the
±1.6% those entries quote. And every task ran an amd64 image on an arm64 host under translation:
one failure (`qemu-alpine-ssh`) is a translation fault that cannot happen on a native amd64 machine,
and the compute-heavy timeouts had a handicap that a native run would not.

For comparison, the same loop on a local `qwen3.8:27b-mlx` over a 14-task subset scored 9/14 at
every reasoning effort that was tried.
