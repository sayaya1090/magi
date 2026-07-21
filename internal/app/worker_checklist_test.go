package app

import (
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/council"
)

func TestWorkerChecklist(t *testing.T) {
	checks := []council.DeliverableCheck{
		{Step: "1", Deliverable: "proto exists", Command: "test -f kv.proto"},
		{Step: "2. [solo] gen", Deliverable: "stubs", Command: "test -f kv_pb2.py"},
		{Step: "3", Command: "python server.py"},
	}

	// Step 1 (idx 0): only its own check, framed as a must-run acceptance checklist.
	got := workerChecklist(checks, 0)
	if !strings.Contains(got, "test -f kv.proto") {
		t.Error("step 1's own check must be included")
	}
	if strings.Contains(got, "server.py") || strings.Contains(got, "kv_pb2.py") {
		t.Errorf("other steps' checks must not leak into step 1:\n%s", got)
	}
	if !strings.Contains(got, "before you report done") {
		t.Error("must carry the run-and-verify instruction")
	}

	// Step 2 (idx 1) matches "2. [solo] gen".
	if !strings.Contains(workerChecklist(checks, 1), "kv_pb2.py") {
		t.Error("step 2's check must match the '2. …' step tag")
	}

	// No check tagged for this step → fall back to ALL (don't let the worker skip anything).
	all := workerChecklist(checks, 9)
	for _, c := range []string{"kv.proto", "kv_pb2.py", "server.py"} {
		if !strings.Contains(all, c) {
			t.Errorf("untagged step should fall back to all checks; missing %q", c)
		}
	}

	if workerChecklist(nil, 0) != "" {
		t.Error("no checks → empty checklist")
	}
}
