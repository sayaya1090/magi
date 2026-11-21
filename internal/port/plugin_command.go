package port

import "context"

// PluginCommandInfo names one plugin slash command for a client's composer.
type PluginCommandInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// PluginCommandHost runs plugin slash commands for a client other than the TUI, which calls the
// plugin host in-process. out is what the command logged while it ran (such a client has no stderr
// to read, so those lines are its answer); handled is false when no plugin owns the name.
type PluginCommandHost interface {
	PluginCommandList() []PluginCommandInfo
	RunPluginCommand(ctx context.Context, name string, args []string) (out string, handled bool, err error)
}
