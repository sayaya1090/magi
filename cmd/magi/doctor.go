package main

import (
	"context"
	"fmt"
	"io"
	"runtime"
	"time"

	"github.com/sayaya1090/magi/internal/config"
)

// doctorCheck is one environment probe's outcome. Status is one of "ok",
// "warn" (works, but degraded or suspicious), "fail" (magi cannot work as
// configured), or "info" (optional capability absent — not an error).
type doctorCheck struct {
	Name   string
	Status string
	Detail string
}

// doctorDeps are the probes runDoctor exercises, injected so the checks are
// testable without a live backend or PATH.
type doctorDeps struct {
	ListModels func(ctx context.Context) ([]string, error)
	LookPath   func(name string) (string, error)
	Model      string
	BaseURL    string
	Council    config.CouncilConfig
	Profiles   map[string]config.LLMProfile
	GOOS       string
}

// doctorChecks probes the environment magi actually depends on: the LLM
// endpoint (the #1 real-world failure — an unreachable backend looks like an
// infinite hang), optional tool binaries that silently degrade when absent,
// the OS sandbox backend, and config references to undefined profiles.
func doctorChecks(ctx context.Context, d doctorDeps) []doctorCheck {
	var out []doctorCheck

	// LLM endpoint: reachable, and is the configured model served?
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	ids, err := d.ListModels(cctx)
	cancel()
	switch {
	case err != nil:
		out = append(out, doctorCheck{"llm endpoint", "fail",
			fmt.Sprintf("%s unreachable: %v — every request will hang/fail (is the backend up? MAGI_BASE_URL right?)", d.BaseURL, err)})
	default:
		out = append(out, doctorCheck{"llm endpoint", "ok", d.BaseURL})
		found := false
		for _, id := range ids {
			if id == d.Model {
				found = true
				break
			}
		}
		if found {
			out = append(out, doctorCheck{"model", "ok", d.Model})
		} else {
			// Cloud-tier models may be served without being listed — warn, not fail.
			out = append(out, doctorCheck{"model", "warn",
				fmt.Sprintf("%q not in the backend's model list (cloud models may still work; try -list-models)", d.Model)})
		}
	}

	// Council member providers must name a defined [llm.profiles.*].
	for _, m := range d.Council.Members {
		if m.Provider == "" {
			continue
		}
		if _, ok := d.Profiles[m.Provider]; !ok {
			out = append(out, doctorCheck{"council member " + m.Name, "warn",
				fmt.Sprintf("provider %q is not a defined [llm.profiles.*] — falls back to the default backend", m.Provider)})
		}
	}

	// Optional tool binaries: absent = graceful degradation, but say so once here
	// instead of the tool discovering it mid-task.
	optional := []struct{ bin, gives string }{
		{"gopls", "lsp_diagnostics/definition/references for Go"},
		{"ast-grep", "structural (AST) search via astgrep"},
		{"rg", "fast grep backend"},
	}
	for _, o := range optional {
		if _, err := d.LookPath(o.bin); err != nil {
			out = append(out, doctorCheck{o.bin, "info", "not on PATH — " + o.gives + " degrades to a fallback"})
		} else {
			out = append(out, doctorCheck{o.bin, "ok", o.gives})
		}
	}

	// OS sandbox backend for the sandboxed bash profiles.
	switch d.GOOS {
	case "darwin":
		if _, err := d.LookPath("sandbox-exec"); err != nil {
			out = append(out, doctorCheck{"sandbox", "warn", "sandbox-exec missing — sandboxed profiles run unconfined"})
		} else {
			out = append(out, doctorCheck{"sandbox", "ok", "seatbelt (sandbox-exec)"})
		}
	case "linux":
		if _, err := d.LookPath("bwrap"); err != nil {
			out = append(out, doctorCheck{"sandbox", "warn", "bwrap missing — sandboxed profiles run unconfined"})
		} else {
			out = append(out, doctorCheck{"sandbox", "ok", "bubblewrap"})
		}
	}
	return out
}

// printDoctor renders the checks and returns the process exit code:
// 0 = no failures (warn/info are advisory), 1 = at least one hard failure.
func printDoctor(w io.Writer, checks []doctorCheck) int {
	icons := map[string]string{"ok": "✓", "warn": "!", "fail": "✗", "info": "–"}
	exit := 0
	for _, c := range checks {
		fmt.Fprintf(w, "%s %-22s %s\n", icons[c.Status], c.Name, c.Detail)
		if c.Status == "fail" {
			exit = 1
		}
	}
	return exit
}

// osGOOS is indirection-free in production; tests build doctorDeps directly.
func defaultDoctorGOOS() string { return runtime.GOOS }
