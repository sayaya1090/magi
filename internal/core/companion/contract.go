// Package companion defines the user-declared public contract of a workspace.
package companion

import "encoding/json"

// Operation describes a directly callable operation, not a learned procedure.
type Operation struct {
	ID           string          `json:"id"`
	Description  string          `json:"description"`
	Kind         string          `json:"kind"`
	Root         string          `json:"root"`
	ReadOnly     bool            `json:"readOnly"`
	InputSchema  json.RawMessage `json:"inputSchema,omitempty"`
	OutputSchema json.RawMessage `json:"outputSchema,omitempty"`
}

type Contract struct {
	Version          int         `json:"version"`
	Responsibilities []string    `json:"responsibilities,omitempty"`
	Tasks            []Task      `json:"tasks"`
	Access           []Access    `json:"access"`
	Purpose          string      `json:"purpose,omitempty"`
	Instructions     []string    `json:"instructions,omitempty"`
	Operations       []Operation `json:"operations"`
}

// Task is work the user offers through model-mediated hand_off.
type Task struct {
	ID          string   `json:"id" toml:"id"`
	Description string   `json:"description" toml:"description"`
	Constraints []string `json:"constraints,omitempty" toml:"constraints"`
}

// Access describes the user's operating boundary; it is not a shell sandbox grant.
type Access struct {
	Resource    string `json:"resource" toml:"resource"`
	Mode        string `json:"mode" toml:"mode"`
	Description string `json:"description,omitempty" toml:"description"`
}
