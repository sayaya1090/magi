package main

import (
	"context"
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
