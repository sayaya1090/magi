package app

import (
	"context"
	"encoding/json"
	task "github.com/sayaya1090/magi/internal/core/codingtask"
	"github.com/sayaya1090/magi/internal/port"
	"path/filepath"
	"reflect"
	"strings"
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

func TestCodingTaskRejectsAmbiguousOperationArguments(t *testing.T) {
	for _, raw := range []string{
		`{"operation":"prepare","requestId":"r","path":"/source"}`,
		`{"operation":"prepare","requestId":"r","source":"/other"}`,
		`{"operation":"prepare","requestId":"r","mode":"copy","base":"HEAD"}`,
		`{"operation":"prepare","taskId":"existing","requestId":"r"}`,
		`{"operation":"diff"}`,
		`{"operation":"diff","taskId":"id","mode":"copy"}`,
		`{"operation":"list","taskId":"id"}`,
		`{"operation":"review","taskId":"id"}`,
		`{"operation":"list"} {"operation":"prepare"}`,
	} {
		if _, err := parseCodingTaskRequest([]byte(raw)); err == nil {
			t.Errorf("ambiguous arguments silently accepted: %s", raw)
		}
	}
	for _, raw := range []string{
		`{"operation":"prepare","requestId":"r","mode":"copy","purpose":"test"}`,
		`{"operation":"prepare","requestId":"r","mode":"git","base":"HEAD"}`,
		`{"operation":"review","taskId":"id","path":"src/main.go"}`,
		`{"operation":"status","taskId":"id"}`,
		`{"operation":"list"}`,
	} {
		if _, err := parseCodingTaskRequest([]byte(raw)); err != nil {
			t.Errorf("valid arguments %s: %v", raw, err)
		}
	}
}

type codingTaskPrepareStub struct {
	port.CodingTasks
	record         task.Record
	observedSource string
}

func (s *codingTaskPrepareStub) Prepare(_ context.Context, source string, _ task.Request) (task.Record, error) {
	s.observedSource = source
	return s.record, nil
}
func TestCodingTaskPreparationReturnsExplicitSessionAndExecutionGuidance(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source with spaces")
	store := filepath.Join(t.TempDir(), "store with spaces")
	stub := &codingTaskPrepareStub{record: task.Record{ID: "task-id", Source: source, Workspace: filepath.Join(store, "tasks", "task-id", "workspace"), State: "ready", Mode: "copy"}}
	a := &App{cfg: Config{ClientRole: "coding", CodingTasks: stub}}
	raw, err := a.codingTask(context.Background(), source, []byte(`{"operation":"prepare","requestId":"r"}`))
	if err != nil {
		t.Fatal(err)
	}
	var reply struct {
		TaskID  string   `json:"taskId"`
		Workdir string   `json:"sessionWorkdir"`
		Changed bool     `json:"sessionWorkdirChanged"`
		Next    string   `json:"nextAction"`
		Command []string `json:"runCommand"`
	}
	if err := json.Unmarshal([]byte(raw), &reply); err != nil {
		t.Fatal(err)
	}
	if stub.observedSource != source || reply.TaskID != "task-id" || reply.Workdir != source || reply.Changed {
		t.Fatalf("session/source contract changed: %+v", reply)
	}
	want := []string{"magi", "coding-task", "run", "--source", source, "--store", store, "--id", "task-id", "--prompt", "<development request>"}
	if !reflect.DeepEqual(reply.Command, want) || !strings.Contains(reply.Next, "NOT moved") {
		t.Fatalf("missing actionable guidance: %+v", reply)
	}
}
