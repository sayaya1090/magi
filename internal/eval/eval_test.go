package eval

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/llm/openai"
	"github.com/sayaya1090/magi/internal/adapter/platform"
)

// TestEvalSuite runs the fixed task suite against the backend named by env and
// prints a scored table. Cross-compare by running it per backend, e.g.:
//
//	MAGI_EVAL_BASE=http://localhost:11434/v1 MAGI_EVAL_MODEL=qwen3-coder:30b \
//	  go test -run TestEvalSuite ./internal/eval -v -timeout 30m
//	MAGI_EVAL_BASE=https://generativelanguage.googleapis.com/v1beta/openai \
//	  MAGI_EVAL_MODEL=gemini-2.5-flash MAGI_EVAL_KEY=AIza... \
//	  go test -run TestEvalSuite ./internal/eval -v -timeout 30m
func TestEvalSuite(t *testing.T) {
	base := os.Getenv("MAGI_EVAL_BASE")
	if base == "" {
		base = "http://localhost:11434/v1"
	}
	model := os.Getenv("MAGI_EVAL_MODEL")
	if model == "" {
		model = "qwen3-coder:30b"
	}
	if base == "disabled" || !serves(base, model) {
		t.Skipf("eval backend does not serve %s at %s (set MAGI_EVAL_BASE/_MODEL/_KEY)", model, base)
	}
	key := os.Getenv("MAGI_EVAL_KEY")
	if key == "" {
		key = os.Getenv("MAGI_API_KEY")
	}

	llm := openai.New(base, key)
	results, err := Run(llm, model, platform.New(), DefaultSuite())
	if err != nil {
		t.Fatalf("eval run: %v", err)
	}
	SortByName(results)
	t.Log(Report(model, results))
}

// serves reports whether base is up AND willing to serve model.
//
// ⚠ **Reachable is not enough, and the difference is a red suite.** The comment above promises that
// plain `go test ./...` stays green on a machine without models; it was only true when nothing
// answered at all. With ollama RUNNING and this model not pulled, the endpoint answers `/models`
// happily and every request then comes back `404 model '…' not found` — measured 2026-09-13 on this
// machine: tests in this tree red for a model nobody asked for. A suite red for its environment is
// one people learn to ignore, which is how a real regression gets through.
//
// A listing this cannot READ is not a refusal: some OpenAI-compatible servers do not enumerate, and
// skipping there would silently stop running the tests where they used to work — so anything that is
// not a well-formed listing goes ahead and lets the request answer. An EMPTY one is different: a
// server that says `{"object":"list","data":null}` has told us it serves nothing, and that is the
// exact answer ollama gives with no models pulled (measured here). Reading that as "cannot tell" is
// what made the first version of this gate let the same three tests through to their 404s.
func serves(base, model string) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(base + "/models")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return false
	}
	var listing struct {
		Object string                `json:"object"`
		Data   []struct{ ID string } `json:"data"`
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil || json.Unmarshal(body, &listing) != nil || listing.Object != "list" {
		return true // cannot tell from here — let the request answer
	}
	for _, m := range listing.Data {
		if m.ID == model {
			return true
		}
	}
	return false
}
