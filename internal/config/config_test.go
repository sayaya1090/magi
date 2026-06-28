package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExpandEnv(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		env      map[string]string
		expected string
	}{
		{
			name:     "no variables",
			input:    "plain text",
			env:      nil,
			expected: "plain text",
		},
		{
			name:     "single variable",
			input:    "Bearer ${TOKEN}",
			env:      map[string]string{"TOKEN": "abc123"},
			expected: "Bearer abc123",
		},
		{
			name:     "multiple variables",
			input:    "${SCHEME}://${HOST}:${PORT}/mcp",
			env:      map[string]string{"SCHEME": "http", "HOST": "localhost", "PORT": "3000"},
			expected: "http://localhost:3000/mcp",
		},
		{
			name:     "undefined variable",
			input:    "Bearer ${UNDEFINED_VAR}",
			env:      nil,
			expected: "Bearer ${UNDEFINED_VAR}",
		},
		{
			name:     "mixed defined and undefined",
			input:    "${DEFINED} and ${UNDEFINED}",
			env:      map[string]string{"DEFINED": "value"},
			expected: "value and ${UNDEFINED}",
		},
		{
			name:     "empty value",
			input:    "Bearer ${EMPTY}",
			env:      map[string]string{"EMPTY": ""},
			expected: "Bearer ${EMPTY}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.env {
				os.Setenv(k, v)
				defer os.Unsetenv(k)
			}

			result := ExpandEnv(tt.input)
			if result != tt.expected {
				t.Errorf("ExpandEnv(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// Top-level keys (model/base_url/permission/experience_dir) parse from config.toml.
func TestLoadTopLevelKeys(t *testing.T) {
	dir := t.TempDir()
	toml := `model = "qwen3-coder:30b"
base_url = "http://localhost:11434/v1"
permission = "auto"
experience_dir = "/team/brain"
`
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.Model != "qwen3-coder:30b" || c.BaseURL != "http://localhost:11434/v1" ||
		c.Permission != "auto" || c.ExperienceDir != "/team/brain" {
		t.Errorf("parsed config = %+v", c)
	}
	// A missing config dir is not an error (zero Config).
	if _, err := Load(t.TempDir()); err != nil {
		t.Errorf("missing config.toml should not error: %v", err)
	}
}
