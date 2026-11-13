package app

import (
	"context"
	"encoding/json"
	"github.com/sayaya1090/magi/internal/adapter/llm/openai"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/port"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Exercise the actual HTTP body, rather than only the builtin registry: IDE
// sessions use the daemon's coding role and its frozen session tool catalog.
func TestMermaidCodingCatalogReachesCustomEndpoint(t *testing.T) {
	a := New(nil, nil, builtin.Default(), nil, nil, Config{ClientRole: "coding"})
	received := make(chan []string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Tools []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		var names []string
		for _, tool := range body.Tools {
			names = append(names, tool.Function.Name)
		}
		received <- names
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()
	stream, err := openai.New(srv.URL, "").StreamChat(context.Background(), port.ChatRequest{
		Model: "custom", Tools: a.sessionToolSpecs("ide", AgentSpec{}),
	})
	if err != nil {
		t.Fatal(err)
	}
	for e := range stream {
		if e.Type == port.ProviderError {
			t.Fatal(e.Err)
		}
	}
	for _, name := range <-received {
		if name == "mermaid" {
			return
		}
	}
	t.Fatal("custom endpoint did not receive mermaid")
}

func TestMermaidNotAdvertisedToOtherClientRoles(t *testing.T) {
	for _, role := range ClientKinds() {
		if role == ClientCoding {
			continue
		}
		t.Run(string(role), func(t *testing.T) {
			reg := builtin.Default()
			reg.Register(builtin.Mermaid{Preview: true})
			a := New(nil, nil, reg, nil, nil, Config{ClientRole: string(role)})
			for _, spec := range a.sessionToolSpecs("s", AgentSpec{}) {
				if spec.Name == "mermaid" || spec.Name == "mermaid_preview" {
					t.Fatalf("exposed %s", spec.Name)
				}
			}
		})
	}
}

func TestMermaidHiddenFromAttachedTerminal(t *testing.T) {
	a := New(nil, nil, builtin.Default(), nil, nil, Config{})
	a.mu.Lock()
	a.stateLocked("s").terminalDiagram = true
	a.mu.Unlock()
	for _, s := range a.toolSpecs("s", AgentSpec{}) {
		if s.Name == "mermaid" {
			t.Fatal("terminal offered inline diagram")
		}
	}
}
func TestVisualExplanationSkillAvailableWithoutInstallation(t *testing.T) {
	a := New(nil, nil, nil, nil, nil, Config{})
	b, ok := a.skillBody(t.TempDir(), "visual-explanation")
	if !ok || !strings.Contains(b, "sequenceDiagram") {
		t.Fatal("built-in diagram guide missing")
	}
}
