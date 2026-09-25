package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/adapter/mcpserve"
	pluginlua "github.com/sayaya1090/magi/internal/adapter/plugin/lua"
	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/adapter/tui"
	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/config"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
	"github.com/sayaya1090/magi/internal/update"
	"github.com/sayaya1090/magi/internal/version"
)

type daemonModeParams struct {
	ctx           context.Context
	a             *app.App
	plat          port.Platform
	cfg           config.Config
	wd            string
	sockPath      string
	sid           session.SessionID
	bound         *daemon.Daemon
	daemonMode    bool
	clientOwned   bool
	noUpdateCheck bool
	store         *jsonl.Store
}

func runDaemonMode(p daemonModeParams) int {
	plat := p.plat
	wd := p.wd
	sockPath := p.sockPath
	sid := p.sid
	cfg := p.cfg
	store := p.store
	a := p.a
	bound := p.bound
	ctx := p.ctx

	// An update this daemon may still be on trial for, settled BEFORE anything is published or
	// bound — a rollback here costs nothing, and after the socket is up it costs a client.
	//
	// CLIENT_LIFECYCLE §9.3: a candidate is confirmed only once it has come up AND stayed up, so
	// the generation the update restarted into keeps the build it replaced for StableWindow. A
	// SECOND start on the same candidate is the failure this exists to catch — the first one did
	// not last that long, so the previous build goes back on disk and the candidate is refused.
	daemonExe, _ := os.Executable()
	// The candidate this process is on trial for, empty when it is not. Read where readiness is
	// declared, further down — see the note there.
	watching := ""
	if daemonExe != "" {
		// First, a replacement nobody got to record — a process or machine that died between
		// writing the backup and writing the journal. The binary at that path is then either
		// the original or a build that never finished its pre-flight, and putting the backup
		// back is right either way (update.Salvage).
		if put, serr := update.Salvage(daemonExe); serr != nil {
			fmt.Fprintln(os.Stderr, "magi: an interrupted update could not be undone:", serr)
		} else if put != "" {
			fmt.Fprintf(os.Stderr, "magi: an update was interrupted before it was recorded — "+
				"the previous build is back at %s. Restarting onto it.\n", put)
			restartOnExit = true
			return 0
		}
		rec, rerr := update.Resume(daemonExe, version.Version)
		switch {
		case rerr != nil:
			// Not fatal: an unsettled journal is a worse update story, not a reason to refuse
			// to serve. Said out loud because silence here is how a stuck transaction survives.
			fmt.Fprintln(os.Stderr, "magi: the update journal could not be settled:", rerr)
		case rec.RolledBack:
			fmt.Fprintf(os.Stderr, "magi: %s came up but did not stay up — %s is back on disk. "+
				"Restarting onto it; it will not be taken again on its own (`magi -update` retries it).\n",
				rec.To, rec.From)
			// The FILE is the previous build now; this process is still the image of the one
			// that fell over, so the only way onto the restored build is to re-exec.
			restartOnExit = true
			return 0
		case rec.Watching:
			// A deliberate stop inside the window is not a build falling over. Without this,
			// stopping a daemon a minute after an update would undo it on the next start.
			defer func() { _ = update.LeftCleanly(daemonExe) }()
			// ⚠ **The clock does NOT start here.** This is before the socket is bound and before
			// the record is published — a build that never manages to serve would still sit out
			// its sixty seconds and be confirmed, which is the opposite of what the window is
			// for (review R11). It starts below, once this daemon is actually listening.
			watching = rec.To
		}
	}
	// Join the owning lineage BEFORE publishing: the record is written once, right below, and
	// the owner id has to be in it. A client reads the record to learn whether the daemon it
	// found is the one it owns, and a field filled in afterwards would be absent exactly
	// during the window that client is looking.
	if p.clientOwned {
		daemon.AdoptOwner()
	}
	howMany, whatOf := countCan(store, wd)
	unpublish, perr := daemon.Publish(sockPath, wd, string(sid),
		daemon.Identity{Name: cfg.Companion.Name, Role: cfg.Companion.Role,
			Team: cfg.Companion.Team, Hub: cfg.Companion.Hub, Can: howMany, Does: whatOf})
	if perr != nil {
		fmt.Fprintln(os.Stderr, "magi:", perr)
		return 1
	}
	defer unpublish()
	// Ctrl-C stops it the way a service stops: cancel, let the run unwind, drop the socket.
	dctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	fmt.Fprintf(os.Stderr, "magi: daemon on %s (session %s) — attach with `magi --attach` in this directory\n",
		sockPath, sid)
	// The stable window, started from READINESS rather than from process start: the socket is
	// bound and the record is published, so what this measures is a daemon that came up and
	// stayed up rather than one that merely got this far (review R11).
	if watching != "" {
		go func(candidate string) {
			select {
			case <-dctx.Done():
			case <-time.After(update.StableWindow):
				if cerr := update.Confirm(daemonExe, candidate); cerr != nil {
					fmt.Fprintln(os.Stderr, "magi: could not confirm the update:", cerr)
				}
			}
		}(watching)
	}
	// Scheduled work starts here and nowhere else.
	//
	// This is the only call to RunCron in the tree, and the placement is the feature: three
	// terminals open in one repo would otherwise be three companions all running the nightly
	// audit, against the same files, at the same second. An interactive session reads the same
	// jobs so its editor can show them, and fires none of them.
	//
	// On dctx, so Ctrl-C stops the schedule with everything else. Its own goroutine because
	// RunCron blocks until then, and Serve is what this process is here to do.
	// Re-read from disk rather than closing over the config loaded at startup: the schedule
	// tool writes config.toml and then calls ReloadCron, and a closure over a snapshot would
	// hand the scheduler the jobs as they were when the daemon booted.
	loadJobs := func() map[string]config.CronJob {
		g, lerr := config.Load(plat.ConfigDir())
		if lerr != nil {
			return nil
		}
		if proj, perr := config.Load(filepath.Join(wd, ".magi")); perr == nil {
			// Trust is re-read here too: a workspace taken off the list should stop scheduling
			// at the next reload rather than at the next restart.
			g, _ = mergeProjectConfigSaying(g, proj, config.Trusted(plat.ConfigDir(), wd))
		}
		return g.Cron
	}
	// Its own cancel, tripped after Serve returns. dctx alone would not do it: a shutdown asked
	// for over the socket ends Serve without cancelling anything, and the schedule would go on
	// firing until the process happened to exit. "Stopping a companion stops its unattended
	// work" is the whole point of the socket call, so it is made to happen here rather than
	// left to process teardown.
	cronCtx, stopCron := context.WithCancel(dctx)
	defer stopCron()
	if daemonMode := p.daemonMode; daemonMode {
		go a.RunCron(cronCtx, wd, loadJobs, func(line string) {
			fmt.Fprintln(os.Stderr, "magi:", line)
		})
	}
	// Staying in the cluster, on the same lifetime as the schedule and for the same reasons: a
	// companion that has been stopped should not go on reaching out to other machines, and an
	// interactive magi should never reach out at all.
	//
	// Started after Publish, which matters — a round sends what this machine knows about
	// itself, and before publishing that does not include this daemon.
	go gossipCluster(cronCtx, plat.ConfigDir(), sshTrade, func(line string) {
		fmt.Fprintln(os.Stderr, "magi: cluster:", line)
	})
	serving := bound
	// ⚠ **The owner's pipe, and the only thing in this tree that carries lifetime authority.**
	//
	// The client that started this daemon holds the write end of its stdin and hands it to
	// nobody. When that process goes — closed, crashed, its extension host killed — the
	// operating system closes the last write end and this read sees EOF. Nothing else produces
	// that: a pipe cannot be guessed, copied out of a file, or read off another process's
	// environment, which is why the ids beside it (owner, instance) are tracking only.
	//
	// Stop() rather than a new ending, so an owner going away unwinds down the SAME path as
	// the `shutdown` door — one spelling of "this daemon is stopping", including the flag that
	// records it as asked for rather than as a signal.
	//
	// Nothing else reads stdin in this mode: resolvePrompt touches it only for `-p -`, which a
	// daemon does not take. A daemon started WITHOUT this flag never gets here, so every
	// non-owned lifetime is exactly what it was.
	if p.clientOwned {
		go func() {
			// Every ending of this read means the same thing — the owner is no longer holding
			// the other end — so the daemon stops either way. The error is still named: a pipe
			// that FAILED and one that closed are different events to whoever reads the log
			// afterwards, and a daemon that stopped for an I/O fault with nothing said about it
			// looks exactly like a window that was closed.
			if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
				fmt.Fprintln(os.Stderr, "magi: the owner's pipe failed:", err)
			}
			serving.StopBecause("the owner closed its pipe")
		}()
	}
	// The daemon's self-update loop: on a schedule it picks up a new release, commits it with
	// rollback, and restarts onto it once idle. --daemon only, so a headless bench never reaches
	// it; and only when [update] auto is on and the operator has not opted out. Same lifetime as
	// the schedule and gossip — a stopped companion stops reaching out, including for updates.
	if cfg.Update.AutoOn() && !p.noUpdateCheck {
		// The loop refuses a dev build and an unknown exe path itself (and says so once); the
		// error is deliberately not fatal — a daemon that cannot self-update still serves.
		exe := daemonExe
		// running() is true while any session has a turn in flight — App.Running returns the
		// running session and a bool; only the bool matters here — OR a meeting round is being
		// composed, which the run states deliberately do not cover (MeetingActive).
		busy := func() bool { return busyNow(a) }
		go daemonAutoUpdate(cronCtx, plat.ConfigDir(), version.Version, exe, sockPath, busy, a.HoldForUpdate, serving.Restart)
	}
	// Wrapped, so the engine the socket talks to can run a command HERE. The workspace is
	// closed over rather than taken from the request: a method that let a caller name the
	// directory would be a way to run commands anywhere on this machine from a page.
	taking := handover{work: a, at: newWhere(sid), workdir: wd, configDir: plat.ConfigDir(),
		receipts: daemon.NewReceipts(), mine: newSideSessions(), rooms: newSideSessions(),
		minutes: newSideSessions(),
		// What it is carrying goes into this companion's own published record, which is where
		// every roster reads it from — including one on another machine, a gossip round later.
		queued: newWaiting(func(n int, handling bool) {
			if aerr := daemon.Announce(sockPath, n, handling); aerr != nil {
				fmt.Fprintln(os.Stderr, "magi:", aerr)
			}
		}),
		// Kept beside the socket and outliving the process, unlike the record above: the week
		// after a companion was killed is when somebody asks whether it was overloaded.
		note: func(full bool, ahead int) {
			if nerr := daemon.NoteLoad(sockPath, cfg.Companion.Name, full, ahead); nerr != nil {
				fmt.Fprintln(os.Stderr, "magi:", nerr)
			}
		}}
	// Older than the window is history, and history that outlives its relevance gets read as
	// current. Once at startup is often enough for a file that grows by a line per request.
	if perr := daemon.PruneLoad(sockPath, time.Now().Add(-daemon.LoadKept)); perr != nil {
		fmt.Fprintln(os.Stderr, "magi:", perr)
	}
	// Starting queued work as the workspace frees up, on the same lifetime as the schedule and
	// the gossip: a companion that has been stopped should not pick up somebody's next piece.
	go taking.run(cronCtx)
	// What this companion is doing, into its own record, so it can travel.
	//
	// Only this process can say it: a console works the state of the companions in its own
	// directory out from a dial, and nothing dials the ones on other machines — which is why a
	// roster used to show them as "elsewhere", a place, in the column about what things are
	// doing. Gossip already carries a sighting every round; this is what rides along with it,
	// the way a node's application state rides Cassandra's.
	//
	// Polled rather than hooked into the turn: "working" is App.Running and "waiting" is the
	// engine's pending ask, both cheap to read and both changing under a dozen code paths that
	// would each have to remember to say so. NoteState writes only when the answer changed, so
	// a companion sitting idle rewrites nothing and wakes no reader.
	go func() {
		tick := time.NewTicker(3 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-cronCtx.Done():
				return
			case <-tick.C:
				if serr := daemon.NoteState(sockPath, companionState(a, sid)); serr != nil {
					fmt.Fprintln(os.Stderr, "magi:", serr)
				}
			}
		}
	}()
	serveErr := serving.Serve(dctx, daemonEngine{
		App: a, workdir: wd, handover: taking, configDir: plat.ConfigDir(),
		republish: func(to session.SessionID) error { return daemon.Moved(sockPath, to) },
		card: func() mcpserve.Card {
			return mcpserve.Card{
				Name: nameOr(cfg.Companion.Name, wd), Role: cfg.Companion.Role,
				Team: cfg.Companion.Team, Hub: cfg.Companion.Hub, Workdir: wd,
				Skills: a.Skills(wd), Reach: reachableServers(wd),
			}
		}})
	stopCron() // whichever way Serve ended, the schedule ends with it
	if serveErr != nil {
		fmt.Fprintln(os.Stderr, "magi:", serveErr)
		return 1
	}
	// Say that it stopped, and why.
	//
	// A clean shutdown is not an error, so nothing here printed anything — and the log of a
	// daemon that has died is then identical to the log of one still serving. Measured while
	// chasing exactly that: a daemon that stopped three times in one session left three
	// startup lines and no ending, so the last thing its file said was that it was listening.
	// Whoever reads it next is reading a sentence that stopped being true hours ago.
	fmt.Fprintf(os.Stderr, "magi: daemon on %s stopped — %s\n", sockPath, serving.Ending())
	// Relaunch onto the new binary rather than exit, when a client asked the daemon to restart
	// (a self-update). main() does the re-exec after this function returns, so the deferred
	// unpublish/socket release above run first — see restartOnExit. The CURRENT conversation
	// (taking.at follows Resume) rides along so the successor reopens it instead of minting an
	// empty one.
	restartOnExit = serving.Restarting()
	if restartOnExit {
		restartSession = string(taking.at.now())
	}
	// Its own background commands and language servers are this process's to reap, unlike an
	// attached viewer's.
	builtin.KillBackgroundProcesses()
	builtin.CloseLSPPool()
	return 0
}

func runInteractiveMode(
	ctx context.Context,
	a *app.App,
	host *pluginlua.Host,
	sid session.SessionID,
	modelID string,
	wd string,
	isDark bool,
	cfg config.Config,
	plat port.Platform,
	noUpdateCheck bool,
) int {
	// Startup update check — interactive TTY only (bench/headless/pipe never
	// reach here or fail the isTTY gate), so a benchmark run makes no network
	// call and gets no surprise install. A required (minor/major) update
	// installs and exits; a patch bump only prints a banner and continues.
	if shouldCheckUpdates(false, term.IsTerminal(os.Stdout.Fd()), noUpdateCheck) {
		exe, _ := os.Executable()
		if maybeUpdateOnStartup(ctx, plat.ConfigDir(), version.Version, exe, os.Stdout) {
			return 0
		}
	}
	// Boot-time composition seam: forks append Go logic (bundled-plugin refresh,
	// periodic update loop, …) that runs once the interactive session is committed
	// to launching. No-op unless something registered a hook in init().
	for _, h := range onInteractiveStart {
		h(ctx, plat.ConfigDir())
	}
	// Apply config color-theme overrides (merged over the NERV/MAGI defaults).
	tui.SetThemePalettes(cfg.Theme.Dark, cfg.Theme.Light)
	// Hot-reload plugins while the session is live.
	_ = host.Watch(ctx)
	// Interactive sessions clean up their background commands on exit so a dev
	// server the agent started doesn't leak past the TUI. Headless (-p) runs
	// deliberately skip this — a launched server must survive for post-run steps.
	defer builtin.KillBackgroundProcesses()
	defer builtin.CloseLSPPool() // twin of the above: reap warm language servers on exit
	if err := tui.Run(ctx, a, host, sid, modelID, wd, isDark, plat.TerminalCaps().Image); err != nil {
		fmt.Fprintln(os.Stderr, "magi: tui:", err)
		return 1
	}
	return 0
}
