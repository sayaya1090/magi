package main

import (
	"bytes"
	"encoding/json"
	task "github.com/sayaya1090/magi/internal/core/codingtask"
	"os"
	"path/filepath"
	"testing"
)

func TestCodingTaskCLISelectionAndRestore(t *testing.T) {
	source := t.TempDir()
	store := filepath.Join(t.TempDir(), "store")
	path := filepath.Join(source, "a")
	if e := os.WriteFile(path, []byte("original"), 0600); e != nil {
		t.Fatal(e)
	}
	call := func(args ...string) []byte {
		t.Helper()
		var out, errs bytes.Buffer
		args = append(args, "--source", source, "--store", store)
		if code := codingTaskCommand(args, &out, &errs); code != 0 {
			t.Fatalf("exit=%d: %s", code, errs.String())
		}
		return out.Bytes()
	}
	var r task.Record
	if e := json.Unmarshal(call("prepare", "--mode", "copy", "--request", "cli"), &r); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(r.Workspace, "a"), []byte("changed"), 0600); e != nil {
		t.Fatal(e)
	}
	var out, errs bytes.Buffer
	if code := codingTaskCommand([]string{"apply", "--source", source, "--store", store, "--id", r.ID, "a"}, &out, &errs); code != 0 {
		t.Fatal(errs.String())
	}
	call("restore", "--id", r.ID)
	b, e := os.ReadFile(path)
	if e != nil || string(b) != "original" {
		t.Fatalf("restored %q: %v", b, e)
	}
}

func TestCodingTaskCLIPartialIsNotSuccessfulExit(t *testing.T) {
	source := t.TempDir()
	store := t.TempDir()
	path := filepath.Join(source, "a")
	if e := os.WriteFile(path, []byte("baseline"), 0600); e != nil {
		t.Fatal(e)
	}
	var out, errs bytes.Buffer
	if code := codingTaskCommand([]string{"prepare", "--source", source, "--store", store, "--mode", "copy"}, &out, &errs); code != 0 {
		t.Fatal(errs.String())
	}
	var r task.Record
	if e := json.Unmarshal(out.Bytes(), &r); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(r.Workspace, "a"), []byte("task edit"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(path, []byte("user edit"), 0600); e != nil {
		t.Fatal(e)
	}
	out.Reset()
	errs.Reset()
	if code := codingTaskCommand([]string{"apply", "--source", source, "--store", store, "--id", r.ID, "a"}, &out, &errs); code != 1 {
		t.Fatalf("partial exit=%d %s", code, out.String())
	}
	var j task.Journal
	if e := json.Unmarshal(out.Bytes(), &j); e != nil || j.State != "partial" {
		t.Fatalf("%+v %v", j, e)
	}
	b, e := os.ReadFile(path)
	if e != nil || string(b) != "user edit" {
		t.Fatalf("user edit lost: %s %v", b, e)
	}
}
