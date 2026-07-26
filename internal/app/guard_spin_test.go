package app

import "testing"

// TestIsNoOpBanner pins the completion-banner classifier. It MUST stay in lockstep with the
// offline calibration classifier that set bannerSpinStop: only echo/printf/true/: (a leading
// cd tolerated), and never when a redirect or pipe makes the command author/feed a file.
func TestIsNoOpBanner(t *testing.T) {
	banners := []string{
		`echo "TASK COMPLETED"`,
		`printf "done\n"`,
		`true`,
		`:`,
		`cd /app && echo "all requirements met"`,
		`echo a && echo b ; echo c`,
	}
	for _, c := range banners {
		if !isNoOpBanner(c) {
			t.Errorf("isNoOpBanner(%q) = false, want true", c)
		}
	}
	notBanners := []string{
		`echo "x" > file.txt`, // authors a file
		`echo "x" | tee f`,    // pipe + tee
		`ls -la`,              // inspects real state
		`cat result.txt`,      // reads real state
		`python solution.py`,  // runs the deliverable
		`exit 0`,              // excluded on purpose (raises pass-run ceiling)
		`false`,               // excluded on purpose
		`cd /app`,             // only a no-op prefix, no actual banner verb
		``,                    // nothing ran
		`echo hi && ls`,       // one real command disqualifies the whole line
	}
	for _, c := range notBanners {
		if isNoOpBanner(c) {
			t.Errorf("isNoOpBanner(%q) = true, want false", c)
		}
	}
}

// noteBanner emits one pure-banner tool call through the guard the way execute.go does.
func noteBanner(g *runGuard) { g.noteSpin("bash", `echo "TASK COMPLETED"`) }

// TestBannerSpinNudgeThenStop drives a pure banner run and asserts the one-shot nudge fires
// at bannerSpinNudge exactly once and the force-stop fires at bannerSpinStop.
func TestBannerSpinNudgeThenStop(t *testing.T) {
	g := newRunGuard()
	nudges, firstNudgeAt, gotStop := 0, 0, false
	for i := 1; i <= bannerSpinStop; i++ {
		noteBanner(g)
		if n := g.shouldNudge(); n == "spin" {
			nudges++
			if firstNudgeAt == 0 {
				firstNudgeAt = i
			}
		}
		if g.stuck() == "spin" {
			gotStop = true
			if i != bannerSpinStop {
				t.Fatalf("spin force-stop fired at streak %d, want %d", i, bannerSpinStop)
			}
			break
		}
	}
	if nudges != 1 {
		t.Errorf("spin nudge fired %d times, want exactly 1 (one-shot)", nudges)
	}
	if firstNudgeAt != bannerSpinNudge {
		t.Errorf("spin nudge first fired at streak %d, want %d", firstNudgeAt, bannerSpinNudge)
	}
	if !gotStop {
		t.Errorf("spin force-stop never fired after %d banners", bannerSpinStop)
	}
}

// TestBannerSpinResetsOnRealAction confirms any non-banner tool call zeroes the streak, so an
// agent that does real work between banners never trips the guard.
func TestBannerSpinResetsOnRealAction(t *testing.T) {
	resets := []struct {
		name, cmd string
	}{
		{"bash", `python solution.py`}, // a real (even if failing) exercise
		{"read", ""},                   // a non-bash tool
		{"write", ""},                  // a mutation
		{"bash", `ls -la`},             // inspection is a real action, not a banner
	}
	for _, r := range resets {
		g := newRunGuard()
		for i := 0; i < bannerSpinStop-1; i++ { // one short of stop
			noteBanner(g)
		}
		g.noteSpin(r.name, r.cmd) // real action resets
		noteBanner(g)             // streak restarts at 1
		if s := g.stuck(); s == "spin" {
			t.Errorf("after reset via %s(%q), stuck()=spin — streak was not reset", r.name, r.cmd)
		}
		if g.bannerSpin != 1 {
			t.Errorf("after reset via %s(%q) + 1 banner, bannerSpin=%d, want 1", r.name, r.cmd, g.bannerSpin)
		}
	}
}

// TestBannerSpinOrthogonalToMutation is the core regression guard: a file mutation must NOT
// shield a spin. mutated() bumps the epoch and resets the stall counter, but a write is not a
// banner tool call, so an UNBROKEN banner run after it still accumulates and stops.
func TestBannerSpinOrthogonalToMutation(t *testing.T) {
	g := newRunGuard()
	g.mutated("solution.py", "v1") // agent wrote a (wrong) deliverable — epoch>0
	for i := 0; i < bannerSpinStop; i++ {
		noteBanner(g) // pure banners only; nothing resets bannerSpin
	}
	if g.stuck() != "spin" {
		t.Fatalf("stuck()=%q, want spin — a mutation must not shield a completion-banner spin", g.stuck())
	}
	if g.mutationEpoch() == 0 {
		t.Fatal("precondition: epoch should be >0 so the loop lands via clean-finish, not error")
	}
}
