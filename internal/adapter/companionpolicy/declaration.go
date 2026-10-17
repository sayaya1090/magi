// Package companionpolicy loads user-owned declarations and executes their bounded operations.
package companionpolicy

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/BurntSushi/toml"
	"github.com/sayaya1090/magi/internal/core/companion"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/core/text"
)

const MaxFileBytes = 1 << 20
const MaxResultBytes = 64 << 10
const inputSchema = `{"type":"object","properties":{"path":{"type":"string","description":"Relative path inside the published root; absolute paths and traversal are refused."},"offset":{"type":"integer","minimum":1},"limit":{"type":"integer","minimum":1,"maximum":1000}},"required":["path"],"additionalProperties":false}`
const outputSchema = `{"type":"object","properties":{"path":{"type":"string"},"offset":{"type":"integer"},"totalLines":{"type":"integer"},"lines":{"type":"array","items":{"type":"string"}}},"required":["path","offset","totalLines","lines"],"additionalProperties":false}`

var identifier = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

// Path identifies a workspace without creating a file or invoking a model.
func Path(configDir, workdir string) (string, error) {
	if configDir == "" || workdir == "" {
		return "", fmt.Errorf("configuration directory and workspace are required")
	}
	abs, err := filepath.Abs(workdir)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("identify workspace: %w", err)
	}
	sum := sha256.Sum256([]byte(filepath.Clean(real)))
	return filepath.Join(configDir, "companion-policies", fmt.Sprintf("%x", sum), "policy.toml"), nil
}

type declaration struct {
	Version          int                `toml:"version"`
	Purpose          string             `toml:"purpose"`
	Responsibilities []string           `toml:"responsibilities"`
	Tasks            []companion.Task   `toml:"tasks"`
	Access           []companion.Access `toml:"access"`
	Instructions     []string           `toml:"instructions"`
	Operations       []struct {
		ID          string `toml:"id"`
		Description string `toml:"description"`
		Kind        string `toml:"kind"`
		Root        string `toml:"root"`
	} `toml:"operations"`
}

// Load returns no exports when absent. Invalid policy is an error, never an implicit grant.
func Load(configDir, workdir string) (companion.Contract, error) {
	out := companion.Contract{Version: 1, Operations: []companion.Operation{}, Tasks: []companion.Task{}, Access: []companion.Access{}}
	path, err := Path(configDir, workdir)
	if err != nil {
		return out, err
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if !info.Mode().IsRegular() {
		return out, fmt.Errorf("companion policy must be a regular file, not a link or special file")
	}
	dir, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return out, err
	}
	defer dir.Close()
	f, err := openRead(dir, filepath.Base(path))
	if err != nil {
		return out, err
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil {
		return out, err
	}
	if !info.Mode().IsRegular() {
		return out, fmt.Errorf("companion policy must be a regular file")
	}

	data, err := io.ReadAll(io.LimitReader(f, MaxFileBytes+1))
	if err != nil {
		return out, err
	}
	if len(data) > MaxFileBytes {
		return out, fmt.Errorf("companion policy exceeds 1 MiB")
	}
	var d declaration
	meta, err := toml.Decode(string(data), &d)
	if err != nil {
		return out, fmt.Errorf("invalid companion policy: %w", err)
	}
	if keys := meta.Undecoded(); len(keys) > 0 {
		return out, fmt.Errorf("unknown companion policy key: %s", keys[0])
	}
	if d.Version != 1 {
		return out, fmt.Errorf("unsupported companion policy version %d", d.Version)
	}
	if len(d.Operations) > 64 {
		return out, fmt.Errorf("at most 64 operations may be published")
	}
	if len(d.Purpose) > 2048 || len(d.Instructions) > 32 {
		return out, fmt.Errorf("purpose or instruction count exceeds the policy limit")
	}
	instructionBytes := 0
	for _, text := range d.Instructions {
		instructionBytes += len(text)
	}
	if instructionBytes > 16384 {
		return out, fmt.Errorf("instructions exceed 16 KiB")
	}
	if len(d.Tasks) > 64 || len(d.Access) > 64 || len(d.Responsibilities) > 32 {
		return out, fmt.Errorf("too many tasks, access entries, or responsibilities")
	}
	taskIDs := map[string]bool{}
	for _, task := range d.Tasks {
		if !identifier.MatchString(task.ID) || taskIDs[task.ID] || strings.TrimSpace(task.Description) == "" || len(task.Description) > 2048 {
			return out, fmt.Errorf("invalid or duplicate task %q", task.ID)
		}
		taskIDs[task.ID] = true
		if len(task.Constraints) > 32 {
			return out, fmt.Errorf("too many constraints on task %s", task.ID)
		}
		for _, s := range task.Constraints {
			if len(s) > 2048 {
				return out, fmt.Errorf("task constraint exceeds 2048 bytes")
			}
		}
	}
	for _, a := range d.Access {
		if strings.TrimSpace(a.Resource) == "" || len(a.Resource) > 4096 || len(a.Description) > 2048 {
			return out, fmt.Errorf("invalid access resource")
		}
		switch a.Mode {
		case "read", "write", "execute", "deny", "approval":
		default:
			return out, fmt.Errorf("invalid access mode %q", a.Mode)
		}
	}
	for _, s := range d.Responsibilities {
		if len(s) > 2048 {
			return out, fmt.Errorf("responsibility exceeds 2048 bytes")
		}
	}
	seen := map[string]bool{}
	for _, op := range d.Operations {
		if !identifier.MatchString(op.ID) || seen[op.ID] {
			return out, fmt.Errorf("invalid or duplicate operation ID %q", op.ID)
		}
		seen[op.ID] = true
		if strings.TrimSpace(op.Description) == "" || len(op.Description) > 2048 {
			return out, fmt.Errorf("operation %s needs a description", op.ID)
		}
		if op.Kind != "read_file" {
			return out, fmt.Errorf("operation %s: unsupported kind %q", op.ID, op.Kind)
		}
		if !filepath.IsAbs(op.Root) {
			return out, fmt.Errorf("operation %s needs an absolute root", op.ID)
		}
		out.Operations = append(out.Operations, companion.Operation{ID: op.ID, Description: op.Description, Kind: op.Kind, Root: filepath.Clean(op.Root), ReadOnly: true, InputSchema: json.RawMessage(inputSchema), OutputSchema: json.RawMessage(outputSchema)})
	}
	out.Purpose, out.Instructions = d.Purpose, d.Instructions
	out.Responsibilities = d.Responsibilities
	out.Tasks = append([]companion.Task{}, d.Tasks...)
	out.Access = append([]companion.Access{}, d.Access...)
	return out, nil
}

func Find(c companion.Contract, id string) (companion.Operation, error) {
	for _, op := range c.Operations {
		if op.ID == id {
			return op, nil
		}
	}
	return companion.Operation{}, fmt.Errorf("operation %q is not published", id)
}

// Summaries avoids sending schemas on every discovery call.
func Summaries(c companion.Contract) companion.Contract {
	c.Operations = append([]companion.Operation{}, c.Operations...)
	for i := range c.Operations {
		c.Operations[i].Description = text.Clip(text.Collapse(c.Operations[i].Description), 160)
		c.Operations[i].InputSchema = nil
		c.Operations[i].OutputSchema = nil
	}
	return c
}

// Call reloads authorization on every invocation. It never runs a model or a shell.
func Call(ctx context.Context, configDir, workdir, id string, args json.RawMessage) (session.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return session.ToolResult{}, err
	}
	c, err := Load(configDir, workdir)
	if err != nil {
		return session.ToolResult{}, err
	}
	op, err := Find(c, id)
	if err != nil {
		return session.ToolResult{}, err
	}
	var in struct {
		Path   string `json:"path"`
		Offset *int   `json:"offset"`
		Limit  *int   `json:"limit"`
	}
	dec := json.NewDecoder(strings.NewReader(string(args)))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&in); err != nil {
		return fail("invalid arguments: " + err.Error()), nil
	}
	var more any
	if dec.Decode(&more) != io.EOF {
		return fail("arguments must be one JSON object"), nil
	}
	if !filepath.IsLocal(in.Path) {
		return fail("path must be relative and stay inside the published root"), nil
	}
	offset, limit := 1, 200
	if in.Offset != nil {
		offset = *in.Offset
	}
	if in.Limit != nil {
		limit = *in.Limit
	}
	if offset < 1 || limit < 1 || limit > 1000 {
		return fail("offset must be positive; limit must be between 1 and 1000"), nil
	}
	root, err := os.OpenRoot(op.Root)
	if err != nil {
		return fail(err.Error()), nil
	}
	defer root.Close()
	// Reject special files before opening (named pipes may block). ReadFile opens relative to
	// the root and prevents symlink traversal races from escaping it.
	info, err := root.Stat(in.Path)
	if err != nil {
		return fail(err.Error()), nil
	}
	if !info.Mode().IsRegular() {
		return fail("only regular files may be read"), nil
	}
	if info.Size() > MaxFileBytes {
		return fail("file exceeds the 1 MiB read limit"), nil
	}
	f, err := openRead(root, in.Path)
	if err != nil {
		return fail(err.Error()), nil
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil {
		return fail(err.Error()), nil
	}
	if !info.Mode().IsRegular() {
		return fail("only regular files may be read"), nil
	}
	b, err := io.ReadAll(io.LimitReader(f, MaxFileBytes+1))
	if err != nil {
		return fail(err.Error()), nil
	}
	if len(b) > MaxFileBytes {
		return fail("file exceeds the 1 MiB read limit"), nil
	}
	if err = ctx.Err(); err != nil {
		return session.ToolResult{}, err
	}
	if !utf8.Valid(b) || strings.ContainsRune(string(b), 0) {
		return fail("binary files are not supported"), nil
	}
	lines := []string{}
	if len(b) > 0 {
		lines = strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	}
	total := len(lines)
	start := offset - 1
	if start > total {
		start = total
	}
	end := start + min(limit, total-start)
	content, _ := json.Marshal(struct {
		Path   string   `json:"path"`
		Offset int      `json:"offset"`
		Total  int      `json:"totalLines"`
		Lines  []string `json:"lines"`
	}{in.Path, offset, total, lines[start:end]})
	if len(content) > MaxResultBytes {
		return fail("selected lines exceed the 64 KiB response limit; request a smaller line range"), nil
	}
	return session.ToolResult{Content: content}, nil
}
func fail(s string) session.ToolResult {
	b, _ := json.Marshal(s)
	return session.ToolResult{IsError: true, Content: b}
}

// Guide tells the agent what the user declared without claiming a process sandbox exists.
func Guide(c companion.Contract) string {
	if c.Purpose == "" && len(c.Instructions) == 0 && len(c.Tasks) == 0 && len(c.Access) == 0 && len(c.Responsibilities) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n# User-declared companion purpose\n" + c.Purpose + "\n")
	for _, s := range c.Responsibilities {
		b.WriteString("Responsibility: " + s + "\n")
	}
	for _, task := range c.Tasks {
		b.WriteString("Offered task " + task.ID + ": " + task.Description + "\n")
		for _, s := range task.Constraints {
			b.WriteString("  Constraint: " + s + "\n")
		}
	}
	for _, a := range c.Access {
		b.WriteString("User-declared access: " + a.Mode + " " + a.Resource + " " + a.Description + "\n")
	}
	for _, s := range c.Instructions {
		b.WriteString("- " + s + "\n")
	}
	b.WriteString("These operating instructions apply to your work. Published operations grant peers only their declared access; they do not expand your own tool permissions.\n")
	return b.String()
}

// Advertisement derives only what the user explicitly offers, never learned skills or MCP connections.
func Advertisement(c companion.Contract) (int, []string) {
	names := make([]string, 0, len(c.Tasks)+len(c.Operations))
	for _, task := range c.Tasks {
		names = append(names, "task:"+task.ID)
	}
	for _, op := range c.Operations {
		names = append(names, "call:"+op.ID)
	}
	return len(names), names
}
