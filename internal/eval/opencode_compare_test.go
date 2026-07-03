package eval

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// opencodeProjectConfig points OpenCode's openai-compatible "ollama" provider at
// the local endpoint and registers the cloud model, so OpenCode runs the SAME
// model as magi for an apples-to-apples scaffold comparison.
const opencodeProjectConfig = `{
  "$schema": "https://opencode.ai/config.json",
  "provider": {
    "ollama": {
      "npm": "@ai-sdk/openai-compatible",
      "name": "Ollama (local)",
      "options": { "baseURL": "http://localhost:11434/v1" },
      "models": { "minimax-m3:cloud": { "name": "MiniMax M3 (cloud via ollama)" } }
    }
  }
}`

// TestOpenCodeAudit runs the SAME complex multi-file audit (reviewCorpus) through
// OpenCode headless on the SAME model, scoring planted-issue coverage with the
// SAME detector — so it is directly comparable to TestMultiAgentAB's magi arms.
//
//	MAGI_AB_OPENCODE=1 MAGI_EVAL_MODEL=minimax-m3:cloud \
//	  go test ./internal/eval/ -run TestOpenCodeAudit -v -timeout 40m
func TestOpenCodeAudit(t *testing.T) {
	if os.Getenv("MAGI_AB_OPENCODE") == "" {
		t.Skip("set MAGI_AB_OPENCODE=1 to run the OpenCode comparison")
	}
	bin, err := exec.LookPath("opencode")
	if err != nil {
		t.Skip("opencode not installed")
	}
	model := os.Getenv("MAGI_EVAL_MODEL")
	if model == "" {
		model = "minimax-m3:cloud"
	}
	const trials = 3
	const prompt = "Audit this Go service for ALL concrete defects across security, concurrency, correctness, robustness, and resource management. There are multiple files. Report every issue you find with the file name and a one-line explanation."

	var covSum int
	var durSum time.Duration
	n := 0
	for i := 0; i < trials; i++ {
		reply, dur, err := runOpenCode(t, bin, model, prompt)
		if err != nil {
			t.Logf("trial %d ERROR: %v", i+1, err)
			continue
		}
		cov, found := coverage(reply)
		t.Logf("trial %d opencode  cov=%2d/10 dur=%s reply=%dB %v", i+1, cov, dur.Round(time.Second), len(reply), found)
		covSum += cov
		durSum += dur
		n++
	}
	if n == 0 {
		t.Fatal("no opencode trials completed")
	}
	t.Logf("=== OPENCODE (same model=%s) ===", model)
	t.Logf("opencode   avg-coverage=%.2f/10  avg-dur=%s  trials=%d",
		float64(covSum)/float64(n), (durSum / time.Duration(n)).Round(time.Second), n)
}

func runOpenCode(t *testing.T, bin, model, prompt string) (string, time.Duration, error) {
	dir, err := os.MkdirTemp("", "opencode-ab-")
	if err != nil {
		return "", 0, err
	}
	defer os.RemoveAll(dir)
	for name, content := range reviewCorpus {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			return "", 0, err
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "opencode.json"), []byte(opencodeProjectConfig), 0o644); err != nil {
		return "", 0, err
	}

	cmd := exec.Command(bin, "run", "-m", "ollama/"+model, "--pure", "--format", "json", prompt)
	cmd.Dir = dir
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	start := time.Now()
	runErr := cmd.Run()
	dur := time.Since(start)
	reply := extractOpenCodeText(out.Bytes())
	if reply == "" && runErr != nil {
		return "", dur, runErr
	}
	return reply, dur, nil
}

// extractOpenCodeText pulls assistant text from OpenCode's JSONL event stream.
// Each line is an event; we accumulate the text of any "text"-typed part.
func extractOpenCodeText(b []byte) string {
	var sb strings.Builder
	seen := map[string]string{} // part id -> latest text (parts stream incrementally)
	var order []string
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 1024*1024), 8*1024*1024)
	for sc.Scan() {
		var ev struct {
			Part struct {
				ID   string `json:"id"`
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"part"`
		}
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		if ev.Part.Type == "text" && ev.Part.Text != "" {
			if _, ok := seen[ev.Part.ID]; !ok {
				order = append(order, ev.Part.ID)
			}
			seen[ev.Part.ID] = ev.Part.Text
		}
	}
	for _, id := range order {
		sb.WriteString(seen[id])
		sb.WriteString("\n")
	}
	return sb.String()
}
