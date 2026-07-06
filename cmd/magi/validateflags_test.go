package main

import (
	"strings"
	"testing"
)

// validateEnumFlags is the up-front guard that makes a mistyped enum flag fail
// loudly (exit 2) instead of silently degrading behavior. -profile is the
// safety-relevant case (O5): an unrecognized value used to fall through to the
// unconfined posture with no warning.
func TestValidateEnumFlags(t *testing.T) {
	cases := []struct {
		name                             string
		output, permission, profile, thm string
		wantErr                          bool
		wantContains                     string
	}{
		{"all-valid", "text", "auto", "safe", "dark", false, ""},
		{"defaults", "text", "", "", "auto", false, ""},
		{"json-ok", "json", "", "", "light", false, ""},
		{"bad-output", "jsn", "", "", "auto", true, "-output"},
		{"bad-permission", "text", "bogus", "", "auto", true, "-permission"},
		{"empty-permission-ok", "text", "", "standard", "auto", false, ""},
		// O5: the footgun. A mistyped safety profile must be rejected, not accepted.
		{"bad-profile", "text", "", "safmode", "auto", true, "-profile"},
		{"profile-case-sensitive", "text", "", "SAFE", "auto", true, "-profile"},
		{"empty-profile-ok", "text", "auto", "", "auto", false, ""},
		// O6: theme was previously accepted silently and auto-fell-back.
		{"bad-theme", "text", "", "", "neon", true, "-theme"},
		{"yolo-ok", "text", "", "yolo", "dark", false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			msg := validateEnumFlags(c.output, c.permission, c.profile, c.thm)
			if c.wantErr && msg == "" {
				t.Fatalf("expected an error message, got none")
			}
			if !c.wantErr && msg != "" {
				t.Fatalf("expected no error, got %q", msg)
			}
			if c.wantContains != "" && !contains(msg, c.wantContains) {
				t.Fatalf("message %q should mention %q", msg, c.wantContains)
			}
		})
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

func TestWarnUnknownConfigKeys(t *testing.T) {
	var b strings.Builder
	warnUnknownConfigKeys(&b, "config.toml", nil)
	if b.String() != "" {
		t.Errorf("no keys should print nothing, got %q", b.String())
	}
	warnUnknownConfigKeys(&b, "config.toml", []string{"profil", "modle"})
	out := b.String()
	if !strings.Contains(out, "config.toml") || !strings.Contains(out, "profil") || !strings.Contains(out, "modle") {
		t.Errorf("warning should name the file and each key, got %q", out)
	}
}

// O5 value-side twin: a typo'd guardrail *value* from config must hard-fail,
// not silently drop to unconfined.
func TestValidateGuardrailValues(t *testing.T) {
	cases := []struct {
		name                        string
		profile, permission, sndbox string
		wantErr                     bool
		wantContains                string
	}{
		{"all-empty", "", "", "", false, ""},
		{"all-valid", "safe", "auto", "workspace-write", false, ""},
		{"bad-profile-value", "saef", "", "", true, "profile"},
		{"bad-permission-value", "", "alow", "", true, "permission"},
		{"bad-sandbox-value", "", "", "workspace-writ", true, "sandbox"},
		{"valid-ro", "", "", "read-only", false, ""},
		{"valid-full", "yolo", "allow", "full", false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			msg := validateGuardrailValues(c.profile, c.permission, c.sndbox)
			if c.wantErr && msg == "" {
				t.Fatalf("expected an error, got none")
			}
			if !c.wantErr && msg != "" {
				t.Fatalf("expected no error, got %q", msg)
			}
			if c.wantContains != "" && !strings.Contains(msg, c.wantContains) {
				t.Fatalf("message %q should mention %q", msg, c.wantContains)
			}
		})
	}
}
