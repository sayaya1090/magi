package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/port"
)

func quietTestJob(t *testing.T) *bgProc {
	t.Helper()
	path := filepath.Join(t.TempDir(), "job.log")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return &bgProc{id: "test-quiet", logPath: path}
}

func TestBackgroundOutputWaitsWithoutCallingModel(t *testing.T) {
	for _, exit := range []bool{false, true} {
		name := "new output"
		if exit {
			name = "exit without output"
		}
		t.Run(name, func(t *testing.T) {
			p := quietTestJob(t)
			ready := make(chan struct{})
			go func() {
				defer close(ready)
				time.Sleep(30 * time.Millisecond)
				if exit {
					p.mu.Lock()
					p.done = true
					p.mu.Unlock()
				} else {
					if err := os.WriteFile(p.logPath, []byte("finished"), 0o600); err != nil {
						return
					}
				}
			}()
			start := time.Now()
			err := waitForBackgroundOutput(context.Background(), p, time.Second, nil)
			<-ready
			if err != nil || time.Since(start) < 30*time.Millisecond || time.Since(start) > 900*time.Millisecond {
				t.Fatalf("elapsed=%s err=%v", time.Since(start), err)
			}
		})
	}
}

func TestBackgroundOutputWaitBufferedCancellationAndDeadline(t *testing.T) {
	p := quietTestJob(t)
	if err := os.WriteFile(p.logPath, []byte("buffered"), 0o600); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := waitForBackgroundOutput(context.Background(), p, time.Minute, nil); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("buffered output blocked")
	}
	p.read = len("buffered")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitForBackgroundOutput(ctx, p, time.Minute, nil); err != context.Canceled {
		t.Fatalf("cancel: %v", err)
	}
	if p.done || p.killed {
		t.Fatal("cancellation stopped job")
	}
	start = time.Now()
	if err := waitForBackgroundOutput(context.Background(), p, 30*time.Millisecond, nil); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) < 30*time.Millisecond {
		t.Fatal("quiet job returned immediately")
	}
}

func TestBashOutputRejectsInvalidWait(t *testing.T) {
	id := startJob(t, "sleep 30")
	defer (BashKill{}).Execute(context.Background(), jsonArgs(map[string]any{"id": id}), port.ToolEnv{})
	for _, wait := range []int{-1, 61} {
		r, e := (BashOutput{}).Execute(context.Background(), jsonArgs(map[string]any{"id": id, "wait": wait}), port.ToolEnv{})
		if e != nil || !r.IsError || !strings.Contains(string(r.Content), "between 0 and 60") {
			t.Fatalf("%+v %v", r, e)
		}
	}
	start := time.Now()
	r, e := (BashOutput{}).Execute(context.Background(), jsonArgs(map[string]any{"id": id, "wait": 0}), port.ToolEnv{})
	if e != nil || r.IsError || time.Since(start) > time.Second {
		t.Fatalf("immediate: %+v %v", r, e)
	}
}
