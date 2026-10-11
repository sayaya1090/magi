# Coding tasks and worktree design

[한국어](CODING_WORKTREE_DESIGN.ko.md) · [Application client design](APPLICATION_CLIENTS_DESIGN.md)

This is a design, not an implemented service. The repository currently has no common service managing user worktrees. Running `git worktree` alone does not manage task lifetime or companion ownership. The service will support existing terminal, VS Code and JetBrains coding agents as well as GitHub.

## 1. Placement

Use `repository → coding task → worktree → dedicated companion → session`. An `internal/app` task service owns lifetime and records; adapters behind ports execute Git and start processes. Every UI calls the same task API. Extensions and IDEs must not implement independent Git commands and recovery state.

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
4. Add CLI first, then GitHub, VS Code and JetBrains. All surfaces must list the same task identically.
5. Implement stop/remove. Test refusals for modified/untracked files, active turns and externally owned worktrees.
6. Run real CLI acceptance and browser Playwright. Another reviewer checks paths, command arguments, recovery and deletion contracts. Mark implementation complete only after recording execution evidence.

Initial application clients work without this service. The GitHub helper connects sessions in an explicitly configured local checkout. Do not add a nonfunctional worktree button before these APIs exist.
