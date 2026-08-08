package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// Three spellings of one word are one label.
//
// The point of a label is that a second piece of work can carry the same one. "Billing", "billing "
// and "BILLING" on three cards is three labels that label nothing, and it is the failure a
// free-form vocabulary invites — so it is closed here rather than left to the model's consistency.
func TestOneWordIsOneLabelHoweverItWasTyped(t *testing.T) {
	var got []string
	env := port.ToolEnv{SetLabels: func(ls []string) { got = ls }}
	res, err := Label{}.Execute(context.Background(),
		json.RawMessage(`{"labels":["Billing","  billing ","BILLING","flaky   test",""]}`), env)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("refused: %s", res.Content)
	}
	if strings.Join(got, "|") != "billing|flaky test" {
		t.Errorf("kept %v — one word however typed, and inner runs of spaces collapse", got)
	}
	// The answer names them back, which is how the next call has the vocabulary in front of it.
	if !strings.Contains(string(res.Content), "billing") {
		t.Errorf("the answer does not say what was recorded: %s", res.Content)
	}
}

// A cap, and clearing.
func TestLabelsAreCappedAndCanBeCleared(t *testing.T) {
	var got []string
	env := port.ToolEnv{SetLabels: func(ls []string) { got = ls }}
	if _, err := (Label{}).Execute(context.Background(),
		json.RawMessage(`{"labels":["a1","b2","c3","d4","e5","f6","g7"]}`), env); err != nil {
		t.Fatal(err)
	}
	if len(got) != most {
		t.Errorf("%d labels kept; a card carrying a dozen chips is one nobody reads", len(got))
	}
	// The whole set each time, so an empty one is a clear rather than a no-op — otherwise there is
	// no way to take back a label that turned out to be wrong.
	if _, err := (Label{}).Execute(context.Background(), json.RawMessage(`{"labels":[]}`), env); err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("an empty set left %v behind; nothing could ever be unlabelled", got)
	}
}

// Without somewhere to put them it says so, rather than reporting success into a void.
func TestLabellingWhereItCannotBeRecordedIsRefused(t *testing.T) {
	res, err := Label{}.Execute(context.Background(), json.RawMessage(`{"labels":["x"]}`), port.ToolEnv{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Error("labelling with no sink reported success")
	}
}
