package operations

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestRestartRequiresOneUseApprovalAndChecksState(t *testing.T) {
	for _, runtime := range []string{"docker", "kubernetes"} {
		calls := []string{}
		s := RestartService{Enabled: true, Deployments: []string{"service"}, Adapter: Adapter{Config: Config{Runtime: runtime, Context: "test", Namespace: "ns", Resources: []string{"service"}}, Run: func(_ context.Context, bin string, args ...string) ([]byte, error) {
			calls = append(calls, bin+" "+strings.Join(args, " "))
			return []byte(`{"Running":true,"Restarting":false}`), nil
		}}}
		proposal := func() RestartProposal {
			out, err := s.Call(context.Background(), s.Adapter.Config.Target(), "propose_restart", json.RawMessage(`{"resource":"service"}`))
			if err != nil {
				t.Fatal(err)
			}
			return out.(map[string]any)["proposal"].(RestartProposal)
		}
		p := proposal()
		if len(calls) != 0 {
			t.Fatal("proposal mutated runtime")
		}
		if _, err := s.Decide(context.Background(), p.ID, false); err != nil {
			t.Fatal(err)
		}
		if len(calls) != 0 {
			t.Fatal("denial executed")
		}
		if _, err := s.Decide(context.Background(), p.ID, true); err == nil {
			t.Fatal("denied token reused")
		}
		p = proposal()
		result, err := s.Decide(context.Background(), p.ID, true)
		if err != nil {
			t.Fatal(err)
		}
		if len(calls) != 2 || result.(map[string]any)["verified"] != true {
			t.Fatalf("restart/check failed %v %v", calls, result)
		}
		if !strings.Contains(calls[0], "--context test") || (runtime == "kubernetes" && !strings.Contains(calls[0], "--namespace ns rollout restart deployment/service")) {
			t.Fatal(calls)
		}
		if _, err := s.Decide(context.Background(), p.ID, true); err == nil {
			t.Fatal("approval reused")
		}
	}
}
