package companion

import (
	"context"

	"encoding/json"
	"github.com/sayaya1090/magi/internal/port"
	"strings"
	"testing"
)

// The companion tools' faces: names, words, schemas that parse — and Hand's description, which is
// not static text but the roster itself, saying honestly when there is nobody.
func portEnv() port.ToolEnv { return port.ToolEnv{} }

func TestCompanionToolFaces(t *testing.T) {
	for name, schema := range map[string]json.RawMessage{
		About{}.Name(): About{}.Schema(),
		List{}.Name():  List{}.Schema(),
		Hand{}.Name():  Hand{}.Schema(),
		Rate{}.Name():  Rate{}.Schema(),
	} {
		if strings.TrimSpace(name) == "" {
			t.Fatal("a nameless tool")
		}
		var v any
		if err := json.Unmarshal(schema, &v); err != nil {
			t.Errorf("%s: schema does not parse: %v", name, err)
		}
	}
	if d := (About{}).Description(); !strings.Contains(d, "hand") {
		t.Errorf("companion_can says when it is worth a call: %q", d)
	}
	if d := (Rate{}).Description(); !strings.Contains(d, "Judge the ANSWER") {
		t.Errorf("rate_handoff says what is being judged: %q", d)
	}
}

// Hand's description carries the roster: with nobody it says the call will refuse, and the past
// record appears only when there is a choice to inform.
func TestHandDescriptionIsTheRoster(t *testing.T) {
	if d := (Hand{}).Description(); !strings.Contains(d, "nobody else is running here") {
		t.Fatalf("an empty roster is said, not blanked: %q", d)
	}
	h := Hand{
		Roster: func() (string, int) { return "design (draws)\napi (serves)", 2 },
		Record: func() string { return "past: design was good once" },
	}
	d := h.Description()
	if !strings.Contains(d, "design (draws)") || !strings.Contains(d, "past: design was good once") {
		t.Fatalf("two to choose between: the roster AND the record inform the choice: %q", d)
	}
	one := Hand{
		Roster: func() (string, int) { return "design (draws)", 1 },
		Record: func() string { return "past: noise" },
	}
	if strings.Contains(one.Description(), "past: noise") {
		t.Fatal("with one companion the record informs nothing and must not weigh every prompt")
	}
}

// List without a reader says so instead of listing an empty machine.
func TestListWithoutAReaderSaysSo(t *testing.T) {
	res, err := (List{}).Execute(context.Background(), nil, portEnv())
	if err != nil || !res.IsError || !strings.Contains(string(res.Content), "no reader") {
		t.Fatalf("(%+v, %v)", res, err)
	}
}
