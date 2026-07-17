package builtin

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// flexInt accepts every integer shape weak models actually emit; junk falls back
// to 0 (unset/default) instead of failing the whole tool call.
func TestFlexIntShapes(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int
	}{
		{`300`, 300},
		{`300.0`, 300},
		{`"300"`, 300},
		{`"300.000000"`, 300},
		{`"300s"`, 300},
		{`"300 s"`, 300},
		{`"120sec"`, 120},
		{`"garbage"`, 0},
		{`null`, 0},
		{`""`, 0},
		// Edges: scientific notation, negatives, out-of-range clamp (float→int of an
		// out-of-range value is implementation-defined in Go — the clamp pins it).
		{`3e2`, 300},
		{`"-5"`, -5},
		{`"  300  "`, 300},
		{`1e20`, 1 << 31},
		{`-1e20`, -(1 << 31)},
		{`[300]`, 0},
		{`{}`, 0},
	} {
		var v flexInt
		if err := json.Unmarshal([]byte(tc.in), &v); err != nil {
			t.Fatalf("flexInt(%s) must never error, got %v", tc.in, err)
		}
		if int(v) != tc.want {
			t.Errorf("flexInt(%s) = %d, want %d", tc.in, v, tc.want)
		}
	}
}

// flexBool accepts the boolean shapes weak models emit; junk means "not set".
func TestFlexBoolShapes(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want bool
	}{
		{`true`, true}, {`"true"`, true}, {`"True"`, true}, {`"yes"`, true},
		{`"on"`, true}, {`1`, true}, {`"1"`, true},
		{`false`, false}, {`"false"`, false}, {`0`, false}, {`"0"`, false},
		{`null`, false}, {`"garbage"`, false}, {`""`, false},
	} {
		var v flexBool
		if err := json.Unmarshal([]byte(tc.in), &v); err != nil {
			t.Fatalf("flexBool(%s) must never error, got %v", tc.in, err)
		}
		if bool(v) != tc.want {
			t.Errorf("flexBool(%s) = %v, want %v", tc.in, v, tc.want)
		}
	}
}

// A string-typed replaceAll must not cost the edit call: "true" behaves as true.
func TestEditStringReplaceAllAccepted(t *testing.T) {
	env := port.ToolEnv{Workdir: t.TempDir()}
	writeFile(env.Workdir, "g.txt", "x x x\n")
	r, _ := Edit{}.Execute(context.Background(),
		json.RawMessage(`{"path":"g.txt","old":"x","new":"y","replaceAll":"true"}`), env)
	if r.IsError {
		t.Fatalf("edit with string replaceAll must succeed: %s", resultText(t, r))
	}
	b, _ := os.ReadFile(env.Workdir + "/g.txt")
	if string(b) != "y y y\n" {
		t.Errorf("replaceAll:\"true\" should replace all, got %q", b)
	}
}

// The exact failure shapes observed in the circuit-fibsqrt run: a bash call with
// timeout:"300.000000" and a read with offset:"315.0"/limit:"50.0" were rejected
// whole ("cannot unmarshal string into … type int") — the model then abandoned the
// action instead of fixing the type. Both must now succeed.
func TestObservedStringNumberArgsAccepted(t *testing.T) {
	env := port.ToolEnv{Workdir: t.TempDir()}

	r, _ := Bash{}.Execute(context.Background(),
		json.RawMessage(`{"command":"echo ok","timeout":"300.000000"}`), env)
	if r.IsError {
		t.Fatalf("bash with string timeout must run: %s", resultText(t, r))
	}

	writeFile(env.Workdir, "f.txt", "l1\nl2\nl3\nl4\n")
	r, _ = Read{}.Execute(context.Background(),
		json.RawMessage(`{"path":"f.txt","offset":"2.0","limit":"2.0"}`), env)
	out := resultText(t, r)
	if r.IsError {
		t.Fatalf("read with string offset/limit must succeed: %s", out)
	}
	if !strings.Contains(out, "l2") || !strings.Contains(out, "l3") || strings.Contains(out, "l4") {
		t.Errorf("offset 2 limit 2 should show l2..l3 only, got %q", out)
	}
}
