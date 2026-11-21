package lua

import (
	"context"
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
	out, handled, err := h.RunCommand("/say", []string{"there"})
	if !handled || err != nil || out != "[cap] hello there\n[cap] world" {
		t.Fatalf("say: handled=%v err=%v out=%q", handled, err, out)
	}
	out, handled, err = h.RunCommand("bad", nil)
	if !handled || err == nil || err.Error() != "no such run" || out != "[cap] tried" {
		t.Fatalf("bad: handled=%v err=%v out=%q", handled, err, out)
	}
	if _, handled, _ := h.RunCommand("nope", nil); handled {
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
