package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sayaya1090/magi/internal/adapter/platform"
	"github.com/sayaya1090/magi/internal/adapter/provider"
	"github.com/sayaya1090/magi/internal/app"
)

// /providers — the TUI's half of the provider picker.
//
// The console's preferences dialog got two dropdowns; a terminal gets the same facts as a listing
// and the same act as arguments. Both read the one roster (internal/adapter/provider), so the two
// surfaces cannot disagree about which backends are serving.
//
//	/providers                          list who is serving, and each one's models
//	/providers <name> <model…>          save [llm.profiles.<name>] pointing at that shim
//
// The model is the REST of the line, spaces included — agy's names carry spaces and parentheses
// ("Gemini 3.1 Pro (High)"), and asking people to quote in a slash command is asking for a typo
// the gateway reports only at request time.

// providersMsg carries the roster (or a finished save) back to the Update loop, off the goroutine
// that probed the shims — discovery makes an HTTP round-trip per candidate, and the Update loop is
// every keystroke.
type providersMsg struct {
	list []provider.Provider
	note string // non-empty: a one-line outcome to show instead of the listing
}

// cmdProviders answers the /providers command. Both forms leave the Update loop immediately and
// come back as a providersMsg.
func (m *Model) cmdProviders(args []string) tea.Cmd {
	a := m.app
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		list := provider.Discover(ctx, platform.New().DataDir())
		if len(args) == 0 {
			return providersMsg{list: list}
		}
		name := args[0]
		model := strings.TrimSpace(strings.Join(args[1:], " "))
		var pick *provider.Provider
		for i := range list {
			if list[i].Name == name {
				pick = &list[i]
			}
		}
		if pick == nil {
			return providersMsg{note: fmt.Sprintf("no provider %q is serving — /providers lists who is", name)}
		}
		if model == "" {
			return providersMsg{note: "which model? /providers " + name + " <model — the rest of the line, spaces and all>"}
		}
		// Held to the shim's own catalog, because a name it does not serve is a 502 on the next
		// request and nothing before that would have said so.
		found := false
		for _, id := range pick.Models {
			if id == model {
				found = true
			}
		}
		if !found {
			return providersMsg{note: fmt.Sprintf("%q is not in %s's catalog — /providers lists it", model, name)}
		}
		// SetProfile registers it live (routable this session) and persists [llm.profiles.<name>].
		a.SetProfile(app.ProfileDef{Name: name, BaseURL: pick.Base, Model: model})
		return providersMsg{note: fmt.Sprintf("profile %q → %s on %s — pick it in /route", name, model, pick.Base)}
	}
}

// renderProviders is the listing: one block, provider by provider, each with its catalog and the
// exact line that would save a profile for it.
func renderProviders(list []provider.Provider) string {
	if len(list) == 0 {
		return "no provider is serving — enable a backend plugin ([plugins.<name>] enabled = true) and start its daemon"
	}
	var b strings.Builder
	b.WriteString("CLI backends serving now:\n")
	for _, p := range list {
		fmt.Fprintf(&b, "\n%s  (%s)\n", p.Name, p.Base)
		for _, id := range p.Models {
			fmt.Fprintf(&b, "  · %s\n", id)
		}
		fmt.Fprintf(&b, "  save a profile: /providers %s <model>\n", p.Name)
	}
	return strings.TrimRight(b.String(), "\n")
}
