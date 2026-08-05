package app

import (
	"sort"

	"github.com/sayaya1090/magi/internal/port"
)

// SubagentInfo is one plugin-declared subagent, for the list a user manages.
type SubagentInfo struct {
	Name        string
	Description string
	Group       string // as the plugin declared it; empty means ungrouped
	Enabled     bool
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
	off := make(map[string]bool, len(a.disabledSubagents))
	for k, v := range a.disabledSubagents {
		off[k] = v
	}
	a.mu.Unlock()

	var out []SubagentInfo
	for _, t := range a.tools.List() {
		m := port.ToolMetaOf(t)
		if !m.Subagent {
			continue
		}
		out = append(out, SubagentInfo{
			Name: t.Name(), Description: t.Description(), Group: m.Group, Enabled: !off[t.Name()],
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
	a.mu.Lock()
	if a.disabledSubagents == nil {
		a.disabledSubagents = map[string]bool{}
	}
	if on {
		delete(a.disabledSubagents, name)
	} else {
		a.disabledSubagents[name] = true
	}
	p := a.cfg.SubagentPersister
	a.mu.Unlock()
	if p == nil {
		return nil
	}
	return p.PersistSubagent(name, on)
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

// subagentDisabled reports whether this tool is a subagent the user switched off.
func (a *App) subagentDisabled(name string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.disabledSubagents[name]
}

// disabledSet turns the configured name list into the lookup the app keeps.
func disabledSet(names []string) map[string]bool {
	if len(names) == 0 {
		return nil
	}
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}
