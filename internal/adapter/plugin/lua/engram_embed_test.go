package lua

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
)

func modelsServer(t *testing.T, ids ...string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		body := `{"data":[`
		for i, id := range ids {
			if i > 0 {
				body += ","
			}
			body += `{"id":"` + id + `"}`
		}
		_, _ = w.Write([]byte(body + `]}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// startEngramWithEmbed loads engram, fires startup and returns what /engram-embed status prints.
// The host hands a command's returned text back through the error value.
func startEngramWithEmbed(t *testing.T, reg *stubEmbedReg, url, configToml string) string {
	t.Helper()
	src := filepath.Join("..", "..", "..", "..", "plugins", "engram")
	if _, err := os.Stat(filepath.Join(src, "init.lua")); err != nil {
		t.Skip("bundled engram plugin not present")
	}
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfgPath, []byte(configToml), 0o600); err != nil {
		t.Fatal(err)
	}
	h := NewHostWithConfig(HostConfig{
		ToolSink:      builtin.NewRegistry(),
		ContextReg:    &fakeContextReg{},
		EmbedReg:      reg,
		PluginConfigs: map[string]map[string]any{"engram": {"embed_url": url}},
		ConfigPath:    cfgPath,
		DataDir:       t.TempDir(),
		Runtime:       RuntimeInfo{Workdir: t.TempDir()},
	})
	if _, err := h.Load(context.Background(), src); err != nil {
		t.Fatalf("load engram: %v", err)
	}
	h.FireEvent("startup")
	handled, err := h.DispatchCommand("/engram-embed", []string{"status"})
	if !handled || err == nil {
		t.Fatalf("/engram-embed status: handled=%v out=%v", handled, err)
	}
	return err.Error()
}

// A running loopback server that has bge-m3 is connected at startup, with the tagged model id.
func TestEngramConnectsARunningLocalEmbeddingServer(t *testing.T) {
	srv := modelsServer(t, "llama3:8b", "bge-m3:latest")
	reg := &stubEmbedReg{}
	out := startEngramWithEmbed(t, reg, srv.URL+"/v1", "")
	if reg.url != srv.URL+"/v1" || reg.model != "bge-m3:latest" {
		t.Fatalf("want %s/v1 with bge-m3:latest, got %q %q", srv.URL, reg.url, reg.model)
	}
	if !strings.Contains(out, "연결됨") || !strings.Contains(out, "bge-m3:latest") {
		t.Fatalf("status should report the connection: %q", out)
	}
}

// A server without the model is left alone; recall stays lexical, and status says why.
func TestEngramDoesNotConnectAServerWithoutTheModel(t *testing.T) {
	srv := modelsServer(t, "llama3:8b")
	reg := &stubEmbedReg{}
	out := startEngramWithEmbed(t, reg, srv.URL+"/v1", "")
	if reg.calls != 0 {
		t.Fatalf("no endpoint should be set, got %q %q", reg.url, reg.model)
	}
	if !strings.Contains(out, "미연결") || !strings.Contains(out, "bge-m3 모델이 없습니다") {
		t.Fatalf("status should say why it is not connected: %q", out)
	}
}

// A user who set embed_model in config.toml keeps their own setting.
func TestEngramLeavesAUserConfiguredEmbeddingAlone(t *testing.T) {
	srv := modelsServer(t, "bge-m3:latest")
	reg := &stubEmbedReg{}
	out := startEngramWithEmbed(t, reg, srv.URL+"/v1", "embed_model = \"nomic-embed-text\"\n")
	if reg.calls != 0 {
		t.Fatalf("a configured embed_model must not be overridden, got %q %q", reg.url, reg.model)
	}
	if !strings.Contains(out, "config.toml") {
		t.Fatalf("status should name the user's setting: %q", out)
	}
}
