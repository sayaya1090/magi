package httpx

import (
	"net/http"
	"sync"
	"testing"
)

// TestAddStaticMergesAndInitsNilMap: AddStatic merges into a header set seeded with nil
// static (the nil-map init branch) and a no-op on empty input.
func TestAddStaticMergesAndInitsNilMap(t *testing.T) {
	h := NewHeaders(nil) // static is nil internally
	h.AddStatic(nil)     // no-op, must not panic or un-empty
	if !h.Empty() {
		t.Fatal("AddStatic(nil) on empty headers should leave it empty")
	}
	h.AddStatic(map[string]string{"X-A": "1"}) // hits the nil-map init branch
	h.AddStatic(map[string]string{"X-B": "2"}) // second source merges
	r := req(t)
	h.Apply(r)
	if r.Header.Get("X-A") != "1" || r.Header.Get("X-B") != "2" {
		t.Fatalf("merge failed: X-A=%q X-B=%q", r.Header.Get("X-A"), r.Header.Get("X-B"))
	}
}

// TestAddStaticOverwritesEarlier: a later AddStatic value replaces an earlier one for the
// same key.
func TestAddStaticOverwritesEarlier(t *testing.T) {
	h := NewHeaders(map[string]string{"X-A": "first"})
	h.AddStatic(map[string]string{"X-A": "second"})
	r := req(t)
	h.Apply(r)
	if got := r.Header.Get("X-A"); got != "second" {
		t.Fatalf("X-A = %q, want second", got)
	}
}

// TestAddProviderNilIgnored: AddProvider(nil) is dropped, so Apply doesn't panic calling it,
// and the set stays empty if that was the only addition.
func TestAddProviderNilIgnored(t *testing.T) {
	h := NewHeaders(nil)
	h.AddProvider(nil)
	if !h.Empty() {
		t.Fatal("AddProvider(nil) should be ignored, leaving headers empty")
	}
	h.Apply(req(t)) // must not panic
}

// TestApplyOverridesExistingRequestHeader: Apply uses Set (replace), so a header already on
// the request is overwritten by a static header — the caller controls ordering by where it
// calls Apply (the doc's "before/after protocol headers" contract).
func TestApplyOverridesExistingRequestHeader(t *testing.T) {
	h := NewHeaders(map[string]string{"X-A": "fromHeaders"})
	r := req(t)
	r.Header.Set("X-A", "preexisting")
	h.Apply(r)
	if got := r.Header.Get("X-A"); got != "fromHeaders" {
		t.Fatalf("X-A = %q, want fromHeaders (Apply should Set-override)", got)
	}
}

// TestApplyNilProviderMapNoop: a provider returning nil contributes nothing and doesn't
// panic (range over a nil map is a no-op).
func TestApplyNilProviderMapNoop(t *testing.T) {
	h := NewHeaders(map[string]string{"X-A": "1"})
	h.AddProvider(func() map[string]string { return nil })
	r := req(t)
	h.Apply(r)
	if r.Header.Get("X-A") != "1" {
		t.Fatalf("static header lost: X-A=%q", r.Header.Get("X-A"))
	}
}

// TestApplyLaterProviderWins: among providers, a later one overlays an earlier one for the
// same key (doc: "among providers, later ones win").
func TestApplyLaterProviderWins(t *testing.T) {
	h := NewHeaders(nil)
	h.AddProvider(func() map[string]string { return map[string]string{"X-K": "early"} })
	h.AddProvider(func() map[string]string { return map[string]string{"X-K": "late"} })
	r := req(t)
	h.Apply(r)
	if got := r.Header.Get("X-K"); got != "late" {
		t.Fatalf("X-K = %q, want late", got)
	}
}

// TestConcurrentApplyAddProviderAddStatic exercises the documented "safe for concurrent
// Apply, AddStatic, and AddProvider" claim — run with -race. AddStatic concurrent with Apply
// is the case that used to be a data race (Apply ranged the live static map outside the lock).
func TestConcurrentApplyAddProviderAddStatic(t *testing.T) {
	h := NewHeaders(map[string]string{"X-Base": "v"})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			h.AddProvider(func() map[string]string { return map[string]string{"X-P": "p"} })
		}()
		go func(i int) {
			defer wg.Done()
			h.AddStatic(map[string]string{"X-S": string(rune('a' + i))})
		}(i)
		go func() {
			defer wg.Done()
			r, _ := http.NewRequest("GET", "http://x", nil)
			h.Apply(r)
			h.Empty()
		}()
	}
	wg.Wait()
}
