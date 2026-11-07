package embed

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// A server that records which model each request named.
type modelServer struct {
	mu     sync.Mutex
	models []string
}

func (m *modelServer) handler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model string   `json:"model"`
		Input []string `json:"input"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	m.mu.Lock()
	m.models = append(m.models, req.Model)
	m.mu.Unlock()
	type row struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	}
	var data []row
	for i := range req.Input {
		data = append(data, row{Index: i, Embedding: []float32{1, 0}})
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
}

// An endpoint set at run time wins over the configured one; clearing it restores the config.
func TestSetEndpointRedirectsAndClears(t *testing.T) {
	configured, late := &modelServer{}, &modelServer{}
	s1 := httptest.NewServer(http.HandlerFunc(configured.handler))
	defer s1.Close()
	s2 := httptest.NewServer(http.HandlerFunc(late.handler))
	defer s2.Close()

	c := &Client{BaseURL: s1.URL, Model: "cfg-model"}
	if _, err := c.Embed(context.Background(), []string{"a"}); err != nil {
		t.Fatal(err)
	}
	c.SetEndpoint(s2.URL, "bge-m3")
	if _, err := c.Embed(context.Background(), []string{"b"}); err != nil {
		t.Fatal(err)
	}
	c.SetEndpoint("", "ignored")
	if _, err := c.Embed(context.Background(), []string{"c"}); err != nil {
		t.Fatal(err)
	}
	if len(configured.models) != 2 || configured.models[1] != "cfg-model" {
		t.Fatalf("configured server should get the first and the last request with its model: %v", configured.models)
	}
	if len(late.models) != 1 || late.models[0] != "bge-m3" {
		t.Fatalf("the late endpoint should get exactly the middle request with its model: %v", late.models)
	}
}

// A client with nothing configured becomes available once an endpoint arrives.
func TestALateEndpointMakesTheClientAvailable(t *testing.T) {
	c := &Client{}
	if c.Available() {
		t.Fatal("an unconfigured client should not be available")
	}
	c.SetEndpoint("http://127.0.0.1:1/v1", "bge-m3")
	if !c.Available() {
		t.Fatal("an endpoint and model set at run time should make it available")
	}
	c.SetEndpoint("", "")
	if c.Available() {
		t.Fatal("clearing the override should return to the unconfigured state")
	}
}
