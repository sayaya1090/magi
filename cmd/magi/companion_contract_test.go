package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/companionpolicy"
	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/adapter/mcpserve"
	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/bus"
)

func TestCompanionOperationsUsePolicyThroughRealDaemonSocket(t *testing.T) {
	cfg, wd := shortTempDir(t), t.TempDir()
	root := filepath.Join(wd, "public")
	os.Mkdir(root, 0700)
	os.WriteFile(filepath.Join(root, "file"), []byte("the original result"), 0600)
	path, err := companionpolicy.Path(cfg, wd)
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Dir(path), 0700)
	policy := fmt.Sprintf("version=1\npurpose='GWT plugin verification'\ninstructions=['Do not deploy']\n[[operations]]\nid='source'\ndescription='Read public source'\nkind='read_file'\nroot=%q\n", filepath.ToSlash(root))
	os.WriteFile(path, []byte(policy), 0600)
	st, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// nil model: the request cannot possibly succeed through generation or delegated work.
	a := app.New(st, nil, builtin.NewRegistry(), bus.New(), nil, app.Config{})
	defer a.Close(context.Background())
	n, names := countCan(st, wd, cfg)
	if n != 1 || len(names) != 1 || names[0] != "call:source" {
		t.Fatalf("public operation missing from publication: %d %v", n, names)
	}
	d := daemonEngine{App: a, configDir: cfg, workdir: wd, card: func() mcpserve.Card { return mcpserve.Card{Name: "gwt"} }}
	listener, err := daemon.Listen(filepath.Join(cfg, "test.sock"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- listener.Serve(ctx, d) }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("daemon did not stop")
		}
	}()
	cl, err := daemon.Dial(filepath.Join(cfg, "test.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	about, err := cl.About()
	if err != nil || !strings.Contains(about, "GWT plugin verification") || !strings.Contains(about, "Do not deploy") {
		t.Fatal(about, err)
	}
	listed, err := cl.CompanionOperation("list", "", nil)
	if err != nil || strings.Contains(string(listed.Declaration), "inputSchema") {
		t.Fatal(string(listed.Declaration), err)
	}
	desc, err := cl.CompanionOperation("describe", "source", nil)
	if err != nil || !strings.Contains(string(desc.Declaration), "inputSchema") {
		t.Fatal(string(desc.Declaration), err)
	}
	out, err := cl.CompanionOperation("call", "source", json.RawMessage(`{"path":"file"}`))
	if err != nil || out.OperationResult == nil || out.OperationResult.IsError || !strings.Contains(string(out.OperationResult.Content), "the original result") {
		t.Fatal(out, err)
	}
	outside, err := cl.CompanionOperation("call", "source", json.RawMessage(`{"path":"../private"}`))
	if err != nil || outside.OperationResult == nil || !outside.OperationResult.IsError {
		t.Fatal(outside, err)
	}
	os.Remove(path)
	if _, err = cl.CompanionOperation("call", "source", json.RawMessage(`{"path":"file"}`)); err == nil {
		t.Fatal("revoked operation still callable")
	}
	if _, err = cl.CompanionOperation("describe", "source", nil); err == nil {
		t.Fatal("revoked operation still described")
	}
}

func TestFleetDoorCarriesPublishedOperationMethods(t *testing.T) {
	for _, method := range []string{"capabilities", "capability", "capability-call"} {
		if !doorAllows(method) {
			t.Errorf("%s denied", method)
		}
	}
	for _, method := range []string{"shell", "submit", "edit-file", "mcp-attach"} {
		if doorAllows(method) {
			t.Errorf("%s must remain local", method)
		}
	}
}
