package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/update"
	"github.com/sayaya1090/magi/internal/version"
)

// The single release-source factory must feed BOTH the interactive startup source
// (latestSource) and the `-update` core path (runCoreUpdate): reassigning
// newReleaseSource is the one edit a fork makes to retarget every self-update path.
func TestNewReleaseSourceFeedsAllPaths(t *testing.T) {
	orig := newReleaseSource
	t.Cleanup(func() { newReleaseSource = orig })

	calls := 0
	newReleaseSource = func() update.Source {
		calls++
		// Same version as the running binary → Run reports "skipped", never touching
		// the executable, so runCoreUpdate is safe to exercise here.
		return fakeSource{rel: update.Release{Version: version.Version}}
	}

	// latestSource delegates to the factory (default wiring, not overridden here).
	if _, ok := latestSource().(fakeSource); !ok {
		t.Fatal("latestSource should build from newReleaseSource")
	}
	// runCoreUpdate must also go through the factory.
	before := calls
	if rc := runCoreUpdate(); rc != 0 {
		t.Fatalf("runCoreUpdate rc = %d, want 0 on a same-version skip", rc)
	}
	if calls <= before {
		t.Fatal("runCoreUpdate did not build its source from newReleaseSource")
	}
}

// The startup composition hook slice runs its hooks in order with the given config dir;
// empty by default so a stock build does nothing extra at boot.
func TestOnInteractiveStartHooksRunInOrder(t *testing.T) {
	orig := onInteractiveStart
	t.Cleanup(func() { onInteractiveStart = orig })

	if len(onInteractiveStart) != 0 {
		t.Fatalf("onInteractiveStart should be empty by default, got %d", len(onInteractiveStart))
	}

	var order []string
	onInteractiveStart = []func(context.Context, string){
		func(_ context.Context, dir string) { order = append(order, "a:"+dir) },
		func(_ context.Context, dir string) { order = append(order, "b:"+dir) },
	}
	for _, h := range onInteractiveStart {
		h(context.Background(), "/cfg")
	}
	if len(order) != 2 || order[0] != "a:/cfg" || order[1] != "b:/cfg" {
		t.Fatalf("hooks should run in order with the config dir, got %v", order)
	}
}

// **The env door points the SOURCE somewhere else and buys nothing beyond that.**
//
// ⚠ This is the rule that keeps the door from being a way to install unsigned builds. `WithAPIBase`
// moves the REST root; the asset name this build asks for and the `checksums.txt` line it demands are
// untouched. If a later edit ever wires the env var into the checksum path, this fails.
func TestTheEnvDoorDoesNotWeakenTheChecksumRule(t *testing.T) {
	t.Setenv(releaseAPIBaseEnv, "https://local.test/api/v3")
	src, ok := newReleaseSource().(*update.GitHubSource)
	if !ok {
		t.Fatalf("the door built %T, not a GitHub source", newReleaseSource())
	}
	if src.APIBase != "https://local.test/api/v3" {
		t.Errorf("the base did not travel: %q", src.APIBase)
	}

	// ⚠ **The env cannot REACH the checksum rule**, and that is a structural fact rather than a
	// promise: the variable is read here, in the construction seam, and the package that verifies a
	// download never mentions it. So no amount of environment changes what is demanded of a release.
	// (What IS demanded — a `checksums.txt` line for this build's asset — is held by
	// TestANonDefaultBaseStillDemandsAChecksum in internal/update, where the internals live.)
	for _, dir := range []string{"../../internal/update"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		read := 0
		for _, e := range entries {
			// Implementation only. A TEST over there names the variable in its prose — that is the
			// documentation this rule wants, and scanning it would make the explanation fail the rule
			// it explains.
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
				continue
			}
			body, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			read++
			if strings.Contains(string(body), releaseAPIBaseEnv) {
				t.Errorf("%s/%s reads %s — the door reaches past the source into the verifying code",
					dir, e.Name(), releaseAPIBaseEnv)
			}
		}
		if read < 3 {
			t.Fatalf("only read %d files in %s — the scan is broken, not the rule", read, dir)
		}
	}
}

// Rule 1: a non-default source is SAID. And the default one is silent, because a line that appears
// every time is a line nobody reads.
func TestANonDefaultReleaseSourceIsAnnounced(t *testing.T) {
	var quiet strings.Builder
	announceReleaseSource(&quiet)
	if quiet.String() != "" {
		t.Errorf("the default source announced itself: %q", quiet.String())
	}

	t.Setenv(releaseAPIBaseEnv, "https://local.test/api/v3")
	var said strings.Builder
	announceReleaseSource(&said)
	for _, want := range []string{"https://local.test/api/v3", releaseAPIBaseEnv, ghOwner + "/" + ghRepo} {
		if !strings.Contains(said.String(), want) {
			t.Errorf("the line does not name %s: %q", want, said.String())
		}
	}
}

// Blank is unset, not an empty base. An exported-but-empty variable is what a shell script leaves
// behind, and reading it as a base sends every lookup to a relative URL.
func TestABlankEnvIsTheDefaultSource(t *testing.T) {
	for _, blank := range []string{"", "   ", "\t"} {
		t.Setenv(releaseAPIBaseEnv, blank)
		src, ok := newReleaseSource().(*update.GitHubSource)
		if !ok {
			t.Fatalf("%q built %T", blank, newReleaseSource())
		}
		// ⚠ Compared against a source built with the variable ABSENT, not against the public API
		// string: an unset APIBase means "the default" and the constant lives in the other package.
		// The claim is «blank behaves as if nothing was said», and that is the comparison that says it.
		os.Unsetenv(releaseAPIBaseEnv)
		want, _ := newReleaseSource().(*update.GitHubSource)
		t.Setenv(releaseAPIBaseEnv, blank)
		if want == nil || src.APIBase != want.APIBase {
			t.Errorf("%q gave base %q, but an absent variable gives %q", blank, src.APIBase, want.APIBase)
		}
		var w strings.Builder
		announceReleaseSource(&w)
		if w.String() != "" {
			t.Errorf("%q announced itself: %q", blank, w.String())
		}
	}
}

// And every path that updates says it — the loop a person is not watching included. A scan, because
// the alternative is standing up three real update runs to read their output.
func TestEveryUpdatePathAnnouncesTheSource(t *testing.T) {
	for _, f := range []string{"main.go", "autoupdate.go"} {
		body, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(body), "announceReleaseSource(") {
			t.Errorf("%s never announces the source", f)
		}
	}
	body, err := os.ReadFile("autoupdate.go")
	if err != nil {
		t.Fatal(err)
	}
	// The daemon's own loop and the interactive startup check are different paths; both must say it.
	if n := strings.Count(string(body), "announceReleaseSource("); n < 2 {
		t.Errorf("autoupdate.go announces on %d of its two update paths", n)
	}
}
