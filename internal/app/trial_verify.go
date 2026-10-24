package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

type pinnedVerifier struct {
	path, digest string
	body         []byte
}

func readTrialVerifier(source, name string) (*pinnedVerifier, error) {
	if name == "" {
		return nil, nil
	}
	if !filepath.IsLocal(name) {
		return nil, fmt.Errorf("trial verifier must be a local relative script path")
	}
	root, err := os.OpenRoot(source)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() || st.Size() > 64*1024 {
		return nil, fmt.Errorf("trial verifier must be a regular script of at most 64 KiB")
	}
	b, err := io.ReadAll(io.LimitReader(f, 64*1024+1))
	if err != nil {
		return nil, err
	}
	if len(b) == 0 || len(b) > 64*1024 || strings.ContainsRune(string(b), 0) {
		return nil, fmt.Errorf("invalid trial verifier script")
	}
	return &pinnedVerifier{path: name, body: b, digest: fmt.Sprintf("%x", sha256.Sum256(b))}, nil
}

// Execute the pre-model script bytes through the usual tool permission and
// sandbox gates, with no model call. The candidate cannot replace this script.
func (a *App) verifyTrial(ctx context.Context, s session.Session, v *pinnedVerifier) *port.TrialVerification {
	if v == nil {
		return nil
	}
	started := time.Now()
	r := &port.TrialVerification{Path: v.path, Digest: v.digest, Status: "not_run"}
	defer func() { r.ElapsedMS = time.Since(started).Milliseconds() }()
	scratch := a.scratchFor(s.ID)
	if scratch == nil {
		r.Output = "private scratch unavailable"
		return r
	}
	path := filepath.Join(scratch.root, "pinned-verifier.sh")
	if err := os.WriteFile(path, v.body, 0600); err != nil {
		r.Output = err.Error()
		return r
	}
	args, err := json.Marshal(map[string]any{"command": "bash -- " + shellQuoted(path), "timeout": 120})
	if err != nil {
		r.Output = err.Error()
		return r
	}
	call := &session.ToolCall{CallID: "verify_" + newID(), Name: "bash", Args: args}
	actor := event.Actor{Kind: event.ActorSystem, ID: "trial-verifier"}
	a.appendPart(ctx, s.ID, actor, "m_"+newID(), session.RoleAssistant, session.Part{Kind: session.PartToolCall, ToolCall: call})
	vctx, cancel := context.WithTimeout(ctx, 125*time.Second)
	defer cancel()
	a.executeTool(vctx, s, AgentSpec{Name: spawnAgentName, Trial: true, Tools: []string{"bash"}}, 1, actor, call, newRunGuard(a.touchesFile), "run pinned trial verifier")
	evs, err := a.store.Read(context.WithoutCancel(ctx), s.ID, 0)
	if err != nil {
		r.Output = "verifier result could not be read: " + err.Error()
		return r
	}
	for _, e := range evs {
		if e.Type != event.TypePartAppended {
			continue
		}
		var d event.PartAppendedData
		if json.Unmarshal(e.Data, &d) != nil || d.Part.ToolResult == nil || d.Part.ToolResult.CallID != call.CallID {
			continue
		}
		result := d.Part.ToolResult
		r.Output = truncateContextBytes(resultText(result.Content), 6000)
		switch {
		case vctx.Err() != nil:
			r.Status = "error"
		case !result.IsError && strings.HasPrefix(r.Output, "exit 0\n"):
			r.Status = "passed"
		case strings.HasPrefix(r.Output, "exit "):
			r.Status = "failed"
		}
		return r
	}
	r.Output = "no persisted verifier result"
	return r
}
