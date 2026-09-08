package update

import (
	"context"
	"os"
	"testing"
	"time"
)

// A live look at which release the updater resolves to, for hand-checking. Off unless asked —
// nothing in CI reaches github.com.
func TestLatestLive(t *testing.T) {
	if os.Getenv("MAGI_LIVE_LATEST") == "" {
		t.Skip("set MAGI_LIVE_LATEST=1 to ask github.com")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	g := NewGitHubSource("sayaya1090", "magi")
	t.Logf("core-latest.txt says: %q", g.coreTag(ctx))
	rel, err := g.Latest(ctx)
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	t.Logf("resolved: %s  sha256=%.16s…", rel.Version, rel.SHA256)
}
