package main

import (
	"context"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/sayaya1090/magi/internal/update"
)

// updateCheckTTL bounds how often the interactive startup check hits the network:
// at most once per this window (tracked by the stamp file's mtime).
const updateCheckTTL = 24 * time.Hour

// daemonAutoUpdateTTL bounds how often a running daemon checks for a new release; daemonIdleCheckInterval
// is how often, after a build is committed, it re-checks whether the daemon has gone idle enough to
// restart. Vars so a test can shrink them.
var (
	daemonAutoUpdateTTL     = 6 * time.Hour
	daemonIdleCheckInterval = 10 * time.Second
)

// daemonAutoUpdate is the daemon's self-update loop. On a schedule, if a newer release exists it
// downloads and commits it with rollback (update.RunCommit), then — once the daemon is idle —
// restarts onto it. It returns after triggering the restart (the process is re-exec'd) or when ctx is
// done.
//
// Bench-safe by construction: it is started ONLY from the --daemon path and ONLY when [update] auto
// is on and the operator has not opted out (--no-update-check), so a benchmark — a headless one-shot,
// never a daemon — cannot reach it, and an operator who turned it off gets only the manual push.
//
// Idle-gated on purpose: a restart mid-turn throws away the in-flight step (the log keeps the rest),
// so once a build is committed the restart waits for `running` to report nothing in flight. The
// binary is already on disk, so even if the daemon never idles the update is there for the next start.
// The initial delay is jittered per-daemon (a hash of the exe path) so a machine's daemons stagger
// rather than all check and restart together.
func daemonAutoUpdate(ctx context.Context, configDir, current, exe string, running func() bool, restart func()) {
	stamp := filepath.Join(configDir, ".daemon-update-check")
	h := fnv.New32a()
	_, _ = h.Write([]byte(exe))
	quarter := daemonAutoUpdateTTL / 4
	jitter := time.Duration(0)
	if quarter > 0 {
		jitter = time.Duration(h.Sum32()) % quarter
	}
	timer := time.NewTimer(jitter)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		timer.Reset(daemonAutoUpdateTTL)
		if !updateCheckDue(stamp, daemonAutoUpdateTTL-time.Minute, time.Now()) {
			continue // a very recent restart already checked; do not hammer the network
		}
		touchStamp(stamp)
		cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		res, err := update.RunCommit(cctx, latestSource(), current, exe)
		cancel()
		if err != nil || !res.Updated {
			continue // offline, already current, or rolled back — try again next cycle
		}
		// A new build is committed to disk; wait for an idle moment, then restart onto it.
		for running() {
			select {
			case <-ctx.Done():
				return
			case <-time.After(daemonIdleCheckInterval):
			}
		}
		restart()
		return
	}
}

// Seams overridable in tests: the release source, the force-install action, and
// the force countdown. Production defaults hit GitHub / run the real installer.
var (
	// latestSource is the startup/force-install source. It delegates to newReleaseSource
	// (main.go) so a fork retargets every self-update path by reassigning that one factory;
	// tests still override latestSource directly to inject a fake.
	latestSource   = func() update.Source { return newReleaseSource() }
	forceInstallFn = func(ctx context.Context, src update.Source, current, exe string) (update.Result, error) {
		return update.Run(ctx, src, current, exe)
	}
	forceAbortWindow = 3 * time.Second
)

// shouldCheckUpdates gates the startup update check. It fires ONLY for an
// interactive TTY session that hasn't opted out — never headless (-p), never a
// non-TTY (pipe/CI/benchmark). This is the bench-safety invariant: a benchmark
// runs headless (and usually non-TTY), so it can never trigger a network call or
// a surprise install.
func shouldCheckUpdates(headless, isTTY, optOut bool) bool {
	return !headless && isTTY && !optOut
}

// updateCheckDue reports whether the TTL has elapsed since the last check (the
// stamp's mtime). A missing or unreadable stamp counts as due.
func updateCheckDue(stamp string, ttl time.Duration, now time.Time) bool {
	fi, err := os.Stat(stamp)
	if err != nil {
		return true
	}
	return now.Sub(fi.ModTime()) >= ttl
}

// touchStamp records "checked now" by (re)writing the stamp file's mtime.
func touchStamp(stamp string) {
	if err := os.MkdirAll(filepath.Dir(stamp), 0o755); err != nil {
		return
	}
	// Truncate-write is enough to bump mtime; content is unused.
	_ = os.WriteFile(stamp, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o644)
}

// maybeUpdateOnStartup runs the interactive-only update check and returns true iff
// it installed a forced update — in which case the caller must exit rather than
// launch the TUI on the now-replaced binary. It is deliberately best-effort: a
// stale cache, offline network, or install failure never blocks or fails startup.
//
// Policy (from update.UpdatePolicy): a patch bump only NOTIFIES (banner, keep
// running); a minor/major bump is treated as required and auto-installs after a
// short abort window. Callers must have already passed shouldCheckUpdates.
func maybeUpdateOnStartup(ctx context.Context, configDir, current, exe string, out io.Writer) (installed bool) {
	stamp := filepath.Join(configDir, ".update-check")
	if !updateCheckDue(stamp, updateCheckTTL, time.Now()) {
		return false
	}
	src := latestSource()
	lctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	rel, err := src.Latest(lctx)
	cancel()
	// Record the attempt regardless, so repeated offline starts don't hammer the
	// network every launch.
	touchStamp(stamp)
	if err != nil {
		return false
	}

	switch update.UpdatePolicy(current, rel.Version) {
	case update.PolicyNotify:
		fmt.Fprintf(out, "\nmagi %s is available (you have %s) — run `magi -update`\n\n", rel.Version, current)
		return false
	case update.PolicyForce:
		fmt.Fprintf(out, "\nmagi %s is a required update (you have %s). Installing… press ctrl-c to cancel.\n", rel.Version, current)
		// A real signal-cancellable context so ctrl-c aborts both the countdown and
		// the install itself, rather than relying on the default SIGINT hard-kill
		// (which would go away the moment anything upstream installs its own handler).
		ictx, stop := signal.NotifyContext(ctx, os.Interrupt)
		defer stop()
		select {
		case <-time.After(forceAbortWindow):
		case <-ictx.Done():
			fmt.Fprintln(out, "update cancelled — continuing on the current version.")
			return false
		}
		res, err := forceInstallFn(ictx, src, current, exe)
		if err != nil {
			fmt.Fprintf(out, "magi: auto-update failed: %v — continuing on %s\n", err, current)
			return false
		}
		if res.Updated {
			fmt.Fprintf(out, "updated %s → %s. Restart magi to use the new version.\n", res.From, res.To)
			return true
		}
		return false
	}
	return false
}
