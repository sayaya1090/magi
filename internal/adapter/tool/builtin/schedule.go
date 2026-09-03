package builtin

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// Schedule sets up work that runs on its own, later, without anyone asking again.
//
// It is the only tool here whose effect outlives the turn that called it, so the description says
// so to the model as plainly as the code says it to a reader: this leaves something behind.
type Schedule struct{}

type scheduleArgs struct {
	Action   string `json:"action"`
	Name     string `json:"name"`
	Schedule string `json:"schedule"`
	Prompt   string `json:"prompt"`
	Enabled  *bool  `json:"enabled"`
	// Command is read only to REFUSE it — see the check in Run.
	//
	// Declared rather than dropped because encoding/json discards a field nobody named, and a
	// request that asked for something and got a normal answer is worse than one that was told no:
	// the model would believe it had scheduled a command that was never written.
	Command string `json:"command"`
}

func (Schedule) Name() string { return "schedule" }

func (Schedule) Description() string {
	return "See or change the work this workspace does on a timer, unattended. " +
		"{action} is \"list\" (the default), \"set\", or \"remove\". " +
		"To set one, pass {name} (short, no spaces or dots), {schedule} as a crontab line " +
		"(\"0 9 * * 1-5\" is weekdays at nine) or a shorthand (@daily, @hourly, @weekly), and " +
		"{prompt} — the words to ask when it runs, written so they make sense to someone with no " +
		"memory of this conversation. {enabled} false switches a job off without deleting it. " +
		"Setting a job leaves it running every time it comes round, for as long as the workspace " +
		"exists, so use it for standing work (a nightly check, a Monday report) rather than for " +
		"something to do once. It only runs while a daemon holds this workspace, and never catches " +
		"up on times that went by while nothing was running."
}

func (Schedule) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{` +
		`"action":{"type":"string","enum":["list","set","remove"],"description":"what to do; defaults to list"},` +
		`"name":{"type":"string","description":"short name for the job — no spaces, quotes, brackets or dots"},` +
		`"schedule":{"type":"string","description":"crontab line (\"0 9 * * 1-5\") or shorthand (@daily); on an existing job, omit to keep its current time"},` +
		`"prompt":{"type":"string","description":"what to ask when it runs; on an existing job, omit to keep its current words"},` +
		`"enabled":{"type":"boolean","description":"false switches the job off without removing it; omit to leave it as it is"}` +
		`}}`)
}

func (Schedule) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	if env.Schedule == nil {
		return errResult("", "scheduling is not available here"), nil
	}
	var a scheduleArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errResult("", "invalid arguments: "+err.Error()), nil
	}
	// **A scheduled command is not yours to write.**
	//
	// A `command` job runs unattended without going through the tool permission gate — writing it
	// into the config file IS the approval, the same way an `allow` rule is. That only holds while
	// the thing being approved was put there by a person: if this tool could write one, a model
	// could grant itself an unattended shell, every night, on a schedule nobody read.
	//
	// Refused with the reason and the other road, rather than dropped: a request that asked for
	// something and got a normal answer would leave the model believing it had scheduled a command
	// that was never written.
	if strings.TrimSpace(a.Command) != "" {
		return errResult("", "a scheduled command is not something this tool may write — it would "+
			"run unattended with no approval step, so it has to come from a person: put it in the "+
			"workspace's .magi/config.toml, or set it from the console or the IDE. A scheduled "+
			"prompt is what this tool writes."), nil
	}
	out, err := env.Schedule(port.ScheduleChange{
		Action: a.Action, Name: a.Name, Schedule: a.Schedule, Prompt: a.Prompt, Enabled: a.Enabled,
	})
	if err != nil {
		return errResult("", "could not change the schedule: "+err.Error()), nil
	}
	return okText("", out), nil
}
