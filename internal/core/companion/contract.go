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
	Version      int         `json:"version"`
	Purpose      string      `json:"purpose,omitempty"`
	Instructions []string    `json:"instructions,omitempty"`
	Operations   []Operation `json:"operations"`
}
