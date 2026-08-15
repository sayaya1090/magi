package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/platform"
	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/config"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// acCapLLM records the request it was handed and replies with a fixed line.
type acCapLLM struct {
	mu   sync.Mutex
	req  port.ChatRequest
	seen bool
	text string
}

func (c *acCapLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	c.mu.Lock()
	c.req = r
	c.seen = true
	c.mu.Unlock()
	ch := make(chan port.ProviderEvent, 2)
	ch <- port.ProviderEvent{Type: port.ProviderText, Text: c.text}
	ch <- port.ProviderEvent{Type: port.ProviderFinish}
	close(ch)
	return ch, nil
}

func (c *acCapLLM) called() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.seen }
func (c *acCapLLM) request() port.ChatRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.req
}

// acFailLLM fails the test the moment it is asked to generate. It stands in for the DEFAULT backend in
// the routing tests, so "a keystroke completion fell through to the main model" is a test failure
// rather than a silent bill.
type acFailLLM struct{ t *testing.T }

func (f acFailLLM) StreamChat(_ context.Context, _ port.ChatRequest) (<-chan port.ProviderEvent, error) {
	f.t.Errorf("the default provider was called: a completion routed to the wrong backend")
	ch := make(chan port.ProviderEvent)
	close(ch)
	return ch, nil
}

func completeApp(t *testing.T, def port.LLMProvider, ac config.AutocompleteConfig,
	providers map[string]port.LLMProvider, profileModels map[string]string) (*App, session.SessionID) {
	t.Helper()
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := closeAfter(t, New(store, def, builtin.Default(), bus.New(), nil, Config{
		Autocomplete:  ac,
		Providers:     providers,
		ProfileModels: profileModels,
	}))
	sid, err := a.CreateSession(context.Background(), command.CreateSession{Workdir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return a, sid
}

// With no code profile routed, code completion must produce nothing AND must not touch the default
// backend — the whole point of the split is that a keystroke never spends the main model.
func TestCompleteCodeSelfDisablesWithoutAProfile(t *testing.T) {
	a, sid := completeApp(t, acFailLLM{t}, config.AutocompleteConfig{ /* CodeProfile empty */ }, nil, nil)
	got, err := a.CompleteCode(context.Background(), sid, "x.go", "func main() {", "}")
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("a completion came back with no profile routed: %q", got)
	}
}

// The master switch off is the same: nothing, and no provider call, even if a profile is named.
func TestCompleteCodeSelfDisablesWhenTurnedOff(t *testing.T) {
	off := false
	cap := &acCapLLM{text: "SHOULD NOT RUN"}
	a, sid := completeApp(t, acFailLLM{t},
		config.AutocompleteConfig{Enabled: &off, CodeProfile: "fast"},
		map[string]port.LLMProvider{"fast": cap}, nil)
	got, err := a.CompleteCode(context.Background(), sid, "x.go", "a := ", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "" || cap.called() {
		t.Errorf("a disabled completion ran anyway: got=%q called=%v", got, cap.called())
	}
}

// Routed to a profile, the completion goes to THAT backend (not the default), carries the profile's
// model, and hands the model both sides of the cursor.
func TestCompleteCodeRoutesToItsProfile(t *testing.T) {
	cap := &acCapLLM{text: "\n\treturn 0\n"}
	a, sid := completeApp(t, acFailLLM{t},
		config.AutocompleteConfig{CodeProfile: "fast"},
		map[string]port.LLMProvider{"fast": cap},
		map[string]string{"fast": "tiny-fim"})
	got, err := a.CompleteCode(context.Background(), sid, "main.go", "func f() int {", "\n}")
	if err != nil {
		t.Fatal(err)
	}
	if !cap.called() {
		t.Fatal("the routed profile was never called")
	}
	if strings.TrimSpace(got) != "return 0" {
		t.Errorf("completion text not returned/trimmed: %q", got)
	}
	req := cap.request()
	if req.Model != "tiny-fim" {
		t.Errorf("the profile's model was not used: %q", req.Model)
	}
	body := ""
	if len(req.Messages) > 0 && len(req.Messages[0].Parts) > 0 {
		body = req.Messages[0].Parts[0].Text
	}
	if !strings.Contains(body, "<CURSOR>") || !strings.Contains(body, "func f() int {") {
		t.Errorf("the model was not shown the cursor and both sides:\n%s", body)
	}
}

// Cancellation is the design: the ctx passed by the caller reaches the provider, so the next
// keystroke's cancel drops the completion in flight. A provider that only unblocks on ctx.Done
// proves the ctx is actually threaded through — if complete() swallowed it, this would hang.
func TestCompleteCancellationReachesTheProvider(t *testing.T) {
	a, sid := completeApp(t, acBlockLLM{},
		config.AutocompleteConfig{CodeProfile: "slow"},
		map[string]port.LLMProvider{"slow": acBlockLLM{}}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_, _ = a.CompleteCode(ctx, sid, "x.go", "a", "")
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("completion did not return after its context was cancelled")
	}
}

// acBlockLLM never sends a token; it closes its stream only when the request context is cancelled.
type acBlockLLM struct{}

func (acBlockLLM) StreamChat(ctx context.Context, _ port.ChatRequest) (<-chan port.ProviderEvent, error) {
	ch := make(chan port.ProviderEvent)
	go func() {
		<-ctx.Done()
		close(ch)
	}()
	return ch, nil
}

// A commit-message draft carries the operator's own rules ([templates] commit) into the prompt when
// they wrote any. Remove the injection and this fails — the rules a project typed would never reach
// the model that writes its commits.
func TestDraftCommitInjectsTemplateRules(t *testing.T) {
	dir := gitRepo(t, []string{"commit", "--allow-empty", "-q", "-m", "init"})
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := acRun(dir, "git", "add", "a.txt"); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	store, _ := jsonl.New(t.TempDir())
	cap := &acCapLLM{text: "add a.txt"}
	const rule = "MAGI-HOUSE-RULE: layer the commits docs then core"
	a := closeAfter(t, New(store, cap, builtin.Default(), bus.New(), platform.New(), Config{
		Templates: config.TemplatesConfig{Commit: rule},
	}))
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: dir})
	if _, err := a.DraftCommit(context.Background(), sid, dir); err != nil {
		t.Fatal(err)
	}
	if !cap.called() {
		t.Fatal("the draft model was never called")
	}
	if !strings.Contains(cap.request().System, rule) {
		t.Errorf("the commit rule did not reach the prompt:\n%s", cap.request().System)
	}
}

// The file the console editor has open rides into the agent's per-turn context by default, is gone
// when [autocomplete] ambient is turned off, and is gone once the editor closes it (empty text).
func TestAmbientOpenFileEntersContext(t *testing.T) {
	a, sid := completeApp(t, completingLLM{}, config.AutocompleteConfig{}, nil, nil) // ambient default on
	a.SetOpenFile(sid, "/w/foo.go", "package main\n\nvar X = brokenHere")
	s := a.sessionInfo(context.Background(), sid)
	ctx := func(a *App) string {
		return a.volatileContext(context.Background(), s, AgentSpec{}, nil, nil, 1, 0, 0)
	}
	if vc := ctx(a); !strings.Contains(vc, "foo.go") || !strings.Contains(vc, "brokenHere") {
		t.Errorf("the open buffer did not enter the context:\n%s", vc)
	}
	// Turned off, it must not be injected.
	off := false
	a.cfg.Autocomplete.Ambient = &off
	if vc := ctx(a); strings.Contains(vc, "brokenHere") {
		t.Errorf("ambient off still injected the buffer:\n%s", vc)
	}
	// Back on, but the editor closed the file (empty text clears it).
	a.cfg.Autocomplete.Ambient = nil
	a.SetOpenFile(sid, "/w/foo.go", "")
	if vc := ctx(a); strings.Contains(vc, "brokenHere") {
		t.Errorf("a closed file still rode into the context:\n%s", vc)
	}
}

// Composer suggestion self-disables with no composer profile routed — no output, no model.
func TestSuggestPromptSelfDisablesWithoutAProfile(t *testing.T) {
	a, sid := completeApp(t, acFailLLM{t}, config.AutocompleteConfig{}, nil, nil)
	got, err := a.SuggestPrompt(context.Background(), sid, "fix the ")
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("a suggestion came back with no composer profile: %q", got)
	}
}

// Routed, the suggestion goes to the composer profile (not the default), carries this workspace's
// past prompts as few-shot, and includes what the person has typed so far.
func TestSuggestPromptLearnsFromPastPrompts(t *testing.T) {
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cap := &acCapLLM{text: "failing tests"}
	a := closeAfter(t, New(store, acFailLLM{t}, builtin.Default(), bus.New(), nil, Config{
		Autocomplete:  config.AutocompleteConfig{ComposerProfile: "smart"},
		Providers:     map[string]port.LLMProvider{"smart": cap},
		ProfileModels: map[string]string{"smart": "smart-model"},
	}))
	wd := t.TempDir()
	past, err := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})
	if err != nil {
		t.Fatal(err)
	}
	const prior = "run the flaky integration tests again"
	if err := a.SeedForTest(context.Background(), past, prior, "done"); err != nil {
		t.Fatal(err)
	}
	cur, err := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})
	if err != nil {
		t.Fatal(err)
	}
	out, err := a.SuggestPrompt(context.Background(), cur, "run the flaky ")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "failing tests" {
		t.Errorf("suggestion text not returned: %q", out)
	}
	if !cap.called() {
		t.Fatal("the composer profile was never called")
	}
	req := cap.request()
	if req.Model != "smart-model" {
		t.Errorf("composer profile's model not used: %q", req.Model)
	}
	body := req.Messages[0].Parts[0].Text
	if !strings.Contains(body, prior) {
		t.Errorf("this user's past prompt was not offered as few-shot:\n%s", body)
	}
	if !strings.Contains(body, "run the flaky ") {
		t.Errorf("what the user typed was not in the prompt:\n%s", body)
	}
}

func acRun(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}
