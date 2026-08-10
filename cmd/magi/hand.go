package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"

	"github.com/sayaya1090/magi/internal/adapter/fleet"
	"github.com/sayaya1090/magi/internal/adapter/tool/companion"
	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/session"
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
