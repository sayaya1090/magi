package daemon

import (
	"context"
	"strings"

	"github.com/sayaya1090/magi/internal/port"
)

// PluginCommandHost is what the plugin-command doors need: the plugin host's slash commands.
type PluginCommandHost = port.PluginCommandHost

const noPluginCommands = "this daemon cannot run plugin commands"

// answerCommands lists the slash commands loaded plugins contribute, so a composer can tell a
// plugin command (/routine-draft …) from text meant for the model.
func answerCommands(ctx context.Context, eng Engine, _ Request) Response {
	h, ok := eng.(PluginCommandHost)
	if !ok {
		return Response{Err: noPluginCommands}
	}
	return Response{OK: true, Commands: h.PluginCommandList()}
}

// answerCommandRun runs one: Name is the command (a leading slash is dropped), Text the rest of the
// line, split on whitespace as the TUI splits it. Out carries what the command logged while it ran;
// its own failure (a refusal it returned) is the answer's error, with whatever it logged before.
func answerCommandRun(ctx context.Context, eng Engine, req Request) Response {
	h, ok := eng.(PluginCommandHost)
	if !ok {
		return Response{Err: noPluginCommands}
	}
	name := strings.TrimPrefix(strings.TrimSpace(req.Name), "/")
	if name == "" {
		return Response{Err: "a command name is required"}
	}
	out, handled, err := h.RunPluginCommand(ctx, name, strings.Fields(req.Text))
	if !handled {
		return Response{Err: "unknown command /" + name}
	}
	if err != nil {
		return Response{Err: err.Error(), Out: out}
	}
	return Response{OK: true, Out: out}
}
