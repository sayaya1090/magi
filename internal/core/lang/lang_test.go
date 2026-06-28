package lang

import "testing"

func TestDetect(t *testing.T) {
	cases := map[string]string{
		"안녕하세요 도와줘":   "Korean (한국어)",
		"こんにちは":       "Japanese (日本語)",
		"привет мир":  "Russian (русский)",
		"hello world": "",
		"":            "",
	}
	for in, want := range cases {
		if got := Detect(in); got != want {
			t.Errorf("Detect(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDirective(t *testing.T) {
	if d := Directive("프로젝트 검토해줘"); d == "" {
		t.Error("Korean text should yield a language directive")
	}
	if d := Directive("review the project"); d != "" {
		t.Errorf("Latin text should yield no directive, got %q", d)
	}
}
