package main

// Plugin lifecycle operations: update/install runners, plugin directory
// resolution, embedded-plugin materialization, the host log sink, and the
// observation-event bridge. Pure wiring — moved out of main.go.

import (
	"bytes"
	"context"
	"fmt"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/sayaya1090/magi/internal/adapter/platform"
	pluginlua "github.com/sayaya1090/magi/internal/adapter/plugin/lua"
	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/atomicfile"
	"github.com/sayaya1090/magi/internal/config"
	"github.com/sayaya1090/magi/internal/envflag"
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

// pluginDirs returns the directories scanned for plugins, in priority order: the global config
// dir, and an optional explicit --plugins directory.
//
// # Why the workspace is not one of them any more
//
// A plugin declares its own permissions, in its own plugin.toml, and the host grants them: a
// manifest saying `permissions = ["exec:sh"]` gets exec:sh. That is a reasonable arrangement for a
// directory the operator installed into — they chose the plugin, and the manifest tells them what
// it will reach for.
//
// <workdir>/.magi/plugins is not that directory. It arrives with a clone, and it was scanned
// automatically, so `git clone && cd && magi` ran whatever Lua the repository shipped — before the
// first model call, with permissions the repository wrote for itself, and with nothing on screen
// saying it had happened. The sandbox does not help: it blocks os and io so that the BRIDGE can be
// the boundary, and the bridge offers exec, http, read_file, write_file and set_base_url to a
// plugin whose manifest asked for them.
//
// The lever to load them is the one that already existed: name the directory. `magi -plugins
// ./.magi/plugins` is somebody deciding, which is the whole of what was missing.
func pluginDirs(plat *platform.OS, workdir, extra string) []string {
	dirs := []string{filepath.Join(plat.ConfigDir(), "plugins")}
	// …unless the operator has said this workspace's file is their own, which is the same sentence
	// for the config beside it. One decision per directory rather than one per kind of thing a
	// directory can carry: somebody who trusts a repo enough to run its hooks is not making a
	// different judgement about its Lua.
	if config.Trusted(plat.ConfigDir(), workdir) {
		dirs = append(dirs, filepath.Join(workdir, ".magi", "plugins"))
	}
	if extra != "" {
		dirs = append(dirs, extra)
	}
	return dirs
}

// workspacePlugins is what the repository brought and this process did not run.
//
// Named on startup rather than passed over in silence. A plugin that used to load and now does not
// is a behaviour change somebody has to be able to see, and an unfamiliar repository carrying one
// is worth knowing about even when the answer is no.
func workspacePlugins(workdir string) []string {
	root := filepath.Join(workdir, ".magi", "plugins")
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if _, serr := os.Stat(filepath.Join(root, e.Name(), "plugin.toml")); serr == nil {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// loadEmbeddedPlugins loads plugins compiled into the binary, so a binary-only
// install (brew / install.sh) can enable them with a config switch. Each is
// OPT-IN ([plugins.<name>] enabled = true — they may spend LLM tokens or write
// workspace files), a user-installed plugin of the same name wins, and the
// materialized copy under <config>/plugins-embedded/ is refreshed to the binary's
// version each start — but only files whose content actually changed are rewritten
// (updates ride magi --update; an unchanged rebuild touches nothing).
func loadEmbeddedPlugins(host *pluginlua.Host, plat *platform.OS, cfg config.Config) {
	if !envflag.Enabled("MAGI_EMBEDDED_PLUGINS", true) {
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
// included — a plugin may bundle scripts/references) into dir. A file whose on-disk
// copy already matches is left untouched: rewriting identical bytes still emits an
// fsnotify event, and a second magi instance sharing this config dir watches these
// paths — an unconditional rewrite hot-reloads the plugin in the OTHER instance,
// which drops any set_base_url redirect it installed. Skipping no-op writes keeps
// one instance's startup from disturbing another's while still tracking the binary.
func materializeEmbedded(pfs fs.FS, name, dir string) error {
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
		b, rerr := fs.ReadFile(pfs, path)
		if rerr != nil {
			return rerr
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if cur, err := os.ReadFile(dst); err == nil && bytes.Equal(cur, b) {
			return nil // already current — don't rewrite (no spurious watcher event)
		}
		return writeAtomic(dst, b)
	})
}

// writeAtomic replaces a file in one step, so a reader never sees it half-written.
//
// os.WriteFile truncates and then writes, and the gap between those two is a window in which the
// file exists and is empty. That window belongs to whoever else is reading — and with several magi
// instances sharing a config directory, somebody is: two daemons started at the same moment into a
// fresh config both materialize the same plugin, and one of them read a plugin.toml the other had
// just truncated. It failed with "manifest missing name", which describes the bytes it saw and
// nothing about why they were like that. Observed starting three daemons at once.
//
// A temp file in the same directory (same filesystem, so the rename is a rename and not a copy)
// followed by os.Rename means a reader gets either the whole old file or the whole new one.
func writeAtomic(dst string, b []byte) error { return atomicfile.Write(dst, b, 0o644) }

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
// GateDeclaration runs the plugins' declaration gates (app.declarationGater); nil host → none.
func (o *pluginObserver) GateDeclaration(ctx context.Context, sid string,
	steps func(context.Context) ([]port.ChildStep, error)) []string {
	h := o.host.Load()
	if h == nil {
		return nil
	}
	return h.RunDeclarationGates(ctx, port.ToolEnv{SessionID: session.SessionID(sid), TurnSteps: steps})
}

func (o *pluginObserver) WantsTurnFinished() bool {
	h := o.host.Load()
	return h != nil && h.HasEventHandlers("turn_finished")
}

// runTrustCmd adds this workspace to the trusted list, or takes it off.
//
// It prints what the decision covers rather than a bare "ok". Trust is the one setting here whose
// effect is entirely in what OTHER files are allowed to say, so a confirmation that does not name
// them leaves somebody to guess whether they just allowed a hook, a plugin, or both.
func runTrustCmd(configDir, workdir string, remove bool, out io.Writer) int {
	if remove {
		was, err := config.Untrust(configDir, workdir)
		if err != nil {
			fmt.Fprintln(os.Stderr, "magi:", err)
			return 1
		}
		if !was {
			fmt.Fprintf(out, "%s was not trusted anyway\n", workdir)
			return 0
		}
		fmt.Fprintf(out, "%s is no longer trusted — its hooks, tool servers, scheduled jobs and "+
			"plugins stop being taken from the next run\n", workdir)
		return 0
	}
	already, err := config.Trust(configDir, workdir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "magi:", err)
		return 1
	}
	if already {
		fmt.Fprintf(out, "%s is already trusted\n", workdir)
		return 0
	}
	fmt.Fprintf(out, "%s is trusted: its .magi/config.toml is taken as written — hooks, tool "+
		"servers, scheduled jobs, its approval list and where its prompts go — and its "+
		".magi/plugins are loaded. `magi --untrust` here undoes it; the list is %s\n",
		workdir, filepath.Join(configDir, config.TrustFile))
	return 0
}
