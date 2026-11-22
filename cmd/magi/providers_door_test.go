package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// The daemon's providers door offers what the web console's picker offers: the plugin-recorded
// backends that answer now, read from the PLUGIN HOST's store under the config directory. A record
// pointing at nothing is left out — a picker entry that cannot serve would strand the companion.
func TestProvidersListsTheShimsThatAnswer(t *testing.T) {
	d, cfgDir, _ := settingsEngine(t)
	shim := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"opus"}]}`))
	}))
	t.Cleanup(shim.Close)
	pd := filepath.Join(cfgDir, "plugin-data")
	if err := os.MkdirAll(pd, 0o700); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(pd, "claudecode.json"), []byte(`{"provider_base":"`+shim.URL+`/v1"}`), 0o600)
	_ = os.WriteFile(filepath.Join(pd, "antigravity.json"), []byte(`{"shim_port":1}`), 0o600)
	list, err := d.ProvidersHere(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Name != "claudecode" || list[0].Base != shim.URL+"/v1" ||
		len(list[0].Models) != 1 || list[0].Models[0] != "opus" {
		t.Fatalf("the picker offered %+v", list)
	}
}
