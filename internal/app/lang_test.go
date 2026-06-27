package app

import "testing"

func TestLangDirective(t *testing.T) {
	cases := []struct {
		text string
		want string // substring expected in the directive ("" = no directive)
	}{
		{"이 프로젝트의 설계문서를 검토해 줘", "Korean"},
		{"このプロジェクトを確認して", "Japanese"},
		{"请审查这个设计文档", "Chinese"},
		{"Проверь этот документ", "Russian"},
		{"please review the design doc", ""},
		{"review DESIGN.md and main.go", ""},
		{"", ""},
	}
	for _, c := range cases {
		got := langDirective(c.text)
		if c.want == "" {
			if got != "" {
				t.Errorf("%q: expected no directive, got %q", c.text, got)
			}
			continue
		}
		if got == "" || !contains(got, c.want) {
			t.Errorf("%q: expected directive mentioning %q, got %q", c.text, c.want, got)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
