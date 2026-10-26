package lua

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// A rejected proposal is reported with what was rejected and why. Measured on aitoon (report
// de5d3e25…): the run ended "no valid, evidence-supported proposal after bounded attempts" with
// no trace of the reply, so whether it was unparseable, too long or missing a field was unknowable.
func TestS2RejectedProposalKeepsTheReplyAndTheReason(t *testing.T) {
	_, compare, _ := harnessLab(t, `{"hypothesis":"SENTINEL-proposal"}`)
	n := 0
	env := port.ToolEnv{Workdir: t.TempDir(), Spawn: func(context.Context, port.SpawnSpec) (port.SpawnResult, error) {
		n++
		return port.SpawnResult{Trial: true, Snapshot: "same-tree", Model: "executor", Provider: "local",
			SessionID: fmt.Sprint(n), Steps: 2, Text: "reported", Workspace: "/private/trial"}, nil
	}, ChildSteps: func(context.Context, string) ([]port.ChildStep, error) {
		return []port.ChildStep{{Name: "bash", Args: json.RawMessage(`{"command":"go test ./..."}`), Output: "exit 0\nPASS", OutputBytes: 11}}, nil
	}}
	res, err := compare.Execute(context.Background(), json.RawMessage(`{"task":"fix parser"}`), env)
	if err != nil {
		t.Fatal(err)
	}
	body := string(res.Content)
	if !res.IsError || !strings.Contains(body, "no valid, evidence-supported proposal") {
		t.Fatalf("an unusable proposal was accepted: %.400s", body)
	}
	if !strings.Contains(body, "SENTINEL-proposal") || !strings.Contains(body, "guidance") {
		t.Fatalf("the rejected proposal or the reason it was rejected was not kept: %.600s", body)
	}
}
