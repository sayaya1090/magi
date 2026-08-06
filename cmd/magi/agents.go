package main

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/fleet"
)

// printAgents renders the fleet for a terminal.
//
// The browser has the same list, and this is not a smaller version of it: a person who is already
// in a shell wants to know which daemon to attach to, and the answer is a workspace path and a
// state. So the path is printed in full rather than clipped to fit a column — it is the thing that
// gets copied into the next command.
func printAgents(w io.Writer, list []fleet.Agent, configDir string) {
	if len(list) == 0 {
		fmt.Fprintf(w, "No magi daemons under %s.\nStart one with `magi --daemon` in a workspace.\n", configDir)
		return
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "STATE\tAGENT\tIDLE\tSTEPS\tWORKSPACE")
	for _, a := range list {
		name := a.Name
		if a.Here {
			name += " *" // the workspace this shell is standing in
		}
		steps := "-"
		if a.Steps > 0 {
			steps = fmt.Sprint(a.Steps)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", a.State, name, since(a.Idle), steps, a.Workdir)
		// The task goes on its own line, indented: it is a sentence, and a sentence in a column
		// either wraps the table or gets cut to uselessness.
		// What it is BLOCKED on displaces what it was doing: the second is context, the first is
		// the reason nothing is happening.
		line := strings.TrimSpace(strings.SplitN(a.Task, "\n", 2)[0])
		if a.Asking != "" {
			line = "⏸ " + strings.TrimSpace(strings.SplitN(a.Asking, "\n", 2)[0])
		}
		if line != "" {
			fmt.Fprintf(tw, "\t%s\t\t\t\n", fleet.Clip(line, 72))
		}
	}
	tw.Flush()
	// Only when it is worth saying. A single live daemon needs no legend.
	if n := deadCount(list); n > 0 {
		fmt.Fprintf(w, "\n%d not running (`stopped`/`abandoned`) — abandoned means a turn was cut off "+
			"and its work is still in the log.\n", n)
	}
}

func deadCount(list []fleet.Agent) int {
	n := 0
	for _, a := range list {
		if !a.Live {
			n++
		}
	}
	return n
}

// since renders an age the way a person reads one. -1 means the log had nothing to date.
func since(sec int) string {
	if sec < 0 {
		return "-"
	}
	d := time.Duration(sec) * time.Second
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", sec)
	case d < time.Hour:
		return fmt.Sprintf("%dm", sec/60)
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", sec/3600)
	}
	return fmt.Sprintf("%dd", sec/86400)
}

// daemonAnswerWait is how long a daemon holds a prompt open for an attached UI.
//
// Long enough to walk back to the desk or pick up a phone; short enough that a viewer closed
// hours ago does not leave the agent stopped in front of one question. On expiry the prompt
// resolves by policy and says so in the transcript, so the record shows a default rather than a
// decision.
const daemonAnswerWait = 3 * time.Minute

// answerWait is the bound for this run: none for a terminal, where the human is present and the
// prompt is on their screen.
func answerWait(daemonAnswerable bool) time.Duration {
	if daemonAnswerable {
		return daemonAnswerWait
	}
	return 0
}
