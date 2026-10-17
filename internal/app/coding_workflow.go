package app

// This is a model instruction, not a filesystem sandbox. The workspace service
// still owns preparation/recovery, and existing permission gates own execution.
func (a *App) codingWorkflowGuide() string {
	if a.cfg.CodingTasks == nil || NormalizeClientKind(a.cfg.ClientRole) != ClientCoding {
		return ""
	}
	if a.cfg.CodingTaskExecution {
		return "\n\n# Coding task execution\nThis runner is ALREADY inside the prepared coding-task workspace. Do not prepare another task or create a nested worktree. Modify and test here. Report the resulting changes and verification to the parent; preparing this workspace was not integration into the original source."
	}
	return codingWorkflowGuide
}

const codingWorkflowGuide = `

# Coding task workflow (required for development changes)
Reading, investigating and answering questions alone do not require a coding task.
Unless the user explicitly requests a different workflow, before changing source in a Git repository, inspect its status and worktree identity. If this is already a linked Git worktree for the task, use it; do not nest worktrees. Otherwise you MUST call coding_task with operation=prepare, mode=git, a new stable requestId, and the task purpose before making development changes. Reuse that requestId only for identical retries. A prepared task is not a completed task.
Preserve pre-existing user changes. Git preparation starts from a commit and does not include uncommitted source edits; do not silently discard or ignore changes needed by this task. Never stash, reset or commit somebody else's changes as cleanup.
Preparation does NOT move this session's file-tool root. Work and run checks in the returned workspace, using the returned runCommand as a separate runner, or explicit bash commands in that directory. Do not pass the workspace to this session's jailed read/grep/edit/write tools and do not fall back to editing the original source. A separate runner already inside that workspace must not prepare another task.
When there is no Git repository, do not initialize Git just for isolation. Use coding_task mode=copy for recoverable changes and the documented selected-path review/apply process.
Before claiming the development request is complete: inspect the task diff, run appropriate verification on its current changes, commit/review the Git changes, and complete the authorized integration into the intended source branch (including PR and passing CI when required by the user's workflow). Check actual integration evidence; a clean task worktree, tool return or model report alone is not proof. If integration is blocked, report the remaining task and cause instead of saying it was merged. Do not call council complete=true to report an unintegrated workspace as a fully completed change.
`
