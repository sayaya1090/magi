package main

import (
	"net/http"
	"strings"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/adapter/provider"
	"github.com/sayaya1090/magi/internal/config"
	"github.com/sayaya1090/magi/internal/core/session"
)

// configBase is the base_url the config file names right now — the "default" roster entry. Read
// per call rather than held: the file is the source of this fact and a copy taken at startup
// would be wrong from the first config edit. A config that fails to load contributes nothing,
// which is also what an empty base contributes.
func (s *server) configBase() string {
	cfg, err := config.Load(s.cfgDir)
	if err != nil {
		return ""
	}
	return cfg.BaseURL
}

// providers answers GET /providers for the preferences dialog's provider picker. The discovery —
// which backend plugins recorded a shim address, and which of those answer with a catalog right
// now — lives in internal/adapter/provider, shared with the TUI's /providers command so the two
// pickers cannot disagree about what exists.
func (s *server) providers(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		s.useProvider(w, r)
		return
	}
	if s.forwarded(w, r, s.proxy) {
		return
	}
	writeJSON(w, "providers", provider.Discover(r.Context(), s.cfgDir, s.configBase()))
}

// useProvider switches one companion to a backend that is serving right now.
//
// The base is checked against the roster rather than taken as given: this is an address this
// process will make a daemon send prompts (and its key) to, and a request that could name any
// address would be a redirect anybody reaching this console could aim. Only a shim that a plugin
// on this machine recorded AND that answers its catalog is accepted.
func (s *server) useProvider(w http.ResponseWriter, r *http.Request) {
	if s.refuseWhenShared(w, "switching this companion's backend") {
		return
	}
	if s.forwarded(w, r, s.proxy) || postOnly(w, r) {
		return
	}
	want := strings.TrimSpace(r.FormValue("base"))
	if want == "" {
		http.Error(w, "no provider named", http.StatusBadRequest)
		return
	}
	ok := false
	for _, p := range provider.Discover(r.Context(), s.cfgDir, s.configBase()) {
		if p.Base == want {
			ok = true
		}
	}
	if !ok {
		http.Error(w, "no provider is serving at that address", http.StatusBadRequest)
		return
	}
	if err := s.withClient(r, func(cl *daemon.Client, _ session.SessionID) error {
		return cl.UseBackend(want)
	}); err != nil {
		http.Error(w, err.Error(), daemonSaysNo(err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
