package embed

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestAShortTextIsOneChunkUnchanged(t *testing.T) {
	for _, s := range []string{"one line", strings.Repeat("가", ChunkRunes)} {
		got := Chunks(s)
		if len(got) != 1 || got[0] != s {
			t.Fatalf("a text of %d runes should come back whole, got %d chunks", utf8.RuneCountInString(s), len(got))
		}
	}
	if Chunks("") != nil {
		t.Fatal("empty text should give no chunks")
	}
}

// Every chunk fits the limit, the chunks start at the text's start and end at its end, and each one
// begins inside the previous one, so no character is skipped.
func TestChunksCoverTheWholeTextWithinTheLimit(t *testing.T) {
	text := strings.Repeat("한국어 문장과 English words 섞인 줄. ", 300) // well over ChunkRunes, no paragraph breaks
	got := Chunks(text)
	if len(got) < 3 {
		t.Fatalf("expected several chunks, got %d", len(got))
	}
	if !strings.HasPrefix(text, got[0]) || !strings.HasSuffix(text, got[len(got)-1]) {
		t.Fatal("chunks do not start at the beginning and end at the end")
	}
	pos := 0
	for i, c := range got {
		if n := utf8.RuneCountInString(c); n > ChunkRunes {
			t.Fatalf("chunk %d has %d runes, over %d", i, n, ChunkRunes)
		}
		at := strings.Index(text[pos:], c)
		if at < 0 {
			t.Fatalf("chunk %d is not a contiguous passage at or after the previous one", i)
		}
		if i > 0 && at > len(got[i-1]) {
			t.Fatalf("gap between chunk %d and %d", i-1, i)
		}
		pos += at
	}
}

func TestAParagraphBreakPastTheMinimumIsTheCut(t *testing.T) {
	first := strings.Repeat("a", 900)
	text := first + "\n\n" + strings.Repeat("b", 900)
	got := Chunks(text)
	if got[0] != first {
		t.Fatalf("first chunk should end at the paragraph break (900 runes), got %d runes", utf8.RuneCountInString(got[0]))
	}
}
