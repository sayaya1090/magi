package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/llm/openai"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

func TestSessionProfileRequestsUseSelectedEndpointAndCredentials(t *testing.T) {
	type observed struct{ key, model string }
	received := make(chan observed, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			received <- observed{r.Header.Get("Authorization"), "catalog"}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"selected-model"}]}`))
			return
		}
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		received <- observed{r.Header.Get("Authorization"), body.Model}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()
	a := &App{llm: namedLLM{"default"}, bus: bus.New(), states: map[session.SessionID]*sessionState{"one": {meta: session.Session{ID: "one"}}}, cfg: Config{NewProvider: func(p ProfileDef) port.LLMProvider { return openai.New(p.BaseURL, p.APIKey) }}}
	if err := a.ApplySessionProfile("one", ProfileDef{BaseURL: server.URL, APIKey: "selected-secret", Model: "selected-model"}); err != nil {
		t.Fatal(err)
	}
	provider := a.providerFor(a.agentFor(session.Session{ID: "one"}))
	stream, err := provider.StreamChat(context.Background(), port.ChatRequest{Model: a.SessionModel("one")})
	if err != nil {
		t.Fatal(err)
	}
	for range stream {
	}
	got := <-received
	if got.key != "Bearer selected-secret" || got.model != "selected-model" {
		t.Fatalf("selected profile request mismatch: model=%s, auth matches=%v", got.model, got.key == "Bearer selected-secret")
	}
	base, name := a.SessionBackend("one")
	if base != server.URL || name != "" {
		t.Fatal("wrong session endpoint/profile")
	}
	models, err := a.ListSessionModels(context.Background(), "one")
	if err != nil || len(models) != 1 || models[0] != "selected-model" {
		t.Fatalf("wrong selected catalog: %v %v", models, err)
	}
	catalog := <-received
	if catalog.key != "Bearer selected-secret" || catalog.model != "catalog" {
		t.Fatal("catalog used wrong credentials")
	}

}
