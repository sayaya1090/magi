package app

import (
	"context"
	"testing"
	"time"
)

// closeAfter drains an App's goroutines when the test ends.
//
// An App that is never closed keeps its run and dispatch goroutines, and they keep writing to the
// store — into a directory t.TempDir() is trying to remove. The cleanup then fails with "directory
// not empty", which names the symptom and nothing else, and it only happens when the machine is
// loaded enough for a goroutine to still be mid-write. Seen exactly that way on TestRewind during
// a full run.
//
// newApp has done this since it existed; every test that builds an App by hand skipped it. This is
// the same three lines, callable from those.
//
// The timeout matters: Close waits for the goroutines, and a test that leaves one stuck would
// otherwise hang the suite instead of failing it.
func closeAfter(t *testing.T, a *App) *App {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.Close(ctx)
	})
	return a
}
