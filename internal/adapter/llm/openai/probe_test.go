package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ProbeContextWindow discovers the context length from each backend convention:
// vLLM (/v1/models max_model_len), LiteLLM (/model/info max_input_tokens), and Ollama
// (/api/show context_length); plain OpenAI (no field anywhere) returns false.
func TestProbeContextWindow(t *testing.T) {
	cases := []struct {
		name    string
		handler http.HandlerFunc
		want    int
		ok      bool
	}{
		{
			name: "vllm max_model_len",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/models" {
					_, _ = w.Write([]byte(`{"data":[{"id":"m","max_model_len":131072}]}`))
					return
				}
				http.NotFound(w, r)
			},
			want: 131072, ok: true,
		},
		{
			name: "litellm model_info",
			handler: func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/models":
					_, _ = w.Write([]byte(`{"data":[{"id":"m"}]}`)) // no context field → falls through
				case "/model/info":
					_, _ = w.Write([]byte(`{"data":[{"model_name":"m","model_info":{"max_input_tokens":200000}}]}`))
				default:
					http.NotFound(w, r)
				}
			},
			want: 200000, ok: true,
		},
		{
			name: "ollama api/show",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/show" && r.Method == http.MethodPost {
					_, _ = w.Write([]byte(`{"model_info":{"llama.context_length":262144}}`))
					return
				}
				http.NotFound(w, r)
			},
			want: 262144, ok: true,
		},
		{
			name: "plain openai exposes nothing",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/models" {
					_, _ = w.Write([]byte(`{"data":[{"id":"m","object":"model"}]}`))
					return
				}
				http.NotFound(w, r)
			},
			want: 0, ok: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(c.handler)
			defer srv.Close()
			got, ok := New(srv.URL, "").ProbeContextWindow(context.Background(), "m")
			if ok != c.ok || got != c.want {
				t.Errorf("ProbeContextWindow = (%d,%v), want (%d,%v)", got, ok, c.want, c.ok)
			}
		})
	}
}

// A zero/garbage context value is ignored (asInt rejects non-positive), so a backend
// returning max_model_len: 0 falls through rather than seeding a useless 0 window.
func TestProbeRejectsZero(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			_, _ = w.Write([]byte(`{"data":[{"id":"m","max_model_len":0}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	if _, ok := New(srv.URL, "").ProbeContextWindow(context.Background(), "m"); ok {
		t.Error("a zero context length should be rejected, not accepted")
	}
}

// A single-model server that advertises the model under an id different from the alias the
// client was launched with (e.g. vLLM exposing the full HF path) still resolves: the sole
// entry carrying a context field is unambiguous. With several models advertised the id is
// genuinely ambiguous, so the fallback declines rather than guess.
func TestProbeSingleModelIDMismatch(t *testing.T) {
	single := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			_, _ = w.Write([]byte(`{"data":[{"id":"Qwen/Qwen3-Coder-30B","max_model_len":262144}]}`))
			return
		}
		http.NotFound(w, r)
	}
	srv := httptest.NewServer(http.HandlerFunc(single))
	defer srv.Close()
	// launched as "qwen3-coder" but the server calls it "Qwen/Qwen3-Coder-30B"
	if got, ok := New(srv.URL, "").ProbeContextWindow(context.Background(), "qwen3-coder"); !ok || got != 262144 {
		t.Fatalf("ProbeContextWindow = (%d,%v), want (262144,true) via single-entry fallback", got, ok)
	}

	multi := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			_, _ = w.Write([]byte(`{"data":[{"id":"a","max_model_len":1000},{"id":"b","max_model_len":2000}]}`))
			return
		}
		http.NotFound(w, r)
	}
	msrv := httptest.NewServer(http.HandlerFunc(multi))
	defer msrv.Close()
	if _, ok := New(msrv.URL, "").ProbeContextWindow(context.Background(), "c"); ok {
		t.Error("with several models and no id match the probe must decline, not guess")
	}
}

// The probe reads the base URL dynamically, so a runtime override installed by a plugin
// (magi.set_base_url) redirects it. This is what lets main() defer the startup probe until
// after plugin startup: the probe then targets the plugin-configured backend instead of the
// default localhost endpoint. A regression here would silently reintroduce the localhost hit.
func TestProbeFollowsRuntimeBaseURL(t *testing.T) {
	var stale, live int
	staleSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stale++
		http.NotFound(w, r) // stands in for the default localhost endpoint: no such model
	}))
	defer staleSrv.Close()
	liveSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		live++
		if r.URL.Path == "/models" {
			_, _ = w.Write([]byte(`{"data":[{"id":"m","max_model_len":131072}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer liveSrv.Close()

	c := New(staleSrv.URL, "")
	c.SetBaseURL(liveSrv.URL) // plugin repoints the backend at runtime
	got, ok := c.ProbeContextWindow(context.Background(), "m")
	if !ok || got != 131072 {
		t.Fatalf("ProbeContextWindow = (%d,%v), want (131072,true) via the overridden base", got, ok)
	}
	if live == 0 {
		t.Error("probe never hit the plugin-set backend")
	}
	if stale != 0 {
		t.Errorf("probe hit the stale default base %d time(s); it must follow the runtime override", stale)
	}
}

// The two Ollama sources answer different questions, and only one of them says what this server
// will actually serve. A server started with a smaller num_ctx still reports the architecture's
// trained length from /api/show, so a caller that trusts it sizes its compaction trigger against
// a limit the backend never reaches — the shape that let a run climb to 123K on a host that
// could not hold it, with the trigger sitting at 210K.
func TestOllamaRuntimeWindowBeatsTheTrainedOne(t *testing.T) {
	var showHits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/ps":
			_, _ = w.Write([]byte(`{"models":[
				{"name":"other:latest","context_length":8192},
				{"name":"qwen3-coder-next:latest","model":"qwen3-coder-next:latest","context_length":98304}]}`))
		case r.URL.Path == "/api/show":
			showHits++
			_, _ = w.Write([]byte(`{"model_info":{"qwen3next.context_length":262144}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	// Fully qualified, and the untagged form a config file would carry: both must find the
	// running instance, or the common spelling silently gets the trained number.
	for _, id := range []string{"qwen3-coder-next:latest", "qwen3-coder-next"} {
		got, ok := New(srv.URL, "").ProbeContextWindow(context.Background(), id)
		if !ok || got != 98304 {
			t.Errorf("ProbeContextWindow(%q) = (%d,%v), want the runtime 98304", id, got, ok)
		}
	}
	if showHits != 0 {
		t.Errorf("/api/show was consulted %d times though the instance was running", showHits)
	}

	// A model that is not loaded is not in /api/ps at all — the runtime value does not exist
	// yet, so the trained length is the only answer there is.
	got, ok := New(srv.URL, "").ProbeContextWindow(context.Background(), "not-loaded")
	if !ok || got != 262144 {
		t.Errorf("an unloaded model must fall back to /api/show, got (%d,%v)", got, ok)
	}
}

// An empty /api/ps (nothing loaded at all) must fall through rather than answer 0.
func TestOllamaEmptyPSFallsThrough(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/ps":
			_, _ = w.Write([]byte(`{"models":[]}`))
		case "/api/show":
			_, _ = w.Write([]byte(`{"model_info":{"qwen3next.context_length":262144}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	if got, ok := New(srv.URL, "").ProbeContextWindow(context.Background(), "m"); !ok || got != 262144 {
		t.Errorf("empty /api/ps must fall through to /api/show, got (%d,%v)", got, ok)
	}
}

// A /api/ps entry with no context_length is not an answer of zero.
func TestOllamaPSWithoutContextLengthFallsThrough(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/ps":
			_, _ = w.Write([]byte(`{"models":[{"name":"m","size":123}]}`))
		case "/api/show":
			_, _ = w.Write([]byte(`{"model_info":{"qwen3next.context_length":262144}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	if got, ok := New(srv.URL, "").ProbeContextWindow(context.Background(), "m"); !ok || got != 262144 {
		t.Errorf("a ps entry with no context_length must fall through, got (%d,%v)", got, ok)
	}
}
