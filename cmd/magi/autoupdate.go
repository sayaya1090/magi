package main

import (
	"context"
	"fmt"
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

// Seams overridable in tests: the release source, the force-install action, and
// the force countdown. Production defaults hit GitHub / run the real installer.
var (
	latestSource   = func() update.Source { return update.NewGitHubSource(ghOwner, ghRepo) }
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
