package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/update"
)

type fakeSource struct {
	rel update.Release
	err error
}

func (f fakeSource) Latest(context.Context) (update.Release, error)   { return f.rel, f.err }
func (f fakeSource) Download(context.Context, string) ([]byte, error) { return nil, nil }

// The bench-safety invariant: the startup check must NOT fire when headless
// (-p) or when stdout is not a TTY (pipe/CI/benchmark), regardless of opt-out.
func TestShouldCheckUpdatesGate(t *testing.T) {
	cases := []struct {
		headless, isTTY, optOut, want bool
	}{
		{false, true, false, true},   // interactive TTY, not opted out → check
		{true, true, false, false},   // headless (-p) → never
		{false, false, false, false}, // non-TTY (pipe/bench) → never
		{true, false, false, false},  // headless + non-TTY → never
		{false, true, true, false},   // opted out → never
	}
	for _, c := range cases {
		if got := shouldCheckUpdates(c.headless, c.isTTY, c.optOut); got != c.want {
			t.Errorf("shouldCheckUpdates(headless=%v,tty=%v,optOut=%v) = %v, want %v",
				c.headless, c.isTTY, c.optOut, got, c.want)
		}
	}
}

func TestUpdateCheckDue(t *testing.T) {
	dir := t.TempDir()
	stamp := filepath.Join(dir, ".update-check")
	now := time.Now()

	// Missing stamp → due.
	if !updateCheckDue(stamp, updateCheckTTL, now) {
		t.Fatal("missing stamp should be due")
	}
	if err := os.WriteFile(stamp, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Just written → not due.
	if updateCheckDue(stamp, updateCheckTTL, now) {
		t.Fatal("fresh stamp should not be due")
	}
	// Past the TTL → due again.
	if !updateCheckDue(stamp, updateCheckTTL, now.Add(25*time.Hour)) {
		t.Fatal("stale stamp should be due")
	}
}

// swapSeams installs test doubles and returns a restore func.
func swapSeams(t *testing.T, rel update.Release, err error, install update.Result, installErr error) *int {
	t.Helper()
	calls := 0
	origSrc, origInstall, origWin := latestSource, forceInstallFn, forceAbortWindow
	latestSource = func() update.Source { return fakeSource{rel: rel, err: err} }
	forceInstallFn = func(context.Context, update.Source, string, string) (update.Result, error) {
		calls++
		return install, installErr
	}
	forceAbortWindow = 0
	t.Cleanup(func() { latestSource, forceInstallFn, forceAbortWindow = origSrc, origInstall, origWin })
	return &calls
}

// A patch bump only notifies: banner printed, no install, keep running (false).
func TestMaybeUpdatePatchNotifies(t *testing.T) {
	calls := swapSeams(t, update.Release{Version: "1.2.4"}, nil, update.Result{}, nil)
	var out bytes.Buffer
	if installed := maybeUpdateOnStartup(context.Background(), t.TempDir(), "1.2.3", "/x/magi", &out); installed {
		t.Fatal("patch bump must not install")
	}
	if *calls != 0 {
		t.Fatalf("forceInstall called %d times on a patch bump, want 0", *calls)
	}
	if !strings.Contains(out.String(), "1.2.4 is available") {
		t.Fatalf("expected notify banner, got %q", out.String())
	}
}

// A minor bump is required: it installs and signals the caller to exit (true).
func TestMaybeUpdateMinorForces(t *testing.T) {
	calls := swapSeams(t, update.Release{Version: "1.3.0"}, nil, update.Result{Updated: true, From: "1.2.3", To: "1.3.0"}, nil)
	var out bytes.Buffer
	installed := maybeUpdateOnStartup(context.Background(), t.TempDir(), "1.2.3", "/x/magi", &out)
	if !installed {
		t.Fatal("minor bump must install and return true (caller exits)")
	}
	if *calls != 1 {
		t.Fatalf("forceInstall called %d times, want 1", *calls)
	}
	if !strings.Contains(out.String(), "required update") {
		t.Fatalf("expected required-update notice, got %q", out.String())
	}
}

// A source build (git-describe stamp) is NEVER force-installed, even on a minor bump: parseSemver
// truncates the suffix and would read it as the release it was built past, so UpdatePolicy says
// Force — but SelfUpdatable rejects it, and the startup check must downgrade to a notice rather than
// replace the developer's own binary (with no .prev, since this seam uses Run/Apply). This is the
// exact case rollback_test.go calls "the dangerous case"; before the gate it force-installed here.
func TestMaybeUpdateDoesNotForceOverASourceBuild(t *testing.T) {
	calls := swapSeams(t, update.Release{Version: "0.23.0"}, nil,
		update.Result{Updated: true, From: "x", To: "0.23.0"}, nil)
	var out bytes.Buffer
	installed := maybeUpdateOnStartup(context.Background(), t.TempDir(), "v0.22.2-13-gabc1234-dirty", "/x/magi", &out)
	if installed {
		t.Fatal("a source build was force-installed on a minor bump — the SelfUpdatable gate is missing")
	}
	if *calls != 0 {
		t.Fatalf("forceInstall called %d times over a source build, want 0", *calls)
	}
	// It still tells the developer a release exists — the information, not the surprise.
	if !strings.Contains(out.String(), "0.23.0 is available") {
		t.Fatalf("expected a notice for the source build, got %q", out.String())
	}
	if strings.Contains(out.String(), "required update") {
		t.Fatalf("a source build must not see the force-install notice: %q", out.String())
	}
}

// A failed install must not wedge startup: swallow the error, keep running (false).
func TestMaybeUpdateForceFailureContinues(t *testing.T) {
	swapSeams(t, update.Release{Version: "2.0.0"}, nil, update.Result{}, context.DeadlineExceeded)
	var out bytes.Buffer
	if installed := maybeUpdateOnStartup(context.Background(), t.TempDir(), "1.2.3", "/x/magi", &out); installed {
		t.Fatal("failed install must not report installed")
	}
	if !strings.Contains(out.String(), "auto-update failed") {
		t.Fatalf("expected failure notice, got %q", out.String())
	}
}

// A fresh stamp short-circuits: no source call, nothing printed.
func TestMaybeUpdateRespectsCache(t *testing.T) {
	calls := swapSeams(t, update.Release{Version: "9.9.9"}, nil, update.Result{}, nil)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".update-check"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if installed := maybeUpdateOnStartup(context.Background(), dir, "1.2.3", "/x/magi", &out); installed {
		t.Fatal("cached check should be a no-op")
	}
	if *calls != 0 || out.Len() != 0 {
		t.Fatalf("cached check should not act: calls=%d out=%q", *calls, out.String())
	}
}

// An offline/errored source is swallowed and stamps the attempt (no hammering).
func TestMaybeUpdateOfflineStampsAndContinues(t *testing.T) {
	swapSeams(t, update.Release{}, context.DeadlineExceeded, update.Result{}, nil)
	dir := t.TempDir()
	var out bytes.Buffer
	if installed := maybeUpdateOnStartup(context.Background(), dir, "1.2.3", "/x/magi", &out); installed {
		t.Fatal("offline check must not install")
	}
	if _, err := os.Stat(filepath.Join(dir, ".update-check")); err != nil {
		t.Fatalf("offline check should still stamp the attempt: %v", err)
	}
}

// 링크 시점 일정 override — **말하고, 말도 안 되는 것은 거절한다**(daemonUpdateEvery 의 두 규칙).
//
// 이 변수의 값은 셋이 아니라 둘을 정한다: 데몬이 실제로 지키는 주기와, 그것을 사람이 알 수 있는가.
// 둘째가 없으면 문서에 없는 주기로 바깥에 손을 뻗는 데몬을 **시간을 재서야** 알아낼 수 있다.
func TestTheScheduleOverrideIsSaidOrRefusedButNeverSilent(t *testing.T) {
	for _, c := range []struct {
		spec  string
		want  time.Duration
		bad   bool
		says  []string
		quiet bool
	}{
		{spec: "", quiet: true},
		{spec: "   ", quiet: true}, // 셸이 남기는 모양 — export 만 하고 값을 안 준 것
		{spec: "3s", want: 3 * time.Second, says: []string{"3s", "6h0m0s", updateEveryVar}},
		{spec: " 90m ", want: 90 * time.Minute, says: []string{"1h30m0s"}},
		{spec: "yesterday", bad: true, says: []string{`"yesterday"`, "not a duration", "6h0m0s"}},
		{spec: "250ms", bad: true, says: []string{"250ms", "floor", "1s"}},
		{spec: "0", bad: true, says: []string{"floor"}},
		{spec: "-5m", bad: true, says: []string{"floor"}},
	} {
		t.Run(c.spec, func(t *testing.T) {
			got, err := updateEvery(c.spec)
			switch {
			case c.bad && err == nil:
				t.Fatalf("%q 를 받아들였다 (%v) — 데몬이 그 주기로 돈다", c.spec, got)
			case !c.bad && err != nil:
				t.Fatalf("%q 를 거절했다: %v", c.spec, err)
			case !c.bad && got != c.want:
				t.Fatalf("%q 가 %v 로 읽혔다, %v 여야 한다", c.spec, got, c.want)
			}
			// 거절은 **0** 을 돌려줘야 한다. 호출자는 그것을 「설정 안 됨」으로 읽어 상수를 쓰므로,
			// 거절하면서 값을 함께 돌려주면 거절이 통과가 된다.
			if c.bad && got != 0 {
				t.Errorf("거절하면서 %v 를 돌려줬다 — 호출자가 그것을 쓸 수 있다", got)
			}

			old := daemonUpdateEvery
			daemonUpdateEvery = c.spec
			defer func() { daemonUpdateEvery = old }()
			var said bytes.Buffer
			announceUpdateSchedule(&said)
			if c.quiet {
				if said.Len() != 0 {
					t.Errorf("기본인데 한 줄을 냈다: %q — 늘 나오는 줄은 아무도 안 읽는다", said.String())
				}
				return
			}
			if said.Len() == 0 {
				t.Fatalf("%q 로 지어졌는데 아무 말도 안 한다 — 시간을 재야 알 수 있는 일정이 된다", c.spec)
			}
			for _, want := range c.says {
				if !strings.Contains(said.String(), want) {
					t.Errorf("낸 줄에 %q 가 없다: %q", want, said.String())
				}
			}
		})
	}
}
