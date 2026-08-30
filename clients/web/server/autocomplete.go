package main

import (
	"net/http"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sayaya1090/magi/internal/config"
)

// The completion settings a person tunes from the console: which fast profile does code completion,
// which does composer suggestion, whether the file being edited rides into the agent's context, and
// the house rules the commit/PR drafts follow.
//
// These are SERVER settings, not the browser's: a profile assignment and a template are read by the
// daemon that runs the model, so they live in that companion's config.toml (or this console's global
// one), written the same way the MCP screen writes servers — key by key, keeping a hand-written file's
// comments — and take effect on that daemon's next start. The two ON/OFF switches for code and
// composer completion are the browser's own (localStorage), because they gate whether THIS console
// asks at all; these are the things the daemon needs to know.
type autocompleteSettings struct {
	Ambient         *bool  `json:"ambient"`
	CrossSession    *bool  `json:"crossSession"`
	CodeProfile     string `json:"codeProfile"`
	ComposerProfile string `json:"composerProfile"`
	CommitTemplate  string `json:"commitTemplate"`
	PRTemplate      string `json:"prTemplate"`
	// Profiles is every [llm.profiles.*] a completion field may point at, each with WHERE it comes
	// from. The name alone was enough to fill the picker and not enough to answer the question a
	// reader actually has in front of it — why a profile they just added is not on the list. It is
	// on the list of the tier they wrote it to, and the picker had no way to say so.
	Profiles []profileChoice `json:"profiles"`
	File     string          `json:"file"` // where a write lands, so a person can go and look
}

func (s *server) autocomplete(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		s.autocompleteWrite(w, r)
		return
	}
	if s.forwarded(w, r, s.proxy) {
		return
	}
	path, err := s.configFileFor(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	cfg, _ := config.Load(filepath.Dir(path))
	ac := cfg.Autocomplete
	writeJSON(w, "autocomplete", autocompleteSettings{
		Ambient: ac.Ambient, CrossSession: ac.CrossSession,
		CodeProfile: ac.CodeProfile, ComposerProfile: ac.ComposerProfile,
		CommitTemplate: cfg.Templates.Commit, PRTemplate: cfg.Templates.PR,
		Profiles: s.profileChoices(path), File: path,
	})
}

// profileChoice is one assignable backend and the tier it was defined in.
type profileChoice struct {
	Name string `json:"name"`
	// Tier is "global" or "project". A name defined in BOTH is reported as project, because that is
	// which one wins: the project table is merged over the global one (mergeProjectConfig), so the
	// picker must not say "global" about a definition the daemon will not use.
	Tier string `json:"tier"`
}

// profileChoices is every [llm.profiles.*] a completion field may point at: the ones in the file
// being edited and the global ones, which is exactly the set the daemon resolves for that file.
func (s *server) profileChoices(path string) []profileChoice {
	tier := map[string]string{}
	add := func(c config.Config, t string) {
		for n := range c.LLM.Profiles {
			tier[n] = t
		}
	}
	if g, err := config.Load(s.cfgDir); err == nil {
		add(g, "global")
	}
	// Second, so a name defined in both ends up marked project — the tier that wins.
	if c, err := config.Load(filepath.Dir(path)); err == nil {
		add(c, "project")
	}
	out := make([]profileChoice, 0, len(tier))
	for n, t := range tier {
		out = append(out, profileChoice{Name: n, Tier: t})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// autocompleteWrite changes one field at a time (the form carries only the control that changed), so
// a person flipping ambient does not rewrite the templates and lose a comment. A boolean goes in raw
// (enabled = false, not "false", which would fail the typed field); the profiles and templates go in
// quoted; an empty value clears the key, which returns that setting to its default.
func (s *server) autocompleteWrite(w http.ResponseWriter, r *http.Request) {
	// A profile assignment decides which backend the daemon spends on a keystroke, and a template
	// steers what it drafts — both are configuration of a running companion, so on a console more
	// people than the operator can reach this is the config route wearing a settings screen.
	if s.refuseWhenShared(w, "changing this companion's completion settings") {
		return
	}
	if postOnly(w, r) || s.forwarded(w, r, s.proxy) {
		return
	}
	path, err := s.configFileFor(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var did error
	// A pointer bool: present in the form means write it, "on"/"off" → raw true/false; "clear" removes
	// the key (back to the default-on).
	boolField := func(field, key string) {
		if did != nil || !r.Form.Has(field) {
			return
		}
		raw := ""
		switch r.FormValue(field) {
		case "on", "true":
			raw = "true"
		case "off", "false":
			raw = "false"
		}
		did = config.SetRawKey(path, "autocomplete", key, raw)
	}
	strField := func(section, field, key string) {
		if did != nil || !r.Form.Has(field) {
			return
		}
		did = config.SetKey(path, section, key, stripControl(strings.TrimSpace(r.FormValue(field))))
	}

	boolField("ambient", "ambient")
	boolField("crossSession", "cross_session")
	strField("autocomplete", "codeProfile", "code_profile")
	strField("autocomplete", "composerProfile", "composer_profile")
	strField("templates", "commitTemplate", "commit")
	strField("templates", "prTemplate", "pr")

	if did != nil {
		http.Error(w, did.Error(), http.StatusInternalServerError)
		return
	}
	writeText(w, "Saved to "+path+". It takes effect when that companion's daemon next starts — this "+
		"changed the file, not a running process.")
}

// stripControl removes characters that config.SetKey's %q would escape into TOML the parser then
// rejects: BurntSushi's decoder (default mode) does not accept \x.., \a or \v, so a control byte in a
// pasted template would write a config.toml that fails to re-parse — bricking the daemon's next start
// and this console's own reads. Tab and newline are kept (a template is often multi-line); a stray CR
// is dropped so CRLF pastes normalise to LF. Everything else below space, and DEL, is removed.
// stripControl is config.StripControl — the one gate, kept beside the writer it protects after
// the socket door was written without it. This alias keeps the console on the shared spelling.
func stripControl(s string) string { return config.StripControl(s) }

// bareName is config.BareName — the one gate for a name that becomes a TOML table header. The rule
// moved beside SetKey (the function it protects) after per-writer copies kept drifting; this alias
// keeps the three web writers (profilesWrite, mcpWrite, cronWrite) on the shared spelling.
func bareName(s string) bool { return config.BareName(s) }
