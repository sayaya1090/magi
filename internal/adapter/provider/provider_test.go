package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The roster is whoever recorded a shim address AND answers on it — both halves, because each
// alone writes profiles pointing at nothing: a name with no probe offers dead backends, a probe
// with no record has nowhere to look.
func TestDiscoverKeepsOnlyTheShimsThatAnswer(t *testing.T) {
	shim := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/models") {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"object":"list","data":[{"id":"alpha"},{"id":"beta"}]}`)
	}))
	defer shim.Close()
	addr := shim.Listener.Addr().String()
	port := addr[strings.LastIndex(addr, ":")+1:]

	dir := t.TempDir()
	pd := filepath.Join(dir, "plugin-data")
	if err := os.MkdirAll(pd, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(pd, "live.json"), []byte(`{"shim_port":`+port+`}`), 0o644)
	os.WriteFile(filepath.Join(pd, "dead.json"), []byte(`{"shim_port":1}`), 0o644)
	os.WriteFile(filepath.Join(pd, "engram.json"), []byte(`{"outcomes":3}`), 0o644)

	got := Discover(context.Background(), dir)
	if len(got) != 1 || got[0].Name != "live" {
		t.Fatalf("the roster is %v; want exactly the shim that answers", got)
	}
	if len(got[0].Models) != 2 || got[0].Models[0] != "alpha" {
		t.Errorf("the catalog came back as %v", got[0].Models)
	}
	if !strings.HasSuffix(got[0].Base, "/v1") {
		t.Errorf("base %q is not the base_url a profile carries", got[0].Base)
	}
}

// A machine where no plugin ever stored anything is the common case, and it is an empty roster,
// not an error.
func TestNoStoresIsAnEmptyRoster(t *testing.T) {
	if got := Discover(context.Background(), t.TempDir()); len(got) != 0 {
		t.Fatalf("an empty data dir produced %v", got)
	}
}
