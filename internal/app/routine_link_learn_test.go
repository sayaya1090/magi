package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	luaplugin "github.com/sayaya1090/magi/internal/adapter/plugin/lua"
	"github.com/sayaya1090/magi/internal/port"
)

// newRoutineRigWithHistory is the routine rig with a fixed record of past tool calls behind
// magi.recent_tool_steps.
func newRoutineRigWithHistory(t *testing.T, dataDir string, past []port.SessionSteps, an luaplugin.Analyzer) *routineRig {
	t.Helper()
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "ask", Interactive: true, AnswerWait: 5 * time.Second})
	ctx := context.Background()
	s := a.sessionInfo(ctx, sid)
	logs := &logBuf{}
	recent := func(context.Context, string, int) ([]port.SessionSteps, error) { return past, nil }
	host := luaplugin.NewHostWithConfig(luaplugin.HostConfig{ToolSink: a.tools.(luaplugin.ToolSink), DataDir: dataDir,
		Runtime: luaplugin.RuntimeInfo{Workdir: s.Workdir}, Logf: logs.add, ContextReg: a, Notify: a.PluginNote, RecentSteps: recent, Analyzer: an})
	if _, err := host.Load(ctx, filepath.Join("..", "..", "plugins", "dummy-plug")); err != nil {
		t.Fatal(err)
	}
	r := &routineRig{a: a, sid: sid, s: s, host: host, logs: logs}
	r.write(t, routineFile)
	return r
}

// One turn: a listing, then a command over a value the model read off the listing's first line.
func linkTurn(turn int, value string) []port.ChildStep {
	args := func(cmd string) json.RawMessage {
		raw, _ := json.Marshal(map[string]string{"command": cmd})
		return raw
	}
	return []port.ChildStep{
		{Name: "bash", ResponseID: "one", CallOrder: 1, ResultOrder: 2, Args: args("git log --oneline -3"), Output: value + " fix the parser\n1234567 an older change\n", Turn: turn},
		{Name: "bash", ResponseID: "two", CallOrder: 3, ResultOrder: 4, Args: args("git show " + value + " --stat"), Output: " a.go | 2 +-\n", Turn: turn},
	}
}

// A value the model already had (it searched for it first) did not come from the listing.
func searchedFirst(turn int, value string) []port.ChildStep {
	args, _ := json.Marshal(map[string]string{"pattern": value})
	return append([]port.ChildStep{{Name: "grep", Args: args, Output: "x.go:1: " + value + "\n", Turn: turn}}, linkTurn(turn, value)...)
}

// The idle review reads past turns and learns where the model took an argument from: a value on a
// fixed spot of one call's result that the next call used. Seen twice with different values, it
// becomes a two-step routine that saves the model response in between. Observed values are not kept.
func TestIdleLearnsAnArgumentTakenFromAPreviousResult(t *testing.T) {
	for _, tc := range []struct {
		name  string
		past  []port.SessionSteps
		saved bool
	}{
		{"repeated", []port.SessionSteps{
			{Session: "s_b", Steps: linkTurn(4, "def5678a")},
			{Session: "s_a", Steps: linkTurn(2, "abc1234f")},
		}, true},
		{"value-already-in-arguments", []port.SessionSteps{
			{Session: "s_b", Steps: searchedFirst(4, "def5678a")},
			{Session: "s_a", Steps: searchedFirst(2, "abc1234f")},
		}, false},
		{"once", []port.SessionSteps{{Session: "s_a", Steps: linkTurn(2, "abc1234f")}}, false},
		{"same-value", []port.SessionSteps{
			{Session: "s_b", Steps: linkTurn(4, "abc1234f")},
			{Session: "s_a", Steps: linkTurn(2, "abc1234f")},
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := t.TempDir()
			model := &fakeAnalyzer{err: errors.New("not expected")}
			r := newRoutineRigWithHistory(t, data, tc.past, model)
			defer func() {
				if model.calls != 0 {
					t.Errorf("a fixed spot fits; the model must not be asked (%d calls)", model.calls)
				}
			}()
			r.fire("idle")
			_ = filepath.Walk(data, func(path string, info os.FileInfo, err error) error {
				if err != nil || info.IsDir() {
					return nil
				}
				if raw, _ := os.ReadFile(path); strings.Contains(string(raw), "abc1234f") || strings.Contains(string(raw), "def5678a") {
					t.Fatalf("plug-in data keeps an observed value: %s", path)
				}
				return nil
			})
			files, _ := filepath.Glob(filepath.Join(r.s.Workdir, ".magi/routines/link-*/routine.md"))
			if !tc.saved {
				if len(files) != 0 {
					t.Fatalf("saved without two different values: %v", files)
				}
				return
			}
			if len(files) != 1 {
				t.Fatalf("want one learned routine, got %v\nlog: %s", files, r.logs.text())
			}
			body, _ := os.ReadFile(files[0])
			for _, seen := range []string{"abc1234f", "def5678a"} {
				if strings.Contains(string(body), seen) {
					t.Fatalf("the routine keeps an observed value %q:\n%s", seen, body)
				}
			}
			if got := routineIndex(t, r, "show the latest change"); !strings.Contains(got, "saves 1 model call per run") {
				t.Fatalf("the learned routine should save one model call: %s", got)
			}
		})
	}
}

// fakeAnalyzer answers magi.analyze with a fixed reply or error and counts the calls.
type fakeAnalyzer struct {
	calls  int
	answer string
	err    error
}

func (f *fakeAnalyzer) Analyze(context.Context, string, string, string) (string, error) {
	f.calls++
	return f.answer, f.err
}

// The value sits on line eight after an arrow: no fixed spot of the deterministic rules fits.
func deepTurn(turn int, value string) []port.ChildStep {
	steps := linkTurn(turn, value)
	steps[0].Output = "x\nx\nx\nx\nx\nx\nx\n  -> " + value + "\n"
	return steps
}

// When the same link repeats with different values but no fixed spot fits, the idle review asks the
// model for an extraction rule. A rule is kept only if it yields each observed value and the routine
// replays; a refusal or a wrong rule saves nothing. Observed values are still never stored.
func TestIdleAsksTheModelWhenNoFixedSpotFits(t *testing.T) {
	past := []port.SessionSteps{
		{Session: "s_b", Steps: deepTurn(4, "def5678a")},
		{Session: "s_a", Steps: deepTurn(2, "abc1234f")},
	}
	for _, tc := range []struct {
		name  string
		model *fakeAnalyzer
		saved bool
	}{
		{"accepted", &fakeAnalyzer{answer: "Here it is: {\"pattern\": \"%-> (%S+)\"}"}, true},
		{"refused", &fakeAnalyzer{err: errors.New("declined")}, false},
		{"wrong-rule", &fakeAnalyzer{answer: `{"line": 1, "field": 1}`}, false},
		// Reads the last occurrence (and would replay) but not the other one: an overfit rule is refused.
		{"overfit", &fakeAnalyzer{answer: `{"pattern": "(abc1234f)"}`}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := t.TempDir()
			r := newRoutineRigWithHistory(t, data, past, tc.model)
			r.fire("idle")
			if tc.model.calls != 1 {
				t.Fatalf("want one model call, got %d", tc.model.calls)
			}
			files, _ := filepath.Glob(filepath.Join(r.s.Workdir, ".magi/routines/link-*/routine.md"))
			if !tc.saved {
				if len(files) != 0 {
					t.Fatalf("saved from a refused or wrong rule: %v", files)
				}
				return
			}
			if len(files) != 1 {
				t.Fatalf("want one routine from the model's rule, got %v\nlog: %s", files, r.logs.text())
			}
			body, _ := os.ReadFile(files[0])
			if strings.Contains(string(body), "abc1234f") || strings.Contains(string(body), "def5678a") {
				t.Fatalf("the routine keeps an observed value:\n%s", body)
			}
			_ = filepath.Walk(data, func(path string, info os.FileInfo, err error) error {
				if err != nil || info.IsDir() {
					return nil
				}
				if raw, _ := os.ReadFile(path); strings.Contains(string(raw), "abc1234f") || strings.Contains(string(raw), "def5678a") {
					t.Fatalf("plug-in data keeps an observed value: %s", path)
				}
				return nil
			})
		})
	}
}

func TestLinkLearningRequiresCausalResponseAndReplay(t *testing.T) {
	for _, test := range []string{"same response", "late output", "unknown response", "repeated anchor", "shell syntax position"} {
		t.Run(test, func(t *testing.T) {
			var past []port.SessionSteps
			for _, v := range []string{"alpha123", "bravo456"} {
				s := linkTurn(1, v)
				switch test {
				case "same response":
					s[1].ResponseID = s[0].ResponseID
				case "late output":
					s[0].ResultOrder = s[1].CallOrder + 1
				case "unknown response":
					s[0].ResponseID = ""
				case "repeated anchor":
					s[0].Output = "id other\na\nb\nc\nd\ne\nid " + v
				case "shell syntax position":
					s[1].Args, _ = json.Marshal(map[string]string{"command": v + " --stat"})
				}
				past = append(past, port.SessionSteps{Session: v, Steps: s})
			}
			r := newRoutineRigWithHistory(t, t.TempDir(), past, nil)
			r.fire("idle")
			files, _ := filepath.Glob(filepath.Join(r.s.Workdir, ".magi/routines/link-*/routine.md"))
			if len(files) > 0 {
				t.Fatalf("unexpected learned link %v", files)
			}
		})
	}
}

func TestLegacyResultLinkRequiresRelearningBeforeExecution(t *testing.T) {
	r := newRoutineRig(t, t.TempDir(), routineFile)
	const name = "link-aaaaaaaabbbb"
	body := strings.Replace(string(routineDoc("obsolete link")), "kind: routine", "kind: routine\nstatus: learned-write\nsource: result value reused aaaaaaaabbbb", 1)
	r.put(t, filepath.Join(".magi", "routines", name, "routine.md"), []byte(body))
	out, asks, failed := r.call(t, "legacy-link", "run_routine", map[string]any{"name": name}, "allow")
	if !failed || len(asks) != 0 || !strings.Contains(out, "needs fresh evidence") {
		t.Fatalf("legacy link reached execution: %q %+v", out, asks)
	}
}
