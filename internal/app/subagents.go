package app

import (
	"sort"
	"strings"

	"github.com/sayaya1090/magi/internal/port"
)

// SubagentInfo is one plugin-declared subagent, for the list a user manages.
type SubagentInfo struct {
	Name        string
	Description string
	Group       string // as the plugin declared it; empty means ungrouped
	Enabled     bool
	// Model and Provider are the user's override, empty when they set none — in which case the
	// child runs on whatever the plugin asked for, or the session's model.
	Model    string
	Provider string
}

// Subagents lists every tool a plugin declared as a subagent, ordered by group then name so the
// list is stable across frames (map iteration is randomized, and a list that reshuffles under the
// cursor is unusable).
//
// magi declares none — the list is empty until a plugin registers one, which is the same reason
// the spawn seam itself is inert out of the box.
func (a *App) Subagents() []SubagentInfo {
	if a.tools == nil {
		return nil
	}
	a.mu.Lock()
	prefs := clonePrefs(a.subagentPrefs)
	a.mu.Unlock()

	var out []SubagentInfo
	for _, t := range a.tools.List() {
		m := port.ToolMetaOf(t)
		if !m.Subagent {
			continue
		}
		// The user's own choice wins; absent, the tool's declaration decides. That is what lets a
		// subagent ship switched off and still be turned on for good.
		pref := prefs[t.Name()]
		enabled := !m.DefaultOff
		if pref.Enabled != nil {
			enabled = *pref.Enabled
		}
		out = append(out, SubagentInfo{
			Name: t.Name(), Description: t.Description(), Group: m.Group,
			Enabled: enabled, Model: pref.Model, Provider: pref.Provider,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Group != out[j].Group {
			// Ungrouped last: a named group is a deliberate grouping and reads better at the top.
			if out[i].Group == "" || out[j].Group == "" {
				return out[j].Group == ""
			}
			return out[i].Group < out[j].Group
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// SetSubagentEnabled turns one subagent on or off and persists the choice.
//
// Off means NOT ADVERTISED: toolSpecs skips it, so the model is never offered a subagent the user
// switched off. It is not unregistered — undoing that would mean holding the tool object aside and
// keeping it in step with plugin reloads, and a disabled set is the same answer without the second
// copy of the truth.
func (a *App) SetSubagentEnabled(name string, on bool) error {
	return a.editSubagent(name, func(pref *SubagentPref) { pref.Enabled = &on })
}

// SetSubagentModel points one subagent at a model (and optionally a named backend), or clears the
// override when both are empty. The user's choice overrides whatever the plugin asked to spawn
// with — planning on a strong model while the work runs on a cheap one is the case this exists for,
// and it belongs where the user can see it rather than in a plugin's own config section.
func (a *App) SetSubagentModel(name, model, provider string) error {
	return a.editSubagent(name, func(pref *SubagentPref) {
		pref.Model, pref.Provider = strings.TrimSpace(model), strings.TrimSpace(provider)
	})
}

// editSubagent applies one change to a subagent's settings and persists the result. One seam, so
// every edit is written the same way and none can forget to persist.
func (a *App) editSubagent(name string, edit func(*SubagentPref)) error {
	a.mu.Lock()
	if a.subagentPrefs == nil {
		a.subagentPrefs = map[string]SubagentPref{}
	}
	pref := a.subagentPrefs[name]
	edit(&pref)
	a.subagentPrefs[name] = pref
	p := a.cfg.SubagentPersister
	a.mu.Unlock()
	if p == nil {
		return nil
	}
	return p.PersistSubagent(name, pref)
}

// subagentOverride is the model a user pointed this subagent at, empty when they set none.
func (a *App) subagentOverride(name string) (model, provider string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	pref := a.subagentPrefs[name]
	return pref.Model, pref.Provider
}

// SetGroupEnabled turns every subagent in a group on or off.
//
// The group has no state of its own — this writes each member. A stored group flag could disagree
// with its members, and nothing would force them back into step; the header a user sees is derived
// from the members instead (all on, all off, or mixed).
func (a *App) SetGroupEnabled(group string, on bool) error {
	var firstErr error
	for _, s := range a.Subagents() {
		if s.Group != group {
			continue
		}
		if err := a.SetSubagentEnabled(s.Name, on); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// subagentDisabled reports whether this subagent must not be advertised: the user switched it off,
// or it ships off and they have not switched it on.
func (a *App) subagentDisabled(name string, meta port.ToolMetadata) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if pref, ok := a.subagentPrefs[name]; ok && pref.Enabled != nil {
		return !*pref.Enabled
	}
	return meta.DefaultOff
}

// clonePrefs copies the settings map so the app never shares one with its caller or its reader.
func clonePrefs(in map[string]SubagentPref) map[string]SubagentPref {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]SubagentPref, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
