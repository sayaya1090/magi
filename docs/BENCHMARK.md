# Benchmarking magi on Terminal-Bench 2.1

What this measures, how to run it yourself, and what the numbers are.

[Terminal-Bench](https://www.tbench.ai) gives an agent a real terminal in a Docker container and a
task, then scores whether the task got done. It is a whole-agent benchmark: the loop, the tools,
the recovery and the model are all under test together, and a run says nothing about which of them
produced the result. That is why the tables below carry cost and call counts beside the verdict —
two agents that both pass are not equivalent if one spent five times as much getting there.

## What is under test

- **The loop**: magi's planner, tools, council and recovery, from a pinned binary.
- **The backend**: whatever `MAGI_BASE_URL` serves. The runs recorded here use the Claude Code CLI
  as a backend through magi's `claudecode` shim, which is why they have a dollar column at all — a
  bare OpenAI-compatible endpoint reports no cost and the column reads `?`.
- **The dataset**: `terminal-bench/terminal-bench-2-1`, 89 tasks.

## Prerequisites

- **Docker**, running, with enough room for the concurrency you ask for. Terminal-Bench launches a
  container per trial and they share what the VM has; `docker info` tells you what that is.
- **[Harbor](https://www.tbench.ai)**, the Terminal-Bench runner (`harbor run`).
- **magi binaries for the container** — `magi-arm64` and/or `magi-amd64` in one directory. The
  adapter uploads them into each container and installs them there; nothing is built inside.
- **magi on this machine**, if you use `SHIM=` — the pool is magi daemons hosting the backend shim.
  `SHIM_BIN` names it; the default is `magi` on `PATH`.
- **The CLI that shim wraps, installed and signed in on THIS machine.** `SHIM=claudecode` runs
  `claude --print` once per model turn, so the host needs a working `claude`; `SHIM=codex` needs
  `codex`. The CLI runs on the HOST, never inside a task container — the containers only ever speak
  HTTP to the shim — which is why the images need nothing added to them.

Check the last one before starting an eighty-nine task run, not after:

```sh
claude --version && claude --print 'reply with: ok' --model sonnet
```

## Running it

```sh
# The magi binaries the container will run (magi-arm64 / magi-amd64).
export BINARY=/tmp/magi-serve

# All 89 tasks. The argument is how many run concurrently.
SHIM=claudecode bench/harbor/run.sh 1
```

`SHIM=<plugin>` starts **one backend per trial** — a magi daemon serving that plugin's loopback
shim, each in its own directory — points the containers at them, samples their spend ledgers, and
stops all of it when the run ends. That is what keeps per-task cost exact at any concurrency, and
it is why the pool is sized from the argument rather than configured:

```sh
SHIM=claudecode bench/harbor/run.sh 4      # four trials, four backends
```

Any OpenAI-compatible endpoint works too; then there is no cost column, because a plain endpoint
reports no spend:

```sh
BASE_URL=http://host.docker.internal:11434/v1 MODEL=qwen3-coder:30b bench/harbor/run.sh 1
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

Cost, by contrast, survives concurrency here, and that is not luck. Attribution works by
differencing a backend's ledger across a trial's window, and nothing on the wire says which trial a
call came from — the task name never crosses an OpenAI-compatible endpoint. So `SHIM=` sizes the
pool from the concurrency argument and each trial claims a backend to itself: one trial per ledger,
one ledger per window, exact at any concurrency. The share-out only appears if you bring your own
backend and run several trials against it, and then `report.py` marks those rows with `~` rather
than passing a share-out off as a measurement.

## Isolation, and how cheating was ruled out

A benchmark whose tasks are public on GitHub has two ways to be wrong: a trial can learn from an
earlier trial, or the agent can look the answer up. Neither is prevented by hoping.

**Nothing carries between trials.** Each task gets a fresh container from its own image. Exactly one
thing crosses in from the host — the magi binary, uploaded and `install`ed, with no network used so
that in-container sabotage of curl/wget cannot fail the install. No host directory is mounted.
`MAGI_DATA_DIR` points inside the container, and the adapter sets no experience, memory or skills
directory at all, so magi's store lives and dies with the container. The backend shim keeps a resume
anchor, but resuming requires the rendered history to be a strict prefix extension of what it
already sent — two different tasks never match, so no session is ever inherited.

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

**One thing is inherited on purpose, and it is not per-task.** The Claude Code CLI reads the
operator's `~/.claude/settings.json`, which is where the HIGH reasoning effort in the results below
comes from — the shim passes no `--effort`, and the neutral working directory isolates the
*workspace*, not user-level settings. That applies identically to every trial, so it is a property
of the run rather than a leak between tasks, and it is stated with the results. User-level skills, memory and a
`CLAUDE.md` do not reach the model, and that is measurable rather than argued: a minimal turn
through the running backend bills **327 input tokens**, of which roughly 76 are magi's own wrapper —
the banner, `The conversation so far:`, the role markers. The CLI's scaffold is 37,659 tokens when
nothing suppresses it; nothing of that size is in 327.

The same probe answers the other half. Sent to a shim that had already served fifteen hundred calls
of other tasks and was holding a live session, it billed the same 327 with `cached_tokens 0` — the
floor does not grow with the shim's history, and the resume anchor's prefix check refused to attach
the probe to any conversation it was carrying. Check it against your own backend rather than taking
the number:

```sh
curl -s localhost:58411/v1/chat/completions -H 'Content-Type: application/json' \
  -d '{"model":"sonnet","stream":true,"messages":[{"role":"user","content":""}]}' \
  | grep usage
```

That probe speaks to the BACKEND path only — it bypasses magi entirely. Whether magi injects its own
memory is the paragraph above: the adapter configures no store for it to inject from.

## Results

magi + Claude Code CLI serving `claude-sonnet-5`, effort HIGH (inherited from the operator's
`~/.claude/settings.json`, not pinned by a flag), `MAGI_COLLAPSE_REPEATS=0`.

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
