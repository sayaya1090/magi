package app

import (
	"context"
	"encoding/json"
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
func newRoutineRigWithHistory(t *testing.T, dataDir string, past []port.SessionSteps) *routineRig {
	t.Helper()
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "ask", Interactive: true, AnswerWait: 5 * time.Second})
	ctx := context.Background()
	s := a.sessionInfo(ctx, sid)
	logs := &logBuf{}
	recent := func(context.Context, string, int) ([]port.SessionSteps, error) { return past, nil }
	host := luaplugin.NewHostWithConfig(luaplugin.HostConfig{ToolSink: a.tools.(luaplugin.ToolSink), DataDir: dataDir,
		Runtime: luaplugin.RuntimeInfo{Workdir: s.Workdir}, Logf: logs.add, ContextReg: a, Notify: a.PluginNote, RecentSteps: recent})
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
		{Name: "bash", Args: args("git log --oneline -3"), Output: value + " fix the parser\n1234567 an older change\n", Turn: turn},
		{Name: "bash", Args: args("git show " + value + " --stat"), Output: " a.go | 2 +-\n", Turn: turn},
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
			r := newRoutineRigWithHistory(t, data, tc.past)
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
