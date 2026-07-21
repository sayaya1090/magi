package app

import (
	"context"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/port"
)

// An EMPTY judge reply is the judge TIMING OUT — inability to judge, not a churn verdict. With the
// wait-lease flag ON it must EXTEND (bounded by the backstop) rather than kill a subagent whose work
// outran a slow judge call — the compile-compcert three-strikes empty-reply kill. With the flag OFF
// the deterministic kill (ext 0) is restored.
func TestJudgeLeaseEmptyReplyExtends(t *testing.T) {
	run := func(flag string) (time.Duration, string) {
		t.Setenv("MAGI_SUBAGENT_WAIT_LEASE", flag)
		llm := &fakeLLM{steps: [][]port.ProviderEvent{{}}} // the judge call gets an empty reply
		a, wd := newApp(t, llm, Config{Permission: "allow", SubagentTimeout: 60 * time.Second})
		p, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})
		c, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})
		return a.judgeLease(context.Background(),
			a.sessionInfo(context.Background(), p), a.sessionInfo(context.Background(), c), "build it", time.Minute)
	}
	if ext, note := run("1"); ext <= 0 {
		t.Fatalf("empty judge reply with flag ON must EXTEND, got ext=%v note=%q", ext, note)
	}
	if ext, _ := run("0"); ext != 0 {
		t.Fatalf("empty judge reply with flag OFF must KILL (ext 0), got %v", ext)
	}
}
