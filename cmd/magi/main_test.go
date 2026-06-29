package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/platform"
	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/config"
	"github.com/sayaya1090/magi/internal/core/command"
	corecouncil "github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// fakeHeadless is a canned headlessApp: Subscribe replays a fixed event slice
// (already closed) and Submit records the prompt, so runHeadless can be driven
// without a real app/LLM.
type fakeHeadless struct {
	events    []event.Event
	subErr    error
	submitErr error
	submitted *command.SubmitPrompt
}

func (f *fakeHeadless) Subscribe(_ context.Context, _ session.SessionID, _ int64) (<-chan event.Event, func(), error) {
	if f.subErr != nil {
		return nil, nil, f.subErr
	}
	ch := make(chan event.Event, len(f.events))
	for _, e := range f.events {
		ch <- e
	}
	close(ch)
	return ch, func() {}, nil
}

func (f *fakeHeadless) Submit(_ context.Context, c command.SubmitPrompt) error {
	if f.submitErr != nil {
		return f.submitErr
	}
	f.submitted = &c
	return nil
}

func partEvent(t *testing.T, p session.Part) event.Event {
	t.Helper()
	b, err := json.Marshal(event.PartAppendedData{Part: p})
	if err != nil {
		t.Fatal(err)
	}
	return event.Event{Type: event.TypePartAppended, Data: b}
}

func errEvent(t *testing.T, msg string) event.Event {
	t.Helper()
	b, err := json.Marshal(event.ErrorData{Message: msg})
	if err != nil {
		t.Fatal(err)
	}
	return event.Event{Type: event.TypeError, Data: b}
}

// runHeadless in text mode renders each part to out, submits the prompt, and exits
// 0 at TurnFinished.
func TestRunHeadlessText(t *testing.T) {
	f := &fakeHeadless{events: []event.Event{
		partEvent(t, session.Part{Kind: session.PartText, Text: "hello world"}),
		partEvent(t, session.Part{Kind: session.PartToolCall, ToolCall: &session.ToolCall{Name: "bash", Args: json.RawMessage(`{"cmd":"ls"}`)}}),
		partEvent(t, session.Part{Kind: session.PartToolResult, ToolResult: &session.ToolResult{Content: json.RawMessage(`"file.txt"`)}}),
		{Type: event.TypeTurnFinished},
	}}
	var out, errw bytes.Buffer
	exit := runHeadless(context.Background(), f, "sid", "do a thing", false, &out, &errw)
	if exit != 0 {
		t.Errorf("exit = %d, want 0", exit)
	}
	s := out.String()
	for _, want := range []string{"hello world", "⚙ bash", `{"cmd":"ls"}`, "✓", "file.txt"} {
		if !strings.Contains(s, want) {
			t.Errorf("text output missing %q in:\n%s", want, s)
		}
	}
	if f.submitted == nil || len(f.submitted.Parts) != 1 || f.submitted.Parts[0].Text != "do a thing" {
		t.Errorf("prompt not submitted as expected: %+v", f.submitted)
	}
	if errw.Len() != 0 {
		t.Errorf("unexpected stderr: %s", errw.String())
	}
}

// JSON mode emits one JSON object per event, each decodable back to an Event.
func TestRunHeadlessJSON(t *testing.T) {
	f := &fakeHeadless{events: []event.Event{
		partEvent(t, session.Part{Kind: session.PartText, Text: "hi"}),
		{Type: event.TypeTurnFinished},
	}}
	var out, errw bytes.Buffer
	if exit := runHeadless(context.Background(), f, "sid", "p", true, &out, &errw); exit != 0 {
		t.Fatalf("exit = %d, want 0", exit)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 JSON lines, got %d:\n%s", len(lines), out.String())
	}
	var e event.Event
	if err := json.Unmarshal([]byte(lines[0]), &e); err != nil {
		t.Errorf("first line not valid Event JSON: %v", err)
	}
}

// A turn Error event makes runHeadless exit 1 and routes the message to stderr.
func TestRunHeadlessError(t *testing.T) {
	f := &fakeHeadless{events: []event.Event{errEvent(t, "boom")}}
	var out, errw bytes.Buffer
	if exit := runHeadless(context.Background(), f, "sid", "p", false, &out, &errw); exit != 1 {
		t.Errorf("exit = %d, want 1 on error event", exit)
	}
	if !strings.Contains(errw.String(), "boom") {
		t.Errorf("error message not on stderr: %q", errw.String())
	}
}

// Subscribe/Submit failures abort with exit 1 before streaming.
func TestRunHeadlessSetupErrors(t *testing.T) {
	var out, errw bytes.Buffer
	if exit := runHeadless(context.Background(), &fakeHeadless{subErr: errors.New("nosub")}, "s", "p", false, &out, &errw); exit != 1 {
		t.Errorf("subscribe error exit = %d, want 1", exit)
	}
	if !strings.Contains(errw.String(), "subscribe") {
		t.Errorf("stderr missing subscribe error: %q", errw.String())
	}
	errw.Reset()
	if exit := runHeadless(context.Background(), &fakeHeadless{submitErr: errors.New("nosubmit")}, "s", "p", false, &out, &errw); exit != 1 {
		t.Errorf("submit error exit = %d, want 1", exit)
	}
	if !strings.Contains(errw.String(), "submit") {
		t.Errorf("stderr missing submit error: %q", errw.String())
	}
}

func TestResolvePrompt(t *testing.T) {
	// A literal flag value is used verbatim (stdin untouched).
	if got, err := resolvePrompt("hello", strings.NewReader("STDIN")); err != nil || got != "hello" {
		t.Errorf("resolvePrompt(literal) = %q, %v", got, err)
	}
	// "-" reads the full prompt from stdin.
	if got, err := resolvePrompt("-", strings.NewReader("from stdin\nline2")); err != nil || got != "from stdin\nline2" {
		t.Errorf("resolvePrompt(stdin) = %q, %v", got, err)
	}
	// A stdin read error propagates.
	if _, err := resolvePrompt("-", errReader{}); err == nil {
		t.Error("expected stdin read error to propagate")
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("read fail") }

// mergeStrMap overlays `over` onto `base` (over wins), used to merge a project
// config / hooks over the global one.
func TestMergeStrMap(t *testing.T) {
	base := map[string]string{"a": "1", "b": "2"}
	got := mergeStrMap(base, map[string]string{"b": "X", "c": "3"})
	if got["a"] != "1" || got["b"] != "X" || got["c"] != "3" {
		t.Errorf("merge = %v (over should win, base kept)", got)
	}
	// Empty override returns the base untouched.
	if got := mergeStrMap(map[string]string{"k": "v"}, nil); got["k"] != "v" || len(got) != 1 {
		t.Errorf("empty over should keep base: %v", got)
	}
	// nil base + override allocates a new map.
	if got := mergeStrMap(nil, map[string]string{"k": "v"}); got["k"] != "v" {
		t.Errorf("nil base + over = %v", got)
	}
}

// env returns the env var when set non-empty, else the default.
func TestEnv(t *testing.T) {
	t.Setenv("MAGI_TEST_ENV", "set-value")
	if got := env("MAGI_TEST_ENV", "def"); got != "set-value" {
		t.Errorf("env(set) = %q, want set-value", got)
	}
	if got := env("MAGI_TEST_UNSET_ENV", "def"); got != "def" {
		t.Errorf("env(unset) = %q, want def", got)
	}
	// An explicitly empty value is treated as unset (falls back to default).
	t.Setenv("MAGI_TEST_EMPTY_ENV", "")
	if got := env("MAGI_TEST_EMPTY_ENV", "def"); got != "def" {
		t.Errorf("env(empty) = %q, want def", got)
	}
}

// orStr returns the first arg when non-empty, else the second.
func TestOrStr(t *testing.T) {
	if got := orStr("a", "b"); got != "a" {
		t.Errorf("orStr(a,b) = %q, want a", got)
	}
	if got := orStr("", "b"); got != "b" {
		t.Errorf("orStr(empty,b) = %q, want b", got)
	}
	if got := orStr("", ""); got != "" {
		t.Errorf("orStr(empty,empty) = %q, want empty", got)
	}
}

// councilSignals puts the `verify` shorthand first (named "verify"), then any
// configured [[council.signal]] entries, skipping ones with an empty command.
func TestCouncilSignals(t *testing.T) {
	// No verify, no signals → empty.
	if got := councilSignals(config.CouncilConfig{}); len(got) != 0 {
		t.Errorf("empty config → %v, want none", got)
	}
	cc := config.CouncilConfig{
		Verify: "go test ./...",
		Signals: []config.CouncilSignalConfig{
			{Name: "lint", Command: "golangci-lint run"},
			{Name: "skipme", Command: ""}, // dropped: no command
		},
	}
	got := councilSignals(cc)
	if len(got) != 2 {
		t.Fatalf("got %d signals, want 2: %+v", len(got), got)
	}
	if got[0] != (app.CouncilSignalSpec{Name: "verify", Command: "go test ./..."}) {
		t.Errorf("verify shorthand not first: %+v", got[0])
	}
	if got[1] != (app.CouncilSignalSpec{Name: "lint", Command: "golangci-lint run"}) {
		t.Errorf("signal[1] = %+v, want lint", got[1])
	}
}

// toCouncilMembers returns nil for no members (app falls back to defaults), and
// otherwise inherits a profile's model only when the member pins no model.
func TestToCouncilMembers(t *testing.T) {
	if got := toCouncilMembers(nil, nil); got != nil {
		t.Errorf("no members → %v, want nil", got)
	}
	profiles := map[string]config.LLMProfile{"fast": {Model: "profile-model"}}
	ms := []config.CouncilMember{
		{Name: "Melchior", Lens: "correctness", Provider: "fast"},                    // inherits profile model
		{Name: "Balthasar", Lens: "verification", Model: "pinned", Provider: "fast"}, // keeps its own model
		{Name: "Casper", Lens: "completeness", Provider: "unknown"},                  // unknown profile → no model
		{Name: "Solo", Lens: "x", Weight: 2},                                         // no provider → unchanged
	}
	got := toCouncilMembers(ms, profiles)
	want := []corecouncil.Member{
		{Name: "Melchior", Lens: "correctness", Model: "profile-model", Provider: "fast"},
		{Name: "Balthasar", Lens: "verification", Model: "pinned", Provider: "fast"},
		{Name: "Casper", Lens: "completeness", Model: "", Provider: "unknown"},
		{Name: "Solo", Lens: "x", Weight: 2},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d members, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("member[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// toAppHooks copies each config hook's event/match/command into an app HookSpec.
func TestToAppHooks(t *testing.T) {
	if got := toAppHooks(nil); len(got) != 0 {
		t.Errorf("nil hooks → %v, want empty", got)
	}
	hs := []config.Hook{{Event: "PreToolUse", Match: "bash", Command: "echo hi"}}
	got := toAppHooks(hs)
	if len(got) != 1 || got[0] != (app.HookSpec{Event: "PreToolUse", Match: "bash", Command: "echo hi"}) {
		t.Errorf("toAppHooks = %+v", got)
	}
}

// pluginDirs lists the global config dir and the project-local .magi/plugins, and
// appends an explicit extra directory only when one is given.
func TestPluginDirs(t *testing.T) {
	plat := platform.New()
	got := pluginDirs(plat, "/work", "")
	if len(got) != 2 {
		t.Fatalf("no extra → %d dirs, want 2: %v", len(got), got)
	}
	if got[1] != filepath.Join("/work", ".magi", "plugins") {
		t.Errorf("project dir = %q", got[1])
	}
	if got[0] != filepath.Join(plat.ConfigDir(), "plugins") {
		t.Errorf("global dir = %q", got[0])
	}
	withExtra := pluginDirs(plat, "/work", "/extra/plugins")
	if len(withExtra) != 3 || withExtra[2] != "/extra/plugins" {
		t.Errorf("extra dir not appended: %v", withExtra)
	}
}

// The orchestrator system prompt must carry the anti-defeatism directives (install
// missing tools / fall back to a present runtime / actually do+verify) and stay
// cross-platform (no single package manager hardcoded). Read-only subagents must NOT
// get install guidance. Keep the substrings in sync with the prompt wording.
func TestSystemPromptPersistence(t *testing.T) {
	p := strings.ToLower(systemPrompt)

	// (a) key anti-defeatism directives present
	for _, want := range []string{
		"don't give up",                     // section intent
		"command -v",                        // investigate/detect before concluding
		"install",                           // install missing tools
		"actually do it",                    // do the task, don't only describe
		"exit code",                         // evidence-based verification
		"adapt to this environment",         // managed→direct fallback (no init assumed)
		"a clean exit message is not proof", // verify by real end state, not exit
	} {
		if !strings.Contains(p, want) {
			t.Errorf("systemPrompt missing persistence directive %q", want)
		}
	}

	// (b) cross-platform: offer >1 package manager (not single-PM-locked), incl. brew (macOS)
	pms := 0
	for _, pm := range []string{"apt", "dnf", "apk", "brew"} {
		if strings.Contains(p, pm) {
			pms++
		}
	}
	if pms < 2 {
		t.Errorf("systemPrompt names %d package managers; want >=2 to stay cross-platform", pms)
	}
	if !strings.Contains(p, "brew") {
		t.Error("systemPrompt should mention brew so macOS is covered (not Linux-only)")
	}

	// (c) read-only subagents (no bash) must NOT carry install-tool guidance
	for _, name := range []string{"explore", "locator", "analyst", "architect", "reviewer", "planner"} {
		if strings.Contains(strings.ToLower(defaultAgents()[name].System), "install") {
			t.Errorf("read-only agent %q should not have install-tool guidance", name)
		}
	}
}

func TestEnvDur(t *testing.T) {
	t.Setenv("MAGI_TEST_DUR", "2s")
	if d := envDur("MAGI_TEST_DUR", time.Second); d != 2*time.Second {
		t.Errorf("envDur parsed = %v, want 2s", d)
	}
	if d := envDur("MAGI_TEST_UNSET_DUR", 5*time.Second); d != 5*time.Second {
		t.Errorf("unset → default, got %v", d)
	}
	t.Setenv("MAGI_TEST_BAD_DUR", "notaduration")
	if d := envDur("MAGI_TEST_BAD_DUR", 7*time.Second); d != 7*time.Second {
		t.Errorf("invalid → default, got %v", d)
	}
}
