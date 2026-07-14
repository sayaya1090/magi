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
