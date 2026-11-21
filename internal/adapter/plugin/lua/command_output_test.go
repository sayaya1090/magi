package lua

import (
	"context"
	"strings"
	"testing"
)

// A client other than the TUI has no stderr to read: what a command logged while it ran is its
// answer there. RunCommand hands those lines back, and only those — a line the plugin logged at load
// is not part of any command's answer. A returned string is still the command's failure.
func TestRunCommandReturnsWhatTheCommandLogged(t *testing.T) {
	dir := writePlugin(t,
		`name="cap"`+"\n"+`capabilities=["command"]`,
		`magi.log("loaded")
		magi.register_command{ name="say", description="say it", execute = function(a) magi.log("hello " .. (a[1] or "")); magi.log("world") end }
		magi.register_command{ name="bad", execute = function() magi.log("tried"); return "no such run" end }`,
	)
	h := hostWith(nil, nil)
	if _, err := h.Load(context.Background(), dir); err != nil {
		t.Fatalf("Load: %v", err)
	}
	out, handled, err := h.RunCommand("", "/say", []string{"there"})
	if !handled || err != nil || out != "[cap] hello there\n[cap] world" {
		t.Fatalf("say: handled=%v err=%v out=%q", handled, err, out)
	}
	out, handled, err = h.RunCommand("", "bad", nil)
	if !handled || err == nil || err.Error() != "no such run" || out != "[cap] tried" {
		t.Fatalf("bad: handled=%v err=%v out=%q", handled, err, out)
	}
	if _, handled, _ := h.RunCommand("", "nope", nil); handled {
		t.Fatal("unknown command handled")
	}
	var names []string
	for _, c := range h.PluginCommands() {
		names = append(names, c.Name()+"="+c.Description())
	}
	if len(names) != 2 || names[0] != "say=say it" || names[1] != "bad=" {
		t.Fatalf("commands: %v", names)
	}
}

// A command run for a conversation can ask which one (magi.command_session); outside such a run the
// answer is empty, so a command typed in the TUI keeps its own fallback.
func TestACommandKnowsTheConversationItRunsFor(t *testing.T) {
	dir := writePlugin(t,
		`name="who"`+"\n"+`capabilities=["command"]`,
		`magi.register_command{ name="who", execute = function() magi.log("for=" .. magi.command_session()) end }`,
	)
	h := hostWith(nil, nil)
	if _, err := h.Load(context.Background(), dir); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if out, _, err := h.RunCommand("s_a", "who", nil); err != nil || out != "[who] for=s_a" {
		t.Fatalf("for s_a: %q %v", out, err)
	}
	if out, _, err := h.RunCommand("", "who", nil); err != nil || out != "[who] for=" {
		t.Fatalf("no session must not keep the last one: %q %v", out, err)
	}
}

// A command looked up before its plugin was unloaded (a reload in between) must not reach the closed
// Lua state: it answers that the plugin went away.
func TestACommandOfAnUnloadedPluginRefusesInsteadOfCrashing(t *testing.T) {
	dir := writePlugin(t,
		`name="gone"`+"\n"+`capabilities=["command"]`,
		`magi.register_command{ name="gone", execute = function() magi.log("ran") end }`,
	)
	h := hostWith(nil, nil)
	if _, err := h.Load(context.Background(), dir); err != nil {
		t.Fatalf("Load: %v", err)
	}
	cmd := h.findCommand("gone")
	if cmd == nil {
		t.Fatal("no command")
	}
	if err := h.Unload("gone"); err != nil {
		t.Fatal(err)
	}
	var out string
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("running a command of an unloaded plugin panicked: %v", r)
			}
		}()
		out, err = cmd.run("s_a", nil, true)
	}()
	if err == nil || !strings.Contains(err.Error(), "unloaded") || out != "" {
		t.Fatalf("want a refusal naming the unload, got out=%q err=%v", out, err)
	}
}
