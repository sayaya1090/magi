package config

import (
	"strings"
	"testing"
)

func onoff(b bool) *bool { return &b }

// The autocomplete switches fold the master: a surface is on only when it is not itself off AND
// the whole feature is on — and every nil means on, because that is what shipping on means.
func TestAutocompleteSwitchesFoldTheMaster(t *testing.T) {
	var a AutocompleteConfig
	if !a.On() || !a.AmbientOn() || !a.CodeOn() || !a.ComposerOn() || !a.CrossSessionOn() {
		t.Fatal("nil everywhere means on everywhere")
	}
	a.Enabled = onoff(false)
	a.Code = onoff(true)
	if a.On() || a.CodeOn() || a.AmbientOn() || a.ComposerOn() {
		t.Fatal("the master off silences every surface, however on the surface says it is")
	}
	a = AutocompleteConfig{Composer: onoff(false)}
	if !a.CodeOn() || a.ComposerOn() {
		t.Fatal("one surface off must not touch its neighbour")
	}
}

// MergeCron: project wins on a shared name, neither input is modified, and nothing-in means
// nil-out (not an empty map somebody then range-writes into config).
func TestMergeCronProjectWinsAndInputsSurvive(t *testing.T) {
	if MergeCron(nil, nil) != nil {
		t.Fatal("nothing in, nil out")
	}
	g := map[string]CronJob{"tick": {Schedule: "1 * * * *"}, "sweep": {Schedule: "2 * * * *"}}
	p := map[string]CronJob{"tick": {Schedule: "9 * * * *"}}
	out := MergeCron(g, p)
	if out["tick"].Schedule != "9 * * * *" || out["sweep"].Schedule != "2 * * * *" {
		t.Fatalf("project overrides by name, the rest carries: %+v", out)
	}
	if g["tick"].Schedule != "1 * * * *" || len(p) != 1 {
		t.Fatal("the caller may be holding a config it did not load — inputs must not be modified")
	}
}

// BareName is the one gate for every name that becomes part of a raw-concatenated TOML header.
func TestBareNameGate(t *testing.T) {
	for _, ok := range []string{"fast", "Qwen3-coder_30b", "a"} {
		if !BareName(ok) {
			t.Errorf("%q is a bare name", ok)
		}
	}
	for _, bad := range []string{"", "a.b", "a b", "a\nb", "[x]", "가나", "a,b", "x#y"} {
		if BareName(bad) {
			t.Errorf("%q must be refused — it would split or break the [table.header]", bad)
		}
	}
}

// capList names every capability, for the message that explains a refusal.
func TestCapListNamesEveryCapability(t *testing.T) {
	l := capList()
	if l == "" || !strings.Contains(l, ", ") {
		t.Fatalf("a list of capabilities should read as one: %q", l)
	}
	for _, must := range []string{"admin", "shell"} {
		if !strings.Contains(l, must) {
			t.Errorf("%q belongs in the capability list, got %q", must, l)
		}
	}
}

// CompanionDir is keyed by the same string the socket is.
func TestCompanionDirKeyedLikeTheSocket(t *testing.T) {
	got := CompanionDir("/cfg", "daemon-abc")
	if !strings.HasSuffix(got, "companions/daemon-abc") || !strings.HasPrefix(got, "/cfg") {
		t.Fatalf("settings live under the config dir, keyed by the door: %q", got)
	}
}
