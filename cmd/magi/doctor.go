package main

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/llm/openai"
	"github.com/sayaya1090/magi/internal/adapter/mcp"
	"github.com/sayaya1090/magi/internal/adapter/platform"
	pluginlua "github.com/sayaya1090/magi/internal/adapter/plugin/lua"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/config"
	"github.com/sayaya1090/magi/internal/port"
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
func doctorChecks(ctx context.Context, d doctorDeps, extra ...doctorCheck) []doctorCheck {
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

	// Plugin-contributed checks come last, after magi's own built-ins, so a
	// plugin can never mask a core failure and the report reads core-then-plugins.
	out = append(out, extra...)
	return out
}

// clampDoctorStatus normalizes an arbitrary probe status to the four known
// values so printDoctor's icon map and the exit-code rule stay well-defined;
// anything unrecognized is treated as advisory "info".
func clampDoctorStatus(s string) string {
	switch s {
	case "ok", "warn", "fail", "info":
		return s
	default:
		return "info"
	}
}

// runPluginDoctorProbes runs each plugin probe under its own short timeout and
// converts it to a doctorCheck. A probe that returns no status is treated as
// "info"; the loop is sequential because the underlying Lua states are not
// concurrency-safe and each probe locks its own plugin.
func runPluginDoctorProbes(ctx context.Context, probes []port.DoctorProbe) []doctorCheck {
	out := make([]doctorCheck, 0, len(probes))
	for _, p := range probes {
		pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		status, detail := p.Run(pctx)
		cancel()
		out = append(out, doctorCheck{Name: p.Name(), Status: clampDoctorStatus(status), Detail: detail})
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

// loadDoctorProbes builds a throwaway plugin host that loads every plugin far
// enough to collect its -doctor probes, but deliberately does NOT fire the
// startup lifecycle — so a diagnostic run never triggers a plugin's interactive
// auth or network flow. Probes register at plugin load time (top-level chunk)
// and are self-contained by contract (they read a persistent store), so load is
// all that's needed. The host and its plugins are abandoned when -doctor returns
// and the process exits. Optional registries left nil (ContextReg/ModelReg/
// UserReg/Prompter) are guarded by the bridge, so a plugin calling them at load
// time simply fails to load and drops out of the report — it cannot hang -doctor.
//
// ConfigPath/DataDir point at the real store on purpose: a probe checks live
// state (e.g. store_get on a cached auth token), which needs genuine read access.
// That also gives a load-time write (set_config_key/store_set) reach to the real
// store — but such a plugin does strictly more at its normal startup, which this
// path skips, so -doctor is never the wider surface.
// doctorSink discards what a plugin registers through the OPTIONAL registries so
// a plugin that touches them at load time (register_context_provider, set_model,
// set_user_label) still LOADS in the -doctor host exactly as it does in a normal
// run. Leaving those registries nil made such a plugin fail to load and silently
// drop out of -doctor together with its probes — the report lied by omission.
type doctorSink struct{}

func (doctorSink) RegisterContextProvider(port.ContextProvider) {}

func (doctorSink) SetModel(string) error { return nil }

func (doctorSink) SetContextWindow(string, int) error { return nil }

func (doctorSink) SetUserLabel(string) {}

// It also returns one check per scanned plugin source (and per load failure), so
// -doctor shows WHY a plugin's probes are absent — a skipped directory or a load
// error used to leave no trace, making a missing plugin undiagnosable.
func loadDoctorProbes(cfg config.Config, plat *platform.OS, wd, pluginsDir string, llm *openai.Client) ([]port.DoctorProbe, []doctorCheck) {
	reg := builtin.Default()
	host := pluginlua.NewHostWithConfig(pluginlua.HostConfig{
		ToolSink:      reg,
		MCPMgr:        mcp.NewManager(reg),
		LLMReg:        llm,
		BaseReg:       llm,
		PluginConfigs: cfg.Plugins,
		ConfigPath:    filepath.Join(plat.ConfigDir(), "config.toml"),
		DataDir:       plat.ConfigDir(),
		// Discard-only registries: a plugin that registers a context provider or
		// sets model/user state at LOAD time must still load here exactly as it
		// does in a normal run — with these nil it failed to load and its probes
		// silently vanished from the report (observed live with engram).
		ContextReg: doctorSink{},
		ModelReg:   doctorSink{},
		UserReg:    doctorSink{},
		Logf:       pluginLogf(),
		Runtime: pluginlua.RuntimeInfo{
			Model:    cfg.Model,
			Platform: runtime.GOOS,
			Workdir:  wd,
		},
	})
	var report []doctorCheck
	for _, dir := range pluginDirs(plat, wd, pluginsDir) {
		loaded, errs := host.LoadDir(context.Background(), dir)
		switch {
		case len(loaded) > 0:
			report = append(report, doctorCheck{Name: "plugins", Status: "ok",
				Detail: dir + " → " + strings.Join(loaded, ", ")})
		case len(errs) == 0:
			report = append(report, doctorCheck{Name: "plugins", Status: "info",
				Detail: dir + " → none (directory absent or empty)"})
		}
		for _, err := range errs {
			report = append(report, doctorCheck{Name: "plugins", Status: "fail",
				Detail: dir + " → load error: " + err.Error()})
		}
	}
	// Embedded plugins register doctor probes too, and the normal path loads them
	// via this same helper — without it, a bundled plugin's probes silently never
	// appear in -doctor (the throwaway host only scanned the on-disk dirs).
	before := len(host.DoctorProbes())
	loadEmbeddedPlugins(host, plat, cfg)
	if n := len(host.DoctorProbes()) - before; n > 0 {
		report = append(report, doctorCheck{Name: "plugins", Status: "ok",
			Detail: fmt.Sprintf("embedded → %d doctor probe(s)", n)})
	}
	return host.DoctorProbes(), report
}
