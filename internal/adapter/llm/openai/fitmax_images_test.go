package openai

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// shots writes n files of size bytes each and returns references to them.
func shots(t *testing.T, size int, n int) []session.ImageRef {
	t.Helper()
	dir := t.TempDir()
	var refs []session.ImageRef
	for i := 0; i < n; i++ {
		p := filepath.Join(dir, fmt.Sprintf("shot%d.png", i))
		if err := os.WriteFile(p, make([]byte, size), 0o600); err != nil {
			t.Fatal(err)
		}
		refs = append(refs, session.ImageRef{Path: p, MIME: "image/png"})
	}
	return refs
}

// withShots is a conversation of `tokens` tokens of text with one tool result carrying refs.
func withShots(tokens int, refs []session.ImageRef) port.ChatRequest {
	return port.ChatRequest{
		Model: "m",
		Messages: []session.Message{
			{Role: session.RoleUser, Parts: []session.Part{
				{Kind: session.PartText, Text: strings.Repeat("x", tokens*4)}}},
			{Role: session.RoleTool, Parts: []session.Part{{Kind: session.PartToolResult,
				ToolResult: &session.ToolResult{
					CallID: "c1", Content: json.RawMessage(`"ok"`), Images: refs}}}},
		},
	}
}

func sees(v bool) func(string) bool { return func(string) bool { return v } }

// The fit measured a request the wire builder no longer sends.
//
// A picture is a REFERENCE in the parts and thousands of tokens on the wire: estimateRequestTokens
// walks the parts, sees a path, and charges the path's length. Vision arrived after the fit did,
// so the cap went out computed from a body that was smaller than the one built a moment later.
// Measured before this: a 200,000 window, 190,001 tokens of text and three renders riding, and the
// fit answered 5,903 — the window spent to the last token before a single picture was added. The
// four-picture budget bounds that error and does not remove it, because four pictures outweigh
// windowMargin on their own.
func TestThePicturesAreChargedAgainstTheWindow(t *testing.T) {
	req := withShots(190_001, shots(t, 1000, 3))
	c := &Client{maxTokens: 64_000, window: func(string) int { return 200_000 }, vision: sees(true)}

	blind := estimateRequestTokens(req)
	got := c.fitMaxTokens(req)
	if want := 200_000 - (blind + 3*imageTokens) - windowMargin; got != want {
		t.Errorf("cap %d, want %d — three riding pictures cost %d tokens the fit did not charge",
			got, want, 3*imageTokens)
	}
	if real := blind + 3*imageTokens; real+got > 200_000 {
		t.Errorf("input %d + cap %d = %d, past the 200,000 window", real, got, real+got)
	}
}

// A model that cannot see is not sent the blocks, so charging it for them would cut its output cap
// for pictures it never receives. The same conversation, the same files, one different answer to
// "can this model look at one".
func TestAModelThatCannotSeeIsNotChargedForPictures(t *testing.T) {
	req := withShots(190_001, shots(t, 1000, 3))
	blind := &Client{maxTokens: 64_000, window: func(string) int { return 200_000 }, vision: sees(false)}
	seeing := &Client{maxTokens: 64_000, window: func(string) int { return 200_000 }, vision: sees(true)}

	want := 200_000 - estimateRequestTokens(req) - windowMargin
	if got := blind.fitMaxTokens(req); got != want {
		t.Errorf("a model that cannot see got %d, want %d — it pays for pictures it is not sent", got, want)
	}
	if got := seeing.fitMaxTokens(req); got >= want {
		t.Errorf("a model that CAN see got %d, no less than the blind %d", got, want)
	}
}

// Charged for what rides, not for what is attached. The budget drops pictures, the same file twice
// is one picture, and a render whose file has gone is not sent at all — every one of those is a
// picture in the parts and no tokens on the wire, and an estimate that counted the parts would cut
// the cap for a body nobody built.
func TestOnlyTheRidingPicturesAreCharged(t *testing.T) {
	window := func(string) int { return 200_000 }
	c := &Client{maxTokens: 64_000, window: window, vision: sees(true)}
	// What the pictures cost, read back out of the fitted cap. Only meaningful while the cap is
	// the binding number: at the configured ceiling or at the floor it stops moving, and the
	// subtraction would measure nothing while still producing a number.
	charged := func(t *testing.T, req port.ChatRequest) int {
		t.Helper()
		got := c.fitMaxTokens(req)
		if got == c.maxTokens || got == minOutputTokens {
			t.Fatalf("cap came out at %d, its ceiling or its floor; this case measures nothing", got)
		}
		return 200_000 - estimateRequestTokens(req) - windowMargin - got
	}

	t.Run("past the count", func(t *testing.T) {
		req := withShots(185_000, shots(t, 1000, imagesPerReply+3))
		if got, want := charged(t, req), imagesPerReply*imageTokens; got != want {
			t.Errorf("charged %d for %d attached pictures; only %d ride", got, imagesPerReply+3, imagesPerReply)
		}
	})
	t.Run("past the byte budget", func(t *testing.T) {
		// Two of these fit; the third is over what is left and is left behind, keeping its line of text.
		req := withShots(190_001, shots(t, imageBudget/2*3/4, 3))
		if got, want := charged(t, req), 2*imageTokens; got != want {
			t.Errorf("charged %d, want %d — the picture over budget is not on the wire", got, want)
		}
	})
	t.Run("the same file twice", func(t *testing.T) {
		one := shots(t, 1000, 1)
		req := withShots(190_001, []session.ImageRef{one[0], one[0]})
		if got, want := charged(t, req), imageTokens; got != want {
			t.Errorf("charged %d for one file named twice, want %d", got, want)
		}
	})
	t.Run("a file that is gone", func(t *testing.T) {
		refs := shots(t, 1000, 2)
		os.Remove(refs[1].Path)
		if got, want := charged(t, withShots(190_001, refs)), imageTokens; got != want {
			t.Errorf("charged %d, want %d — a render that is not on disk is not sent", got, want)
		}
	})
}
