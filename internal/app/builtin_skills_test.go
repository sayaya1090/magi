package app

import (
	"strings"
	"testing"
)

// The OOAD guide ships inside the binary, so it is general knowledge only: what a course or an
// assignment taught is the person's own material and stays out of it.
func TestOOADSkillAvailableWithoutInstallation(t *testing.T) {
	a := New(nil, nil, nil, nil, nil, Config{})
	b, ok := a.skillBody(t.TempDir(), "ooad")
	if !ok {
		t.Fatal("built-in ooad skill missing")
	}
	for _, want := range []string{"System Sequence Diagram", "Domain Model", "Inception", "classDiagram", "sequenceDiagram"} {
		if !strings.Contains(b, want) {
			t.Errorf("ooad skill lacks %q", want)
		}
	}
	for _, private := range []string{"인증과제", "건국대", "강사", "교안", "pptx", "똑닥", "화상 회의", "AA course", "instructor"} {
		if strings.Contains(b, private) {
			t.Errorf("ooad skill carries course material %q", private)
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
