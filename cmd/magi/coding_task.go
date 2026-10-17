package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/codingworkspace"
	"github.com/sayaya1090/magi/internal/adapter/platform"
	task "github.com/sayaya1090/magi/internal/core/codingtask"
)

// Coding tasks have a separate entrypoint so non-coding clients never acquire
// workspace mutation capabilities merely by opening a conversation.
type codingTaskRunKey struct{}

func codingTaskCommand(args []string, out, errOut io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(errOut, "usage: magi coding-task prepare|list|status|diff|review|apply|restore|recover|run [options]")
		return 2
	}
	fs := flag.NewFlagSet("coding-task "+args[0], flag.ContinueOnError)
	fs.SetOutput(errOut)
	source := fs.String("source", ".", "source workspace")
	root := fs.String("store", filepath.Join((platform.OS{}).ConfigDir(), "coding-tasks"), "task storage outside source")
	path := fs.String("path", "", "relative changed path for review")
	id := fs.String("id", "", "task ID")
	mode := fs.String("mode", "auto", "auto, git, or copy")
	base := fs.String("base", "", "explicit Git base; excludes uncommitted changes")
	request := fs.String("request", "", "idempotent preparation request ID")
	purpose := fs.String("purpose", "", "task purpose")
	timeout := fs.Duration("timeout", 10*time.Minute, "operation time budget (positive duration)")
	prompt := fs.String("prompt", "", "development request for run")
	if e := fs.Parse(args[1:]); e != nil {
		return 2
	}
	if fs.NArg() > 0 && args[0] != "apply" {
		fmt.Fprintln(errOut, "unexpected positional arguments; use named options")
		return 2
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if *timeout <= 0 {
		fmt.Fprintln(errOut, "timeout must be positive")
		return 2
	}
	ctx, deadlineCancel := context.WithTimeout(ctx, *timeout)
	defer deadlineCancel()
	m := codingworkspace.New(*root)
	var result any
	var e error
	switch args[0] {
	case "prepare":
		result, e = m.Prepare(ctx, *source, task.Request{RequestID: *request, Mode: *mode, Base: *base, Purpose: *purpose})
	case "list":
		result, e = m.List(ctx, *source)
	case "status":
		result, e = m.Status(ctx, *source, *id)
	case "review":
		result, e = m.Review(ctx, *source, *id, *path)
	case "diff":
		result, e = m.Diff(ctx, *source, *id)
	case "apply":
		result, e = m.Apply(ctx, *source, *id, fs.Args())
	case "recover":
		result, e = m.Recover(ctx, *source, *id)
	case "restore":
		result, e = m.Restore(ctx, *source, *id)
	case "run":
		if *prompt == "" {
			fmt.Fprintln(errOut, "run requires --prompt")
			return 2
		}
		result, e = m.Run(ctx, *source, *id, func(ctx context.Context, workspace string) error {
			// Keep the lease in the process that owns the model and tools. A
			// supervisor-only lock would be released if the supervisor crashed
			// while its child kept modifying the workspace.
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			if err = os.Chdir(workspace); err != nil {
				return err
			}
			defer os.Chdir(cwd)
			previousArgs, previousFlags, previousStdout := os.Args, flag.CommandLine, os.Stdout
			defer func() { os.Args = previousArgs; flag.CommandLine = previousFlags; os.Stdout = previousStdout }()
			os.Args = []string{previousArgs[0], "--client-role", "coding", "-p", *prompt}
			flag.CommandLine = flag.NewFlagSet(previousArgs[0], flag.ContinueOnError)
			os.Stdout = os.Stderr // stdout remains the task JSON result
			code := runWithContext(context.WithValue(ctx, codingTaskRunKey{}, true))
			if err := ctx.Err(); err != nil {
				return err
			}
			if code != 0 {
				return fmt.Errorf("coding execution failed (exit %d); workspace preserved", code)
			}
			return nil
		})
	default:
		fmt.Fprintln(errOut, "unknown coding-task operation")
		return 2
	}
	hasResult := result != nil
	switch v := result.(type) {
	case task.Record:
		hasResult = v.ID != ""
	case task.Journal:
		hasResult = v.ID != ""
	}
	if hasResult {
		if encErr := json.NewEncoder(out).Encode(task.Public(result)); encErr != nil {
			fmt.Fprintln(errOut, encErr)
			return 1
		}
	}
	if e == nil {
		if j, ok := result.(task.Journal); ok && (j.State == "partial" || j.State == "uncertain") {
			e = fmt.Errorf("integration %s; inspect per-path outcomes; current user edits are preserved", j.State)
		}
	}
	if e != nil {
		fmt.Fprintln(errOut, e)
		return 1
	}
	return 0
}
