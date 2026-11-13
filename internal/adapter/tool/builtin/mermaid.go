package builtin

import (
	"context"
	"encoding/json"
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
	return "Prepare a Mermaid diagram for explaining complex architecture, component relations, sequences or state transitions. Return the fenced diagram in your answer with a short explanation. This checks input limits and policy, not Mermaid grammar; the client renders and reports syntax errors. Load skill visual-explanation for UML guidance."
}
func (Mermaid) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"title":{"type":"string"},"source":{"type":"string","description":"Mermaid source without fences. Plain labels; no HTML, URLs, click directives or configuration."}},"required":["title","source"],"additionalProperties":false}`)
}

var diagramUnsafe = regexp.MustCompile("(?im)%%\\s*\\{|^\\s*---|^\\s*click\\s|<\\s*/?\\s*[a-z]|https?://|```")

func (m Mermaid) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	var args struct {
		Title  string `json:"title"`
		Source string `json:"source"`
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
	if !m.Preview {
		return okText("", fmt.Sprintf("Prepared for client rendering (syntax has not been parsed). Include this diagram and explain its important relationships:\n\n```mermaid\n%s\n```", args.Source)), nil
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
		_ = os.Remove(path)
		return errResult("", "Could not save preview"), nil
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
