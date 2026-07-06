//go:build !windows

package builtin

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/port"
)

// A background command must run in its OWN session/process group (Setsid), so it
// is detached from magi's group and survives magi's exit. Verify the child's pgid
// differs from magi's — the property the file-backed, session-detached design relies on.
func TestBackgroundDetachesSession(t *testing.T) {
	env := port.ToolEnv{Workdir: t.TempDir()}
	start, _ := Bash{}.Execute(context.Background(),
		json.RawMessage(`{"command":"echo $$; sleep 1","background":true}`), env)
	id := bgIDRE.FindString(resultText(t, start))
	if id == "" {
		t.Fatal("no background id")
	}
	idArg := json.RawMessage(`{"id":"` + id + `"}`)
	var childPID int
	for i := 0; i < 100 && childPID == 0; i++ {
		r, _ := BashOutput{}.Execute(context.Background(), idArg, env)
		for _, f := range strings.Fields(resultText(t, r)) {
			if n, err := strconv.Atoi(f); err == nil && n > 1 {
				childPID = n
			}
		}
		if childPID == 0 {
			time.Sleep(20 * time.Millisecond)
		}
	}
	if childPID == 0 {
		t.Fatal("never observed child pid")
	}
	pgid, err := syscall.Getpgid(childPID)
	if err != nil {
		t.Fatalf("getpgid(%d): %v", childPID, err)
	}
	if pgid == syscall.Getpgrp() {
		t.Errorf("child pgid %d == magi pgid; not detached", pgid)
	}
	if pgid != childPID {
		t.Errorf("session leader pgid %d != child pid %d", pgid, childPID)
	}
}
