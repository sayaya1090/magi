package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/adapter/fleet"
	"github.com/sayaya1090/magi/internal/adapter/mcpserve"
	"github.com/sayaya1090/magi/internal/adapter/tool/companion"
	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// The two doors work arrives and answers leave by, when the companions are on different machines.
//
// # Same shape as --members, and for the same reason
//
// A subcommand reading JSON on stdin and writing JSON on stdout, run over ssh. No port, no
// listener, no token: the far side needs magi on its PATH and a shell, which is the boundary the
// whole cluster already rests on. Nothing here decides who may ask — ssh did that.
//
// # What is trusted from over there
//
// Words, and a name. The request is text that ends up in a prompt, and the target is resolved
// against companions published HERE — an arriving message cannot name a socket, a workspace or a
// command. The label above the request is the asker's, and it is the one field this accepts
// verbatim, because a paraphrase of "who sent this" is the failure this tree has recorded most.
//
// # Read-only on the way back
//
// --handoff-state answers questions about a session and changes nothing. It is called on a timer by
// whoever is waiting, so it has to be cheap and it must never be a way to make something happen.

// handRequest is work arriving from another machine.
type handRequest struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Request string `json:"request"`
	Label   string `json:"label"`
}

type handReply struct {
	Refused string `json:"refused,omitempty"`
	Name    string `json:"name,omitempty"`
	Workdir string `json:"workdir,omitempty"`
	Session string `json:"session,omitempty"`
	Since   int64  `json:"since,omitempty"`
}

// handHere takes work handed to a companion on this machine.
func handHere(in io.Reader, out, errOut io.Writer, r fleet.Reader, configDir string) int {
	var req handRequest
	if err := readJSON(in, &req); err != nil {
		fmt.Fprintln(errOut, "magi:", err)
		return 1
	}
	if req.To == "" || req.Request == "" {
		return writeJSON(out, errOut, handReply{Refused: "a request needs somebody to do it and something to do"})
	}
	ctx := context.Background()
	// here is empty: nothing published on this machine is "the caller", because the caller is on
	// another one. Passing our own socket would be inventing an identity for a stranger.
	target, refused := companion.Target(ctx, r, nil, configDir, "", req.To)
	if refused != "" {
		return writeJSON(out, errOut, handReply{Refused: refused})
	}
	// Where their log stands BEFORE the work goes in. The answer is the first turn that finishes
	// past this point, and taken afterwards it would already include the turn being started.
	since, _, err := r.NewSince(ctx, session.SessionID(target.Session), 0)
	if err != nil {
		return writeJSON(out, errOut, handReply{Refused: fmt.Sprintf(
			"%s's transcript cannot be read here, so an answer could not be found again: %v",
			target.Name, err)})
	}
	label := req.Label
	if label == "" {
		// An arrival with no label still gets one. A request with no attribution is indistinguishable
		// from something the person typed, and the no-chaining rule is read off exactly this mark.
		label = fleet.DispatchedFrom(orSomebody(req.From), "")
	}
	if serr := companion.Send(ctx, target, label, req.Request); serr != nil {
		return writeJSON(out, errOut, handReply{Refused: serr.Error()})
	}
	return writeJSON(out, errOut, handReply{
		Name: target.Name, Workdir: target.Workdir, Session: target.Session, Since: since,
	})
}

type stateRequest struct {
	Session string `json:"session"`
	Since   int64  `json:"since"`
}

type stateReply struct {
	Done   bool   `json:"done,omitempty"`
	Answer string `json:"answer,omitempty"`
	News   string `json:"news,omitempty"`
	Over   bool   `json:"over,omitempty"`
}

// handoffStateHere answers what became of work handed to this machine.
//
// The finished answer comes from app.AnswerSince — the same code a local wait reads a peer's log
// with. Not a second definition of "they finished and this is what they said": that phrase has
// three edge cases in it (a turn that ended, the LAST assistant text, and a turn that finished
// saying nothing), and two implementations of it would differ on one of them.
func handoffStateHere(in io.Reader, out, errOut io.Writer, a *app.App, configDir string) int {
	var req stateRequest
	if err := readJSON(in, &req); err != nil {
		fmt.Fprintln(errOut, "magi:", err)
		return 1
	}
	if req.Session == "" {
		fmt.Fprintln(errOut, "magi: no session named")
		return 1
	}
	ctx := context.Background()
	if done, answer := a.AnswerSince(ctx, session.SessionID(req.Session), req.Since); done {
		return writeJSON(out, errOut, stateReply{Done: true, Answer: answer})
	}
	// Not finished. Then the other question: is anybody still doing it, or is this silence
	// permanent — which is the whole reason the waiting side asks at all.
	list, err := fleet.List(ctx, a, configDir, "")
	if err != nil {
		return writeJSON(out, errOut, stateReply{})
	}
	news, over := companion.StateOf(list, req.Session)
	return writeJSON(out, errOut, stateReply{News: news, Over: over})
}

// aboutHere describes a companion published on this machine, for somebody who cannot see it.
//
// The third door, and the read-only one. It answers the question a roster line raises and does not
// answer: the line names what a companion can do, and this says what each of those names means.
//
// Names travel on the wire with every sighting because a roster has to be readable without asking
// anybody. Descriptions do not, because they would be a hundred bytes per skill per member on
// every exchange, every minute, to fill a line that shows names. So they are fetched, once, by
// whoever actually wants to know — which is this.
func aboutHere(in io.Reader, out, errOut io.Writer, r fleet.Reader, skills func(string) []port.Skill,
	reach func(string) []string, configDir, who string) int {

	if strings.TrimSpace(who) == "" {
		fmt.Fprintln(errOut, "magi: --about needs the name of a companion here")
		return 1
	}
	list, err := fleet.List(context.Background(), r, configDir, "")
	if err != nil {
		fmt.Fprintln(errOut, "magi:", err)
		return 1
	}
	var found *fleet.Agent
	for i, a := range list {
		// By name only, and exactly. Resolve's role and team matching is for a person or a model
		// choosing somebody; this is a machine asking about one it has already chosen, and a
		// near-match would answer about the wrong companion without anybody noticing.
		if strings.EqualFold(a.Name, who) && a.State != fleet.Remote {
			found = &list[i]
			break
		}
	}
	if found == nil {
		fmt.Fprintf(errOut, "magi: nothing called %q is published here\n", who)
		return 1
	}
	card := mcpserve.Card{Name: found.Name, Role: found.Role, Team: found.Team, Hub: found.Hub,
		Workdir: found.Workdir}
	if skills != nil {
		card.Skills = skills(found.Workdir)
	}
	if reach != nil {
		card.Reach = reach(found.Workdir)
	}
	if _, werr := io.WriteString(out, mcpserve.Describe(card)); werr != nil {
		fmt.Fprintln(errOut, "magi:", werr)
		return 1
	}
	return 0
}

// describeCompanion answers "what can X do" for a companion anywhere in the cluster.
//
// The local branch is in-process and not a subprocess of ourselves: it is the same call --about
// makes, and spawning a copy of this binary to ask a question this one can answer would be a
// process per call to arrive at the identical string.
//
// One decision about where a companion is, made here, the way cluster.Reach makes it for a peer
// command — a hostname that is this machine is not a network hop, and going through ssh to reach
// yourself fails on most hosts.
func describeCompanion(r fleet.Reader, skills func(string) []port.Skill, reach func(string) []string,
	configDir string) func(context.Context, string, string) (string, error) {

	here := daemon.Host()
	return func(ctx context.Context, host, name string) (string, error) {
		if host == "" || (here != "" && strings.EqualFold(host, here)) {
			var out, errOut bytes.Buffer
			if code := aboutHere(nil, &out, &errOut, r, skills, reach, configDir, name); code != 0 {
				return "", fmt.Errorf("%s", firstLineOf(strings.TrimSpace(errOut.String())))
			}
			return out.String(), nil
		}
		cctx, cancel := context.WithTimeout(ctx, crossAsk)
		defer cancel()
		said, err := sshCross(cctx, host, []string{"--about", name}, nil)
		if err != nil {
			return "", err
		}
		return string(said), nil
	}
}

// crossAsk bounds one description fetch. Shorter than a hand-off crossing: this runs while a model
// waits on a tool call, and a question about what somebody can do is not worth half a minute.
const crossAsk = 15 * time.Second

// sshCross runs a magi subcommand on another machine.
//
// BatchMode, always: every caller of this is a tool call or a timer, and neither has a terminal for
// ssh to ask a passphrase on. Without it the call hangs until its context kills it.
func sshCross(ctx context.Context, host string, args []string, stdin []byte) ([]byte, error) {
	argv := append([]string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=10", host, "magi"}, args...)
	cmd := exec.CommandContext(ctx, "ssh", argv...)
	cmd.Stdin = bytes.NewReader(stdin)
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	said, err := cmd.Output()
	if err != nil {
		if msg := firstLineOf(errBuf.String()); msg != "" {
			return nil, fmt.Errorf("%w: %s", err, msg)
		}
		return nil, err
	}
	return said, nil
}

func readJSON(in io.Reader, v any) error {
	if in == nil {
		return fmt.Errorf("nothing on stdin")
	}
	b, err := io.ReadAll(in)
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return fmt.Errorf("nothing on stdin")
	}
	return json.Unmarshal(b, v)
}

// writeJSON prints one reply and reports the exit code.
//
// A refusal exits 0. It is an answer to the question that was asked — this companion cannot take
// the work, and here is why — and a non-zero exit would make ssh look like it failed, which is a
// different thing the caller must be able to tell apart.
func writeJSON(out, errOut io.Writer, v any) int {
	b, err := json.Marshal(v)
	if err != nil {
		fmt.Fprintln(errOut, "magi:", err)
		return 1
	}
	if _, err := out.Write(append(b, '\n')); err != nil {
		fmt.Fprintln(errOut, "magi:", err)
		return 1
	}
	return 0
}

func orSomebody(s string) string {
	if s == "" {
		return "another companion"
	}
	return s
}

func firstLineOf(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			return s[:i]
		}
	}
	return s
}
