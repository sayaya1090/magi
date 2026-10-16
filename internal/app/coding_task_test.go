package app

import (
	"context"
	"testing"
)

func TestCodingTaskRoleGuard(t *testing.T) {
	for _, kind := range ClientKinds() {
		a := &App{cfg: Config{ClientRole: string(kind)}}
		_, err := a.codingTask(context.Background(), t.TempDir(), []byte(`{"operation":"list"}`))
		if kind != ClientCoding && (err == nil || err.Error() != "coding workspaces require a coding agent") {
			t.Fatalf("%s: %v", kind, err)
		}
		if kind != ClientCoding && ClientRoleAllows(string(kind), "coding_task") {
			t.Fatalf("%s advertised coding task", kind)
		}
	}
}
