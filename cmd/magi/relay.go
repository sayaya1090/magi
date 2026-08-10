package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Carrying the daemon's own protocol across a machine, and nothing more.
//
// # Why a pipe and not a subcommand
//
// The first version of this crossing was three subcommands run over ssh — take work, say what
// became of it, describe yourself. Each was a fresh process that read a config directory, listed
// the daemons in it, resolved a name and dialled a local socket, to work out things the daemon it
// finally reached already knew about itself.
//
// Everything awkward came from that. Which user ssh landed as decided which config directory got
// read, so two accounts saw two different clusters and the symptom was "nobody here is called
// design". A container has its own filesystem, so there was nothing to read at all. And "is this
// host us" had to be decided over and over, in five places, from a hostname.
//
// A relay removes the question instead of answering it. Whoever opens the pipe has connected to one
// specific companion, so there is nothing to resolve: the socket IS the identity. A new kind of
// boundary — a container, a jump host, something not invented yet — is a new way to make a pipe and
// not a new way to ask a question.
//
// # It is deliberately stupid
//
// It copies bytes. It does not parse the protocol, does not know what a session is, and has no
// opinion about who may ask what. The daemon on the far end applies whatever rules there are, the
// same ones it applies to a terminal attached beside it — one place, not two.
//
// # Permission is the operating system's answer
//
// The socket is owner-only. A relay run by a user who does not own it fails at connect, with the
// system's own words, before a byte of protocol is exchanged. That is the right authority: magi
// deciding would be magi deciding to trust its own reading of who somebody is.

// relayHere pipes stdin and stdout to a daemon socket on this machine.
func relayHere(in io.Reader, out io.Writer, errOut io.Writer, socket string) int {
	if socket == "" {
		fmt.Fprintln(errOut, "magi: --relay needs the socket of a daemon here")
		return 1
	}
	conn, err := net.Dial("unix", socket)
	if err != nil {
		// Said plainly and not translated. "permission denied" is the answer when the daemon
		// belongs to another account, and it is more useful than anything magi could say instead.
		fmt.Fprintf(errOut, "magi: cannot reach the daemon at %s: %v\n", socket, err)
		return 1
	}
	defer conn.Close()

	// Only the DAEMON's side ending ends the relay.
	//
	// Not the first of the two, which is what this did first and is wrong in the ordinary case: a
	// client writes its request, closes stdin, and waits for the reply. Ending there closed the
	// answer's way home before it had been sent, and every exchange became a race the caller
	// usually lost. Caught by a test that echoed bytes back and got nothing.
	//
	// stdin ending is still news for the far side — it means no more requests are coming — so it
	// is passed on as a half-close rather than ignored, and a daemon reading for the next one
	// stops instead of holding the connection open.
	sent := make(chan error, 1)
	go func() {
		_, err := io.Copy(conn, in)
		if cw, ok := conn.(interface{ CloseWrite() error }); ok {
			// A half-close that fails changes nothing: the deferred Close below tells the daemon
			// the same thing a moment later. This is the one discarded return here that is
			// discarded because there is genuinely nothing to do about it.
			_ = cw.CloseWrite()
		}
		sent <- err
	}()

	if _, err := io.Copy(out, conn); err != nil {
		// A truncated reply is not a reply. Said rather than swallowed: the caller is a daemon
		// client waiting on a JSON line, and silence here becomes "the far side gave no answer",
		// which is true of a working companion and of a broken link alike.
		fmt.Fprintf(errOut, "magi: the connection to %s broke while reading: %v\n", socket, err)
		return 1
	}
	if err := <-sent; err != nil {
		fmt.Fprintf(errOut, "magi: the connection to %s broke while sending: %v\n", socket, err)
		return 1
	}
	return 0
}

// pipeTo opens a two-way pipe to a process, for the daemon protocol to run over.
//
// The command is built by the caller from ITS OWN template — ssh, docker exec, whatever reaches
// that machine. Nothing about it arrives over the network, which is the rule the whole cluster
// keeps: a hostname is data and a command line is code.
type pipe struct {
	cmd *exec.Cmd
	w   io.WriteCloser
	r   io.ReadCloser
}

func pipeTo(cmd *exec.Cmd) (*pipe, error) {
	w, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	r, err := cmd.StdoutPipe()
	if err != nil {
		w.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		w.Close()
		r.Close()
		return nil, err
	}
	return &pipe{cmd: cmd, w: w, r: r}, nil
}

func (p *pipe) Read(b []byte) (int, error)  { return p.r.Read(b) }
func (p *pipe) Write(b []byte) (int, error) { return p.w.Write(b) }

// Close ends the conversation and the process with it.
//
// Killed rather than waited for. The far side is a relay blocked reading a socket that will not
// close on its own, so waiting is waiting forever — and there is nothing to collect from it, since
// everything it had to say came back through the pipe.
func (p *pipe) Close() error {
	err := p.w.Close()
	if rerr := p.r.Close(); err == nil {
		err = rerr
	}
	if p.cmd.Process != nil {
		if kerr := p.cmd.Process.Kill(); kerr != nil && err == nil && !errors.Is(kerr, os.ErrProcessDone) {
			err = kerr
		}
	}
	// Wait's error is not reported and that is deliberate: it is "signal: killed" every time,
	// because the line above is what ended it. Called for the reaping, not the answer.
	_ = p.cmd.Wait()
	return err
}

// nameOr is what a companion is called: what it declared, or the base name of its workspace.
func nameOr(declared, workdir string) string {
	if n := strings.TrimSpace(declared); n != "" {
		return n
	}
	return filepath.Base(workdir)
}

// relayTo builds the pipe that reaches a companion's daemon on another machine.
//
// The command is assembled HERE, from this machine's own template, with nothing taken from the
// member entry but a hostname and the socket path it answers on. That is the rule the whole cluster
// keeps and the reason a member cannot make anybody run anything it chose.
//
// The context is the only bound on the crossing. A pipe has no deadline to set, unlike a socket, so
// what stops a wedged link from holding a wait open is the process being killed — which is what
// CommandContext does when the caller's timeout expires.
//
// BatchMode, always: every caller is a tool call or a timer, and neither has a terminal for ssh to
// ask a passphrase on. Without it the call hangs until the context kills it.
//
// ssh only, for now. A container or a jump host is a different template and the same pipe — which
// is the point of the relay being dumb; see the note at the top of this file.
func relayTo(ctx context.Context, host, socket string) (*pipe, error) {
	if host == "" || socket == "" {
		return nil, errNoRelay
	}
	return pipeTo(exec.CommandContext(ctx, "ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10",
		host, "magi", "--relay", socket))
}

var errNoRelay = errors.New("no way to reach that machine")
