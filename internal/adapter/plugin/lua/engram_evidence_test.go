package lua

import (
	"encoding/json"
	"testing"
	"time"
)

func TestEngramKeepsTurnSuccessSeparateFromCausalAttribution(t *testing.T) {
	wd := t.TempDir()
	h := loadEngram(t, wd, &fakeAnalyzer{reply: `{"lesson":{"task":"parser repair","approach":"changed validation","change":"reject empty field","evidence":"reported parser check","attribution":"helped","outcome":"success","lesson":"empty fields need explicit handling"},"skill":null}`}, &syncLog{})
	h.FireEventWith("user_message", map[string]string{"session": "s", "text": "repair parser"})
	h.FireEventWith("turn_finished", map[string]string{"session": "s", "text": "reported parser check", "outcome": "verified"})
	h.DrainEvents(5 * time.Second)
	p := h.plugins["engram"]
	raw, _ := p.readStore()["learning_evidence_v1:"+wd].(string)
	var fields []map[string]any
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		t.Fatal(raw, err)
	}
	if len(fields) != 1 || fields[0]["attribution"] != "confounded" || fields[0]["host_outcome"] != "verified" || fields[0]["verified_application"] != false || fields[0]["change"] != "reject empty field" || fields[0]["source"] != "conversation_excerpt" {
		t.Fatalf("model promoted its own causal claim: %s", raw)
	}
}
