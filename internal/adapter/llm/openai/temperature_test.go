package openai

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// An explicit temperature 0 is the pin that matters most — a structured-JSON call asking for
// reproducibility — and it is also the value a naive `omitempty` float would erase. It must reach
// the wire as `"temperature":0`.
func TestPinnedZeroTemperatureReachesTheWire(t *testing.T) {
	body, _ := json.Marshal(buildRequest(port.ChatRequest{
		Model:  "m",
		Params: map[string]any{"temperature": 0.0},
	}, true, false, "", 0))
	if !strings.Contains(string(body), `"temperature":0`) {
		t.Fatalf("temperature 0 was dropped from the request body: %s", body)
	}
}

// No pin means no field: the provider's own default must stay in force, so an ordinary agent turn
// is unaffected by this plumbing.
func TestNoTemperatureParamOmitsTheField(t *testing.T) {
	body, _ := json.Marshal(buildRequest(port.ChatRequest{Model: "m"}, true, false, "", 0))
	if strings.Contains(string(body), "temperature") {
		t.Fatalf("an unpinned request must not carry a temperature: %s", body)
	}
}

func TestTemperatureOfAcceptsTheShapesAParamMapCarries(t *testing.T) {
	for _, c := range []struct {
		name string
		in   any
		want float64
	}{
		{"float64", 0.7, 0.7},
		{"float32", float32(0.5), 0.5},
		{"int", 1, 1},
		{"int64", int64(2), 2},
		{"json.Number", json.Number("0.25"), 0.25},
	} {
		got := temperatureOf(map[string]any{"temperature": c.in})
		if got == nil {
			t.Fatalf("%s: a usable temperature was ignored", c.name)
		}
		if *got != c.want {
			t.Fatalf("%s: got %v, want %v", c.name, *got, c.want)
		}
	}
}

// An unusable value is IGNORED, never coerced: silently sending 0 for a typo would re-pin the
// model to a setting the caller never asked for, which is worse than leaving the default alone.
func TestTemperatureOfIgnoresWhatItCannotRead(t *testing.T) {
	for _, c := range []struct {
		name string
		in   map[string]any
	}{
		{"absent", map[string]any{}},
		{"nil map", nil},
		{"string", map[string]any{"temperature": "0.7"}},
		{"bool", map[string]any{"temperature": true}},
		{"negative", map[string]any{"temperature": -1.0}},
		{"unparseable number", map[string]any{"temperature": json.Number("abc")}},
	} {
		if got := temperatureOf(c.in); got != nil {
			t.Fatalf("%s: expected no temperature, got %v", c.name, *got)
		}
	}
}
