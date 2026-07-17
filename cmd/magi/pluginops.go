package main

// Plugin lifecycle operations: update/install runners, plugin directory
// resolution, embedded-plugin materialization, the host log sink, and the
// observation-event bridge. Pure wiring — moved out of main.go.

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/sayaya1090/magi/internal/adapter/platform"
	pluginlua "github.com/sayaya1090/magi/internal/adapter/plugin/lua"
	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/config"
	pluginupd "github.com/sayaya1090/magi/internal/update/plugin"
	"github.com/sayaya1090/magi/plugins"
)

// runPluginUpdates fast-forwards every managed (git) plugin found under the plugin
// roots. Non-git plugins are reported as skipped, never mutated.
func runPluginUpdates(extra string) int {
	wd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "magi: getwd:", err)
		return 1
	}
	roots := pluginDirs(platform.New(), wd, extra)
	managed := pluginupd.Discover(roots)
	if len(managed) == 0 {
		fmt.Println("no plugins found")
		return 0
	}
	for _, m := range managed {
		res := pluginupd.UpdateOne(context.Background(), m)
		switch {
		case res.Skipped != "":
			fmt.Printf("· %s: %s\n", res.Name, res.Skipped)
		case res.Updated:
			fmt.Printf("✓ %s: %s → %s\n", res.Name, res.From, res.To)
		default:
			fmt.Printf("· %s: up to date\n", res.Name)
		}
	}
	return 0
}

// runPluginInstall clones a plugin from a git URL into the user plugins dir.
func runPluginInstall(url, pin string) int {
	dest := filepath.Join(platform.New().ConfigDir(), "plugins")
	m, err := pluginupd.Install(context.Background(), url, pin, dest)
	if err != nil {
		fmt.Fprintln(os.Stderr, "magi: plugin install:", err)
		return 1
	}
	fmt.Printf("installed %s %s → %s\n", m.Name, m.Version, m.Dir)
	return 0
}

// pluginDirs returns the directories scanned for plugins, in priority order:
// the global config dir, a project-local .magi/plugins, and an optional
// explicit --plugins directory.
func pluginDirs(plat *platform.OS, workdir, extra string) []string {
	dirs := []string{
		filepath.Join(plat.ConfigDir(), "plugins"),
		filepath.Join(workdir, ".magi", "plugins"),
	}
	if extra != "" {
		dirs = append(dirs, extra)
	}
	return dirs
}

// planDepthFromEnv reads MAGI_MAX_PLAN_DEPTH as the recursive-planner depth cap. It exists
// so the benchmark harness (which runs `magi -p` with no config.toml) can toggle recursion
// without a rebuild: depth 2 = full recursion, depth 1 = top-level plan + single-level
// delegate but no child re-planning or failure-recursion. Unset/invalid → 0, which lets the
// app apply its default of 2. Negative values are ignored.
// loadEmbeddedPlugins loads plugins compiled into the binary, so a binary-only
// install (brew / install.sh) can enable them with a config switch. Each is
// OPT-IN ([plugins.<name>] enabled = true — they may spend LLM tokens or write
// workspace files), a user-installed plugin of the same name wins, and the
// materialized copy under <config>/plugins-embedded/ is overwritten every start
// so it always tracks the binary's version (updates ride magi --update).
func loadEmbeddedPlugins(host *pluginlua.Host, plat *platform.OS, cfg config.Config) {
	if strings.EqualFold(os.Getenv("MAGI_EMBEDDED_PLUGINS"), "off") {
		return // global kill switch for automation/bench (measurement must not shift)
	}
	for name, ep := range plugins.Embedded {
		enabled := ep.DefaultOn
		if v, ok := cfg.Plugins[name]["enabled"]; ok {
			if b, isB := v.(bool); isB {
				enabled = b // an explicit config bool always wins
			}
		}
		if !enabled {
			continue
		}
		if host.Has(name) {
			continue // a user-installed plugin of the same name won
		}
		dir := filepath.Join(plat.ConfigDir(), "plugins-embedded", name)
		if err := materializeEmbedded(ep.FS, name, dir); err != nil {
			fmt.Fprintf(os.Stderr, "embedded plugin %s: %v\n", name, err)
			continue
		}
		if _, err := host.Load(context.Background(), dir); err != nil {
			fmt.Fprintf(os.Stderr, "embedded plugin %s failed to load: %v\n", name, err)
		}
	}
}

// materializeEmbedded copies every embedded file under <name>/ (subdirectories
// included — a plugin may bundle scripts/references) into dir, overwriting.
func materializeEmbedded(pfs embed.FS, name, dir string) error {
	return fs.WalkDir(pfs, name, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(name, path)
		if rerr != nil || rel == "." {
			return nil
		}
		dst := filepath.Join(dir, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		b, rerr := pfs.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, b, 0o644)
	})
}

// pluginLogf returns the plugin host's log sink: silent by default (plugin
// chatter must not pollute the TUI/headless stdout), stderr with MAGI_DEBUG=1
// so a misbehaving plugin (or a silently-failing observer) is diagnosable.
func pluginLogf() func(string) {
	if os.Getenv("MAGI_DEBUG") == "" {
		return nil // NewHostWithConfig defaults to a no-op
	}
	return func(s string) { fmt.Fprintln(os.Stderr, "[plugin] "+s) }
}

// pluginObserver forwards app conversation milestones to the plugin host's
// observation events. Late-bound: the app is constructed before the host, so
// the host pointer lands via bind(); events before that (none in practice —
// the first turn starts after plugin load) are dropped.
type pluginObserver struct {
	host atomic.Pointer[pluginlua.Host]
}

func (o *pluginObserver) bind(h *pluginlua.Host) { o.host.Store(h) }

func (o *pluginObserver) UserMessage(sid, text string) {
	if h := o.host.Load(); h != nil {
		if os.Getenv("MAGI_DEBUG") != "" {
			fmt.Fprintf(os.Stderr, "[observer] user_message sid=%s len=%d\n", sid, len(text))
		}
		h.FireEventWith("user_message", map[string]string{"session": sid, "text": text})
	}
}

func (o *pluginObserver) TurnFinished(sid string, ob app.TurnObservation) {
	if h := o.host.Load(); h != nil {
		if os.Getenv("MAGI_DEBUG") != "" {
			fmt.Fprintf(os.Stderr, "[observer] turn_finished sid=%s outcome=%s skills=%v len=%d\n",
				sid, ob.Outcome, ob.SkillsLoaded, len(ob.FinalText))
		}
		h.FireEventWith("turn_finished", map[string]string{
			"session": sid, "text": ob.FinalText, "outcome": ob.Outcome, "reason": ob.Reason,
			"skills": strings.Join(ob.SkillsLoaded, ","), "user": ob.UserLabel,
		})
	}
}

// WantsTurnFinished lets the app skip the per-turn store scan when no loaded
// plugin actually listens for turn_finished.
func (o *pluginObserver) WantsTurnFinished() bool {
	h := o.host.Load()
	return h != nil && h.HasEventHandlers("turn_finished")
}
