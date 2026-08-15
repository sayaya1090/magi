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
	Ambient         *bool    `json:"ambient"`
	CrossSession    *bool    `json:"crossSession"`
	CodeProfile     string   `json:"codeProfile"`
	ComposerProfile string   `json:"composerProfile"`
	CommitTemplate  string   `json:"commitTemplate"`
	PRTemplate      string   `json:"prTemplate"`
	Profiles        []string `json:"profiles"` // the [llm.profiles.*] a profile field may name
	File            string   `json:"file"`     // where a write lands, so a person can go and look
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
		Profiles: s.profileNames(path), File: path,
	})
}

// profileNames is every [llm.profiles.*] a completion field may point at: the ones in the file being
// edited, plus this console's global ones, since a project inherits those. Sorted and de-duplicated.
func (s *server) profileNames(path string) []string {
	seen := map[string]bool{}
	add := func(c config.Config) {
		for n := range c.LLM.Profiles {
			seen[n] = true
		}
	}
	if g, err := config.Load(s.cfgDir); err == nil {
		add(g)
	}
	if c, err := config.Load(filepath.Dir(path)); err == nil {
		add(c)
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
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
	if s.forwarded(w, r, s.proxy) || postOnly(w, r) {
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
		did = config.SetKey(path, section, key, strings.TrimSpace(r.FormValue(field)))
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
