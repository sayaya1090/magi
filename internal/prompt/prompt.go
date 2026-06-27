// Package prompt defines the host's interactive prompt contract, shared by the
// plugin host (which requests prompts via magi.ask) and the TUI (which renders
// them). Keeping the types here avoids a dependency between those packages.
package prompt

// Field types accepted in a Field.Type.
const (
	TypeText        = "text"        // single-line input
	TypePassword    = "password"    // masked single-line input
	TypeNumber      = "number"      // numeric input (digits/.//-)
	TypeMultiline   = "multiline"   // multi-line input (Enter inserts a newline)
	TypeSelect      = "select"      // pick one of Options
	TypeMultiselect = "multiselect" // pick zero or more of Options
	TypeConfirm     = "confirm"     // yes/no → bool
	TypeNote        = "note"        // non-input display text (instructions)
)

// Field is one prompt field.
type Field struct {
	Name    string   // key in the answers map (ignored for note)
	Type    string   // one of the Type* constants (default text)
	Label   string   // shown to the user (defaults to Name)
	Options []string // choices for select/multiselect
	Default string   // initial value (text/number/multiline) or selected option
}

// Spec is a prompt: a title and an ordered set of fields.
type Spec struct {
	Title  string
	Fields []Field
}

// Prompter renders a Spec interactively and returns the answers keyed by field
// name: string (text/password/number/select), bool (confirm), or []string
// (multiselect). It returns an error when no interactive terminal is available
// (e.g. headless) so the caller can fall back.
type Prompter interface {
	Ask(Spec) (map[string]any, error)
}
