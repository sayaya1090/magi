//go:build !windows

package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/port"
)

// A person's message while wait_for waits ends the wait at once (2026-11-17: "when wait_for runs you
// should be back where you can take my message"). The wait says so; the job is not touched.
func TestWaitForReturnsWhenAMessageArrives(t *testing.T) {
	wake := make(chan struct{})
	env := port.ToolEnv{Workdir: t.TempDir(), PromptArrived: func() <-chan struct{} { return wake }}
	go func() { time.Sleep(200 * time.Millisecond); close(wake) }()
	start := time.Now()
	res, err := WaitFor{}.Execute(context.Background(), json.RawMessage(`{"condition":"false","timeout":30,"interval":5}`), env)
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("the wait did not end on the message: %s", d)
	}
	var s string
	_ = json.Unmarshal(res.Content, &s)
	if !strings.Contains(s, "message") {
		t.Errorf("the result must say a message arrived: %q", s)
	}
}
