package main

// The small adapter types that persist interactive edits (permission rules,
// per-agent routes, the session model, profiles) back to config files, plus the
// prompt adapter the plugin host uses. Pure wiring — moved out of main.go.

import (
	"os"
	"path/filepath"

	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/config"
	"github.com/sayaya1090/magi/internal/prompt"
)

// promptFunc adapts tui.RunPrompt to the prompt.Prompter interface the plugin
// host expects.
type promptFunc func(prompt.Spec) (map[string]any, error)

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

func (m modelSetter) SetModel(modelID string) error { m.setModel(modelID); return nil }

func (m modelSetter) SetContextWindow(modelID string, tokens int) error {
	return m.setWindow(modelID, tokens)
}

// userLabelSetter adapts App.SetUserLabel (fire-and-forget: applies to the live
// session and broadcasts) to the plugin host's UserLabelRegistry.
type userLabelSetter struct{ set func(string) }

func (u userLabelSetter) SetUserLabel(label string) { u.set(label) }

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
