package app

import (
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
)

const leakedKey = "sk-proj-ABCDEF0123456789abcdef"

// What a person watching a retry sees: why, which attempt of how many, how long, and how to stop it
// — and never a credential a backend echoed back in its refusal.
func TestRetryNoteSaysWhyHowLongAndHowToStop(t *testing.T) {
	fastTransient(t)
	evs := runFlaky(t, &flakyLLM{failN: 2, msg: "llm: rate limited (status 429): slow down, key " + leakedKey + " Authorization: Bearer tok_SECRETSECRETSECRET"})
	var notes []string
	for _, e := range evs {
		if e.Type == event.TypeToolProgress && strings.Contains(string(e.Data), "retrying in") {
			notes = append(notes, string(e.Data))
		}
	}
	if len(notes) != 2 {
		t.Fatalf("want 2 retry notes, got %d: %v", len(notes), notes)
	}
	for i, n := range notes {
		for _, want := range []string{"backend rate_limit", "retrying in ", []string{"attempt 1 of 6", "attempt 2 of 6"}[i], "stop the turn to cancel"} {
			if !strings.Contains(n, want) {
				t.Errorf("note %d lacks %q: %s", i+1, want, n)
			}
		}
		if strings.Contains(n, leakedKey) || strings.Contains(n, "SECRETSECRET") {
			t.Errorf("note %d carries a credential: %s", i+1, n)
		}
	}
}

// A refusal that ends the step is recorded in the log; a credential in it must not be.
func TestRecordedBackendErrorCarriesNoCredential(t *testing.T) {
	fastTransient(t)
	evs := runFlaky(t, &flakyLLM{failN: 99, msg: "llm: unauthorized (status 401): Incorrect API key provided: " + leakedKey})
	found := false
	for _, e := range evs {
		if e.Type == event.TypeError {
			found = true
			if strings.Contains(string(e.Data), leakedKey) {
				t.Errorf("error event carries the key: %s", e.Data)
			}
		}
	}
	if !found {
		t.Fatal("no error recorded")
	}
}
