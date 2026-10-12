package appbridge

import (
	"context"
	"testing"
	"time"
)

func TestRestartCannotInvalidateConcurrentSessionTransition(t *testing.T) {
	newEntered := make(chan struct{})
	releaseNew := make(chan struct{})
	restartEntered := make(chan struct{}, 1)
	socket := mockDaemon(t, func(req map[string]any) any {
		switch req["method"] {
		case "session-new":
			close(newEntered)
			<-releaseNew
			return map[string]any{"ok": true, "session": "new"}
		case "restart":
			restartEntered <- struct{}{}
			return map[string]any{"ok": true}
		default:
			return map[string]any{"ok": true}
		}
	})
	s, err := New(Config{App: "figma", ConfigDir: t.TempDir(), Token: "test"})
	if err != nil {
		t.Fatal(err)
	}
	s.bindings["binding"] = &Binding{ID: "binding", Session: "old", Target: "document", socket: socket, pending: make(chan *toolCall, 1)}
	s.targets["document"] = "binding"
	newDone := make(chan error, 1)
	go func() { _, e := s.Request(context.Background(), "binding", Request{Method: "new"}); newDone <- e }()
	<-newEntered
	restartDone := make(chan error, 1)
	go func() {
		_, e := s.Request(context.Background(), "binding", Request{Method: "restart"})
		restartDone <- e
	}()
	select {
	case <-restartEntered:
		close(releaseNew)
		t.Fatal("restart crossed an unfinished session transition")
	case <-time.After(50 * time.Millisecond):
	}
	close(releaseNew)
	if err := <-newDone; err != nil {
		t.Fatal(err)
	}
	if err := <-restartDone; err == nil {
		t.Fatal("restart of stale session was accepted")
	}
	bound, err := s.binding("binding")
	if err != nil || bound.Session != "new" {
		t.Fatalf("new binding invalidated: %+v %v", bound, err)
	}
}
