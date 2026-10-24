package appbridge

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
)

func figmaCheckpointFixture(t *testing.T, history []event.Event) (*Server, Binding) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "figma-cp-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socket := filepath.Join(dir, "daemon.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				scan := bufio.NewScanner(conn)
				for scan.Scan() {
					var req map[string]any
					if json.Unmarshal(scan.Bytes(), &req) != nil {
						return
					}
					enc := json.NewEncoder(conn)
					if req["method"] == "transcript" {
						for _, ev := range history {
							if err := enc.Encode(map[string]any{"ok": true, "event": ev}); err != nil {
								return
							}
						}
						if err := enc.Encode(map[string]any{"ok": true, "live": true}); err != nil {
							return
						}
					} else {
						if err := enc.Encode(map[string]any{"ok": true}); err != nil {
							return
						}
					}
				}
			}()
		}
	}()
	s, err := New(Config{App: "figma", Token: "test", Tools: []Tool{{Name: "figma_checkpoint", InputSchema: map[string]any{"type": "object"}}}})
	if err != nil {
		t.Fatal(err)
	}
	b := Binding{ID: "binding", Session: "session", Target: "figma:file", socket: socket, pending: make(chan *toolCall, 8), lastPoll: time.Now()}
	s.bindings[b.ID] = &b
	return s, b
}
func checkpointFinish(seq int64, kind event.ActorKind, unverified bool) event.Event {
	data, _ := json.Marshal(event.TurnFinishedData{Unverified: unverified})
	return event.Event{Seq: seq, SessionID: "session", Type: event.TypeTurnFinished, Actor: event.Actor{Kind: kind}, Data: data}
}

func TestFigmaCheckpointOnlyPublishesAfterNewVerifiedCompletion(t *testing.T) {
	finish := checkpointFinish(11, event.ActorAgent, false)
	s, b := figmaCheckpointFixture(t, []event.Event{finish})
	after, err := s.figmaCheckpointCursor(context.Background(), b, "figma_checkpoint", json.RawMessage(`{"action":"finish"}`))
	if err != nil || after != 11 {
		t.Fatalf("cursor: %d %v", after, err)
	}
	if err := s.recordFigmaCheckpoint(b, "figma_checkpoint", json.RawMessage(`{"action":"finish"}`), "host-token", 10, map[string]any{"checkpoint_ready": true, "checkpoint_token": "host-token"}); err != nil {
		t.Fatal(err)
	}
	s.observeFigmaCheckpoint(b, checkpointFinish(9, event.ActorAgent, false))
	select {
	case <-b.pending:
		t.Fatal("replayed completion published")
	default:
	}
	s.observeFigmaCheckpoint(b, finish)
	var call *toolCall
	select {
	case call = <-b.pending:
	case <-time.After(time.Second):
		t.Fatal("verified completion did not reach plugin")
	}
	if call.Name != "__magi_checkpoint_complete" || !strings.Contains(string(call.Args), "host-token") {
		t.Fatalf("wrong completion capability: %+v", call)
	}
	call.reply <- toolReply{Result: map[string]any{"phase": "cleaned"}}
	deadline := time.Now().Add(time.Second)
	for {
		s.mu.Lock()
		done := s.bindings[b.ID].checkpoint == nil
		s.mu.Unlock()
		if done {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("reservation not consumed")
		}
		time.Sleep(time.Millisecond)
	}
	s.observeFigmaCheckpoint(b, finish)
	select {
	case <-b.pending:
		t.Fatal("completion replay published twice")
	default:
	}
}

func TestFigmaCheckpointCancelAndUnverifiedCompletionDisarm(t *testing.T) {
	for _, finish := range []event.Event{checkpointFinish(11, event.ActorSystem, false), checkpointFinish(11, event.ActorAgent, true)} {
		s, b := figmaCheckpointFixture(t, nil)
		s.bindings[b.ID].checkpoint = &figmaCheckpointReservation{Token: "t", After: 10}
		s.observeFigmaCheckpoint(b, finish)
		s.observeFigmaCheckpoint(b, checkpointFinish(12, event.ActorAgent, false))
		s.mu.Lock()
		held := s.bindings[b.ID].checkpoint
		s.mu.Unlock()
		if held != nil {
			t.Fatal("failed task left completion armed")
		}
		select {
		case <-b.pending:
			t.Fatal("failed task published")
		default:
		}
	}
}

func TestFigmaCheckpointCorrectedFinishAndHiddenModelCallAreRefused(t *testing.T) {
	verified := checkpointFinish(11, event.ActorAgent, false)
	corrected := checkpointFinish(12, event.ActorAgent, true)
	s, b := figmaCheckpointFixture(t, []event.Event{verified, corrected})
	reservation := &figmaCheckpointReservation{Token: "t", After: 10}
	s.bindings[b.ID].checkpoint = reservation
	if err := s.completeFigmaCheckpoint(context.Background(), b, reservation, 11); err == nil {
		t.Fatal("corrected completion published")
	}
	req := httptest.NewRequest("POST", "/mcp/"+b.ID+"/"+b.Session, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"__magi_checkpoint_complete","arguments":{"token":"t"}}}`))
	req.Header.Set("Authorization", "Bearer test")
	out := httptest.NewRecorder()
	s.Mux.ServeHTTP(out, req)
	if !strings.Contains(out.Body.String(), `"isError":true`) {
		t.Fatalf("model invoked host-only completion: %s", out.Body.String())
	}
	select {
	case <-b.pending:
		t.Fatal("hidden model call reached Figma")
	default:
	}
}
