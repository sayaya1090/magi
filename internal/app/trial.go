package app

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/sayaya1090/magi/internal/port"
)

// trialSource is private to one tool invocation. Paths are allocated by the
// host, never accepted from a plugin, and every trial clones this pinned tree.
type trialSource struct {
	mu                sync.Mutex
	dir, tree, memory string
	err               error
	started           bool
}

func (t *trialSource) get(ctx context.Context, a *App, parent string) (string, string, string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.started {
		t.started = true
		t.memory = a.projectMemory(parent)
		t.dir, _, t.err = a.cloneWorkspace(ctx, parent)
		if t.err == nil {
			t.tree, t.err = gitRun(ctx, t.dir, "rev-parse", "HEAD^{tree}")
			t.tree = strings.TrimSpace(t.tree)
		}
	}
	return t.dir, t.tree, t.memory, t.err
}

func (t *trialSource) close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.dir != "" {
		if err := os.RemoveAll(filepath.Dir(t.dir)); err != nil {
			log.Printf("trial source cleanup: %v", err)
		}
		t.dir = ""
	}
}

func validateTrial(s port.SpawnSpec) error {
	if len(s.Tools) == 0 || s.Review != nil {
		return fmt.Errorf("trial: explicit local tools required; review callbacks are not supported")
	}
	for _, name := range s.Tools {
		switch name {
		case "read", "grep", "glob", "list", "write", "edit", "multiedit", "bash":
		default:
			return fmt.Errorf("trial: tool %q is outside the local coding surface", name)
		}
	}
	return nil
}
