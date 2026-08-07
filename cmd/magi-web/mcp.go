package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sayaya1090/magi/internal/config"
)

// The external tool servers a companion can reach, and editing them.
//
// # Why this is a screen at all
//
// An MCP server is where a companion's reach leaves this machine's file system: a ticket tracker, a
// design tool, an internal API. Which ones a companion has decides what it can do, and until now
// the only way to know was to open its config file — per companion, on whichever machine it is on.
// A supervisor holding five of them had no way to answer "which of these can see production".
//
// # Why editing here is not the same as a companion editing it
//
// A person at the console changing their own companion's config is the same act as opening the file
// in an editor, and it lands in the same place: `<workspace>/.magi/config.toml`, in git, readable
// and diffable. What is refused elsewhere in this tree is a COMPANION accepting server definitions
// from another companion (see cmd/magi/join.go) — because that is a command arriving from the
// network, and this is a person deciding.
//
// # What it does not do
//
// It does not start, stop or restart anything. Servers are attached when a daemon starts, so an
// edit takes effect on that companion's next start — said plainly in the answer rather than left
// for somebody to discover when their new server never appears.
type mcpServer struct {
	Name      string   `json:"name"`
	Companion string   `json:"companion,omitempty"` // whose config it is in; empty = this console's global
	Socket    string   `json:"socket,omitempty"`
	Tier      string   `json:"tier"`          // "project" | "global"
	URL       string   `json:"url,omitempty"` // HTTP transport
	Command   string   `json:"command,omitempty"`
	Args      []string `json:"args,omitempty"`
	// EnvNames is the names only. A value here is a token, and a token on a page is a token in a
	// browser history, a screenshot and a support ticket.
	EnvNames []string `json:"envNames,omitempty"`
	File     string   `json:"file"` // where it is written, so a person can go and look
}

func (s *server) mcp(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		s.mcpWrite(w, r)
		return
	}
	out := []mcpServer{}
	if global, err := config.Load(s.cfgDir); err == nil {
		out = append(out, serversOf(global, "global", "", "", filepath.Join(s.cfgDir, "config.toml"))...)
	}
	local, err := s.companionDirs(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for _, c := range local {
		dir := filepath.Join(c.workdir, ".magi")
		cfg, cerr := config.Load(dir)
		if cerr != nil {
			continue
		}
		out = append(out, serversOf(cfg, "project", c.name, c.socket, filepath.Join(dir, "config.toml"))...)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Tier != out[j].Tier {
			return out[i].Tier == "global" // the one every companion here inherits, first
		}
		if out[i].Companion != out[j].Companion {
			return out[i].Companion < out[j].Companion
		}
		return out[i].Name < out[j].Name
	})
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(out); err != nil {
		log.Printf("magi-web: writing the mcp servers: %v", err)
	}
}

func serversOf(c config.Config, tier, companion, socket, file string) []mcpServer {
	out := make([]mcpServer, 0, len(c.MCP))
	for name, srv := range c.MCP {
		e := mcpServer{
			Name: name, Companion: companion, Socket: socket, Tier: tier,
			URL: srv.URL, Command: srv.Command, Args: srv.Args, File: file,
		}
		for _, kv := range srv.Env {
			n, _, _ := strings.Cut(kv, "=")
			e.EnvNames = append(e.EnvNames, n)
		}
		out = append(out, e)
	}
	return out
}

// mcpWrite adds, changes or removes one server definition.
func (s *server) mcpWrite(w http.ResponseWriter, r *http.Request) {
	if s.forwarded(w, r, s.proxy) || postOnly(w, r) {
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" || strings.ContainsAny(name, " \t[]\"'#.") {
		// The name becomes a TOML table header, so it has to be one. Refused rather than sanitised:
		// a name quietly rewritten is a server the person cannot find again in their own file.
		http.Error(w, "a server needs a name, and the name cannot contain spaces, quotes, "+
			"brackets, a hash or a dot", http.StatusBadRequest)
		return
	}
	path, err := s.mcpFileFor(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	section := "mcp." + name

	if r.FormValue("delete") != "" {
		if err := config.RemoveSection(path, section); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeMCPResult(w, "removed "+name+" from "+path)
		return
	}

	url, command := strings.TrimSpace(r.FormValue("url")), strings.TrimSpace(r.FormValue("command"))
	switch {
	case url == "" && command == "":
		http.Error(w, "a server is either a url (HTTP) or a command (stdio) — this is neither",
			http.StatusBadRequest)
		return
	case url != "" && command != "":
		// Both would silently take the URL branch at startup, so the command would sit in the file
		// looking configured and never run.
		http.Error(w, "a server is either a url or a command, not both", http.StatusBadRequest)
		return
	}

	// Written key by key onto whatever is already there, so a table somebody hand-wrote keeps its
	// comments and any key this build does not know about.
	set := func(key, val string) error { return config.SetKey(path, section, key, val) }
	setRaw := func(key, raw string) error { return config.SetRawKey(path, section, key, raw) }
	steps := []func() error{
		func() error { return set("url", url) },
		func() error { return set("command", command) },
		func() error { return setRaw("args", tomlList(splitArgs(r.FormValue("args")))) },
		func() error { return setRaw("env", tomlList(splitArgs(r.FormValue("env")))) },
	}
	for _, step := range steps {
		if err := step(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	writeMCPResult(w, "wrote "+name+" to "+path+
		". It attaches when that companion's daemon next starts — this changed the file, not a "+
		"running process.")
}

// mcpFileFor is which config a write lands in: a named companion's project file, or this console's
// global one when nothing is named.
//
// The path comes from the published record, never from the request. A page that could name the file
// could name any file, and this writes to it.
func (s *server) mcpFileFor(r *http.Request) (string, error) {
	if r.URL.Query().Get("d") == "" && r.FormValue("tier") == "global" {
		return filepath.Join(s.cfgDir, "config.toml"), nil
	}
	in, err := s.target(r)
	if err != nil {
		return "", err
	}
	if in.Workdir == "" {
		return "", fmt.Errorf("that companion published no workspace, so there is no project config to write")
	}
	return filepath.Join(in.Workdir, ".magi", "config.toml"), nil
}

func writeMCPResult(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if _, err := w.Write([]byte(msg)); err != nil {
		log.Printf("magi-web: answering an mcp write: %v", err)
	}
}

// splitArgs reads a command line's worth of arguments, one per line or separated by spaces.
//
// Newlines first, because an argument with a space in it is ordinary and a person typing a list one
// per line means one per line. Only a single-line value is split on spaces.
func splitArgs(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var raw []string
	if strings.ContainsAny(s, "\r\n") {
		raw = strings.FieldsFunc(s, func(r rune) bool { return r == '\n' || r == '\r' })
	} else {
		raw = strings.Fields(s)
	}
	out := make([]string, 0, len(raw))
	for _, a := range raw {
		if a = strings.TrimSpace(a); a != "" {
			out = append(out, a)
		}
	}
	return out
}

// tomlList renders a TOML array, or "" to clear the key.
func tomlList(xs []string) string {
	if len(xs) == 0 {
		return ""
	}
	quoted := make([]string, len(xs))
	for i, x := range xs {
		quoted[i] = strconvQuote(x)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

func strconvQuote(s string) string {
	b, err := json.Marshal(s)
	if err != nil { // a string always marshals; a broken one must not become unquoted TOML
		return `""`
	}
	return string(b)
}
