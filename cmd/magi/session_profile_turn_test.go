package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/llm/openai"
	"github.com/sayaya1090/magi/internal/adapter/platform"
	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

func TestSavedProfileFullTurnUsesProductionFactory(t *testing.T) {
	for _, workflow := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "workflow"}[workflow], func(t *testing.T) {
			received := make(chan string, 100)
			selected := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Model string `json:"model"`
				}
				_ = json.NewDecoder(r.Body).Decode(&body)
				received <- r.Header.Get("Authorization") + "|" + body.Model
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"Complete\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
			}))
			defer selected.Close()
			defaultHits := make(chan bool, 100)
			fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defaultHits <- true
				http.Error(w, "missing API key", 401)
			}))
			defer fallback.Close()
			cfgDir := t.TempDir()
			cfg := "[llm.profiles.saved]\nbase_url = " + `"` + selected.URL + `"` + "\napi_key = \"selected-key\"\nmodel = \"selected-model\"\n"
			if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(cfg), 0600); err != nil {
				t.Fatal(err)
			}
			store, err := jsonl.New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			a := app.New(store, openai.New(fallback.URL, ""), builtin.Default(), bus.New(), platform.New(), app.Config{Model: session.ModelRef{Model: "default-model"}, Permission: "allow", MaxSteps: 1, Workflow: workflow, NewProvider: newProviderFactory(nil, fallback.URL)})
			defer a.Close(context.Background())
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			sid, err := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir(), Model: session.ModelRef{Model: "default-model"}})
			if err != nil {
				t.Fatal(err)
			}
			d := daemonEngine{App: a, configDir: cfgDir}
			if err := d.UseProfile(sid, "saved"); err != nil {
				t.Fatal(err)
			}
			sub, unsub, err := a.Subscribe(ctx, sid, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer unsub()
			if err := a.Submit(ctx, command.SubmitPrompt{SessionID: sid, Parts: []session.Part{{Kind: session.PartText, Text: "Say complete"}}, Actor: event.Actor{Kind: event.ActorUser, ID: "test"}}); err != nil {
				t.Fatal(err)
			}
			done := false
			for !done {
				select {
				case e := <-sub:
					if e.Type == event.TypeTurnFinished {
						done = true
					}
				case <-ctx.Done():
					t.Fatal("turn timed out")
				}
			}
			if len(defaultHits) > 0 {
				t.Fatal("turn used default endpoint without selected API key")
			}
			if len(received) == 0 {
				t.Fatal("selected endpoint was not called")
			}
			for len(received) > 0 {
				got := <-received
				if got != "Bearer selected-key|selected-model" {
					t.Fatalf("wrong auth/model: %s", got)
				}
			}
		})
	}
}
