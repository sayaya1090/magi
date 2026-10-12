package operations

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Opt-in live test. Creates and removes only its own networkless container.
func TestLiveDockerInspectionAndApprovedRestart(t *testing.T) {
	if os.Getenv("MAGI_OPERATIONS_LIVE_DOCKER") != "1" {
		t.Skip("requires explicit live Docker opt-in")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	name := fmt.Sprintf("magi-operations-qa-%d", time.Now().UnixNano())
	cmd := exec.CommandContext(ctx, "docker", "--context", "default", "run", "-d", "--name", name, "--network", "none", "--read-only", "--memory", "32m", "alpine:latest", "sh", "-c", "echo magi-stdout; echo magi-stderr >&2; while true; do sleep 1; done")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("create fixture: %s %v", out, err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if out, err := exec.CommandContext(cleanup, "docker", "--context", "default", "rm", "-f", name).CombinedOutput(); err != nil {
			t.Errorf("remove own fixture: %s %v", out, err)
		}
	})
	a := Adapter{Config: Config{Runtime: "docker", Context: "default", Resources: []string{name}}}
	args, _ := json.Marshal(map[string]string{"resource": name})
	if _, err := a.Call(ctx, a.Config.Target(), "status", args); err != nil {
		t.Fatal(err)
	}
	logs, err := a.Call(ctx, a.Config.Target(), "logs", args)
	if err != nil {
		t.Fatal(err)
	}
	text := logs.(map[string]any)["output"].(string)
	if !strings.Contains(text, "magi-stdout") || !strings.Contains(text, "magi-stderr") {
		t.Fatalf("lost log stream: %s", text)
	}
	s := RestartService{Adapter: a, Enabled: true}
	proposal, err := s.Call(ctx, a.Config.Target(), "propose_restart", args)
	if err != nil {
		t.Fatal(err)
	}
	p := proposal.(map[string]any)["proposal"].(RestartProposal)
	result, err := s.Decide(ctx, p.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.(map[string]any)["verified"] != true {
		t.Fatalf("post-restart verification: %#v", result)
	}
}
