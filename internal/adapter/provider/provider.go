// Package provider finds the CLI backend shims serving on this machine, for the pickers.
//
// No provider name is known here. A backend plugin records the port its shim bound into its own
// store (`<root>/plugin-data/<name>.json`, key "shim_port") every time it comes up, so the
// roster is "whoever said where their shim answers" — a fourth backend plugin appears in every
// picker by doing what the first three do, with nothing to add on this side.
//
// One package because there are two pickers: the console's preferences dialog (GET /providers)
// and the TUI's /providers command. Two copies of "read the stores, probe the shims" is two
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

// Provider is one shim that answered: its plugin name, the base_url a profile should carry, and
// the models it offers.
type Provider struct {
	Name   string   `json:"name"`
	Base   string   `json:"base"`
	Models []string `json:"models"`
}

// Discover reads every plugin store that names a shim_port and keeps the ones whose shim answers
// GET /v1/models right now. A recorded port is a claim, not a fact — the daemon may be down, or
// the port re-bound by something else — so a dead shim is left out rather than returned marked: a
// picker offering a provider that cannot serve would write a profile pointing at nothing.
// root is the directory the PLUGIN HOST files stores under — cfg.DataDir in host.go, which
// cmd/magi wires to plat.ConfigDir(), NOT plat.DataDir(). The first version of this read
// DataDir() (the cache directory on macOS) and returned an empty roster forever while two shims
// were serving; the stub test masked it by setting MAGI_DATA_DIR explicitly.
func Discover(ctx context.Context, root string) []Provider {
	out := []Provider{}
	entries, err := os.ReadDir(filepath.Join(root, "plugin-data"))
	if err != nil {
		return out // no plugin ever stored anything: an empty roster, not an error
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
			ShimPort float64 `json:"shim_port"`
		}
		if json.Unmarshal(b, &store) != nil || store.ShimPort <= 0 || store.ShimPort > 65535 {
			continue
		}
		base := fmt.Sprintf("http://127.0.0.1:%d/v1", int(store.ShimPort))
		models := shimModels(ctx, base)
		if len(models) == 0 {
			continue
		}
		out = append(out, Provider{Name: name, Base: base, Models: models})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// shimModels asks one shim for its catalog. The deadline is short on purpose: this runs once per
// candidate on a picker open, against loopback, and a shim that cannot list its models inside a
// second is one no picker should offer.
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
