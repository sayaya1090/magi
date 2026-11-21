package main

import (
	"errors"
	"log"
	"net/http"
	"strings"
	"unicode"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// slashCommand splits `/name rest`. Only the shape: whether name is a command is the companion's
// list to say, and a `/word` it does not list is still a line said to the model. `//` is not one.
func slashCommand(text string) (name, rest string, ok bool) {
	if !strings.HasPrefix(text, "/") || strings.HasPrefix(text, "//") {
		return "", "", false
	}
	body := text[1:]
	i := strings.IndexFunc(body, unicode.IsSpace)
	if i < 0 {
		return body, "", body != ""
	}
	return body[:i], strings.TrimSpace(body[i:]), i > 0
}

// commandAnswer is what /submit says when the line was a plugin command rather than a prompt.
type commandAnswer struct {
	code int
	text string
}

// runIfCommand runs the line as a plugin slash command when its first word is one the companion
// lists; nil means it is not one and the line is said as before. Refused while a turn runs, as the
// TUI refuses it — no plugin command is in its safeWhileRunning list. A failure to reach the daemon
// is returned for the caller's usual handling.
func runIfCommand(cl *daemon.Client, sid session.SessionID, text string) (*commandAnswer, error) {
	name, rest, ok := slashCommand(text)
	if !ok {
		return nil, nil
	}
	listed, err := cl.PluginCommands()
	if err != nil {
		return nil, nil // a daemon without the door: every line is a prompt, as before
	}
	found := false
	for _, c := range listed {
		found = found || c.Name == name
	}
	if !found {
		return nil, nil
	}
	if st, err := cl.Status(string(sid)); err == nil && st.Doing != "" {
		return &commandAnswer{http.StatusConflict, "/" + name + " cannot run while a turn is working"}, nil
	}
	out, err := cl.RunPluginCommand(name, rest)
	var refused daemon.Refused
	switch {
	case errors.As(err, &refused):
		return &commandAnswer{http.StatusUnprocessableEntity, "/" + name + ": " + refused.Why}, nil
	case err != nil:
		return nil, err
	case strings.TrimSpace(out) == "":
		return &commandAnswer{http.StatusOK, "/" + name + ": done"}, nil
	}
	return &commandAnswer{http.StatusOK, out}, nil
}

func (a *commandAnswer) write(w http.ResponseWriter) {
	if a.code != http.StatusOK {
		http.Error(w, a.text, a.code)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Magi-Command", "ran")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte(a.text)); err != nil {
		log.Printf("magi-web: answering a plugin command: %v", err)
	}
}

func (s *server) pluginCommands(w http.ResponseWriter, r *http.Request) {
	if s.forwarded(w, r, s.proxy) {
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	list := []port.PluginCommandInfo{}
	err := s.browse(r, func(cl *daemon.Client, _ session.SessionID) error {
		commands, err := cl.PluginCommands()
		list = append(list, commands...)
		return err
	})
	if err != nil {
		http.Error(w, err.Error(), daemonSaysNo(err))
		return
	}
	writeJSON(w, "plugin commands", list)
}
