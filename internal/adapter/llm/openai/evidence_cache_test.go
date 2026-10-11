package openai

import (
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
	"testing"
)

func TestEvidenceBreakpointPreservesTextAndStopsBeforeJudgment(t *testing.T) {
	head := "A\nB\nprevious judgment\nC\n"
	tail := "\ncurrent snapshot and judging instructions"
	req := port.ChatRequest{Model: "test", CachePrefix: head, Messages: []session.Message{{Role: session.RoleUser, Parts: []session.Part{{Kind: session.PartText, Text: head + tail}}}}}
	cached := buildRequest(req, false, true, "", 0, Sampling{}, nil)
	blocks, ok := cached.Messages[0].Content.([]textBlock)
	if !ok || len(blocks) != 2 || blocks[0].Text != head || blocks[0].CacheControl == nil || blocks[1].CacheControl != nil || blocks[0].Text+blocks[1].Text != head+tail {
		t.Fatalf("wrong breakpoint: %#v", cached.Messages[0].Content)
	}
	plain := buildRequest(req, false, false, "", 0, Sampling{}, nil)
	if plain.Messages[0].Content != head+tail {
		t.Fatal("cache rejection fallback changed content")
	}
	req.CachePrefix = "different head"
	mismatch := buildRequest(req, false, true, "", 0, Sampling{}, nil)
	if mismatch.Messages[0].Content != head+tail {
		t.Fatal("mismatched prefix must not mark unrelated text")
	}
}
