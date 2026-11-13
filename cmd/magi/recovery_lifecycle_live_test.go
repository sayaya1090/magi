//go:build !windows

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/shortdir"
)

// Opt in to verifying installed bytes; update/rollback tests still build their own binaries.
func recoveryVerificationBinary(t *testing.T) string {
	t.Helper()
	exe := os.Getenv("MAGI_VERIFY_BINARY")
	if exe == "" {
		return buildMagi(t, t.TempDir())
	}
	if !filepath.IsAbs(exe) {
		t.Fatal("MAGI_VERIFY_BINARY must be absolute")
	}
	version, err := exec.Command(exe, "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("verification binary: %v: %s", err, version)
	}
	t.Logf("verifying installed binary: %s", strings.TrimSpace(string(version)))
	return exe
}

func TestRecoveryLifecycleThroughRealDaemon(t *testing.T) {
	if testing.Short() {
		t.Skip("starts real daemon processes")
	}
	exe := recoveryVerificationBinary(t)
	root, err := shortdir.Make("mgr")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(root); err != nil {
			t.Error(err)
		}
	})
	cfg, ws, data := filepath.Join(root, "cfg"), filepath.Join(root, "ws"), filepath.Join(root, "data")
	for _, dir := range []string{cfg, ws, data} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	// The daemon uses os.Getwd, which resolves /tmp -> /private/tmp on macOS.
	ws, err = filepath.EvalSymlinks(ws)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	var calls atomic.Int32
	var mode atomic.Int32
	continued, held, release := make(chan struct{}, 1), make(chan struct{}, 1), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			fmt.Fprint(w, `{"data":[{"id":"fixture","object":"model"}]}`)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		// Endpoint capability probes use /responses without chat messages.
		var request struct {
			Messages []json.RawMessage `json:"messages"`
		}
		if err := json.Unmarshal(body, &request); err != nil {
			t.Error(err)
			return
		}
		if len(request.Messages) == 0 {
			http.Error(w, "chat completions only", http.StatusNotFound)
			return
		}
		n := calls.Add(1)
		if mode.Load() == 1 {
			select {
			case held <- struct{}{}:
			default:
			}
			select {
			case <-r.Context().Done():
			case <-ctx.Done():
			}
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if n == 1 {
			w.Header().Set("Content-Length", "10000")
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"SAVED_PREFIX\"}}]}\n\n")
			w.(http.Flusher).Flush()
			return
		}
		if n == 2 {
			if !bytes.Contains(body, []byte("SAVED_PREFIX")) {
				t.Error("continuation lost partial output")
			}
			continued <- struct{}{}
			select {
			case <-release:
			case <-ctx.Done():
				return
			}
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"RECOVERED_FINAL\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)
	// Cancel outstanding handlers before closing httptest.Server, including failed assertions.
	t.Cleanup(cancel)
	env := append(os.Environ(), "MAGI_CONFIG_DIR="+cfg, "MAGI_SOCKET_DIR="+cfg, "MAGI_DATA_DIR="+data,
		"MAGI_BASE_URL="+server.URL+"/v1", "MAGI_MODEL=fixture", "MAGI_API_KEY=x", "OPENAI_API_KEY=",
		"MAGI_PERMISSION=allow", "MAGI_WORKFLOW=false", "MAGI_EMBEDDED_PLUGINS=off")
	sock := daemon.SocketPath(cfg, ws)
	dial := func() *daemon.Client {
		c, err := daemon.DialWithin(sock, time.Second, 5*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	start := func(selected string) (*exec.Cmd, <-chan error) {
		log, err := os.CreateTemp(root, "daemon-*.log")
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(exe, "--daemon", "--no-update-check")
		cmd.Dir = ws
		cmd.Env = append(append([]string{}, env...), restartSessionEnv+"="+selected)
		cmd.Stdout = log
		cmd.Stderr = log
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait(); close(done) }()
		t.Cleanup(func() {
			if err := cmd.Process.Kill(); err != nil && !strings.Contains(err.Error(), "already finished") {
				t.Log(err)
			}
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("daemon cleanup timed out")
			}
			if err := log.Close(); err != nil {
				t.Error(err)
			}
		})
		began := time.Now()
		for time.Since(began) < 15*time.Second {
			c, err := daemon.DialWithin(sock, 100*time.Millisecond, time.Second)
			if err == nil {
				_, err = c.Hello()
				c.Close()
				if err == nil {
					t.Logf("daemon ready in %v, selected=%s", time.Since(began).Round(time.Millisecond), selected)
					return cmd, done
				}
			}
			select {
			case <-done:
				b, _ := os.ReadFile(log.Name())
				t.Fatalf("daemon exited: %s", b)
			default:
			}
			time.Sleep(25 * time.Millisecond)
		}
		b, _ := os.ReadFile(log.Name())
		t.Fatalf("daemon startup timed out: %s", b)
		return nil, nil
	}
	history := func(sid string) []event.Event {
		c := dial()
		defer c.Close()
		evs, err := c.History(sid)
		if err != nil {
			t.Fatal(err)
		}
		return evs
	}
	submit := func(sid, text string) {
		c := dial()
		defer c.Close()
		if err := c.Submit(ctx, command.SubmitPrompt{SessionID: session.SessionID(sid), Parts: []session.Part{{Kind: session.PartText, Text: text}}}); err != nil {
			t.Fatal(err)
		}
	}
	wait := func(ch <-chan struct{}) {
		select {
		case <-ch:
		case <-ctx.Done():
			t.Fatal("provider stage timed out")
		}
	}
	count := func(evs []event.Event, kind event.Type) int {
		n := 0
		for _, e := range evs {
			if e.Type == kind && (kind != event.TypePromptSubmitted || e.Actor.Kind == event.ActorUser) {
				n++
			}
		}
		return n
	}
	d1, done1 := start("")
	c := dial()
	a, err := c.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.NewSessionKeeping()
	c.Close()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("real session IDs: selected=%s unopened=%s", a, b)
	submit(a, "finish after a temporary connection loss")
	wait(continued)
	before := history(a)
	cursor := before[len(before)-1].Seq
	// Attach a real transcript socket and close it while the continuation is in flight.
	stream := dial()
	streamDone := make(chan error, 1)
	attached := make(chan struct{}, 1)
	go func() {
		streamDone <- stream.Transcript(a, 0, nil, func(e event.Event) bool {
			if e.Seq == cursor {
				select {
				case attached <- struct{}{}:
				default:
				}
			}
			return true
		})
	}()
	wait(attached)
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-streamDone:
	case <-ctx.Done():
		t.Fatal("closed transcript did not unwind")
	}
	close(release)
	reattached := dial()
	var tail []event.Event
	err = reattached.Transcript(a, cursor, nil, func(e event.Event) bool {
		if e.Seq > 0 && e.Seq <= cursor {
			t.Errorf("duplicate event %d", e.Seq)
		}
		tail = append(tail, e)
		return e.Type != event.TypeTurnFinished
	})
	reattached.Close()
	if err != nil {
		t.Fatal(err)
	}
	all := history(a)
	if calls.Load() != 2 || count(all, event.TypePromptSubmitted) != 1 || count(all, event.TypeTurnFinished) != 1 {
		t.Fatalf("replayed task: requests=%d prompts=%d finishes=%d", calls.Load(), count(all, event.TypePromptSubmitted), count(all, event.TypeTurnFinished))
	}
	found := false
	for _, e := range tail {
		found = found || bytes.Contains(e.Data, []byte("RECOVERED_FINAL"))
	}
	if !found {
		t.Fatal("reattachment missed final answer")
	}
	t.Logf("disconnect recovery: requests=2 prompts=1, replay cursor=%d", cursor)
	mode.Store(1)
	submit(a, "hold until process crash")
	wait(held)
	if err := d1.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done1:
	case <-ctx.Done():
		t.Fatal("killed daemon did not exit")
	}
	// A second saved interrupted conversation must stay untouched until opened.
	store, err := jsonl.New(data)
	if err != nil {
		t.Fatal(err)
	}
	// Empty sessions are deliberately not persisted until their first fact.
	existing, err := store.Read(ctx, session.SessionID(b), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(existing) == 0 {
		created, err := json.Marshal(event.SessionCreatedData{Workdir: ws, Model: session.ModelRef{Provider: "openai", Model: "fixture"}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = store.Append(ctx, session.SessionID(b), event.Event{Type: event.TypeSessionCreated, Actor: event.Actor{Kind: event.ActorUser, ID: "verification"}, TS: time.Now(), Data: created}); err != nil {
			t.Fatal(err)
		}
	}
	payload, err := json.Marshal(event.PromptSubmittedData{MessageID: "unopened-prompt", Parts: []session.Part{{Kind: session.PartText, Text: "unfinished background conversation"}}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Append(ctx, session.SessionID(b), event.Event{Type: event.TypePromptSubmitted, Actor: event.Actor{Kind: event.ActorUser, ID: "verification"}, TS: time.Now(), Data: payload})
	if err != nil {
		t.Fatal(err)
	}
	unopened, err := store.Read(ctx, session.SessionID(b), 0)
	if err != nil {
		t.Fatal(err)
	}
	mode.Store(2)
	_, done2 := start(a)
	recovered := history(a)
	if count(recovered, event.TypeTurnFinished) != 2 {
		t.Fatal("selected interrupted turn not closed")
	}
	untouched, err := store.Read(ctx, session.SessionID(b), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(untouched) != len(unopened) {
		t.Fatal("startup changed unopened session")
	}
	foundRecovery := false
	for _, e := range recovered {
		if e.Actor.ID == "recovery" && e.Type == event.TypeError {
			foundRecovery = true
			var d event.ErrorData
			if err := json.Unmarshal(e.Data, &d); err != nil {
				t.Fatal(err)
			}
			if !d.Resumable {
				t.Fatal("pure model request not resumable")
			}
		}
	}
	if !foundRecovery {
		t.Fatal("missing interrupted recovery event")
	}
	c = dial()
	err = c.Resume(session.SessionID(b))
	c.Close()
	if err != nil {
		t.Fatal(err)
	}
	recoveredB := history(b)
	if count(recoveredB, event.TypeTurnFinished) != 1 {
		t.Fatal("switch did not recover opened session")
	}
	if len(history(b)) != len(recoveredB) {
		t.Fatal("reconnect repeated recovery")
	}
	if calls.Load() != 3 {
		t.Fatalf("restart replayed a request: %d", calls.Load())
	}
	submit(b, "continue")
	c = dial()
	err = c.Transcript(b, recoveredB[len(recoveredB)-1].Seq, nil, func(e event.Event) bool { return e.Type != event.TypeTurnFinished })
	c.Close()
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 4 {
		t.Fatalf("follow-up request count=%d", calls.Load())
	}
	c = dial()
	err = c.Shutdown()
	c.Close()
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done2:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("shutdown timed out")
	}
	if _, err := os.Stat(sock); !os.IsNotExist(err) {
		t.Fatalf("shutdown left socket: %v", err)
	}
	rows, err := daemon.List(cfg)
	if err != nil || len(rows) != 0 {
		t.Fatalf("shutdown left registry: %v %v", rows, err)
	}
	t.Log("restart, lazy session switch, follow-up and shutdown cleanup passed")
}
