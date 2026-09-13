package main

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
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

// daemonUpdateEvery replaces the six-hour schedule above in a binary built with
//
//	go build -ldflags "-X main.daemonUpdateEvery=3s"
//
// and is empty in everything we ship. It exists because the LOOP — not the update, the loop — had no
// way to be measured: `daemonAutoUpdateTTL` is a var a unit test can shrink, and the loop's risk is
// not in a unit. What it does unattended (notice a release, install it, wait for a quiet moment,
// restart onto it, come up as the new build) only happens in a real daemon in its own process, and
// that process's first check is up to 1.5 hours away.
//
// # Why a link-time string and not an environment variable
//
// `MAGI_RELEASE_API_BASE` (main.go) is an environment variable because pointing at a different
// release server is a PRODUCT capability — GitHub Enterprise, a private fork — that was merely
// unsayable at runtime. A check schedule is not: [update] auto turns the loop off, and how often it
// runs when on is this project's decision, not a knob an operator is asking for. Adding an
// environment variable for it would ship a switch nobody wants in order to let a test run.
//
// The same comment refused a test-only BUILD TAG for the release source, and the reason applies here
// unchanged: a tag left on in a release build changes every self-update path silently. A linker
// substitution is narrower than both — it must name this one variable, it cannot be flipped on a
// binary that is already built, and a release build that never passes it gets the schedule in the
// constant above.
//
// Two rules travel with it:
//
//  1. **It says so**, on the daemon's own stream when the loop starts and in `magi -version`
//     (announceUpdateSchedule). A daemon reaching out on a schedule nobody documented should not
//     have to be timed to be found out.
//  2. **It is refused when it is nonsense**, out loud, and the constant stands — a build asked for a
//     schedule and quietly given a different one is worse than one that says no.
var daemonUpdateEvery string

// updateEveryVar is the linker's name for the variable above, used in the lines that mention it so
// the text a person reads is the text they would type.
const updateEveryVar = "main.daemonUpdateEvery"

// updateEveryFloor is the shortest schedule accepted. A cycle downloads, verifies, and installs with
// a ten-minute timeout, so anything under a second is not a schedule — it is a typo that would have
// the daemon spend itself on a release server.
const updateEveryFloor = time.Second

// updateEvery reads the override. (0, nil) means "not set — use the constant"; an error means it was
// set to something this build will not honour, phrased for the line that says so.
func updateEvery(spec string) (time.Duration, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(spec)
	if err != nil {
		return 0, fmt.Errorf("%s is %q, which is not a duration", updateEveryVar, spec)
	}
	if d < updateEveryFloor {
		return 0, fmt.Errorf("%s is %v, shorter than the %v floor", updateEveryVar, d, updateEveryFloor)
	}
	return d, nil
}

// announceUpdateSchedule writes rule 1's line: what this build's schedule actually is, when it is not
// the documented one. Silent otherwise — for the same reason announceReleaseSource is silent on the
// default source: a line that always appears is a line people learn to skip past.
func announceUpdateSchedule(w io.Writer) {
	d, err := updateEvery(daemonUpdateEvery)
	switch {
	case err != nil:
		fmt.Fprintf(w, "magi: auto-update: %v — the %v schedule stands\n", err, daemonAutoUpdateTTL)
	case d > 0:
		fmt.Fprintf(w, "magi: auto-update: this build checks every %v, not %v — it was built with %s set\n",
			d, daemonAutoUpdateTTL, updateEveryVar)
	}
}

// daemonAutoUpdate is the daemon's self-update loop. On a schedule, if a newer release exists it
// downloads and commits it with rollback (update.RunCommit), then — once the daemon is idle —
// restarts onto it. It returns after triggering the restart (the process is re-exec'd) or when ctx is
// done.
//
// Bench-safe by construction: it is started ONLY from the --daemon path and ONLY when [update] auto
// is on and the operator has not opted out (--no-update-check), so a benchmark — a headless one-shot,
// never a daemon — cannot reach it, and an operator who turned it off gets only the manual push.
//
// Dev-safe too: only a clean release tag auto-updates (SelfUpdatable) — a "dev" build or a
// git-describe source build ("v0.22.2-13-g…") is somebody's own binary, and IsNewer deliberately
// lets only an EXPLICIT `magi -update` move it onto a release. This loop inheriting that would
// silently replace a developer's own build within hours of `go build && ./magi --daemon`. The
// interactive startup check carries the same SelfUpdatable gate before its force-install.
//
// Idle-gated on purpose: a restart mid-turn throws away the in-flight step (the log keeps the rest),
// so once a build is committed the restart waits for `running` to report nothing in flight. The
// binary is already on disk, so even if the daemon never idles the update is there for the next start.
// The initial delay is jittered per-daemon — seeded from the SOCKET path, which is the one string
// distinct per daemon on a machine (they usually share one exe) — and scaled across a quarter of the
// TTL, so a machine's daemons stagger rather than all check together. The stamp file carries the
// same per-daemon suffix: shared, one daemon's check silenced every other's for the whole TTL, and
// the others then never restarted onto a build the first had already committed.
func daemonAutoUpdate(ctx context.Context, configDir, current, exe, sock string, running func() bool,
	hold func() (func(), bool), restart func()) {
	if !update.SelfUpdatable(current) {
		fmt.Fprintf(os.Stderr, "magi: auto-update off: %q is not a release build\n", current)
		return
	}
	if exe == "" {
		fmt.Fprintln(os.Stderr, "magi: auto-update off: the running binary's path is unknown")
		return
	}
	h := fnv.New64a()
	h.Write([]byte(sock)) //nolint:errcheck // hash.Hash never errors
	stamp := filepath.Join(configDir, fmt.Sprintf(".daemon-update-check-%016x", h.Sum64()))
	// Rule 1 of daemonUpdateEvery: the schedule this daemon is actually keeping is said before it
	// starts keeping it. Resolved from the same function that said it, so the line and the timer can
	// never disagree; a refused override has already been reported and leaves the constant standing.
	ttl := daemonAutoUpdateTTL
	announceUpdateSchedule(os.Stderr)
	if over, err := updateEvery(daemonUpdateEvery); err == nil && over > 0 {
		ttl = over
	}
	quarter := ttl / 4
	// A 64-bit hash modulo the window. The two obvious 32-bit spellings are both wrong: a uint32
	// read directly as a Duration is at most ~4.3 SECONDS of nanoseconds (so `sum32 % quarter` was a
	// no-op), and scaling as `quarter*sum32>>32` overflows uint64 at any quarter past ~4.3s — which
	// collapsed the spread right back to [0, 4.3s). Sum64 % quarter cannot overflow (both operands
	// fit) and its modulo bias over a 1.5h window is nanoseconds — irrelevant here.
	jitter := time.Duration(0)
	if quarter > 0 {
		jitter = time.Duration(h.Sum64() % uint64(quarter))
	}
	timer := time.NewTimer(jitter)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		timer.Reset(ttl)
		// A minute of slack, so a restart's own check a moment ago counts — but never more slack than
		// a quarter of the window, or a short schedule would hand its whole window away and this gate
		// would be deciding by arithmetic sign rather than by policy.
		slack := time.Minute
		if q := ttl / 4; slack > q {
			slack = q
		}
		if !updateCheckDue(stamp, ttl-slack, time.Now()) {
			continue // this daemon's own recent restart already checked; do not hammer the network
		}
		touchStamp(stamp)
		// Rule 1 again, on the path a person is NOT watching. A daemon that quietly takes builds from
		// somewhere else is the shape this announcement exists to prevent.
		announceReleaseSource(os.Stderr)
		cctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		res, err := update.RunCommit(cctx, latestSource(), current, exe)
		cancel()
		// Being offline and shipping a build that cannot run are not the same news.
		//
		// One branch used to cover both, and the loop said nothing either way — so a release whose
		// binary failed the pre-flight was retried every six hours, forever, in silence. That is
		// how the archive-instead-of-binary defect survived: `magi --update-core` reported it, and
		// nothing else ever did (internal/update/unpack.go).
		//
		// A rollback means the download WAS installed and then undone. Said once per cycle, on the
		// same stream every other daemon line uses. Offline and already-current stay quiet — they
		// are the weather, and a line every six hours about the network is a line people learn to
		// skip past.
		var rolled *update.RolledBackError
		if errors.As(err, &rolled) {
			fmt.Fprintf(os.Stderr, "magi: auto-update: %v — staying on %s\n", rolled, current)
			continue
		}
		if err != nil || !res.Updated {
			continue // offline or already current — try again next cycle
		}
		// A new build is committed to disk; wait for an idle moment, then restart onto it.
		//
		// ⚠ **The same atomic safe point the pressed button uses.** Polling `running()` and then
		// restarting leaves a turn able to start between the two lines, to be thrown away by a
		// restart that had just concluded there was none — CLIENT_LIFECYCLE §9.3 asks for the
		// judgement and the closing of the door to be one step, and this loop was the other half of
		// the fix that only reached the door (3f693903). `hold` shuts it in the step that finds it
		// quiet; a caller that has none falls back to the poll, which is what this always did.
		var release func()
		for {
			if hold != nil {
				var held bool
				if release, held = hold(); held {
					break
				}
			} else if !running() {
				break
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(daemonIdleCheckInterval):
			}
		}
		// The daemon may have begun stopping while we polled (Restart itself refuses after a stop
		// has begun, but respect our own ctx too rather than racing it).
		if ctx.Err() != nil {
			if release != nil {
				release()
			}
			return
		}
		restart()
		if release != nil {
			release()
		}
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
	// Rule 1 of releaseAPIBaseEnv: an update that is about to happen says where it comes from. Before
	// the lookup, not after — if the far side is slow or wrong, the line a person needs is the one
	// naming who was asked.
	announceReleaseSource(out)
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
		// A forced install REPLACES the running binary, and only a clean release tag is ours to
		// replace. A source build carries a git-describe suffix ("v0.22.2-13-gabc1234-dirty") that
		// parseSemver truncates at the '-', so UpdatePolicy read it as the release it was built PAST
		// and would force it out from under the developer — with no .prev, since this seam uses
		// Run/Apply. SelfUpdatable is the same gate the daemon paths carry (daemonAutoUpdate,
		// daemonEngine.Update); this path was the one that skipped it. A non-release build still gets
		// the notice — the information without the surprise install.
		if !update.SelfUpdatable(current) {
			fmt.Fprintf(out, "\nmagi %s is available (you have %s) — run `magi -update`\n\n", rel.Version, current)
			return false
		}
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
