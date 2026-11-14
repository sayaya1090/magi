package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/diagram"
	"github.com/sayaya1090/magi/internal/port"
)

// Mermaid prepares a portable diagram. Parsing and layout happen in the client.
type Mermaid struct{ Preview bool }

func (m Mermaid) ReadOnly() bool { return !m.Preview }

func (m Mermaid) Name() string {
	if m.Preview {
		return "mermaid_preview"
	}
	return "mermaid"
}
func (m Mermaid) Description() string {
	if m.Preview {
		return "Create an offline interactive Mermaid HTML preview for this local terminal. Return ONLY the preview link and a short explanation to the user; never print the Mermaid source. Browser capability is checked before this tool is offered and again at execution."
	}
	return "Prepare a Mermaid diagram. Use it by default whenever you explain relationships, ordering or causation with two or more steps: architecture, component relations, request or data flows, sequences, state transitions, root-cause chains, the order of an operation, a change across several files, or a comparison of alternatives. Return the fenced diagram in your answer with a short explanation. This checks input limits and policy, not Mermaid grammar; the client renders and reports syntax errors. Load skill visual-explanation for UML guidance. Colour nodes by meaning with colors, choosing Material palette roles by their purpose (primary, secondary, tertiary, error, neutral); a #RRGGBB code only when a specific colour is required. Uncoloured nodes keep the theme."
}
func (Mermaid) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"title":{"type":"string"},"source":{"type":"string","description":"Mermaid source without fences. Plain labels; no HTML, URLs, click directives or configuration."},"descriptions":{"type":"object","additionalProperties":{"type":"string"},"description":"Optional plain-text explanations keyed by the exact visible node label. Keep labels unique."},"colors":{"type":"object","additionalProperties":{"type":"string"},"description":"Optional colour per exact visible node label. Prefer a Material palette role, chosen by purpose: primary = the subject of the explanation and its main path; secondary = supporting components; tertiary = what must stand out by contrast (an external system, a proposal, a change); error = failure, risk or root cause; neutral = background context or out of scope. The client paints each role with the person's theme in light or dark. Use #RRGGBB only when a specific colour is required."},"animate":{"type":"boolean","description":"Animate data-flow arrows in flowcharts only; use only when arrows represent data movement."}},"required":["title","source"],"additionalProperties":false}`)
}

// diagramColour is a node colour: a Material palette role the client paints from the person's theme,
// or a #RRGGBB code for the rare node that needs a specific colour.
var diagramColour = regexp.MustCompile(`^(primary|secondary|tertiary|error|neutral|#[0-9A-Fa-f]{6})$`)

var diagramUnsafe = regexp.MustCompile("(?im)%%\\s*\\{|^\\s*---|^\\s*click\\s|<\\s*/?\\s*[a-z]|https?://|```")

func (m Mermaid) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	var args struct {
		Title        string            `json:"title"`
		Source       string            `json:"source"`
		Descriptions map[string]string `json:"descriptions"`
		Colors       map[string]string `json:"colors"`
		Animate      bool              `json:"animate"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return errResult("", "invalid diagram arguments"), nil
	}
	args.Title = strings.TrimSpace(args.Title)
	args.Title = strings.NewReplacer("[", "(", "]", ")", "\n", " ", "\r", " ").Replace(args.Title)
	args.Source = strings.TrimSpace(args.Source)
	if args.Title == "" || args.Source == "" || len(args.Title) > 200 || len(args.Source) > 24000 || diagramUnsafe.MatchString(args.Source) {
		return errResult("", "Provide a title (1–200 bytes) and plain Mermaid source (1–24000 bytes), without fences, HTML, links, click directives or configuration."), nil
	}
	if len(args.Descriptions) > 100 {
		return errResult("", "Provide at most 100 node descriptions."), nil
	}
	for label, description := range args.Descriptions {
		if strings.TrimSpace(label) == "" || len(label) > 200 || len(description) > 2000 || diagramUnsafe.MatchString(label+"\n"+description) {
			return errResult("", "Use plain node labels (1–200 bytes) and descriptions (at most 2000 bytes), without HTML, links or directives."), nil
		}
	}
	if len(args.Descriptions) > 0 {
		metadata, err := json.Marshal(args.Descriptions)
		if err != nil {
			return errResult("", "Could not encode node descriptions."), nil
		}
		args.Source += "\n%% magi-descriptions " + string(metadata)
	}
	if len(args.Colors) > 100 {
		return errResult("", "Provide at most 100 node colours."), nil
	}
	for label, colour := range args.Colors {
		if strings.TrimSpace(label) == "" || len(label) > 200 || diagramUnsafe.MatchString(label) || !diagramColour.MatchString(colour) {
			return errResult("", "Colour each node label (1–200 bytes) with a palette role — primary, secondary, tertiary, error or neutral — or, only when a specific colour is required, #RRGGBB."), nil
		}
	}
	if len(args.Colors) > 0 {
		metadata, err := json.Marshal(args.Colors)
		if err != nil {
			return errResult("", "Could not encode node colours."), nil
		}
		args.Source += "\n%% magi-colors " + string(metadata)
	}
	if args.Animate {
		args.Source += "\n%% magi-animate"
	}
	if len(args.Source) > 24000 {
		return errResult("", "Diagram including descriptions exceeds 24000 bytes."), nil
	}
	if !m.Preview {
		return okText("", fmt.Sprintf("Prepared for client rendering (syntax has not been parsed). Include this diagram verbatim, preserving its metadata comments, and explain its important relationships:\n\n```mermaid\n%s\n```", args.Source)), nil
	}
	if !DiagramBrowserAvailable(ctx) {
		return errResult("", "Local browser preview is unavailable. Explain in text; do not claim a preview was opened."), nil
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		return errResult("", err.Error()), nil
	}
	dir = filepath.Join(dir, "magi", "diagrams")
	if err = os.MkdirAll(dir, 0700); err != nil {
		return errResult("", err.Error()), nil
	}
	f, err := os.CreateTemp(dir, "diagram-*.html")
	if err != nil {
		return errResult("", err.Error()), nil
	}
	path := f.Name()
	_, writeErr := f.WriteString(diagram.HTML(args.Title, args.Source))
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		failure := errors.Join(writeErr, closeErr)
		if removeErr := os.Remove(path); removeErr != nil {
			failure = errors.Join(failure, fmt.Errorf("remove incomplete preview %s: %w", path, removeErr))
		}
		return errResult("", "Could not save preview: "+failure.Error()), nil
	}
	link := (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String()
	return okText("", fmt.Sprintf("[%s](%s)\nLocal HTML preview saved; it has not been opened. Show this link and a short explanation only. Source copying is available inside the preview.", args.Title, link)), nil
}

// Fail closed: a GUI session and a known installed browser are required. An opener
// executable alone says nothing about whether anyone can view its window.
func DiagramBrowserAvailable(ctx context.Context) bool {
	if os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_TTY") != "" || os.Getenv("CI") != "" {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	switch runtime.GOOS {
	case "darwin":
		out, err := exec.CommandContext(ctx, "/usr/bin/stat", "-f", "%Su", "/dev/console").Output()
		if err != nil || strings.TrimSpace(string(out)) != os.Getenv("USER") {
			return false
		}
		return exec.CommandContext(ctx, "/usr/bin/open", "-Ra", "Safari").Run() == nil
	case "linux":
		if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
			return false
		}
		if _, err := exec.LookPath("xdg-open"); err != nil {
			return false
		}
		out, err := exec.CommandContext(ctx, "xdg-settings", "get", "default-web-browser").Output()
		if err != nil || strings.TrimSpace(string(out)) == "" {
			return false
		}
		for _, name := range []string{"firefox", "chromium", "chromium-browser", "google-chrome"} {
			if _, err := exec.LookPath(name); err == nil {
				return true
			}
		}
	}
	return false // other platforms remain hidden until their GUI probe is implemented
}
