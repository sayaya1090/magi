package main

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
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

// **Every path that BUILDS a release source announces it — asked of the paths, not of the files.**
//
// ⚠ This guard replaces one that counted `announceReleaseSource(` occurrences per file, and that
// count is exactly why it missed `daemonEngine.Update` — the console's button applied the environment
// and said nothing, while the file it lives in had plenty of announcements elsewhere. A guard that
// counts is a guard that can be satisfied by the wrong lines.
//
// So the question is asked of the SYNTAX: every function that calls the factory (or `latestSource`,
// which delegates to it) must also announce inside that same function. The two definitions of those
// seams are not callers and are skipped by name.
func TestEveryPathThatBuildsASourceAnnouncesIt(t *testing.T) {
	const announce = "announceReleaseSource"
	builders := map[string]bool{"newReleaseSource": true, "latestSource": true}
	checked := 0
	for _, name := range []string{"main.go", "autoupdate.go"} {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			var builds, says bool
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				id, ok := call.Fun.(*ast.Ident)
				if !ok {
					return true
				}
				if builders[id.Name] {
					builds = true
				}
				if id.Name == announce {
					says = true
				}
				return true
			})
			if !builds {
				continue
			}
			checked++
			if !says {
				t.Errorf("%s: %s builds a release source and never announces it — a person updating from "+
					"here is not told the build comes from somewhere else", name, fn.Name.Name)
			}
		}
	}
	// ⚠ The floor is the only evidence this measured anything: a parser change or a rename would
	// otherwise leave it green with nothing inspected.
	if checked < 3 {
		t.Fatalf("only %d functions build a source — the scan is broken, not the code", checked)
	}
}
