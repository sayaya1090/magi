package main

import (
	"net/http"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sayaya1090/magi/internal/config"
)

// The named LLM backends ([llm.profiles.*]) a person can define and edit from the console, so a fast
// autocomplete profile — or any per-agent backend — can be set up without opening config.toml or the
// TUI's /route editor.
//
// Same shape and same rules as the MCP screen (mcp.go): a named table written key by key into the
// config of whichever companion the console is looking at (or the global config), keeping a hand-
// written file's comments, taking effect on that daemon's next start. The one difference is the KEY:
// an api_key is a secret, so — like the MCP screen names env vars rather than their values — the GET
// never returns the key, only whether one is set. A write leaves the key untouched unless a new one
// is given (so editing the model does not wipe it), and clearKey removes it.
type llmProfile struct {
	Name      string `json:"name"`
	Companion string `json:"companion,omitempty"` // whose config it is in; empty = this console's global
	Socket    string `json:"socket,omitempty"`
	Tier      string `json:"tier"` // "project" | "global"
	BaseURL   string `json:"baseUrl,omitempty"`
	Model     string `json:"model,omitempty"`
	HasKey    bool   `json:"hasKey"` // whether api_key is set — the value is never sent to the browser
	File      string `json:"file"`   // where it is written, so a person can go and look
}

func (s *server) profilesList(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		s.profilesWrite(w, r)
		return
	}
	if s.forwarded(w, r, s.proxy) {
		return
	}
	out := []llmProfile{}
	if global, err := config.Load(s.cfgDir); err == nil {
		out = append(out, profilesOf(global, "global", "", "", filepath.Join(s.cfgDir, "config.toml"))...)
	}
	local, err := s.companionDirs(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for _, c := range local {
		dir := filepath.Join(c.workdir, ".magi")
		if cfg, cerr := config.Load(dir); cerr == nil {
			out = append(out, profilesOf(cfg, "project", c.name, c.socket, filepath.Join(dir, "config.toml"))...)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Tier != out[j].Tier {
			return out[i].Tier == "global" // the ones every companion here inherits, first
		}
		if out[i].Companion != out[j].Companion {
			return out[i].Companion < out[j].Companion
		}
		return out[i].Name < out[j].Name
	})
	// A project row names a workspace's own endpoint and whether it carries a key; filtered by whose
	// config it came out of, like the MCP list.
	out = onlySeen(s, r, out, func(p llmProfile) (string, string) { return p.Companion, "" })
	writeJSON(w, "profiles", out)
}

func profilesOf(c config.Config, tier, companion, socket, file string) []llmProfile {
	out := make([]llmProfile, 0, len(c.LLM.Profiles))
	for name, p := range c.LLM.Profiles {
		out = append(out, llmProfile{
			Name: name, Companion: companion, Socket: socket, Tier: tier,
			BaseURL: p.BaseURL, Model: p.Model, HasKey: strings.TrimSpace(p.APIKey) != "", File: file,
		})
	}
	return out
}

// profilesWrite adds, changes or removes one profile definition.
func (s *server) profilesWrite(w http.ResponseWriter, r *http.Request) {
	// A profile is an endpoint this machine will send prompts (and its key) to. Editing it on a
	// console more people than the operator can reach is the config route wearing a settings screen
	// — the same reason the MCP write refuses there.
	if s.refuseWhenShared(w, "changing this machine's model profiles") {
		return
	}
	if s.forwarded(w, r, s.proxy) || postOnly(w, r) {
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if !bareName(name) {
		// The name becomes a bare TOML table header, so it is held to that set — refused rather than
		// sanitised. An allowlist ([A-Za-z0-9_-]): anything else (a space, dot, bracket, control
		// character, comma, non-ASCII letter) would split or break the header and leave the file
		// unparseable, so it is rejected here rather than reaching config.SetKey.
		http.Error(w, "a profile needs a name, and the name can only contain letters, numbers, "+
			"hyphens and underscores", http.StatusBadRequest)
		return
	}
	path, err := s.configFileFor(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	section := "llm.profiles." + name

	if r.FormValue("delete") != "" {
		if err := config.RemoveSection(path, section); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeText(w, "removed profile "+name+" from "+path)
		return
	}

	set := func(key, val string) error { return config.SetKey(path, section, key, strings.TrimSpace(val)) }
	if err := set("base_url", r.FormValue("baseUrl")); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := set("model", r.FormValue("model")); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// The key is written only when a new one is given, so editing the endpoint or model does not wipe
	// a key set earlier; clearKey removes it. ${ENV} is the recommended form (kept out of the file),
	// but a literal value is the operator's own choice — the same latitude the /route editor gives.
	switch {
	case r.FormValue("clearKey") != "":
		if err := set("api_key", ""); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	case strings.TrimSpace(r.FormValue("apiKey")) != "":
		if err := set("api_key", r.FormValue("apiKey")); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	writeText(w, "wrote profile "+name+" to "+path+". It applies when that companion's daemon next "+
		"starts — this changed the file, not a running process.")
}
