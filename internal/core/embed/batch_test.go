package embed

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// batchServer answers /embeddings, records the size of every request, and fails the request whose
// 1-based number is failOn (0 never fails).
type batchServer struct {
	mu     sync.Mutex
	sizes  []int
	failOn int
}

func (b *batchServer) handler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Input []string `json:"input"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	b.mu.Lock()
	b.sizes = append(b.sizes, len(req.Input))
	n := len(b.sizes)
	b.mu.Unlock()
	if n == b.failOn {
		http.Error(w, `{"error":{"message":"overloaded"}}`, http.StatusServiceUnavailable)
		return
	}
	type row struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	}
	var data []row
	for i, s := range req.Input {
		data = append(data, row{Index: i, Embedding: []float32{float32(len(s)), 1}})
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
}

func texts(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("text number %d %s", i, string(make([]byte, i)))
	}
	return out
}

// Twenty uncached texts go out as 8 + 8 + 4, come back in input order, and a second call is served
// from the cache without a request.
func TestUncachedTextsGoOutInBatches(t *testing.T) {
	b := &batchServer{}
	srv := httptest.NewServer(http.HandlerFunc(b.handler))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, Model: "m", CacheDir: t.TempDir()}
	in := texts(20)
	vecs, err := c.Embed(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(b.sizes) != "[8 8 4]" {
		t.Fatalf("request sizes %v, want [8 8 4]", b.sizes)
	}
	for i, v := range vecs {
		if v == nil || v[0] != float32(len(in[i])) {
			t.Fatalf("vector %d is not the one for its text: %v", i, v)
		}
	}
	if _, err := c.Embed(context.Background(), in); err != nil || len(b.sizes) != 3 {
		t.Fatalf("second call should be all cache: err=%v requests=%d", err, len(b.sizes))
	}
}

// A failure in the second batch still leaves the first batch cached, so the next call asks only for
// what was not finished. Before batching, one failed request cached nothing and every call restarted.
func TestAFailedBatchKeepsTheBatchesBeforeIt(t *testing.T) {
	b := &batchServer{failOn: 2}
	srv := httptest.NewServer(http.HandlerFunc(b.handler))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, Model: "m", CacheDir: t.TempDir()}
	in := texts(20)
	if _, err := c.Embed(context.Background(), in); err == nil {
		t.Fatal("the failed batch should surface as an error")
	}
	b.mu.Lock()
	b.sizes, b.failOn = nil, 0
	b.mu.Unlock()
	if _, err := c.Embed(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(b.sizes) != "[8 4]" {
		t.Fatalf("after a failure the first 8 should be cached, so 12 remain as [8 4]; got %v", b.sizes)
	}
}
