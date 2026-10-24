package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

func checkpointHistoryBoundary(messages []session.Message) []session.Message {
	completed := map[string]bool{}
	for _, m := range messages {
		for _, p := range m.Parts {
			if p.ToolResult != nil {
				completed[p.ToolResult.CallID] = true
			}
		}
	}
	for i, m := range messages {
		for _, p := range m.Parts {
			if p.ToolCall != nil && !completed[p.ToolCall.CallID] {
				return messages[:i]
			}
		}
	}
	return messages
}
func (a *App) captureCheckpointContext(ctx context.Context, w *checkpointWorkspace, sid session.SessionID) error {
	a.mu.Lock()
	st := a.stateLocked(sid)
	encoded := append([]byte(nil), st.lastRequestContext...)
	system, encodingErr := st.lastRequestSystem, st.lastRequestContextError
	a.mu.Unlock()
	if encodingErr != "" {
		return fmt.Errorf("checkpoint context cannot be preserved: %s", encodingErr)
	}
	evs, err := a.store.Read(ctx, sid, 0)
	if err != nil {
		return err
	}
	if len(encoded) > 0 {
		if err := json.Unmarshal(encoded, &w.context); err != nil {
			return err
		}
		w.contextSystem = system
	} else {
		w.context = checkpointHistoryBoundary(reconstruct(a.liveEvents(sid, evs)))
	}
	if err := a.pinContextImages(w, w.context); err != nil {
		return err
	}
	encoded, err = json.Marshal(w.context)
	if err != nil {
		return err
	}
	w.contextDigest = fmt.Sprintf("%x", sha256.Sum256(encoded))
	w.contextSource = string(sid)
	return nil
}
func (a *App) pinContextImages(w *checkpointWorkspace, messages []session.Message) error {
	root := filepath.Join(w.root, "context-assets")
	if a.cfg.StoreDir != "" {
		root = filepath.Join(a.cfg.StoreDir, "context-assets")
	}
	pin := func(image *session.ImageRef) error {
		if image == nil {
			return nil
		}
		data, err := os.ReadFile(image.Path)
		if err != nil {
			return fmt.Errorf("checkpoint context attachment: %w", err)
		}
		if err := os.MkdirAll(root, 0700); err != nil {
			return err
		}
		path := filepath.Join(root, fmt.Sprintf("%x", sha256.Sum256(data)))
		if err := checkpointCopyFile(image.Path, path, 0600); err != nil {
			return err
		}
		image.Path = path
		return nil
	}
	for _, m := range messages {
		for _, p := range m.Parts {
			if err := pin(p.Image); err != nil {
				return err
			}
			if p.ToolResult != nil {
				for i := range p.ToolResult.Images {
					if err := pin(&p.ToolResult.Images[i]); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}
func (a *App) seedCheckpointContext(ctx context.Context, child session.SessionID, w *checkpointWorkspace) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	body, err := json.Marshal(event.CheckpointContextData{Messages: w.context, SourceSession: w.contextSource, Digest: w.contextDigest})
	if err != nil {
		return err
	}
	return a.appendFact(ctx, child, event.TypeCheckpointContext, event.Actor{Kind: event.ActorSystem, ID: "checkpoint"}, body)
}
func (a *App) recordCheckpointBranchContext(ctx context.Context, w *checkpointWorkspace, branch, sid string) error {
	evs, err := a.store.Read(context.WithoutCancel(ctx), session.SessionID(sid), 0)
	if err != nil {
		return err
	}
	messages := checkpointHistoryBoundary(reconstruct(evs))
	if err := a.pinContextImages(w, messages); err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	b := w.branches[branch]
	if b == nil {
		return fmt.Errorf("checkpoint: completed branch is missing")
	}
	b.Context, b.Session = messages, sid
	return w.save()
}
