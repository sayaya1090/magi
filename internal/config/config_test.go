package config

import (
	"os"
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
