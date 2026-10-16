package builtin

import (
	"context"
	"encoding/json"
	"github.com/sayaya1090/magi/internal/port"
	"testing"
)

func TestCodingTaskUsesTrustedHostHook(t *testing.T) {
	called := false
	env := port.ToolEnv{CodingTask: func(ctx context.Context, raw json.RawMessage) (string, error) {
		called = true
		return `{"state":"ready"}`, nil
	}}
	r, e := (CodingTask{}).Execute(context.Background(), json.RawMessage(`{"operation":"prepare"}`), env)
	if e != nil || !called || r.IsError {
		t.Fatalf("result=%+v error=%v", r, e)
	}
	r, e = (CodingTask{}).Execute(context.Background(), nil, port.ToolEnv{})
	if e != nil || !r.IsError {
		t.Fatalf("unavailable result=%+v error=%v", r, e)
	}
}
