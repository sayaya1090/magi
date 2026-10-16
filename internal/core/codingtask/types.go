// Package codingtask defines coding-only workspace lifecycle contracts.
package codingtask

import "time"

type Request struct {
	RequestID string   `json:"requestId"`
	TaskID    string   `json:"taskId,omitempty"`
	Purpose   string   `json:"purpose,omitempty"`
	Mode      string   `json:"mode,omitempty"` // auto, git, copy
	Base      string   `json:"base,omitempty"`
	Paths     []string `json:"paths,omitempty"`
	Prompt    string   `json:"prompt,omitempty"`
}

type File struct {
	Digest string `json:"digest"`
	Mode   uint32 `json:"mode"`
	Size   int64  `json:"size"`
}
type Manifest map[string]File

type Record struct {
	Version       int       `json:"version"`
	ID            string    `json:"taskId"`
	RequestID     string    `json:"requestId"`
	RequestDigest string    `json:"requestDigest"`
	Purpose       string    `json:"purpose"`
	Source        string    `json:"source"`
	Workspace     string    `json:"workspace"`
	Mode          string    `json:"mode"`
	Base          string    `json:"base,omitempty"`
	Branch        string    `json:"branch,omitempty"`
	CommonDir     string    `json:"commonDir,omitempty"`
	State         string    `json:"state"`
	Error         string    `json:"error,omitempty"`
	Baseline      Manifest  `json:"baseline"`
	Excluded      []string  `json:"excluded,omitempty"`
	Created       time.Time `json:"created"`
	Updated       time.Time `json:"updated"`
	Session       string    `json:"session,omitempty"`
}

type Change struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"` // create, modify, delete
	Before *File  `json:"before,omitempty"`
	After  *File  `json:"after,omitempty"`
}
type Outcome struct {
	Path   string `json:"path"`
	State  string `json:"state"` // applied, restored, conflict, unchanged, uncertain
	Reason string `json:"reason,omitempty"`
}
type Journal struct {
	ID            string    `json:"id"`
	TaskID        string    `json:"taskId"`
	Kind          string    `json:"kind"`
	State         string    `json:"state"`
	Changes       []Change  `json:"changes"`
	Outcomes      []Outcome `json:"outcomes"`
	ApplyState    string    `json:"applyState,omitempty"`
	ApplyOutcomes []Outcome `json:"applyOutcomes,omitempty"`
}

// Summary keeps large baseline manifests out of model/UI context. The full
// manifest remains private in task.json for recovery and integrity checks.
type Summary struct {
	ID        string    `json:"taskId"`
	Source    string    `json:"source"`
	Workspace string    `json:"workspace"`
	Purpose   string    `json:"purpose,omitempty"`
	Mode      string    `json:"mode"`
	Base      string    `json:"base,omitempty"`
	Branch    string    `json:"branch,omitempty"`
	State     string    `json:"state"`
	Error     string    `json:"error,omitempty"`
	FileCount int       `json:"fileCount"`
	ByteCount int64     `json:"byteCount"`
	Excluded  []string  `json:"excluded,omitempty"`
	Updated   time.Time `json:"updated"`
}

func Summarize(r Record) Summary {
	var bytes int64
	for _, f := range r.Baseline {
		bytes += f.Size
	}
	return Summary{ID: r.ID, Source: r.Source, Workspace: r.Workspace, Purpose: r.Purpose, Mode: r.Mode, Base: r.Base, Branch: r.Branch, State: r.State, Error: r.Error, FileCount: len(r.Baseline), ByteCount: bytes, Excluded: r.Excluded, Updated: r.Updated}
}
func Public(value any) any {
	switch v := value.(type) {
	case Record:
		return Summarize(v)
	case []Record:
		out := make([]Summary, 0, len(v))
		for _, r := range v {
			out = append(out, Summarize(r))
		}
		return out
	default:
		return value
	}
}

type Review struct {
	Change
	BeforeText *string `json:"beforeText,omitempty"`
	AfterText  *string `json:"afterText,omitempty"`
	Omitted    string  `json:"omitted,omitempty"`
}
