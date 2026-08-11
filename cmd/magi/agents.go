package main

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
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
		// What it is FOR, when it says. A name answers "which one is this"; a role answers "which
		// one do I want", which is the question somebody with several companions actually has.
		if a.Role != "" {
			name += "  — " + fleet.Clip(a.Role, 40)
		}
		if a.Team != "" {
			name += "  [" + a.Team + map[bool]string{true: "*"}[a.Hub] + "]"
		}
		steps := "-"
		if a.Steps > 0 {
			steps = fmt.Sprint(a.Steps)
		}
		// How far through its own plan, in the steps column: the two numbers answer the same
		// question a step count only half answers — is it getting anywhere.
		if a.PlanTotal > 0 {
			steps += fmt.Sprintf(" (%d/%d)", a.PlanDone, a.PlanTotal)
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
		// Work handed over by other companions, which the state column cannot show: that is read
		// from the session this listing offers to attach to, and handed-over work runs in
		// conversations of its own. Without this line a companion working through somebody else's
		// request reads as idle, which is true of the conversation and false of the machine.
		if load := fleet.Carrying(a); load != "" {
			fmt.Fprintf(tw, "\t↪ %s (handed over)\t\t\t\n", load)
		}
	}
	tw.Flush()
	// Only when it is worth saying. A single live daemon needs no legend.
	if n := deadCount(list); n > 0 {
		fmt.Fprintf(w, "\n%d not running (`stopped`/`abandoned`) — abandoned means a turn was cut off "+
			"and its work is still in the log.\n", n)
	}
	printPressure(w, daemon.LoadSince(configDir, time.Now().Add(-pressureWindow)))
}

// pressureWindow is how far back the load footer looks.
//
// A week, because the thing it is evidence for takes about that long to be believable and because
// a working week is the unit the person reading it plans in. The file keeps a month, so a longer
// look is a matter of reading it.
const pressureWindow = 7 * 24 * time.Hour

// printPressure says which companions were asked for more than they could take.
//
// Separate from the table above and shaped nothing like it, because it answers a different
// question. The table is "which one do I attach to, right now". This is "is one of these enough" —
// which cannot be read off any instant, and is the reason the moments are written down at all.
//
// The counts are stated and the conclusion is not drawn. Whether a companion that refused a dozen
// requests should be run in triplicate depends on what else that machine is doing, which nothing
// here can see.
func printPressure(w io.Writer, load []daemon.Pressure) {
	var busy []daemon.Pressure
	for _, p := range load {
		if p.Busy() {
			busy = append(busy, p)
		}
	}
	if len(busy) == 0 {
		return // work that arrived and started straight away, every time, is a companion doing its job
	}
	fmt.Fprintf(w, "\nHanded-over work over the last %d days:\n", int(pressureWindow/(24*time.Hour)))
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, p := range busy {
		name := p.Name
		if name == "" {
			name = filepath.Base(p.Socket)
		}
		line := fmt.Sprintf("%d taken", p.Taken)
		if p.Deepest > 0 {
			line += fmt.Sprintf(", up to %d already waiting", p.Deepest)
		}
		if p.Refused > 0 {
			line += fmt.Sprintf(", %d turned away", p.Refused)
		}
		fmt.Fprintf(tw, "  %s\t%s\n", name, line)
	}
	tw.Flush()
	fmt.Fprintln(w, "  Turned away means the queue was full when somebody asked. Repeatedly, and one "+
		"copy of that companion is not enough for what is being asked of it.")
}

// deadCount is how many companions HERE are not running.
//
// Remote rows are excluded, and the exclusion is the point: a companion on another machine that
// nobody has sighted lately is also !Live, and counting it here would have put it under a legend
// saying it stopped and left work in a log — neither of which this machine established.
func deadCount(list []fleet.Agent) int {
	n := 0
	for _, a := range list {
		if !a.Live && a.State != fleet.Remote {
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

// daemonAnswerWait is how long a daemon holds an AUTO-mode prompt open for an attached UI.
//
// Long enough to walk back to the desk or pick up a phone; short enough that a viewer closed hours
// ago does not leave the agent stopped in front of one question. On expiry the prompt resolves by
// policy and says so in the transcript, so the record shows a default rather than a decision.
const daemonAnswerWait = 3 * time.Minute

// answerWait is how long a prompt waits for a person, and it follows the PERMISSION MODE rather
// than the process.
//
//   - ask   — forever. Choosing "ask" is choosing to be asked; resolving it by default after a few
//     minutes answers the question on the person's behalf, which is the one thing the mode exists
//     to prevent. The companion sits in the fleet's `waiting` state, badged on the console and
//     pushed to a phone, until somebody answers it. That state is a first-class thing to be in.
//   - auto  — bounded, on a daemon. Here the prompts are the residue: file edits are already
//     approved and what is left is bash and the network, where "carry on without me" is a
//     defensible default and being stopped for hours is not.
//   - allow — never prompts, so the number never applies.
//
// A terminal waits forever in every mode: the person is sitting in front of it, and a prompt that
// expires while they are reading it is a decision taken out of their hands.
func answerWait(daemonAnswerable bool, perm string) time.Duration {
	if daemonAnswerable && perm == "auto" {
		return daemonAnswerWait
	}
	return 0
}
