# Benchmarking magi on Terminal-Bench 2.1

What this measures, how to run it yourself, and what the numbers are.

[Terminal-Bench](https://www.tbench.ai) gives an agent a real terminal in a Docker container and a
task, then scores whether the task got done. It is a whole-agent benchmark: the loop, the tools,
the recovery and the model are all under test together, and a run says nothing about which of them
produced the result. That is why the tables below carry token and call counts beside the verdict —
two agents that both pass are not equivalent if one took five times the work to get there.

## What is under test

- **The loop**: magi's tools, guards, council and recovery, from a pinned binary.
- **The backend**: whatever `MAGI_BASE_URL` serves.
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
| `council` | the tally on the turn's completion declaration, `done 3-0`. `— none` means the turn never declared finished |
| `usd` | what the backend charged, where it reports a price; `~` marks a figure shared across trials |

`turns` and `calls` differ by more than an order of magnitude and both are real: one task, one
prompt, forty model calls inside it. A trial killed by the agent timeout never reaches
`turn.finished`, so its input reads `?` rather than `0` — a zero would be a claim.

## The one thing that will bite you

**Concurrency can move the score, and only downward.** Trials share the Docker VM's CPU and memory.
On a 7-CPU, 8 GB VM, four trials get 1.75 cores and 2 GB each — and a task that compiles or trains
something then times out for a reason that has nothing to do with the agent. The distortion runs one
way: starvation can turn a pass into a timeout and never the reverse. Check `docker info` before
raising it, and compare a score only against runs given the same room.

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
grep -ohE '⚙ (read|grep|bash) \{[^}]{0,200}' "$@" | grep -E 'test_outputs\.py|correct_output|_gtruth'
```

Scope matters: an unscoped glob sweeps every job the repo has ever collected, and an audit that
counts other runs' calls is not an audit of this one.

**The dataset answers to two names, and a pattern that knows one of them is blind.** The GitHub
organisation is `harbor-framework`, hyphenated; the hub and the task registry are
`harborframework.com`, not. An audit pattern written from the repository URLs alone therefore never
sees `registry.harborframework.com/tasks/...`, which is the task page served under its other name.
That gap was found by eye on 2026-08-26, in a re-run of a task that had already been quarantined
once, and closing it (`harbor-?framework`) turned up three more trials the earlier sweeps had
passed. Any name the dataset can be reached by belongs in the pattern; write them from the hosts
that actually served content, not from the one URL shape you happen to remember.

**A trial that went looking for the answer is re-run on its own.** The check above is not
decorative: a trial can and does find the benchmark's own task pages on GitHub, and once it has, its
verdict is not evidence about the agent. So any trial whose web calls reached the dataset's task
pages, its solution files, or its grading tests is quarantined: the result stands in no table on its
own, the task is queued again after the last of the eighty-nine, and **the re-run is the result that
counts**. Which tasks those were is named with the results rather than folded silently into the
total, because a reader cannot audit what a table does not mention.

**The web is half the check.** A grader that is already in the container is one `read` away, and
no amount of reading web calls will see it. Extending the scan to local reads on 2026-08-26 (for
`test_outputs.py`, `correct_output`, `ground_truth`, `/tests/test.sh`) turned up exactly one run
across 123: `break-filter-js-from-html`, which read `/app/test_outputs.py` and ran it. That one is
not a hold. Its Dockerfile has `COPY tests/test_outputs.py /app` and its instruction ends "You can
run /app/test_outputs.py to verify", so there the grader is the specification and reading it is
doing as told. It is the only task of the eighty-nine that hands its grader over.

Keeping the name list narrow is what makes the scan quiet enough to read: a repository under repair
brings its own suite, and caffe's `src/caffe/test/test_io.cpp` and `fix-code-vulnerability`'s
`/app/test/test_environ.py` both stayed silent.

**A grading test is worse than a solution file, so it is held the same way.** On 2026-08-23 two
`headless-terminal` trials fetched `tests/test_outputs.py` from the dataset's repository. A solution
file shows one way to the answer; the grading test names the assertions the verdict is computed
from, which is the answer key itself. Both trials are held.

**When the re-run reaches the same page, the rule loops -- and then the result stands, named.**
`mteb-leaderboard` fetched its own task README on all five trials this repository has watched (two
on 2026-08-23, one on 08-24, two on 08-26). That is the task's shape, not the agent's habit: the
instruction asks for *"the best embedding model on the Scandinavian MTEB leaderboard as of August
2025"*, which cannot be answered without the web, and that search puts the dataset's own task page
near the top. Quarantine → re-run → reach again repeats without end, so this one row **carries its
re-run result into the table and its reason into this section.** What the hold is for is stopping a
reader from weighing that row like the others, and naming it does that too.

**A hold covers the trial's predecessors too.** Dropping the one quarantined trial is not enough on
its own: the task's next-oldest trial moves up in its place and the table shows a number the rule
never adopted. Wiring the hold on 2026-08-26 made that visible in one step — three trials were held
and the totals did not move, because three older trials had stepped in. So a hold reaches every
trial of that task at or before it, and the task counts as unmeasured until the re-run lands.

**The audit of the run below.** Across the 89 trials the agent made 41 web fetches and 18 web
searches, and every one was read. Two of them reached the dataset's own pages on GitHub:
`mteb-leaderboard` fetched that task's README, and `qemu-alpine-ssh` fetched an issue thread while
diagnosing a translation fault. Neither is a solution file and neither changed a verdict — the
first passes, the second fails for the reason named above.

Two earlier trials did reach solution files, which is why the quarantine rule exists rather than
being hypothetical. `extract-elf` fetched a `solution/solve.sh` and `regex-chess` decoded a
solution blob from a third-party mirror of the dataset. Both were re-run. `extract-elf`'s re-run
made no such fetch and passed again in fewer calls than the quarantined attempt (20 against 29).
`regex-chess` did pass unaided on that occasion, but it has not repeated it: on 2026-08-26 two
successive trials each fetched `tasks/regex-chess/solution/solve.sh` from the dataset, the second
by `webfetch` twice and `curl` once. Both are held, and the task counts as unmeasured rather than
as the 1.00 those trials returned — a pass read off the answer file measures nothing. This is the
opposite ending to `mteb-leaderboard`'s: there the reached page is the task's own README and the
instruction cannot be answered without the web, so the row stands with its reason named; here the
reached file is the answer, so no row stands at all.

**Audit the other side too, or the comparison is only half-checked.** A leaderboard row is a
number until its trials are read, and the same question — did it find the answer instead of working
it out — applies to the baseline. Harbor Hub serves each trial's full trajectory as JSON, no login
required, at `/api/trials/{trialId}/trajectory?jobId={jobId}&trajectory_path=trials%2F{trialId}%2Ftrajectory.json`;
the trial ids come from the row's result pages, which are server-rendered, so the whole set can be
pulled with `curl` and scanned offline. Doing that for row `d7540f21` (claude-code 2.1.205 /
claude-sonnet-5, 445 trials) on 2026-08-26 fetched 440 trajectories, 439 of them non-empty, and the
same three patterns found **zero** trials reaching the dataset's task pages, solution files or
grading tests.

**What that scan also settled is that both agents used the web.** An earlier draft of the
comparison page claimed Claude Code had no web access, on the strength of a few sampled trajectories
showing only Bash, Read and Edit. Bash is enough: 54 of the 439 trials (22 tasks) called `curl` or
`wget` against an external host, and 30 of them (13 tasks) reached somewhere that was not a package
or distribution mirror. Six trials across four tasks — `build-pov-ray`, `crack-7z-hash`,
`dna-assembly`, `regex-chess` — called a search engine directly: no search tool, but search all the
same. Those counts are a floor, not the total, because a scan keyed on `curl` and `wget` misses the
other ways out: `git clone`, and Python's own `urllib.request`, which one trial used to query
`api.github.com/search/repositories`. Counting every URL that appears anywhere in a trajectory
raises it to **165 of 439 trials across 45 of the 89 tasks** — near half the set went outside.
Grep for the verbs you remember and you will measure the verbs you remember; grep for URLs and you
measure what actually left the container. Where both agents went out, they often went to the same place: `dna-assembly` to
`www.neb.com` on both sides, `build-pov-ray` to `povray.org` and a search engine on both,
`caffe-cifar-10` to `cs.toronto.edu` on all five baseline trials. One baseline trial of
`torch-pipeline-parallelism` fetched `api.github.com/repos/huggingface/picotron` and its README —
the same reference implementation that put this repository's own trial of that task under
quarantine. Read the other side before calling a tool difference an advantage.

**Reaching the dataset is not the only way to reach the answer.** A task whose answer is a well
known public project is reachable by its own name, and the audit patterns above — dataset, solution
file, grading test — will not see it. `regex-chess` is the case in full: the task asks for a chess
move generator built only from regular expressions, its author is Nicholas Carlini, and
`github.com/carlini/regex-chess` is his published implementation of exactly that. This repository's
three trials on 2026-08-26 each reached it, by three different routes — the dataset's own
`solution/solve.sh` twice, then a third-party mirror of the dataset, the author's own repository,
his write-up, and a third party's collection of solved trajectories. The baseline is not clean here
either: one of its five trials searched GitHub through `urllib`, found the repository, and cloned
it. Note the asymmetry that matters, though — the other four baseline trials went nowhere at all
and passed anyway, so this is a task solvable without looking. A judge for such a task has to name
the origin project alongside the dataset, which is what `scratchpad/rc_clean.py` does.

**A contaminated trial does not need to finish.** The verdict comes from its web calls, and those
land in the first minutes; waiting out the remaining half hour buys nothing. So the re-run loop
polls the transcript, kills the trial the moment it reaches, and starts the next one — which turns
a thirty-minute attempt into a twenty-second one and makes matching the baseline's k=5 affordable.
Kill by PID: `pkill -f <pattern>` matches the loop's own command line and has killed the loop
itself more than once in this repository.

The check is complete rather than partial, and that is measurable: this run made **zero** `spawn`,
`meeting` and `hand_off` calls, so no delegated subagent kept a session store of its own and the
parent transcripts are the entire record of what the agent did.

**The backend's own settings apply identically to every trial.** Reasoning effort and the like are
properties of the endpoint, not of any one task, so they are a property of the RUN — stated with the
results below rather than left to be inferred from the numbers.

## Results

**The head-to-head report is a page of its own:**
[magi vs Claude Code on Terminal-Bench 2.1](https://claude.ai/code/artifact/b8bbb95a-24f0-4ed5-a2c2-4d27b3981a0e). It carries the per-task table — magi's verdict,
call and token counts beside the baseline's five trials and its cost — plus a written card for
every task magi failed and every task only magi solved, the quarantine list with the reason each
trial was held, and the audit of both sides' web use. It is regenerated from the job directories by
`bench/harbor/compare/build_page.py`, so it is a view of the run rather than a transcription of it.
The page is private until it is shared from its own share menu.

magi on `claude-sonnet-5`, reasoning effort HIGH (a setting of the backend, not pinned by a magi
flag), repeat-collapsing off (an env switch at the time; the mechanism has since been removed),
one attempt per task, one or two trials at a time.

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

**One attempt per task**, where the published leaderboard entries are five, so this pass rate
carries a much wider error bar than the ±1.6% those entries quote.

### Per task

Every task, in the order the dataset names them. `in` counts cache reads, so `in − cached` is what was written fresh; a
trial killed by the agent timeout never reaches `turn.finished` and reads `?` rather than `0`,
because a zero would be a claim. `council` is the tally on the turn's completion declaration —
`— none` means it never got that far. This table was recorded before `report.py` grew its `usd`
column, so it has one column fewer than a report run today; nothing else about it changed.

| task | | min | turns | calls | in | cached | out | council |
|---|---|---:|---:|---:|---:|---:|---:|---|
| `adaptive-rejection-sampler` | ✅ PASS | 15 | 3 | 38 | 1,440,668 | 1,105,646 | 68,128 | done 3-0 |
| `bn-fit-modify` | ✅ PASS | 7 | 2 | 35 | 897,651 | 688,096 | 19,943 | done 3-0 |
| `break-filter-js-from-html` | ✅ PASS | 5 | 2 | 20 | 311,187 | 225,487 | 15,868 | done 3-0 |
| `build-cython-ext` | ✅ PASS | 10 | 3 | 68 | 3,046,657 | 2,553,973 | 22,803 | done 3-0 |
| `build-pmars` | ✅ PASS | 12 | 2 | 87 | 4,147,391 | 3,319,594 | 39,728 | continue 1-2 |
| `build-pov-ray` | ✅ PASS | 7 | 2 | 47 | 1,522,215 | 1,220,568 | 10,805 | done 3-0 |
| `caffe-cifar-10` | ✅ PASS | 40 | 2 | 64 | 2,442,396 | 1,993,433 | 22,136 | done 3-0 |
| `cancel-async-tasks` | ✅ PASS | 4 | 3 | 25 | 428,024 | 316,250 | 15,145 | done 3-0 |
| `chess-best-move` | ⏱ TIME | 16 | 2 | 38 | ? | ? | 56,307 | — none |
| `circuit-fibsqrt` | ✅ PASS | 26 | 4 | 33 | 1,148,930 | 818,913 | 99,700 | done 3-0 |
| `cobol-modernization` | ✅ PASS | 13 | 5 | 55 | 1,891,579 | 1,505,162 | 58,807 | done 3-0 |
| `code-from-image` | ✅ PASS | 3 | 1 | 19 | 257,221 | 178,090 | 6,446 | done 3-0 |
| `compile-compcert` | ✅ PASS | 20 | 1 | 35 | 830,665 | 643,978 | 12,347 | done 3-0 |
| `configure-git-webserver` | ✅ PASS | 8 | 2 | 36 | 696,797 | 484,981 | 24,970 | done 3-0 |
| `constraints-scheduling` | ✅ PASS | 6 | 2 | 14 | 225,016 | 142,827 | 31,867 | done 3-0 |
| `count-dataset-tokens` | ✅ PASS | 3 | 2 | 16 | 241,814 | 145,960 | 4,266 | done 3-0 |
| `crack-7z-hash` | ✅ PASS | 15 | 1 | 32 | 989,766 | 805,681 | 6,431 | done 3-0 |
| `custom-memory-heap-crash` | ✅ PASS | 18 | 2 | 46 | 1,560,459 | 1,216,969 | 71,367 | done 3-0 |
| `db-wal-recovery` | ❌ FAIL | 16 | 4 | 43 | 923,238 | 553,646 | 64,458 | done 3-0 |
| `distribution-search` | ✅ PASS | 5 | 3 | 18 | 367,615 | 268,256 | 22,374 | done 3-0 |
| `dna-assembly` | ✅ PASS | 23 | 5 | 53 | 2,339,706 | 1,802,281 | 112,882 | done 3-0 |
| `dna-insert` | ❌ FAIL | 12 | 1 | 32 | 902,799 | 714,020 | 48,978 | done 3-0 |
| `extract-elf` | ✅ PASS | 6 | 2 | 20 | 367,858 | 259,271 | 26,768 | done 3-0 |
| `extract-moves-from-video` | ⏱ TIME | 32 | 1 | 30 | ? | ? | 10,010 | — none |
| `feal-differential-cryptanalysis` | ✅ PASS | 10 | 3 | 18 | 468,530 | 330,455 | 45,073 | done 3-0 |
| `feal-linear-cryptanalysis` | ✅ PASS | 20 | 2 | 30 | 1,101,435 | 870,283 | 94,922 | done 3-0 |
| `filter-js-from-html` | ❌ FAIL | 23 | 6 | 40 | 1,170,484 | 812,129 | 65,480 | done 3-0 |
| `financial-document-processor` | ✅ PASS | 5 | 1 | 16 | 268,092 | 138,996 | 16,624 | done 3-0 |
| `fix-code-vulnerability` | ✅ PASS | 3 | 1 | 31 | 669,854 | 524,008 | 6,386 | done 3-0 |
| `fix-git` | ✅ PASS | 3 | 2 | 15 | 239,694 | 140,023 | 10,869 | done 3-0 |
| `fix-ocaml-gc` | ✅ PASS | 45 | 1 | 59 | 1,874,773 | 1,475,071 | 16,687 | done 3-0 |
| `gcode-to-text` | ⏱ TIME | 15 | 3 | 52 | ? | ? | 58,369 | — none |
| `git-leak-recovery` | ✅ PASS | 2 | 1 | 13 | 141,033 | 87,760 | 3,462 | done 3-0 |
| `git-multibranch` | ✅ PASS | 5 | 1 | 36 | 683,876 | 510,832 | 10,951 | done 3-0 |
| `gpt2-codegolf` | ⏱ TIME | 16 | 3 | 27 | ? | ? | 71,833 | — none |
| `headless-terminal` | ✅ PASS | 5 | 4 | 22 | 363,307 | 261,228 | 18,591 | done 3-0 |
| `hf-model-inference` | ✅ PASS | 4 | 3 | 13 | 443,407 | 312,411 | 8,531 | done 3-0 |
| `install-windows-3.11` | ❌ FAIL | 57 | 5 | 116 | 7,187,422 | 5,747,391 | 131,504 | done 2-1 |
| `kv-store-grpc` | ✅ PASS | 3 | 1 | 21 | 304,773 | 224,158 | 5,651 | done 3-0 |
| `large-scale-text-editing` | ✅ PASS | 6 | 2 | 17 | 284,771 | 189,384 | 18,637 | done 3-0 |
| `largest-eigenval` | ✅ PASS | 5 | 4 | 23 | 380,108 | 285,554 | 13,042 | done 3-0 |
| `llm-inference-batching-scheduler` | ✅ PASS | 14 | 5 | 44 | 1,849,604 | 1,427,495 | 68,297 | done 3-0 |
| `log-summary-date-ranges` | ✅ PASS | 2 | 3 | 18 | 277,563 | 188,576 | 6,698 | done 3-0 |
| `mailman` | ✅ PASS | 20 | 4 | 77 | 3,972,780 | 3,163,747 | 65,203 | done 3-0 |
| `make-doom-for-mips` | ⏱ TIME | 16 | 2 | 46 | ? | ? | 66,226 | — none |
| `make-mips-interpreter` | ❌ FAIL | 30 | 5 | 53 | 3,626,465 | 2,777,225 | 147,035 | done 3-0 |
| `mcmc-sampling-stan` | ✅ PASS | 18 | 4 | 73 | 5,624,196 | 4,489,664 | 22,807 | done 3-0 |
| `merge-diff-arc-agi-task` | ✅ PASS | 5 | 2 | 32 | 698,206 | 553,270 | 13,707 | done 3-0 |
| `model-extraction-relu-logits` | ✅ PASS | 5 | 1 | 15 | 291,140 | 181,564 | 22,567 | done 3-0 |
| `modernize-scientific-stack` | ✅ PASS | 3 | 2 | 20 | 291,293 | 210,861 | 9,013 | done 3-0 |
| `mteb-leaderboard` | ✅ PASS | 21 | 5 | 66 | 3,208,550 | 2,655,831 | 20,609 | done 3-0 |
| `mteb-retrieve` | ✅ PASS | 7 | 5 | 26 | 423,831 | 296,069 | 8,599 | done 3-0 |
| `multi-source-data-merger` | ✅ PASS | 4 | 2 | 23 | 306,685 | 112,815 | 12,158 | done 3-0 |
| `nginx-request-logging` | ✅ PASS | 5 | 1 | 23 | 391,873 | 261,208 | 15,843 | done 2-1 |
| `openssl-selfsigned-cert` | ❌ FAIL | 3 | 3 | 24 | 388,691 | 267,546 | 6,743 | done 3-0 |
| `overfull-hbox` | ✅ PASS | 5 | 1 | 20 | 354,784 | 242,842 | 14,927 | done 3-0 |
| `password-recovery` | ✅ PASS | 6 | 1 | 29 | 577,451 | 436,767 | 24,587 | done 3-0 |
| `path-tracing` | ⏱ TIME | 30 | 6 | 62 | ? | ? | 135,337 | — none |
| `path-tracing-reverse` | ⏱ TIME | 31 | 8 | 43 | ? | ? | 139,512 | — none |
| `polyglot-c-py` | ✅ PASS | 5 | 1 | 16 | 260,952 | 166,832 | 12,925 | done 3-0 |
| `polyglot-rust-c` | ✅ PASS | 7 | 2 | 15 | 372,933 | 277,544 | 28,579 | done 3-0 |
| `portfolio-optimization` | ✅ PASS | 5 | 2 | 23 | 393,289 | 261,708 | 11,381 | done 3-0 |
| `protein-assembly` | ❌ FAIL | 28 | 4 | 64 | 6,304,097 | 3,864,733 | 81,182 | done 3-0 |
| `prove-plus-comm` | ✅ PASS | 2 | 3 | 13 | 153,488 | 105,200 | 4,994 | done 3-0 |
| `pypi-server` | ✅ PASS | 4 | 2 | 24 | 364,535 | 255,976 | 4,973 | done 3-0 |
| `pytorch-model-cli` | ✅ PASS | 6 | 2 | 34 | 610,251 | 463,591 | 11,308 | done 3-0 |
| `pytorch-model-recovery` | ✅ PASS | 5 | 2 | 17 | 276,851 | 177,817 | 10,624 | done 3-0 |
| `qemu-alpine-ssh` | ❌ FAIL | 11 | 4 | 70 | 2,083,920 | 1,719,255 | 25,630 | done 3-0 |
| `qemu-startup` | ✅ PASS | 9 | 5 | 44 | 1,113,744 | 888,035 | 26,269 | done 3-0 |
| `query-optimize` | ✅ PASS | 19 | 2 | 16 | 234,979 | 157,869 | 13,641 | done 3-0 |
| `raman-fitting` | ❌ FAIL | 9 | 6 | 38 | 832,768 | 596,260 | 35,366 | done 3-0 |
| `regex-chess` | ✅ PASS | 40 | 6 | 48 | 2,086,417 | 1,685,916 | 130,722 | done 3-0 |
| `regex-log` | ✅ PASS | 7 | 1 | 12 | 148,460 | 76,937 | 15,855 | done 3-0 |
| `reshard-c4-data` | ✅ PASS | 23 | 4 | 40 | 1,044,314 | 822,756 | 80,736 | done 3-0 |
| `rstan-to-pystan` | ⏱ TIME | 31 | 7 | 66 | ? | ? | 39,027 | continue 1-2 |
| `sam-cell-seg` | ✅ PASS | 13 | 5 | 35 | 1,052,136 | 784,669 | 39,538 | done 3-0 |
| `sanitize-git-repo` | ⏱ TIME | 16 | 3 | 54 | ? | ? | 20,489 | continue 1-2 |
| `schemelike-metacircular-eval` | ⏱ TIME | 41 | 1 | 15 | ? | ? | 45,534 | — none |
| `sparql-university` | ✅ PASS | 6 | 2 | 18 | 338,046 | 229,593 | 19,903 | done 3-0 |
| `sqlite-db-truncate` | ✅ PASS | 3 | 1 | 13 | 172,093 | 108,351 | 9,134 | done 3-0 |
| `sqlite-with-gcov` | ✅ PASS | 5 | 1 | 30 | 592,914 | 452,392 | 7,803 | done 3-0 |
| `torch-pipeline-parallelism` | ❌ FAIL | 12 | 2 | 24 | 504,037 | 341,632 | 29,230 | done 3-0 |
| `torch-tensor-parallelism` | ✅ PASS | 11 | 1 | 17 | 329,882 | 214,404 | 24,074 | done 3-0 |
| `train-fasttext` | ⏱ TIME | 70 | 4 | 50 | ? | ? | 22,049 | — none |
| `tune-mjcf` | ✅ PASS | 10 | 2 | 27 | 572,953 | 439,049 | 17,864 | done 3-0 |
| `video-processing` | ❌ FAIL | 8 | 4 | 21 | 510,815 | 406,257 | 29,850 | — none |
| `vulnerable-secret` | ✅ PASS | 13 | 1 | 20 | 289,763 | 193,780 | 6,782 | done 3-0 |
| `winning-avg-corewars` | ⏱ TIME | 61 | 3 | 68 | ? | ? | 259,604 | continue 0-3 |
| `write-compressor` | ⏱ TIME | 16 | 1 | 2 | ? | ? | 5,344 | — none |

Seven of the eleven verifier failures carry `done 3-0`: the council agreed, unanimously, that work
was complete which the verifier then rejected. Two trials were held back by it and timed out
(`continue 1-2`, `continue 0-3`), and in both the verifier agreed with the council rather than the
agent. On this backend the council errs toward letting work through, and that is the largest single
defect this run surfaced — larger than anything about tokens. The requirements walk and the closing
call (ARCHITECTURE §5) were added against this measurement specifically, and postdate this run.

### The machine, and the tasks it cannot do justice to

| | |
|---|---|
| host | Mac mini (Mac16,11), Apple M4 Pro, 14 cores (10P + 4E), 64 GB, macOS 26.5.2, arm64 |
| Docker VM | 7 CPUs, 7.7 GiB — which is what the trials actually share, not the 64 GB above |
| task images | amd64, run on arm64 **under translation** |

The VM allocation is the number that matters: at two concurrent trials each gets ~3.5 cores and
~3.8 GiB, and a task that compiles or trains something is working inside that, not inside a 14-core
machine. Raising the VM's share is what would make higher concurrency safe.

Four consequences are visible in the results rather than hypothetical:

- **`qemu-alpine-ssh` starts from a handicap this architecture creates.** Its container logs
  `rosetta error: Unimplemented syscall number 282` before the task's own work begins — an x86
  binary hitting a hole in Apple's translation layer. On a native amd64 host the hole is not there,
  so part of this task's 900-second budget pays for the host rather than for the task. An earlier
  draft of this section called the task unpassable here. Two trials on 2026-08-26 showed that was
  wrong: the handicap is a cost, not a wall.
- **Two separate ways past the hole, found by two trials.** `qemu-startup` searched the error
  string, landed on `docker/for-mac#7475`, and installed an **arm64-native QEMU inside the amd64
  container** (`dpkg --add-architecture arm64`, then `apt-get install qemu-system-x86:arm64`),
  which never enters the translation layer at all; that trial passed. `qemu-alpine-ssh` found the
  same multi-arch route blocked by gstreamer dependency conflicts and went a layer lower instead,
  **with no web access at all**: `objdump -T` located `qemu_signalfd`, and a 32-line `LD_PRELOAD`
  interposer returned `ENOSYS` for syscalls 282 (`signalfd`) and 289 (`signalfd4`) so QEMU fell
  back to its own pipe-based path. SeaBIOS booted, and Alpine 3.19 reached `localhost login:` after
  3 m 2 s of VM time — but the 15-minute budget ended moments later with sshd untouched. What beat
  that trial was the budget, not the architecture.
- **`install-windows-3.11` ran QEMU without KVM** (`/dev/kvm` absent), so its guest ran on pure
  software emulation for 57 minutes and lost. A Linux host would need `--device /dev/kvm` passed
  through to do better, so this one is not purely a macOS penalty — but it is a penalty.
- **The compute-heavy timeouts carried the translation overhead**, which falls hardest on exactly
  the work they do: `path-tracing`, `path-tracing-reverse`, `make-doom-for-mips`, `gpt2-codegolf`.
  How much of each timeout is translation and how much is the agent is not separated here, and
  should not be guessed at.

A score from this machine is comparable to another score from a machine given the same room, and
not otherwise.

For comparison, the same loop on a local `qwen3.8:27b-mlx` over a 14-task subset scored 9/14 at
every reasoning effort that was tried.
