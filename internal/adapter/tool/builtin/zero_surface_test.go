package builtin

import "testing"

// IsCommentStart is one rule shared with the app layer's redirect scan: a `#` opens a comment
// after anything that ends a word or opens a command position, and never mid-word.
func TestIsCommentStartIsTheOneRule(t *testing.T) {
	for _, b := range []byte{' ', '\t', '\n', ';', '|', '&', '('} {
		if !IsCommentStart(b) {
			t.Errorf("after %q a # opens a comment", b)
		}
	}
	for _, b := range []byte{'e', '1', '$', '{', ')', '"'} {
		if IsCommentStart(b) {
			t.Errorf("after %q a # is part of a word (file#1, ${x#y})", b)
		}
	}
}

// KnownNames is the whole built-in vocabulary, headless-only tools included — what the
// contradiction pass compares a model's tool talk against.
func TestKnownNamesSpansTheVocabulary(t *testing.T) {
	names := KnownNames()
	if len(names) < 20 {
		t.Fatalf("the vocabulary is the whole built-in set, got %d names", len(names))
	}
	for _, must := range []string{"bash", "read", "write", "route_interjection", "council"} {
		if !names[must] {
			t.Errorf("%q belongs to the vocabulary", must)
		}
	}
	if names["mcp__x__y"] {
		t.Error("the vocabulary is the built-ins, not whatever attached today")
	}
}
