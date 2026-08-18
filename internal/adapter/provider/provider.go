// Package provider finds the LLM backends reachable on this machine, for the pickers.
//
// No provider name is known here. A backend plugin records where its backend answers into its own
// store (`<root>/plugin-data/<name>.json`) every time it comes up, so the roster is "whoever said
// where their backend answers" — a new backend plugin appears in every picker by doing what the
// existing ones do, with nothing to add on this side. Two records are understood:
//
//	shim_port      a loopback OpenAI shim this plugin serves itself (the CLI backends)
//	provider_base  a full base URL the plugin routes to (a remote gateway); may come with
//	               provider_models, the catalog it last saw, for gateways whose /models
//	               endpoint demands the auth this unauthenticated probe cannot send
//
// The backend the CONFIG file names is also on the roster, as "default" — it is the one backend
// that exists without any plugin saying so, and leaving it out made every switch a one-way door:
// the picker could move a companion onto a CLI backend but never back.
//
// One package because there are two pickers: the console's provider dropdown (GET /providers)
// and the TUI's /providers command. Two copies of "read the stores, probe the backends" is two
// answers to which providers exist, and they would disagree the first time one changed.
package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Provider is one backend that answered: its name (the plugin's, or "default" for the config
// file's backend), the base_url a profile should carry, and the models it offers.
type Provider struct {
	Name   string   `json:"name"`
	Base   string   `json:"base"`
	Models []string `json:"models"`
}

// Discover reads every plugin store that names a backend and keeps the ones that answer right
// now, then adds the config file's own backend as "default" when cfgBase names one. A recorded
// address is a claim, not a fact — the daemon may be down, or the port re-bound by something
// else — so a dead backend is left out rather than returned marked: a picker offering a provider
// that cannot serve would write a profile pointing at nothing.
//
// root is the directory the PLUGIN HOST files stores under — cfg.DataDir in host.go, which
// cmd/magi wires to plat.ConfigDir(), NOT plat.DataDir(). The first version of this read
// DataDir() (the cache directory on macOS) and returned an empty roster forever while two shims
// were serving; the stub test masked it by setting MAGI_DATA_DIR explicitly.
//
// cfgBase is the config file's base_url, "" to skip. It is probed like the rest — a config
// pointing at a stopped Ollama is not a backend anybody can be put on — and dropped when a
// plugin's record already names the same address, so taking over the default does not list the
// same backend twice.
func Discover(ctx context.Context, root, cfgBase string) []Provider {
	out := []Provider{}
	taken := map[string]bool{}
	entries, err := os.ReadDir(filepath.Join(root, "plugin-data"))
	if err != nil {
		entries = nil // no plugin ever stored anything: an empty roster, not an error
	}
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok {
			continue
		}
		b, rerr := os.ReadFile(filepath.Join(root, "plugin-data", e.Name()))
		if rerr != nil {
			continue
		}
		var store struct {
			ShimPort       float64  `json:"shim_port"`
			ProviderBase   string   `json:"provider_base"`
			ProviderModels []string `json:"provider_models"`
		}
		if json.Unmarshal(b, &store) != nil {
			continue
		}
		base, recorded := "", []string(nil)
		switch {
		case store.ProviderBase != "":
			base, recorded = strings.TrimRight(store.ProviderBase, "/"), store.ProviderModels
		case store.ShimPort > 0 && store.ShimPort <= 65535:
			base = fmt.Sprintf("http://127.0.0.1:%d/v1", int(store.ShimPort))
		default:
			continue
		}
		models, reachable := backendModels(ctx, base)
		if len(models) == 0 && reachable && len(recorded) > 0 {
			// The server is there but its catalog is behind auth this probe cannot send (a
			// gateway 401). The plugin's own record — written by a client that IS signed in —
			// stands in. A server that did not answer at all gets no such benefit: recorded
			// models on a dead address would be a picker entry pointing at nothing.
			models = recorded
		}
		if len(models) == 0 {
			continue
		}
		out = append(out, Provider{Name: name, Base: base, Models: models})
		taken[base] = true
	}
	if cfgBase = strings.TrimRight(strings.TrimSpace(cfgBase), "/"); cfgBase != "" && !taken[cfgBase] {
		if models, _ := backendModels(ctx, cfgBase); len(models) > 0 {
			out = append(out, Provider{Name: "default", Base: cfgBase, Models: models})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// backendModels asks one backend for its catalog; reachable reports whether anything answered at
// all, so a gateway that refuses the unauthenticated probe (401) can still be told apart from a
// dead address. The deadline is short on purpose — this runs once per candidate on a picker open —
// but not one second: a shim whose catalog comes from its CLI can spend a couple of seconds on the
// FIRST answer after a daemon start, and at 1s the roster dropped a serving backend for exactly
// those moments, which read as "antigravity is gone". The shims also warm their catalogs at
// activation now; this is the second belt.
func backendModels(ctx context.Context, base string) (models []string, reachable bool) {
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, base+"/models", nil)
	if err != nil {
		return nil, false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, true
	}
	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if json.NewDecoder(resp.Body).Decode(&body) != nil {
		return nil, true
	}
	for _, m := range body.Data {
		if m.ID != "" {
			models = append(models, m.ID)
		}
	}
	return models, true
}
