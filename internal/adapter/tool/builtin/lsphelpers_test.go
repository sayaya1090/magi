package builtin

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

func TestRegistryUnregister(t *testing.T) {
	r := NewRegistry()
	r.Register(Read{})
	if _, ok := r.Get("read"); !ok {
		t.Fatal("read should be registered")
	}
	r.Unregister("read")
	if _, ok := r.Get("read"); ok {
		t.Error("read should be gone after Unregister")
	}
}

// websearch Execute validates the query before any network call.
func TestWebSearchArgErrors(t *testing.T) {
	ctx := context.Background()
	if r, _ := (WebSearch{}).Execute(ctx, json.RawMessage(`{"query":"  "}`), port.ToolEnv{}); !r.IsError {
		t.Error("blank query should error before any request")
	}
	if r, _ := (WebSearch{}).Execute(ctx, json.RawMessage(`bad`), port.ToolEnv{}); !r.IsError {
		t.Error("invalid args should error")
	}
}
