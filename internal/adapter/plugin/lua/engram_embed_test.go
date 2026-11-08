package lua

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func writeFakeDocker(t *testing.T, binDir, stateDir, arch string) string {
	t.Helper()
	logPath := filepath.Join(stateDir, "docker.log")
	script := `#!/bin/sh
echo "$*" >> "` + logPath + `"
case "$1" in
  version)
    echo "` + arch + `"
    exit 0
    ;;
  inspect)
    if [ -f "` + stateDir + `/container_running" ]; then
      echo "true"
      exit 0
    fi
    exit 1
    ;;
  run)
    touch "` + stateDir + `/container_running"
    echo "fake-container-id"
    exit 0
    ;;
  start)
    touch "` + stateDir + `/container_running"
    exit 0
    ;;
  exec)
    touch "` + stateDir + `/model_pulled"
    echo "pulling manifest... success"
    exit 0
    ;;
esac
exit 0
`
	dockerPath := filepath.Join(binDir, "docker")
	if err := os.WriteFile(dockerPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return logPath
}

// When no server is running, /engram-embed start (or auto_start) detects the container
// runtime/arch, launches the magi-engram-embed container in the background, pulls bge-m3
// asynchronously without blocking recall, and connects the embedding endpoint once ready.
func TestEngramStartsContainerPullsModelInBackgroundAndConnects(t *testing.T) {
	src := filepath.Join("..", "..", "..", "..", "plugins", "engram")
	if _, err := os.Stat(filepath.Join(src, "init.lua")); err != nil {
		t.Skip("bundled engram plugin not present")
	}

	binDir := t.TempDir()
	stateDir := t.TempDir()
	logPath := writeFakeDocker(t, binDir, stateDir, "arm64")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		if _, err := os.Stat(filepath.Join(stateDir, "container_running")); err != nil {
			http.Error(w, "server down", http.StatusServiceUnavailable)
			return
		}
		if _, err := os.Stat(filepath.Join(stateDir, "model_pulled")); err == nil {
			if _, werr := w.Write([]byte(`{"object":"list","data":[{"id":"bge-m3:latest"}]}`)); werr != nil {
				t.Log(werr)
			}
			return
		}
		if _, werr := w.Write([]byte(`{"object":"list","data":null}`)); werr != nil {
			t.Log(werr)
		}
	}))
	t.Cleanup(srv.Close)

	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfgPath, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	reg := &stubEmbedReg{}
	h := NewHostWithConfig(HostConfig{
		ToolSink:   builtin.NewRegistry(),
		ContextReg: &fakeContextReg{},
		EmbedReg:   reg,
		PluginConfigs: map[string]map[string]any{
			"engram": {
				"embed_url":        srv.URL + "/v1",
				"embed_auto_start": "true",
			},
		},
		ConfigPath: cfgPath,
		DataDir:    t.TempDir(),
		Runtime:    RuntimeInfo{Workdir: t.TempDir()},
	})
	if _, err := h.Load(context.Background(), src); err != nil {
		t.Fatalf("load engram: %v", err)
	}

	// Startup begins background container provisioning without blocking or setting endpoint yet.
	h.FireEvent("startup")
	if reg.calls != 0 {
		t.Fatalf("endpoint should not be set before container and model are ready, got %d calls", reg.calls)
	}

	// Poll /engram-embed status to advance the background state machine (run -> pull -> connect).
	var statusOut string
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		handled, err := h.DispatchCommand("/engram-embed", []string{"status"})
		if !handled || err == nil {
			t.Fatalf("/engram-embed status: handled=%v err=%v", handled, err)
		}
		statusOut = err.Error()
		if reg.calls > 0 {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}

	if reg.url != srv.URL+"/v1" || reg.model != "bge-m3:latest" {
		t.Fatalf("want auto-connected endpoint %s/v1 (bge-m3:latest), got %q (%q); last status=%q", srv.URL, reg.url, reg.model, statusOut)
	}
	if !strings.Contains(statusOut, "연결됨") || !strings.Contains(statusOut, "[docker/arm64]") {
		t.Fatalf("status should report connected endpoint and [docker/arm64], got %q", statusOut)
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read docker log: %v", err)
	}
	logText := string(logBytes)
	if !strings.Contains(logText, "run -d --name magi-engram-embed") {
		t.Fatalf("expected docker run for magi-engram-embed, got:\n%s", logText)
	}
	if !strings.Contains(logText, "exec magi-engram-embed ollama pull bge-m3") {
		t.Fatalf("expected docker exec ollama pull bge-m3, got:\n%s", logText)
	}

	// Verify /engram-embed reselect re-evaluates runtime and arch.
	handled, err := h.DispatchCommand("/engram-embed", []string{"reselect"})
	if !handled || err == nil {
		t.Fatalf("/engram-embed reselect: handled=%v err=%v", handled, err)
	}
	if !strings.Contains(err.Error(), "docker (arch=arm64, image=ollama/ollama)") {
		t.Fatalf("unexpected reselect output: %q", err.Error())
	}
}
