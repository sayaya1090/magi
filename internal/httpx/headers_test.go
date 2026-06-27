package httpx

import (
	"net/http"
	"testing"
)

func req(t *testing.T) *http.Request {
	t.Helper()
	r, err := http.NewRequest("GET", "http://x", nil)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestHeadersStaticAndProviderOverlay(t *testing.T) {
	h := NewHeaders(map[string]string{"X-A": "static", "X-B": "static"})
	h.AddProvider(func() map[string]string { return map[string]string{"X-B": "dynamic"} })

	r := req(t)
	h.Apply(r)
	if r.Header.Get("X-A") != "static" {
		t.Errorf("X-A = %q", r.Header.Get("X-A"))
	}
	if r.Header.Get("X-B") != "dynamic" { // provider overlays static
		t.Errorf("X-B = %q, want dynamic (provider overlays static)", r.Header.Get("X-B"))
	}
}

func TestHeadersProviderReEvaluated(t *testing.T) {
	n := 0
	h := NewHeaders(nil)
	h.AddProvider(func() map[string]string {
		n++
		return map[string]string{"X-Seq": string(rune('0' + n))}
	})
	r1, r2 := req(t), req(t)
	h.Apply(r1)
	h.Apply(r2)
	if r1.Header.Get("X-Seq") == r2.Header.Get("X-Seq") {
		t.Errorf("provider not re-evaluated: %q == %q", r1.Header.Get("X-Seq"), r2.Header.Get("X-Seq"))
	}
}

func TestHeadersStaticCopied(t *testing.T) {
	src := map[string]string{"X-A": "v"}
	h := NewHeaders(src)
	src["X-A"] = "mutated" // must not affect the header set
	r := req(t)
	h.Apply(r)
	if r.Header.Get("X-A") != "v" {
		t.Errorf("static map not copied: X-A = %q", r.Header.Get("X-A"))
	}
}

func TestHeadersEmpty(t *testing.T) {
	if !NewHeaders(nil).Empty() {
		t.Error("fresh headers should be empty")
	}
	h := NewHeaders(nil)
	h.AddProvider(func() map[string]string { return nil })
	if h.Empty() {
		t.Error("headers with a provider should not be empty")
	}
}
