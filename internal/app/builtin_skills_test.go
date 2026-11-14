package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The OOAD family ships inside the binary, so it is general knowledge only: what a course or an
// assignment taught is the person's own material and stays out of it.
var ooadFamily = map[string][]string{
	"ooad":                 {"Inception", "traceability", "`uml-ssd`", "`uml-sequence`", "`uml-domain-model`", "`uml-class`", "`uml-architecture`", "`design-patterns`", "`architecture-tactics`"},
	"uml-ssd":              {"System Sequence Diagram", "black box", "sequenceDiagram"},
	"uml-sequence":         {"Combined fragments", "sequenceDiagram", "create participant"},
	"uml-domain-model":     {"Domain Model", "classDiagram", "no operations"},
	"uml-class":            {"Design Class Diagram", "classDiagram", "Realization"},
	"uml-architecture":     {"Component diagram", "Deployment diagram", "Package diagram"},
	"design-patterns":      {"GRASP", "Information Expert", "Strategy", "Observer"},
	"architecture-tactics": {"quality attribute", "Availability", "Redundancy"},
}

func TestOOADSkillsAvailableWithoutInstallation(t *testing.T) {
	a := New(nil, nil, nil, nil, nil, Config{})
	for name, wants := range ooadFamily {
		b, ok := a.skillBody(t.TempDir(), name)
		if !ok {
			t.Errorf("built-in %s skill missing", name)
			continue
		}
		for _, want := range wants {
			if !strings.Contains(b, want) {
				t.Errorf("%s skill lacks %q", name, want)
			}
		}
		for _, private := range []string{"인증과제", "건국대", "강사", "교안", "pptx", "똑닥", "화상 회의", "AA course", "instructor", "OOPT"} {
			if strings.Contains(b, private) {
				t.Errorf("%s skill carries course material %q", name, private)
			}
		}
	}
}

func TestVisualExplanationPointsToOOAD(t *testing.T) {
	a := New(nil, nil, nil, nil, nil, Config{})
	b, _ := a.skillBody(t.TempDir(), "visual-explanation")
	if !strings.Contains(b, "`ooad`") {
		t.Fatal("visual-explanation does not name the ooad skill")
	}
}

// Built-in skills are listed like any other, so the model can reach them without being pointed at
// them, but only for the coding agent: office, mail and other companions also hold the skill tool.
func TestBuiltinSkillsAreListedForTheCodingAgentOnly(t *testing.T) {
	coding := New(nil, nil, nil, nil, nil, Config{ClientRole: "coding"})
	var listed bool
	for _, s := range coding.availableSkills(t.TempDir()) {
		if s.Name == "visual-explanation" {
			listed = s.Description != ""
		}
	}
	if !listed {
		t.Error("the coding agent's skill list lacks the built-in visual-explanation with a description")
	}
	word := New(nil, nil, nil, nil, nil, Config{ClientRole: "word"})
	for _, s := range word.availableSkills(t.TempDir()) {
		if s.Name == "visual-explanation" {
			t.Error("a Word companion lists a built-in coding skill")
		}
	}
	if _, ok := word.skillBody(t.TempDir(), "visual-explanation"); ok {
		t.Error("a Word companion loads a built-in coding skill")
	}
}

func TestAWorkspaceSkillShadowsTheBuiltinOfTheSameName(t *testing.T) {
	dir := t.TempDir()
	skill := filepath.Join(dir, ".claude", "skills", "visual-explanation")
	if err := os.MkdirAll(skill, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("---\ndescription: local guide\n---\nlocal body"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := New(nil, nil, nil, nil, nil, Config{ClientRole: "coding"})
	n := 0
	for _, s := range a.availableSkills(dir) {
		if s.Name == "visual-explanation" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("visual-explanation listed %d times", n)
	}
	if b, _ := a.skillBody(dir, "visual-explanation"); !strings.Contains(b, "local body") {
		t.Error("the workspace skill does not win over the built-in")
	}
}

// Each built-in names the agent kinds it is for in its frontmatter (clients:); without it a skill is
// for the coding agent only, so a new file cannot reach office or mail companions by accident.
func TestBuiltinSkillsGoToTheAgentKindsTheyName(t *testing.T) {
	all := map[string]builtinSkill{
		"code-only": parseBuiltinSkill("---\nname: code-only\ndescription: x\n---\nbody"),
		"docs":      parseBuiltinSkill("---\nname: docs\ndescription: x\nclients: word, powerpoint\n---\nbody"),
		"shared":    parseBuiltinSkill("---\nname: shared\ndescription: x\nclients: coding, word\n---\nbody"),
	}
	has := func(kind ClientKind) map[string]bool {
		out := map[string]bool{}
		for name := range builtinsFor(kind, all) {
			out[name] = true
		}
		return out
	}
	if c := has(ClientCoding); !c["code-only"] || c["docs"] || !c["shared"] {
		t.Errorf("coding got %v", c)
	}
	if w := has(ClientWord); w["code-only"] || !w["docs"] || !w["shared"] {
		t.Errorf("word got %v", w)
	}
	if m := has(ClientMail); len(m) != 0 {
		t.Errorf("mail got %v", m)
	}
}

func TestShippedBuiltinSkillsNameKnownAgentKinds(t *testing.T) {
	for name, s := range builtinSkills {
		if len(s.clients) == 0 {
			t.Errorf("%s declares no clients", name)
		}
		for _, k := range s.clients {
			if !k.Valid() {
				t.Errorf("%s names unknown agent kind %q", name, k)
			}
		}
	}
}
