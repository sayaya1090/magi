# Coding task isolation, recovery and review design

[한국어](CODING_WORKTREE_DESIGN.ko.md) · [Application client design](APPLICATION_CLIENTS_DESIGN.md)

This document combines the coding-task design with the implementation and validation recorded in §16. Shared preparation/inspection and isolated CLI execution are available; dedicated task UI, automatic companion switching and PR orchestration remain planned. Running `git worktree` alone does not manage task lifetime or companion ownership.

2026-10-16 refinement: this is a coding-agent-only feature. Support Git-free work without silently running `git init`. Sections 7–15 extend the Git preparation/execution/cleanup contracts in sections 1–6 with environment selection, a durable mutation journal, recovery and PR/integration lifecycle. Sections 1–15 describe target contracts; §16 distinguishes implemented behavior from remaining work.

## 1. Placement

Use `source workspace → coding task → isolated workspace → dedicated companion → session`. An `internal/app` task service owns lifetime and records; adapters behind ports execute Git and start processes. Every UI calls the same task API. Extensions and IDEs must not implement independent Git commands and recovery state.

The companion owns the canonical worktree path. The original repository companion keeps its original workspace. Do not mutate the parent session's workdir or move its conversation into the new task. Figma, mail, Slack and Office may delegate coding work but do not receive worktree tools by default.

## 2. Creation and agent execution

Keep two operations distinct.

| Operation | Work performed | Model execution |
|---|---|---|
| Prepare worktree | Resolve base commit, create task branch/worktree, persist record | None |
| Start coding task | Prepare or reuse worktree, ensure companion, create task session, submit first request | Starts after submission |

“Start work on this issue” should connect a dedicated coding agent by default. “Create worktree” alone must not start a billed turn. UI distinguishes preparing a companion process from calling a model.

## 3. Records and API

A record includes `taskId`, canonical `repositoryCommonDir`, base commit SHA, branch, canonical worktree path, purpose (issue, PR or user description), companion socket, sessionId, state, and creation/update times. No model keys or tokens are stored. The user configuration directory holds the task index; it is not automatically committed to a repository.

Planned methods are `coding-task-prepare`, `coding-task-start`, `coding-task-list`, `coding-task-status`, `coding-task-stop` and `coding-task-remove`. These names are not registered daemon methods yet. Create tasks from a trusted coding companion's repository, not arbitrary client-supplied paths. Browser clients reference only explicitly registered repositories.

`prepare` returns taskId, branch, worktree and state. `start` adds sessionId and companion information. Retrying the same request ID returns the existing task without creating another branch. `list/status` compares persisted records with actual Git registrations.

## 4. States and recovery

States are `preparing → ready → starting → active → stopped`; failures record `failed`. A mismatch between Git registrations and records becomes `recovery-required`. Process existence, model activity and waiting for a user are separate observations, not overloaded task states.

Before creation, inspect `git rev-parse --git-common-dir`, the base SHA, `git worktree list --porcelain`, and branch/path occupancy. A per-repository interprocess lock covers creation and record persistence. Branches such as `magi/<taskId>` distinguish task branches from user branches. Do not automatically stash, hard-reset, clean or replace a branch.

If Git creation succeeds but persistence fails, return recovery information including path, branch and SHA. Replace records atomically. After restart, inspect Git registrations and expose recoverable tasks. Do not automatically adopt arbitrary directories. A companion startup failure preserves the prepared worktree so that start can be retried.

## 5. Stop and cleanup

`stop` interrupts that task's turn or stops its companion, while retaining the worktree, branch and session log for review and resumption.

`remove` verifies that record and Git agree on path/branch and that no turn is running. Modified files, untracked files, Git conflicts and ownership by another companion prevent automatic removal. Show current state and commits to preserve before deletion. Removing a worktree and deleting a branch are separate actions; retain the branch by default. Keep a cleanup result and session location in the record.

## 6. Implementation order and acceptance

1. Implement records, states, canonical repository identity and API types. Test transitions and idempotent request IDs without Git.
2. Connect prepare to real temporary Git repositories. Test spaces, linked worktrees, branch collisions, concurrent creation and persistence failure recovery.
3. Connect start to companion startup. Check fixed per-task workdirs/sockets/sessions and that preparation alone never calls a model.
4. Add CLI first, then VS Code and JetBrains. All surfaces must list the same task identically.
5. Implement stop/remove. Test refusals for modified/untracked files, active turns and externally owned worktrees.
6. Run real CLI acceptance and browser Playwright. Another reviewer checks paths, command arguments, recovery and deletion contracts. Mark implementation complete only after recording execution evidence.

Initial application clients work without this service. Do not add a nonfunctional worktree button before these APIs exist.

## 7. Coding role and environment selection

Only the server-verified `ClientCoding` receives workspace tools. VS Code, IntelliJ, TUI and CLI use the same service; changing a role string in model arguments does not grant access. Other application agents delegate through existing companion tools.

| Environment/choice | Workspace | Integration |
| --- | --- | --- |
| Healthy Git repository with HEAD | Dedicated branch/worktree | PR or explicit local integration |
| No Git repository, isolation selected | Independent manifest-based copy | Selected files after baseline comparison |
| Explicit direct editing | Fixed source directory | Only journal-supported operations are recoverable |
| Git repository without initial commit | Explicit copy or wait for user decision | Never initialize or commit automatically |
| Git corruption/access failure | Fail or require recovery | Never silently classify as no Git |

Preparation does not call a model, push, create a PR or merge. A Git task uses a commit SHA; uncommitted source changes remain untouched. An unavailable PR service does not waive a required review policy.

## 8. Existing implementation and limits

`internal/app/workspace.go` already supports optional child clones, including uncommitted changes. Its spawn contract stays unchanged. The child journal in `restore.go` is mainly in memory; the non-Git backups in `trash.go` use hardlinks and assume atomic replacement. They do not cover arbitrary shell writes or durable recovery.

New backups use independent bytes, never hardlinks to a file that can be modified in place. Per-path success and failure reporting is retained.

## 9. Durable change journal

Store records and backups outside the source, under `<config>/coding-tasks/tasks/<taskId>`. Link the request, source, workspace, mode, baseline, session and integration results. Protect files with private permissions. No backup is uploaded or committed automatically.

Persist backups and operation intent before changing source files. Each operation records relative paths, existence, content digest, supported permissions and before/after blob references. Record the observed result after writing; interruption between writing and recording requires comparison with both states. A file matching neither is a conflict. Never replay an arbitrary shell command to resolve uncertainty.

The initial recovery scope covers regular-file contents, existence and supported permission bits. Treat moves as creation/deletion until a dedicated move contract exists. ACLs, extended attributes, ownership, links, special files and external services require separate support. Verify path boundaries and file identity; modification time alone cannot establish equality.

## 10. Independent copies and source integration

Create a manifest and develop in an independent copy. Report exclusions and file/byte budgets. Excluded dependencies and environment files are not silently available. Detect changes during reads; ordinary copying is not an atomic filesystem snapshot.

Compare baseline B, task output W and current source C. B=W means no task change; C=B permits application; C=W means already applied; otherwise preserve a conflict. Persist source recovery data first and recheck before each write. Serialize Magi writers. Uncooperative external writers can still race these checks.

Workspace isolation does not contain arbitrary absolute-path shell writes, network operations or database changes. OS sandbox and tool permissions remain separate controls.

## 11. Recovery, stopping and retention

Restore before only when the current file still equals this task's after. Remove a created file only if unchanged; preserve later human edits. Never recursively remove a directory containing external files. Report partial recovery explicitly and retain the before blob for inspection.

Stopping model work and restoring files are different operations. Stop writers before restoring. Process termination cannot undo external side effects. Recovery outcomes include restored, conflict, unsupported and uncertain.

Do not automatically delete active workspaces or the last recovery material for unresolved operations. Stop changes on storage failure and preserve the last durable state.

## 12. Review, PR and merge lifecycle

Keep execution state separate from integration state. Execution uses ready/running/stopped/failed/recovery-required; integration uses unreviewed/verified/review-pending/changes-requested/ready-to-integrate/integrated. Model completion text cannot establish verification or integration.

A remote adapter records PR identity, base/head SHA, required checks and reviews. Retries find existing PRs by task identity. Recheck permissions, current head/base and unresolved reviews before integration. Automatic merging requires explicit repository policy; the default is a user decision.

Without a PR service, review commits for explicit local Git integration. Without Git, review the manifest/diff for selected-file application. Workspace/branch cleanup remains separate. Git worktrees share refs, configuration, hooks and external resources.

## 13. Responsibilities, alternatives and quality requirements

`internal/app` owns role checks and orchestration. Storage, snapshots, Git, remote reviews and process execution sit behind ports. Clients do not implement independent merge/recovery logic. Execute commands with argument arrays.

| Choice | Reason | Cost/limit |
| --- | --- | --- |
| Git worktree | Separate branch, index and working files | Shared Git state and external effects |
| Independent copy | Isolation without Git | Storage, copy time and exclusions |
| Direct editing with journal | Avoid copying large projects | Cannot capture every external writer |
| No automatic git init | Preserve repository policy | Separate non-Git diff/integration needed |
| Durable intent and bounded retries | Distinguish interrupted writes | More I/O and recovery states |

Evaluate fault tolerance through interruption recovery, usability through source-change and next-action reporting, security through role/path checks, performance through bounded copying and cancellation, and modifiability through shared ports. Measure concrete limits with real workspaces.

## 14. Acceptance scenarios

| ID | Situation | Expected result |
| --- | --- | --- |
| W1 | Git, no Git, unborn repository, Git error | Correct selection; no silent error downgrade |
| W2 | Concurrent/restarted identical request | One task and branch |
| W3 | Crash before/after backup or write | Compare files; do not report uncertainty as success |
| W4 | Create/modify/delete/move and permissions | Supported before state restored; failures explicit |
| W5 | Human edit during development/application/recovery | Preserve conflicts |
| W6 | Spaces, Unicode, case, symlink/junction/hardlink | Boundaries respected; independent copies |
| W7 | Shell writes and external services | State scope; no external rollback claim |
| W8 | Budget, disk, read or cancellation failure | Fail before mutation or preserve interrupted state |
| W9 | Head/base changes after checks | Re-review required |
| W10 | IDE/TUI inspection and non-coding request | Same shared state; role restriction |
| W11 | Real isolated development request | Model edits/tests, source unchanged, reviewed apply/recovery |

Record command, mode, observation and untested scope. Use disposable repositories/directories. Distinguish real models from scripted providers. The QA request does not authorize unrelated remote publication or merging.

## 15. Implementation stages

1. Implement environment selection, role checks, durable records and prepare/list/status.
2. Add snapshots/diff, selected application, conflict checks and restart recovery.
3. Bind model execution to task workspaces and expose common CLI/IDE/TUI tools. Preparation does not execute models.
4. Add direct journaling and safe stop/remove. Reject unsupported modes clearly.
5. Add remote PR/check/review adapters with evidence tied to head SHA.
6. Record real development tests, fault injection and Playwright results for UI changes. Identify completed and pending stages.

## 16. Implemented scope and usage (2026-10-16)

This section records the initial implemented scope, not completion of every preceding design goal. `internal/adapter/codingworkspace` implements filesystem/Git operations behind `port.CodingTasks`. The application checks the coding role and takes the source from the actual session workdir.

### 16.1 Available operations

- The CLI provides `magi coding-task prepare/list/status/diff/review/run/apply/restore/recover`. Preparation never calls a model. Run uses the existing Magi runner in the task workspace; the process owning model/tool execution also owns the task lease.
- Coding sessions in VS Code, IntelliJ and TUI share the `coding_task` tool for prepare/list/status/diff/review. This tool does not switch the current session's workdir. Start isolated execution with CLI run. Dedicated task UI and automatic companion switching remain pending.
- Git preparation creates a separate branch/worktree without changing the source index. The default is clean HEAD. Dirty sources require explicit `--base HEAD` or `--mode copy`. Git mode rejects repository subdirectories and names the required repository root.
- Without Git, preparation creates an independent copy and never initializes or commits a repository. Diff reports changed paths, hashes and permissions. Review returns exact before/after text for a selected file; binary files and files over 256 KiB are omitted with a reason.
- Copy-mode apply changes only selected paths, preserving conflicts against the baseline. Restore changes a file only while it still matches the applied state. Records and journals survive process restart. Partial/uncertain CLI results retain their JSON outcomes and return exit code 1.
- Recover reopens an interrupted complete workspace after acquiring its lease. Incomplete preparation is preserved for inspection with guidance to use a new request ID. Existing integration journals block repeat application; prepare a new task for subsequent changes after restoration.

Default copying limits are 5,000 regular files and 128 MiB. Retained blobs are limited to 512 MiB and 20,000 entries per task, so repeated reviews cannot accumulate unlimited versions. Exceeding the limit fails without deleting existing recovery data. Exclusions are `.git`, `.magi`, `.codex`, `.ssh`, `.aws`, `node_modules`, `.venv`, `venv`, `.DS_Store`, `.env` and `.env.*`, except `.env.example`. This list does not identify every secret. Symlinks and special files are rejected. Storage defaults to `<config>/coding-tasks`; source and storage cannot contain each other. Resolve existing parent aliases so macOS `/var` and `/tmp` do not change task identity after restart.

CLI operations default to a ten-minute time budget, adjustable with `--timeout 20m`. Public record responses include counts, bytes and exclusions instead of the full baseline manifest. Private records retain the manifest and independent blobs.

### 16.2 Usage

```sh
# Run from the repository root. Auto selects copy when no repository exists.
magi coding-task prepare --request feature-001 --purpose 'Add input validation'
# Use the returned taskId. Preparation does not change source files or call models.
magi coding-task run --id TASK_ID --prompt 'Add input validation and run tests'
magi coding-task diff --id TASK_ID
magi coding-task review --id TASK_ID --path src/example.go
# Non-Git or explicit copy mode: select only reviewed paths.
magi coding-task apply --id TASK_ID src/example.go
magi coding-task restore --id TASK_ID
# Inspect the preserved workspace before recovering an interrupted run.
magi coding-task recover --id TASK_ID
```

Source defaults to the current directory. From elsewhere, pass the same `--source /path/to/project` on every command. Reuse `--store` if customized. Put options before apply's path list. Git tasks refuse file promotion: commit, verify and review a PR from the returned workspace. The task service does not yet create PRs, inspect CI or merge.

### 16.3 Validation and remaining work

Disposable Git repositories and non-Git directories cover independent writes, idempotent/concurrent preparation, path/symlink/budget failures, damaged backups, deletion restoration, restart, restoration after an unrecorded write and human-edit preservation. Built-CLI tests with a scripted provider exercise actual tools and workspace binding. These are not real-model quality results.

A separate real-model test used this machine's `qwen3.8:27b-mlx` to change an erroneous subtraction to addition in temporary `calculator.py` and run `python3 -m unittest -v` with exit 0. It confirmed unchanged source before application, selected application after text review, passing source tests, partial restoration preserving a later human edit, and baseline restoration after explicitly selecting the task version. Council was disabled for this isolation test. It did not validate council approval or a remote PR.

Reproduce with `python3 tools/test-coding-task.py --binary /path/to/magi --model MODEL`. The script leaves logs and acceptance JSON in a temporary directory without modifying existing projects or configuration. It grants allow permissions within this disposable task, so use a test environment. It does not discover or download models.

Remaining work includes dedicated IDE/TUI task UI, automatic session switching, durable direct-edit journaling, stop/remove and retention policy, and PR/CI/review integration. Windows coverage is cross-compilation, not native acceptance. Copies do not contain external shell, database or network effects. Uncooperative external writers can still race path checks; full filesystem durability under power loss is not guaranteed. Partial failure is never reported as complete success.

### 16.4 Additional turn/concurrency validation (20 repetitions)

The following tests were repeated with `-race` and fixed shuffle seed `20261016`. Built-CLI tests also compile the executed Magi binary with `-race`. These runs use a scripted provider to control ordering; they are not twenty real-model runs. Files, processes, Git, leases and durable events are real.

| Suite | Top-level tests | Repetitions | Passes |
| --- | --- | --- | --- |
| Workspace/recovery/concurrency after ownership-check fix | 20 | 20 | 400 |
| Core interjection/cancellation/finish-boundary/model-setting races | 11 | 20 | 220 |
| CLI selected application/partial failure/normal turn/cancelled turn | 4 | 20 | 80 |
| Two separate processes preparing the same task | 1 | 20 | 20 |

The table does not double-count nested subtests. Normal and cancelled turns each ran twenty times. JSONL auditing checks session/workdir identity, increasing sequence numbers, exactly one result following each call, a single completion, and no conversation content after completion. The normal turn expects two foreground model requests. Engram's post-turn lesson analysis is classified separately and must not appear as another answer or completion.

New contention tests verify that two tasks applying to the same source produce one winner and one conflict, that restoring the loser does not undo the winner, and that duplicate application of one task creates one journal. Application/restoration waits behind an active execution lease and can be cancelled without changing source files. Separate processes using the same request ID must prepare only one task.

Testing reproduced an ownership-check ordering problem: using an active task ID from another source waited for its lease until timeout instead of immediately rejecting the source mismatch. A model inside the task workspace could consequently wait on its own execution lease when inspecting the original task ID. `lockExisting` now validates task/source identity before waiting; existing validation still runs after acquisition. The review/diff/apply/restore/recover/run paths each passed twenty repetitions. No failures or race detector reports remained in the repeated suites above.

Reproduce with `go test -race -shuffle=20261016 -count=20 ./internal/adapter/codingworkspace` and `go test -race -shuffle=20261016 -count=20 -run '^TestCodingTask' ./cmd/magi`. The core run selected eleven tests from `interject_races_test.go`, `steer_finish_race_test.go`, `cancel_then_ask_test.go`, `cancel_clears_queue_test.go` and `routing_race_test.go`. Individual names, repetitions and raw-log digests are retained in `docs/qa/coding-task-race20-20261016.json`. Native Windows behavior, nondeterministic real-model decisions and every uncooperative external writer are outside this coverage.

### 16.5 Tool contracts and Gradle output handling

Preparation and execution are separate operations. `coding_task` does not move the current session. Prepare/status responses include `sessionWorkdir`, `sessionWorkdirChanged:false`, guidance and a `runCommand` argument array for a separate runner. Preserve argument boundaries when a path contains spaces. Inspect task results from the source session using taskId with diff/review.

| operation | Required inputs | Additional inputs and scope |
| --- | --- | --- |
| prepare | requestId | mode/base/purpose. No path or taskId. base selects a Git commit/ref and cannot be combined with copy |
| list | none | Tasks belonging to the current session source. No taskId or path |
| status / diff | taskId | ID returned by prepare. No path |
| review | taskId, path | File-relative path within the task, such as `src/main.go` |

Unknown fields and fields unused by an operation are rejected with guidance. Prepare never interprets path as a source selector or silently ignores it. File-tool descriptions state their session boundary and explain that bash cd does not change it. Existing permission and sandbox policies remain in force.

Gradle output is recognized as a `build` directory beside `build.gradle`, `build.gradle.kts`, `settings.gradle` or `settings.gradle.kts`. Cache directories `.gradle`, `.kotlin` and `.gradle-test-kit` are also excluded. These additional rules do not exclude directories containing Git-tracked or existing baseline files. A source directory such as `src/build` without a module marker is retained. Existing baseline scope is preserved, preventing the new policy from turning managed files into apparent deletions. Existing secret/metadata exclusions remain in effect.

The real Gradle project used for reproduction previously exceeded the copy budget at a generated `gradle-api-9.4.1.jar`. The fixed runner prepared a copy in separate temporary storage: 93 files and 454,829 bytes, state ready, with output/cache exclusions disclosed. Source caches and build directories were not deleted. This read-only-source preparation check does not establish that the project's build succeeds.

`bash.timeout` limits foreground execution only, not background jobs. When a job wait expires while the job remains running, `wait_for` returns a status observation with `IsError=false` and guidance to wait again. It neither restarts nor terminates the job. Explicit cancellation, missing jobs and invalid arguments remain errors. The existing string response format is retained.

Regression tests cover generated output, preservation of ordinary/tracked/baseline source, wait expiry versus cancellation, operation argument validation and execution guidance. Each targeted test passed twenty repetitions; full race tests for the affected packages also passed. Running daemons hold tool definitions in memory: replacing the installed binary does not update an existing daemon. Restart it after its active work finishes to use the new contracts and behavior.
