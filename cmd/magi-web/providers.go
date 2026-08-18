package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The provider picker's facts: which backend plugins are serving an OpenAI shim right now, and
// what models each offers.
//
// # Where the list comes from
//
// No provider name is known to this process. A backend plugin records the port its shim bound
// into its own store (`<dataDir>/plugin-data/<name>.json`, key "shim_port") every time it comes
// up, so the roster here is "whoever said where their shim answers" — a fourth backend plugin
// appears in the picker by doing what the first three do, with nothing to add on this side.
//
// A recorded port is a claim, not a fact: the daemon may be down, or the port re-bound by
// something else. So each candidate is asked GET /v1/models with a short deadline, and only the
// ones that answer with a catalog are returned. A dead shim is left out rather than shown greyed —
// a picker offering a provider that cannot serve would write a profile pointing at nothing.
type providerInfo struct {
	Name   string   `json:"name"`
	Base   string   `json:"base"` // the base_url a profile should carry
	Models []string `json:"models"`
}

// providers answers GET /providers for the preferences dialog's provider picker.
func (s *server) providers(w http.ResponseWriter, r *http.Request) {
	if s.forwarded(w, r, s.proxy) {
		return
	}
	out := []providerInfo{}
	entries, err := os.ReadDir(filepath.Join(s.dataDir, "plugin-data"))
	if err != nil {
		writeJSON(w, "providers", out) // no plugin ever stored anything: an empty picker, not a 500
		return
	}
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok {
			continue
		}
		b, rerr := os.ReadFile(filepath.Join(s.dataDir, "plugin-data", e.Name()))
		if rerr != nil {
			continue
		}
		var store struct {
			ShimPort float64 `json:"shim_port"`
		}
		if json.Unmarshal(b, &store) != nil || store.ShimPort <= 0 || store.ShimPort > 65535 {
			continue
		}
		base := fmt.Sprintf("http://127.0.0.1:%d/v1", int(store.ShimPort))
		models := shimModels(r.Context(), base)
		if len(models) == 0 {
			continue // recorded but not answering: the daemon is down, or the port moved on
		}
		out = append(out, providerInfo{Name: name, Base: base, Models: models})
	}
	writeJSON(w, "providers", out)
}

// shimModels asks one shim for its catalog. The deadline is short on purpose: this runs once per
// candidate on a dialog open, against loopback, and a shim that cannot list its models inside a
// second is one the picker should not offer.
func shimModels(ctx context.Context, base string) []string {
	cctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, base+"/models", nil)
	if err != nil {
		return nil
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if json.NewDecoder(resp.Body).Decode(&body) != nil {
		return nil
	}
	models := make([]string, 0, len(body.Data))
	for _, m := range body.Data {
		if m.ID != "" {
			models = append(models, m.ID)
		}
	}
	return models
}
