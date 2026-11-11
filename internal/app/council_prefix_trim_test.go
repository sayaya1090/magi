package app

import (
	"regexp"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

var firstEntry = regexp.MustCompile(`(?m)^## (\d+) `)

// Once the history outgrows its cap, dropping one entry per append rewrites the head on every
// call: the prefill child sees a prefix that no longer starts with the one it cached, closes,
// and writes the whole history again. Trimming well below the cap keeps the head fixed for
// many appends so each call only extends what is already cached.
func TestCouncilPrefixOverflowKeepsHeadStableAcrossAppends(t *testing.T) {
	body := strings.Repeat("x", 3000)
	var evs []event.Event
	prev, trimmedAt, heads, lastHead := "", -1, 0, ""
	for i := 1; i <= 200; i++ {
		evs = append(evs, prefixPart(int64(i), session.Part{Kind: session.PartText, Text: body}))
		got := councilPrefix(evs, councilPrefixCap)
		if len(got) > councilPrefixCap {
			t.Fatalf("append %d: prefix %d bytes exceeds cap %d", i, len(got), councilPrefixCap)
		}
		if m := firstEntry.FindStringSubmatch(got); m != nil && m[1] != lastHead {
			heads++
			lastHead = m[1]
		}
		if trimmedAt < 0 && strings.Contains(got, "omitted") {
			trimmedAt = i
		} else if trimmedAt > 0 && i == trimmedAt+1 && !strings.HasPrefix(got, prev) {
			t.Fatalf("append %d right after the first trim rewrote the head (was %s)", i, firstEntry.FindStringSubmatch(prev)[1])
		}
		prev = got
	}
	if trimmedAt < 0 {
		t.Fatal("history never reached the cap; test does not exercise trimming")
	}
	// 200 entries of ~3 KB is ~9x the cap; halving gives a handful of heads, one-by-one gives ~180.
	if heads > 20 {
		t.Fatalf("head changed %d times over 200 appends", heads)
	}
}

// The history cap follows the main model's window: 80% of it at 2 bytes per token, less the
// room the rest of a council call needs. Unknown windows keep the fixed cap; tiny ones a floor.
func TestCouncilPrefixCapFollowsMainWindow(t *testing.T) {
	for _, c := range []struct{ window, want int }{
		{0, councilPrefixCap},
		{200000, 200000*8/10*2 - councilFixedReserve},
		{1000000, 1000000*8/10*2 - councilFixedReserve},
		{32000, councilPrefixFloor},
	} {
		if got := councilPrefixCapFor(c.window); got != c.want {
			t.Errorf("window %d: cap %d, want %d", c.window, got, c.want)
		}
	}
	// 200k-token window: history plus reserve stays within 80% of the window at 2 bytes/token.
	if got := councilPrefixCapFor(200000); (got+councilFixedReserve)/2 > 160000 {
		t.Errorf("200k window: history %d B + reserve exceeds 80%% of the window", got)
	}
}
