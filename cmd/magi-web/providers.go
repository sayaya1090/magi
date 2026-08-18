package main

import (
	"net/http"

	"github.com/sayaya1090/magi/internal/adapter/provider"
)

// providers answers GET /providers for the preferences dialog's provider picker. The discovery —
// which backend plugins recorded a shim address, and which of those answer with a catalog right
// now — lives in internal/adapter/provider, shared with the TUI's /providers command so the two
// pickers cannot disagree about what exists.
func (s *server) providers(w http.ResponseWriter, r *http.Request) {
	if s.forwarded(w, r, s.proxy) {
		return
	}
	writeJSON(w, "providers", provider.Discover(r.Context(), s.dataDir))
}
