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
	}, true, false, "", 0, Sampling{}))
	if !strings.Contains(string(body), `"temperature":0`) {
		t.Fatalf("temperature 0 was dropped from the request body: %s", body)
	}
}

// No pin means no field: the provider's own default must stay in force, so an ordinary agent turn
// is unaffected by this plumbing.
func TestNoTemperatureParamOmitsTheField(t *testing.T) {
	body, _ := json.Marshal(buildRequest(port.ChatRequest{Model: "m"}, true, false, "", 0, Sampling{}))
	if strings.Contains(string(body), "temperature") {
		t.Fatalf("an unpinned request must not carry a temperature: %s", body)
	}
}

func TestFloatParamAcceptsTheShapesAParamMapCarries(t *testing.T) {
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
		got := floatParam(map[string]any{"temperature": c.in}, "temperature")
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
func TestFloatParamIgnoresWhatItCannotRead(t *testing.T) {
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
		if got := floatParam(c.in, "temperature"); got != nil {
			t.Fatalf("%s: expected no temperature, got %v", c.name, *got)
		}
	}
}

func ptrF(f float64) *float64 { return &f }
func ptrI(n int) *int         { return &n }

// [sampling] reaches every ordinary request. top_k rides along only when set, because it is not
// part of the OpenAI schema and a strict backend rejects fields it does not know.
func TestConfiguredSamplingReachesTheWire(t *testing.T) {
	body, _ := json.Marshal(buildRequest(port.ChatRequest{Model: "m"}, true, false, "", 0,
		Sampling{Temperature: ptrF(0.2), TopP: ptrF(0.8), TopK: ptrI(20)}))
	for _, want := range []string{`"temperature":0.2`, `"top_p":0.8`, `"top_k":20`} {
		if !strings.Contains(string(body), want) {
			t.Errorf("configured sampling lost %s from the body: %s", want, body)
		}
	}
}

func TestUnsetSamplingFieldsAreOmitted(t *testing.T) {
	body, _ := json.Marshal(buildRequest(port.ChatRequest{Model: "m"}, true, false, "", 0,
		Sampling{Temperature: ptrF(0.2)}))
	for _, unwanted := range []string{"top_p", "top_k"} {
		if strings.Contains(string(body), unwanted) {
			t.Errorf("an unconfigured %s must not be sent: %s", unwanted, body)
		}
	}
}

// The council pins temperature 0 on its member polls so a deliberation is reproducible. A
// configured default must not quietly un-pin that — and it must not clobber the OTHER fields
// either: a call pinning only the temperature keeps the configured top_p.
func TestAPerRequestPinOutranksTheConfiguredDefaultFieldByField(t *testing.T) {
	body, _ := json.Marshal(buildRequest(port.ChatRequest{
		Model:  "m",
		Params: map[string]any{"temperature": 0.0},
	}, true, false, "", 0, Sampling{Temperature: ptrF(0.9), TopP: ptrF(0.8)}))
	if !strings.Contains(string(body), `"temperature":0`) || strings.Contains(string(body), `"temperature":0.9`) {
		t.Errorf("the pinned temperature 0 must win over the configured 0.9: %s", body)
	}
	if !strings.Contains(string(body), `"top_p":0.8`) {
		t.Errorf("pinning the temperature must leave the configured top_p in place: %s", body)
	}
}

// top_k is a count. A fraction is a mistake, and truncating 0.5 to 0 would forbid every token.
func TestIntParamRejectsAFractionRatherThanTruncating(t *testing.T) {
	if got := intParam(map[string]any{"top_k": 0.5}, "top_k"); got != nil {
		t.Fatalf("a fractional top_k must be ignored, got %d", *got)
	}
	if got := intParam(map[string]any{"top_k": 20.0}, "top_k"); got == nil || *got != 20 {
		t.Fatalf("a whole-numbered float top_k must be read: %v", got)
	}
}

func TestEnvSamplingOverridesAndIgnoresJunk(t *testing.T) {
	t.Setenv("MAGI_TEMPERATURE", "0.3")
	t.Setenv("MAGI_TOP_P", "0.75")
	t.Setenv("MAGI_TOP_K", "not-a-number")
	s := Sampling{Temperature: ptrF(1), TopP: ptrF(0.95), TopK: ptrI(40)}
	envSampling(&s)
	if *s.Temperature != 0.3 || *s.TopP != 0.75 {
		t.Errorf("env must override the configured values, got %v/%v", *s.Temperature, *s.TopP)
	}
	if s.TopK == nil || *s.TopK != 40 {
		t.Errorf("an unparseable MAGI_TOP_K must leave the configured value alone, got %v", s.TopK)
	}
}

// An env var that is absent must not erase a configured value — the override is per-field.
func TestEnvSamplingLeavesUnsetVarsAlone(t *testing.T) {
	t.Setenv("MAGI_TEMPERATURE", "")
	t.Setenv("MAGI_TOP_P", "")
	t.Setenv("MAGI_TOP_K", "")
	s := Sampling{Temperature: ptrF(0.2)}
	envSampling(&s)
	if s.Temperature == nil || *s.Temperature != 0.2 {
		t.Fatalf("an unset env var must not clear the configured temperature, got %v", s.Temperature)
	}
}
