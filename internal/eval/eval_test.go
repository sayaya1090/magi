package eval

import (
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
	if base == "disabled" || !reachable(base) {
		t.Skipf("eval backend not reachable at %s (set MAGI_EVAL_BASE/_MODEL/_KEY)", base)
	}
	model := os.Getenv("MAGI_EVAL_MODEL")
	if model == "" {
		model = "qwen3-coder:30b"
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

func reachable(base string) bool {
	c := &http.Client{Timeout: 3 * time.Second}
	req, err := http.NewRequest(http.MethodGet, base+"/models", nil)
	if err != nil {
		return false
	}
	resp, err := c.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode < 500
}
