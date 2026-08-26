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
| `calls` | tool calls magi made in that trial -- the number that scales with how hard it worked |
| `council` | rounds held on the turn's completion declaration. Zero is what a timeout looks like: a council opens on a declaration, and a clock that cuts mid-work leaves none |
| `in` / `cache read` / `out` | magi's own token accounting for that trial. `in` includes the cached part, so `in` minus `cache read` is what was written fresh |

The verdict column carries four marks. `PASS` and `FAIL` are the verifier's. `TIME` is the agent
timeout, which is a fail with a cause worth separating -- nine of this run's seventeen. `RESCORED`
is a trial the verifier passed and the leaderboard's reward-hacking rule sets to zero.

The report page carries more per task -- cost, the baseline's five trials, and a link to the trial's
own archive. This table is the part that belongs in a diff.

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
[magi vs Claude Code on Terminal-Bench 2.1](https://sayaya1090.github.io/magi/bench/tb21-magi-vs-claude-code.html). It carries the per-task table — magi's verdict,
call and token counts beside the baseline's five trials and its cost — plus a written card for
every task magi failed and every task only magi solved, the quarantine list with the reason each
trial was held, and the audit of both sides' web use. It is regenerated by `bench/harbor/compare/build_page.py` from the job directories, or from the
trial archives committed beside it, so it is a view of the run rather than a transcription of it.
The copy under `docs/bench/` is what a diff reviews; the link above is the same file served by
Pages, because GitHub hands a repository's `.html` back as text.

magi on `claude-sonnet-5`, reasoning effort HIGH (a setting of the backend, not pinned by a magi
flag), one attempt per task, one or two trials at a time, on the tree at `f4c94d00`.

| | |
|---|---|
| pass rate | **72 / 89 = 80.9%** |
| of the 17 that did not pass | 9 hit the agent timeout, 5 exited cleanly and wrong, 2 landed UNVERIFIED, 1 was rescored to zero |
| wall clock | 22.5 hours, 15.1 min per task |
| model calls | 2,897 total, 33 per task |
| tool calls | 2,292 total, 103 council rounds |
| input tokens | 138.1M, of which 128.8M were cache reads |
| output tokens | 3.87M |

The rescored row is `regex-chess`: the grader passed it, and the leaderboard's own reward-hacking
rule sets it to zero. Its card on the report page has the account.

Regenerate this section with:

```sh
python3 bench/harbor/compare/build_page.py --markdown
```

It reads the same job directories the report page reads, so the two cannot drift. They did once --
this section quoted 65 / 89 from an earlier run for weeks after the run above replaced it, which is
why it is generated now rather than transcribed.

**One attempt per task**, where the published leaderboard entries are five, so this pass rate
carries a much wider error bar than the 1.6% those entries quote.

### Per task

Every task in alphabetical order, with what the run spent on it. `cache read` is the part of `in`
that came from cache, so `in` minus `cache read` is what was written fresh. `council` counts the
rounds held on the turn's completion declaration -- zero is what a timeout looks like, because a
council opens on a declaration and a clock that cuts mid-work leaves none. On the
[report page](https://sayaya1090.github.io/magi/bench/tb21-magi-vs-claude-code.html) each task name
links to that trial's full archive.

| task | | calls | council | tokens in | cache read | tokens out |
|---|---|---:|---:|---:|---:|---:|
| `adaptive-rejection-sampler` | ✅ PASS | 15 | 1 | 748,131 | 664,355 | 44,220 |
| `bn-fit-modify` | ✅ PASS | 18 | 2 | 611,527 | 529,298 | 38,746 |
| `break-filter-js-from-html` | ✅ PASS | 22 | 1 | 838,305 | 774,470 | 34,293 |
| `build-cython-ext` | ✅ PASS | 56 | 1 | 3,504,267 | 3,374,449 | 29,723 |
| `build-pmars` | ✅ PASS | 47 | 1 | 2,741,551 | 2,622,315 | 17,050 |
| `build-pov-ray` | ❌ FAIL | 22 | 1 | 758,862 | 692,805 | 15,258 |
| `caffe-cifar-10` | ✅ PASS | 41 | 1 | 2,522,152 | 2,294,848 | 31,614 |
| `cancel-async-tasks` | ✅ PASS | 23 | 2 | 694,228 | 573,419 | 34,138 |
| `chess-best-move` | ✅ PASS | 64 | 0 | 4,089,467 | 3,944,799 | 62,681 |
| `circuit-fibsqrt` | ✅ PASS | 42 | 1 | 4,853,150 | 4,694,244 | 113,474 |
| `cobol-modernization` | ✅ PASS | 26 | 1 | 1,295,484 | 1,193,984 | 52,406 |
| `code-from-image` | ✅ PASS | 19 | 1 | 420,367 | 383,351 | 9,441 |
| `compile-compcert` | ✅ PASS | 51 | 1 | 2,289,223 | 2,181,818 | 21,416 |
| `configure-git-webserver` | ✅ PASS | 19 | 1 | 518,562 | 455,693 | 19,030 |
| `constraints-scheduling` | ✅ PASS | 7 | 1 | 217,344 | 161,689 | 23,495 |
| `count-dataset-tokens` | ✅ PASS | 10 | 1 | 405,551 | 342,367 | 8,071 |
| `crack-7z-hash` | ✅ PASS | 22 | 1 | 569,208 | 529,927 | 6,877 |
| `custom-memory-heap-crash` | ✅ PASS | 32 | 1 | 1,868,298 | 1,767,924 | 43,398 |
| `db-wal-recovery` | ✅ PASS | 7 | 1 | 194,833 | 165,904 | 10,997 |
| `distribution-search` | ✅ PASS | 8 | 1 | 263,098 | 221,700 | 24,843 |
| `dna-assembly` | ✅ PASS | 39 | 2 | 3,290,258 | 3,036,658 | 153,782 |
| `dna-insert` | ✅ PASS | 18 | 1 | 640,913 | 579,294 | 30,440 |
| `extract-elf` | ✅ PASS | 10 | 1 | 328,249 | 275,319 | 27,091 |
| `extract-moves-from-video` | ⏱ TIME | 29 | 0 | 1,049,975 | 927,339 | 10,186 |
| `feal-differential-cryptanalysis` | ✅ PASS | 10 | 1 | 609,458 | 536,515 | 54,990 |
| `feal-linear-cryptanalysis` | ⏱ TIME | 18 | 0 | 1,048,335 | 983,689 | 43,381 |
| `filter-js-from-html` | ❌ FAIL | 11 | 1 | 543,474 | 466,816 | 44,167 |
| `financial-document-processor` | ✅ PASS | 22 | 3 | 938,348 | 807,375 | 63,307 |
| `fix-code-vulnerability` | ✅ PASS | 19 | 1 | 501,639 | 454,170 | 10,255 |
| `fix-git` | ✅ PASS | 11 | 1 | 338,531 | 287,047 | 21,908 |
| `fix-ocaml-gc` | ✅ PASS | 34 | 1 | 2,412,538 | 2,292,742 | 31,104 |
| `gcode-to-text` | ❌ FAIL | 6 | 1 | 387,945 | 251,904 | 14,979 |
| `git-leak-recovery` | ✅ PASS | 10 | 1 | 237,792 | 207,164 | 11,493 |
| `git-multibranch` | ✅ PASS | 27 | 1 | 751,981 | 696,659 | 15,059 |
| `gpt2-codegolf` | ⏱ TIME | 25 | 0 | 1,532,964 | 1,422,370 | 73,920 |
| `headless-terminal` | ✅ PASS | 13 | 1 | 465,137 | 414,009 | 19,422 |
| `hf-model-inference` | ✅ PASS | 13 | 1 | 289,461 | 253,698 | 10,353 |
| `install-windows-3.11` | ❌ FAIL | 81 | 0 | 7,492,616 | 7,042,715 | 79,782 |
| `kv-store-grpc` | ✅ PASS | 10 | 1 | 227,340 | 182,842 | 11,533 |
| `large-scale-text-editing` | ✅ PASS | 9 | 1 | 263,731 | 222,890 | 17,719 |
| `largest-eigenval` | ✅ PASS | 20 | 1 | 853,236 | 772,169 | 51,200 |
| `llm-inference-batching-scheduler` | ✅ PASS | 25 | 3 | 1,737,607 | 1,573,032 | 83,053 |
| `log-summary-date-ranges` | ✅ PASS | 11 | 2 | 306,068 | 258,416 | 20,983 |
| `mailman` | ✅ PASS | 51 | 1 | 4,121,189 | 3,963,473 | 40,524 |
| `make-doom-for-mips` | ⏱ TIME | 60 | 0 | 7,120,167 | 6,946,462 | 63,317 |
| `make-mips-interpreter` | ✅ PASS | 44 | 0 | 3,208,574 | 2,924,988 | 141,342 |
| `mcmc-sampling-stan` | ✅ PASS | 50 | 2 | 3,811,897 | 3,645,734 | 39,824 |
| `merge-diff-arc-agi-task` | ✅ PASS | 21 | 1 | 879,897 | 815,978 | 18,890 |
| `model-extraction-relu-logits` | ✅ PASS | 4 | 1 | 186,279 | 150,118 | 21,064 |
| `modernize-scientific-stack` | ✅ PASS | 8 | 1 | 200,117 | 172,473 | 8,989 |
| `mteb-leaderboard` | ✅ PASS | 21 | 1 | 658,922 | 602,271 | 20,194 |
| `mteb-retrieve` | ✅ PASS | 10 | 1 | 239,337 | 204,446 | 11,013 |
| `multi-source-data-merger` | ✅ PASS | 7 | 1 | 209,503 | 173,715 | 14,174 |
| `nginx-request-logging` | ✅ PASS | 21 | 3 | 705,676 | 566,613 | 54,741 |
| `openssl-selfsigned-cert` | ❌ FAIL | 10 | 1 | 284,853 | 243,686 | 9,424 |
| `overfull-hbox` | ✅ PASS | 21 | 2 | 799,908 | 684,272 | 45,284 |
| `password-recovery` | ✅ PASS | 18 | 2 | 603,779 | 460,713 | 65,893 |
| `path-tracing` | ❌ FAIL | 36 | 1 | 2,477,631 | 2,342,727 | 86,890 |
| `path-tracing-reverse` | ⏱ TIME | 52 | 0 | 3,709,068 | 3,414,503 | 137,465 |
| `polyglot-c-py` | ✅ PASS | 10 | 1 | 289,055 | 249,126 | 18,059 |
| `polyglot-rust-c` | ✅ PASS | 7 | 1 | 424,810 | 351,728 | 55,077 |
| `portfolio-optimization` | ✅ PASS | 14 | 1 | 356,462 | 317,704 | 18,104 |
| `protein-assembly` | ✅ PASS | 47 | 3 | 3,911,346 | 3,613,506 | 124,425 |
| `prove-plus-comm` | ✅ PASS | 5 | 1 | 115,396 | 93,283 | 6,306 |
| `pypi-server` | ✅ PASS | 22 | 1 | 474,926 | 437,761 | 7,823 |
| `pytorch-model-cli` | ✅ PASS | 28 | 1 | 904,820 | 847,949 | 16,155 |
| `pytorch-model-recovery` | ✅ PASS | 12 | 1 | 376,059 | 329,639 | 19,014 |
| `qemu-alpine-ssh` | ⏱ TIME | 68 | 0 | 6,038,848 | 5,893,120 | 36,180 |
| `qemu-startup` | ✅ PASS | 38 | 0 | 1,605,353 | 1,535,025 | 19,310 |
| `query-optimize` | ✅ PASS | 9 | 1 | 311,542 | 265,705 | 22,949 |
| `raman-fitting` | ⏱ TIME | 16 | 2 | 560,275 | 393,173 | 76,507 |
| `regex-chess` | ♻ RESCORED | 48 | 2 | 3,791,772 | 3,559,872 | 122,866 |
| `regex-log` | ✅ PASS | 8 | 1 | 241,220 | 183,581 | 26,663 |
| `reshard-c4-data` | ✅ PASS | 21 | 1 | 898,720 | 830,194 | 50,017 |
| `rstan-to-pystan` | ✅ PASS | 36 | 2 | 2,065,544 | 1,934,432 | 38,265 |
| `sam-cell-seg` | ✅ PASS | 52 | 4 | 3,633,152 | 3,450,624 | 81,107 |
| `sanitize-git-repo` | ⏱ TIME | 40 | 5 | 3,079,953 | 2,864,954 | 65,989 |
| `schemelike-metacircular-eval` | ✅ PASS | 40 | 1 | 3,405,551 | 3,161,410 | 169,521 |
| `sparql-university` | ✅ PASS | 13 | 2 | 481,698 | 391,639 | 37,929 |
| `sqlite-db-truncate` | ✅ PASS | 6 | 1 | 178,279 | 142,077 | 19,974 |
| `sqlite-with-gcov` | ✅ PASS | 28 | 1 | 853,477 | 792,949 | 14,041 |
| `torch-pipeline-parallelism` | ✅ PASS | 28 | 1 | 1,683,184 | 1,549,357 | 60,415 |
| `torch-tensor-parallelism` | ✅ PASS | 17 | 1 | 687,733 | 621,390 | 33,409 |
| `train-fasttext` | ⏱ TIME | 61 | 0 | 5,102,892 | 4,956,287 | 34,615 |
| `tune-mjcf` | ✅ PASS | 28 | 1 | 1,027,889 | 949,262 | 30,875 |
| `video-processing` | ❌ FAIL | 22 | 0 | 2,833,439 | 2,690,560 | 81,741 |
| `vulnerable-secret` | ✅ PASS | 13 | 1 | 335,039 | 296,045 | 9,122 |
| `winning-avg-corewars` | ✅ PASS | 79 | 1 | 6,377,681 | 6,106,130 | 194,143 |
| `write-compressor` | ✅ PASS | 20 | 1 | 1,221,505 | 1,134,152 | 61,890 |

All five clean-exit failures carry `done 3-0`: the council agreed, unanimously, that work was
complete which the verifier then rejected. `build-pov-ray`, `filter-js-from-html`,
`gcode-to-text`, `openssl-selfsigned-cert` and `path-tracing` are the five, and their cards say what
each was wrong about. Two of them turn on the same shape -- an assumption about how the grader
would invoke or locate something, never checked -- and one on evidence the agent chose for itself.
On this backend the council still errs toward letting work through, which is the largest defect
this run surfaces.

Nine of the seventeen are the clock rather than a wrong answer, and seven of those nine never
convened a council at all: a timeout leaves no completion declaration for one to open on. Two runs
declared nothing and landed UNVERIFIED on purpose, which is the honest ending -- `video-processing`
knew its heuristic had not converged and said so instead of claiming done.

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
  **without making one web call** — the tools were there and it did not reach for them:
  `objdump -T` located `qemu_signalfd`, and a 32-line `LD_PRELOAD`
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
