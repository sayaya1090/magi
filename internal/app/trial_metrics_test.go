package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/port"
)

func cleanupTrialCheckout(t *testing.T, dir string) {
	t.Helper()
	if dir != "" {
		if err := os.RemoveAll(filepath.Dir(dir)); err != nil {
			t.Error(err)
		}
	}
}

func TestTrialMetricsSeparateMissingUsageFromMeasuredTokens(t *testing.T) {
	for _, reported := range []bool{true, false} {
		llm := &usageLLM{text: "done"}
		if reported {
			llm.in, llm.out = 123, 17
		}
		a, parent, _ := spawnApp(t, llm)
		parent.Workdir = gitRepo(t)
		h := a.spawnFnFor(0, parent, event.Actor{Kind: event.ActorAgent}, "metrics", "experiment")
		r, err := h.Spawn(context.Background(), port.SpawnSpec{Trial: true, Prompt: "inspect", Tools: []string{"read"}, MaxSteps: 4})
		h.Close()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { cleanupTrialCheckout(t, r.Workspace) })
		if r.Metrics.Calls != 1 || r.Metrics.TokensKnown != reported {
			t.Fatalf("incorrect usage completeness: %+v", r.Metrics)
		}
		if reported && (r.Metrics.Input != 123 || r.Metrics.Output != 17 || r.Metrics.UsageReports != 1) {
			t.Fatalf("provider usage lost: %+v", r.Metrics)
		}
	}
}

func TestTrialVerifierRejectsMissingEscapingAndInvalidScripts(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string][]byte{"valid.sh": []byte("exit 0\n"), "empty.sh": {}, "nul.sh": {0}, "large.sh": make([]byte, 65537)} {
		if err := os.WriteFile(filepath.Join(root, name), body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"../outside.sh", "/tmp/outside.sh", "missing.sh", "empty.sh", "nul.sh", "large.sh", "."} {
		if _, err := readTrialVerifier(root, name); err == nil {
			t.Fatalf("accepted %q", name)
		}
	}
	v, err := readTrialVerifier(root, "valid.sh")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "valid.sh"), []byte("exit 1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if string(v.body) != "exit 0\n" || len(v.digest) != 64 {
		t.Fatalf("not pinned: %+v", v)
	}
}
