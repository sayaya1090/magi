package embed

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The request is the OpenAI shape and nothing else.
//
// This is the whole portability claim. Ollama, vLLM, LiteLLM and the hosted APIs all answer
// POST /v1/embeddings with {model, input[]}; anything vendor-specific here would tie a search to
// one backend, and the endpoint is already configured once for the rest of magi.
func TestItSpeaksTheOneShapeEverybodyAnswers(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{
			{"index": 0, "embedding": []float32{1, 0}},
			{"index": 1, "embedding": []float32{0, 1}},
		}})
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL + "/v1", APIKey: "k", Model: "any-embed", CacheDir: t.TempDir(), HTTP: srv.Client()}
	got, err := c.Embed(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/v1/embeddings" {
		t.Errorf("posted to %q", gotPath)
	}
	if gotAuth != "Bearer k" {
		t.Errorf("authorization %q", gotAuth)
	}
	if gotBody["model"] != "any-embed" {
		t.Errorf("model %v", gotBody["model"])
	}
	if in, ok := gotBody["input"].([]any); !ok || len(in) != 2 {
		t.Errorf("input %v — the batch has to be one request, not one per text", gotBody["input"])
	}
	if len(got) != 2 || len(got[0]) != 2 {
		t.Fatalf("got %v", got)
	}
}

// Embedded once, then read from disk — and an edited note is a different note.
func TestAVectorIsPaidForOnceAndAnEditIsANewKey(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var in struct {
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		data := make([]map[string]any, len(in.Input))
		for i := range in.Input {
			data[i] = map[string]any{"index": i, "embedding": []float32{float32(len(in.Input[i])), 1}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer srv.Close()

	dir := t.TempDir()
	c := &Client{BaseURL: srv.URL + "/v1", Model: "m", CacheDir: dir, HTTP: srv.Client()}
	if _, err := c.Embed(context.Background(), []string{"the invoice job"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Embed(context.Background(), []string{"the invoice job"}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("%d requests for the same text twice; the cache did nothing", calls)
	}
	// Content-addressed: a note that was edited must not be served the old note's vector, which is
	// a wrong answer no reader could detect.
	if _, err := c.Embed(context.Background(), []string{"the invoice job, amended"}); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Errorf("%d requests; edited text has to be a new key", calls)
	}
	// And a different model does not read another model's vectors — they are not comparable.
	other := &Client{BaseURL: srv.URL + "/v1", Model: "other", CacheDir: dir, HTTP: srv.Client()}
	if _, err := other.Embed(context.Background(), []string{"the invoice job"}); err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Errorf("%d requests; one model read another's cache", calls)
	}
}

// A short answer is refused rather than paired up wrongly.
//
// The protocol is positional. A backend that returns three vectors for four inputs would, if
// trusted, rank every document after the gap against somebody else's text — wrong in a way nobody
// reading the results could see.
func TestAShortAnswerIsRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{
			{"index": 0, "embedding": []float32{1, 0}},
		}})
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL + "/v1", Model: "m", CacheDir: t.TempDir(), HTTP: srv.Client()}
	_, err := c.Embed(context.Background(), []string{"a", "b"})
	if err == nil {
		t.Fatal("two inputs and one vector was accepted")
	}
	if !strings.Contains(err.Error(), "2") || !strings.Contains(err.Error(), "1") {
		t.Errorf("the error does not say how many were missing: %v", err)
	}
}

// No endpoint, no model: the caller is told before it asks rather than after a failed request.
func TestAvailableSaysWhetherThereIsAnythingToAsk(t *testing.T) {
	for _, c := range []struct {
		name string
		c    *Client
		want bool
	}{
		{"nil", nil, false},
		{"no url", &Client{Model: "m"}, false},
		{"no model", &Client{BaseURL: "http://x/v1"}, false},
		{"both", &Client{BaseURL: "http://x/v1", Model: "m"}, true},
	} {
		if got := c.c.Available(); got != c.want {
			t.Errorf("%s: %v", c.name, got)
		}
	}
}

// Cosine has no opinion where it cannot have one.
func TestCosine(t *testing.T) {
	if got := Cosine([]float32{1, 0}, []float32{1, 0}); got < 0.999 {
		t.Errorf("identical vectors scored %v", got)
	}
	if got := Cosine([]float32{1, 0}, []float32{0, 1}); got > 0.001 || got < -0.001 {
		t.Errorf("orthogonal vectors scored %v", got)
	}
	// Length mismatch and empties are zero rather than a panic or a guess: a backend that changed
	// dimension mid-store must cost a comparison, not the process.
	if got := Cosine([]float32{1, 0}, []float32{1, 0, 0}); got != 0 {
		t.Errorf("mismatched lengths scored %v", got)
	}
	if got := Cosine(nil, nil); got != 0 {
		t.Errorf("empty vectors scored %v", got)
	}
}

// Fusion rewards agreement, and neither half can win alone.
//
// This is the reason both rankings are kept. An exact rare token must not lose to something that
// merely feels related, and something related must still be findable when the words do not match.
func TestFusionPutsWhatBothAgreeOnFirst(t *testing.T) {
	// lexical liked 7 then 3; semantic liked 3 then 9. 3 is the one both saw.
	got := Fuse([]int{7, 3}, []int{3, 9})
	if len(got) != 3 || got[0] != 3 {
		t.Errorf("fused to %v; the document both rankings found belongs first", got)
	}
	// A document only one side found still appears — being unknown to one method is not a verdict.
	seen := map[int]bool{}
	for _, id := range got {
		seen[id] = true
	}
	for _, want := range []int{7, 9} {
		if !seen[want] {
			t.Errorf("%d was dropped for appearing in only one ranking: %v", want, got)
		}
	}
	// One list alone is that list.
	if got := Fuse([]int{4, 5, 6}); len(got) != 3 || got[0] != 4 || got[2] != 6 {
		t.Errorf("a single ranking came back reordered: %v", got)
	}
	if got := Fuse(); got != nil {
		t.Errorf("nothing fused to %v", got)
	}
}

// A backend that OMITS the index field must not collapse every vector onto slot 0. A plain int
// decoded the missing field to 0, so the loop mapped all rows to text[0] (leaving the rest nil and
// caching the wrong vector under text[0]'s key). With the index absent, order is positional.
func TestOmittedIndexFallsBackToPositionalOrder(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Three rows, embedding only — no "index".
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{
			{"embedding": []float32{1, 0}},
			{"embedding": []float32{0, 1}},
			{"embedding": []float32{1, 1}},
		}})
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL + "/v1", APIKey: "k", Model: "any-embed", CacheDir: t.TempDir(), HTTP: srv.Client()}
	got, err := c.Embed(context.Background(), []string{"a", "b", "c"})
	if err != nil {
		t.Fatal(err)
	}
	want := [][]float32{{1, 0}, {0, 1}, {1, 1}}
	for i := range want {
		if len(got[i]) != 2 || got[i][0] != want[i][0] || got[i][1] != want[i][1] {
			t.Errorf("vector %d = %v, want %v — omitted index collapsed the batch", i, got[i], want[i])
		}
	}
}
