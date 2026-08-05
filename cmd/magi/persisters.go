package main

// The small adapter types that persist interactive edits (permission rules,
// per-agent routes, the session model, profiles) back to config files, plus the
// prompt adapter the plugin host uses. Pure wiring — moved out of main.go.

import (
	"os"
	"path/filepath"
	"strconv"

	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/config"
	"github.com/sayaya1090/magi/internal/prompt"
)

// promptFunc adapts tui.RunPrompt to the prompt.Prompter interface the plugin
// host expects.
type promptFunc func(prompt.Spec) (map[string]any, error)

//coverage:ignore func-to-interface adapter: it calls the function it wraps and nothing else
func (f promptFunc) Ask(s prompt.Spec) (map[string]any, error) { return f(s) }

// routePersister writes /route editor edits back to the global config.toml,
// preserving its comments, so per-agent routing and the session model survive
// restarts.
// permPersister appends "always allow (project)" rules to the project config
// (.magi/config.toml), which teams commit — so a trusted tool stays trusted for
// everyone across sessions. The directory is created on first use.
type permPersister struct{ path string }

func (p permPersister) PersistAllow(rule string) error {
	if err := os.MkdirAll(filepath.Dir(p.path), 0o755); err != nil {
		return err
	}
	return config.AppendListItem(p.path, "allow", rule)
}

// modelSetter adapts App's model-configuration methods to the plugin host's
// ModelRegistry: SetModel (fire-and-forget — applies to the live session and
// best-effort persists) and SetContextWindow (returns a note we discard and an
// error we surface). Both close over the current session id.
type modelSetter struct {
	setModel  func(string)
	setWindow func(model string, tokens int) error
}

//coverage:ignore delegation to the closure the struct was built with
func (m modelSetter) SetModel(modelID string) error { m.setModel(modelID); return nil }

//coverage:ignore delegation to the closure the struct was built with
func (m modelSetter) SetContextWindow(modelID string, tokens int) error {
	return m.setWindow(modelID, tokens)
}

// userLabelSetter adapts App.SetUserLabel (fire-and-forget: applies to the live
// session and broadcasts) to the plugin host's UserLabelRegistry.
type userLabelSetter struct{ set func(string) }

//coverage:ignore delegation to the closure the struct was built with
func (u userLabelSetter) SetUserLabel(label string) { u.set(label) }

// subagentPersister records a /subagents toggle so it survives a restart. The list is names that
// are OFF: a subagent is on unless something says otherwise, which is what makes an unknown name
// (a plugin that was uninstalled) harmless.
type subagentPersister struct{ path string }

func (s subagentPersister) PersistSubagent(name string, pref app.SubagentPref) error {
	// One table per subagent, so the enabled flag and the model override sit together under a name
	// a reader recognises: [subagents.<name>].
	table := "subagents." + name
	if pref.Enabled != nil {
		if err := config.SetKey(s.path, table, "enabled", strconv.FormatBool(*pref.Enabled)); err != nil {
			return err
		}
	}
	if err := config.SetKey(s.path, table, "model", pref.Model); err != nil {
		return err
	}
	return config.SetKey(s.path, table, "provider", pref.Provider)
}

// toSubagentPrefs converts the config shape into the app's, preserving the nil Enabled that means
// "the user made no choice" — collapsing it to false would switch off every subagent that ships on.
func toSubagentPrefs(in map[string]config.SubagentConfig) map[string]app.SubagentPref {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]app.SubagentPref, len(in))
	for name, c := range in {
		out[name] = app.SubagentPref{Enabled: c.Enabled, Model: c.Model, Provider: c.Provider}
	}
	return out
}

type routePersister struct{ path string }

func (r routePersister) PersistRoute(agent, value string) error {
	return config.SetKey(r.path, "routing", agent, value)
}

func (r routePersister) PersistModel(modelID string) error {
	return config.SetKey(r.path, "", "model", modelID)
}

func (r routePersister) PersistProfile(p app.ProfileDef) error {
	sec := "llm.profiles." + p.Name
	if err := config.SetKey(r.path, sec, "base_url", p.BaseURL); err != nil {
		return err
	}
	if err := config.SetKey(r.path, sec, "api_key", p.APIKey); err != nil {
		return err
	}
	if err := config.SetKey(r.path, sec, "model", p.Model); err != nil {
		return err
	}
	for k, v := range p.Headers {
		if err := config.SetKey(r.path, sec+".headers", k, v); err != nil {
			return err
		}
	}
	return nil
}
