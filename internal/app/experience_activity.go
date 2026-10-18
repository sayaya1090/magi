package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

const experienceLease = 2 * time.Minute

type loadedExperience struct {
	ref                       port.ExperienceReference
	document, script, command string
}
type turnExperience struct {
	pending        int
	foregroundDone bool
	cleanupOnce    sync.Once
	mu             sync.Mutex
	store          port.ExperienceActivityStore
	id, workdir    string
	loaded         map[string]loadedExperience
	cancel         context.CancelFunc
	done           chan struct{}
}

func (a *App) newTurnExperience(ctx context.Context, workdir string) *turnExperience {
	store, ok := a.cfg.Experience.(port.ExperienceActivityStore)
	if !ok {
		return nil
	}
	ctx, cancel := context.WithCancel(a.bgContext())
	x := &turnExperience{store: store, id: newID(), workdir: workdir, loaded: map[string]loadedExperience{}, cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(x.done)
		timer := time.NewTicker(30 * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				x.renew(ctx)
			}
		}
	}()
	return x
}
func (x *turnExperience) references() []loadedExperience {
	x.mu.Lock()
	defer x.mu.Unlock()
	out := make([]loadedExperience, 0, len(x.loaded))
	for _, r := range x.loaded {
		out = append(out, r)
	}
	return out
}
func (x *turnExperience) renew(ctx context.Context) {
	for _, r := range x.references() {
		c, cancel := context.WithTimeout(ctx, 5*time.Second)
		if err := x.store.ProtectExperience(c, r.ref, x.id, time.Now().Add(experienceLease)); err != nil {
			log.Printf("experience reference renewal failed: %v", err)
		}
		cancel()
	}
}
func (x *turnExperience) cleanup() {
	x.cleanupOnce.Do(func() {
		x.cancel()
		<-x.done
		for _, r := range x.references() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if err := x.store.ReleaseExperience(ctx, r.ref, x.id); err != nil {
				log.Printf("experience reference release failed: %v", err)
			}
			cancel()
		}
	})
}
func (x *turnExperience) close() {
	if x == nil {
		return
	}
	x.mu.Lock()
	x.foregroundDone = true
	ready := x.pending == 0
	x.mu.Unlock()
	if ready {
		x.cleanup()
	}
}
func (x *turnExperience) background() func() {
	if x == nil {
		return func() {}
	}
	x.mu.Lock()
	x.pending++
	x.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			x.mu.Lock()
			x.pending--
			ready := x.foregroundDone && x.pending == 0
			x.mu.Unlock()
			if ready {
				x.cleanup()
			}
		})
	}
}
func fileDigest(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func shellQuoted(path string) string { return "'" + strings.ReplaceAll(path, "'", "'\"'\"'") + "'" }
func (x *turnExperience) load(ctx context.Context, name, body string) string {
	if x == nil || name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\") {
		return body
	}
	refs, err := x.store.ExperienceReferences(ctx, name)
	if err != nil {
		return body
	}
	var commands []string
	for _, ref := range refs {
		dir := filepath.Join(x.workdir, ".claude", "skills", name)
		r := loadedExperience{ref: ref, document: filepath.Join(dir, "SKILL.md"), script: filepath.Join(dir, "scripts", "verify.sh")}
		// Never offer a command from a stale/mismatched publication snapshot.
		if ref.DocumentDigest != "" && ref.VerificationDigest != "" && fileDigest(r.document) == ref.DocumentDigest && fileDigest(r.script) == ref.VerificationDigest {
			r.command = "bash -- " + shellQuoted(r.script)
		}
		if err := x.store.ProtectExperience(ctx, ref, x.id, time.Now().Add(experienceLease)); err != nil {
			continue
		}
		x.mu.Lock()
		x.loaded[ref.ObjectID] = r
		x.mu.Unlock()
		if r.command != "" {
			commands = append(commands, r.command)
		}
	}
	if len(commands) > 0 {
		body += "\n\n[Host verification contract: loading is not proof of application. After applying this skill, run its unchanged verification script through the normal bash tool, in the foreground, with this exact command. A successful execution can renew this local skill revision's retention; it does not prove every procedure in the skill was followed.]\n" + strings.Join(commands, "\n")
	}
	return body
}
func (x *turnExperience) before(tc *session.ToolCall) []loadedExperience {
	if x == nil || tc.Name != "bash" {
		return nil
	}
	var args struct {
		Command    string `json:"command"`
		Background bool   `json:"background"`
	}
	if json.Unmarshal(tc.Args, &args) != nil || args.Background {
		return nil
	}
	var out []loadedExperience
	for _, r := range x.references() {
		if r.command != "" && args.Command == r.command && r.matches() {
			out = append(out, r)
		}
	}
	return out
}
func (r loadedExperience) matches() bool {
	return fileDigest(r.document) == r.ref.DocumentDigest && fileDigest(r.script) == r.ref.VerificationDigest
}
func (x *turnExperience) confirm(ctx context.Context, tc *session.ToolCall, res session.ToolResult, refs []loadedExperience, sourceID string) {
	if x == nil || res.IsError || res.Background || ctx.Err() != nil || sourceID == "" {
		return
	}
	var output string
	// An arbitrary successful tool named bash is insufficient: require the real
	// foreground bash result's successful exit status, not a queued/background job.
	if json.Unmarshal(res.Content, &output) != nil || !strings.HasPrefix(output, "exit 0\n") {
		return
	}
	for _, r := range refs {
		if !r.matches() {
			continue
		}
		if err := x.store.ConfirmExperienceApplication(ctx, r.ref, x.id+":"+sourceID, sourceID+":"+r.ref.VerificationDigest); err != nil {
			log.Printf("experience application not recorded: %v", err)
		}
	}
}

// Verify persistence as well as successful execution. appendPart historically
// reports no error; a failed log write must not manufacture a durable receipt.
func (a *App) persistedVerification(ctx context.Context, sid session.SessionID, message string, res session.ToolResult) bool {
	evs, err := a.store.Read(ctx, sid, 0)
	if err != nil {
		return false
	}
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].Type != event.TypePartAppended {
			continue
		}
		var d event.PartAppendedData
		if json.Unmarshal(evs[i].Data, &d) != nil || d.MessageID != message || d.Role != session.RoleTool || d.Part.ToolResult == nil {
			continue
		}
		r := d.Part.ToolResult
		return r.CallID == res.CallID && !r.IsError && !r.Background && string(r.Content) == string(res.Content)
	}
	return false
}

func (x *turnExperience) holdMemories(ctx context.Context, memories []port.Memory) {
	if x == nil {
		return
	}
	ids := make([]string, 0, len(memories))
	for _, m := range memories {
		ids = append(ids, m.ID)
	}
	refs, err := x.store.ManagedMemoryReferences(ctx, ids)
	if err != nil {
		return
	}
	for _, ref := range refs {
		if err := x.store.ProtectExperience(ctx, ref, x.id, time.Now().Add(experienceLease)); err != nil {
			continue
		}
		x.mu.Lock()
		x.loaded[ref.ObjectID] = loadedExperience{ref: ref}
		x.mu.Unlock()
	}
}
func guardExperience(g *runGuard) *turnExperience {
	if g == nil {
		return nil
	}
	return g.experience
}
