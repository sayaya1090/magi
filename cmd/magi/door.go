package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"strings"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
)

// The narrow way in from another machine.
//
// # What was there before
//
// `magi --relay <socket>` copies bytes between ssh and a daemon socket, and it is deliberately
// dumb: the daemon at the far end applies the rules, which is one place instead of two. That is
// right when the person on the other side of the ssh connection is you.
//
// It stops being right when it is somebody else. `--relay` carries the WHOLE protocol — submit,
// steer, interrupt, set-model, shell — so an ssh key restricted to `command="magi --relay …"`
// still hands over everything the daemon can do. And the socket is whatever argv says, so the
// same key can be pointed at any unix socket on the machine, magi's or not.
//
// # What this is
//
// The same pipe with two rules:
//
//   - the caller may only reach a companion this account has PUBLISHED, named by its socket, which
//     is checked against the record beside it rather than taken on trust;
//   - only three methods cross — ask what a companion is, hand it work, ask how that work went.
//     Everything else is refused here, without being forwarded, in the daemon's own error shape.
//
// So an authorized_keys line can now mean "this key may ask my companions for things" instead of
// "this key is me".
//
//	command="magi --fleet-door",restrict,no-pty ssh-ed25519 AAAA… lee@laptop
//
// The forced command is what makes it a door: ssh ignores whatever the client asked to run, so the
// caller cannot choose the flags, the socket, or the program.
//
// # Why the socket arrives in the stream and not in argv
//
// Because of that same forced command: with one, argv is ours and not theirs. So the first line of
// the conversation says which companion is wanted, and that line is checked. It is also the reason
// the far side dials nothing until the caller has said something — a door that connects first and
// asks later has already spent the thing it was guarding.
const doorMethods = "about, hand, hand-state"

// doorOpen is the first line a caller sends: which companion, on this machine.
type doorOpen struct {
	Socket string `json:"socket"`
}

// doorAllows reports whether a request method may cross a fleet door.
//
// A list of what is allowed rather than of what is not. The protocol grows — three methods were
// added to it this month — and a deny-list would have silently let each new one through.
func doorAllows(method string) bool {
	switch method {
	case "about", "hand", "hand-state":
		return true
	}
	return false
}

// fleetDoor serves one connection from another machine: read the companion it wants, check it is
// ours, then carry the three methods and refuse the rest.
func fleetDoor(in io.Reader, out io.Writer, errOut io.Writer, configDir string) int {
	br := bufio.NewReader(in)
	first, err := br.ReadString('\n')
	if err != nil && strings.TrimSpace(first) == "" {
		fmt.Fprintln(errOut, "magi: the fleet door was opened and nothing was said")
		return 1
	}
	var want doorOpen
	if jerr := json.Unmarshal([]byte(first), &want); jerr != nil || strings.TrimSpace(want.Socket) == "" {
		fmt.Fprintln(errOut, `magi: a fleet door opens with {"socket":"…"} naming a companion here`)
		return 1
	}
	socket, ok := publishedHere(configDir, want.Socket)
	if !ok {
		// Named and refused, without saying what else is on this machine: a caller that may not
		// reach a companion may not enumerate them either.
		fmt.Fprintf(errOut, "magi: no companion of this account answers at %s\n", want.Socket)
		return relayNoDaemon
	}
	conn, err := net.Dial("unix", socket)
	if err != nil {
		fmt.Fprintf(errOut, "magi: cannot reach the daemon at %s: %v\n", socket, err)
		return relayNoDaemon
	}
	defer conn.Close()
	return doorCarry(br, out, errOut, conn)
}

// doorCarry carries one request at a time and waits for its answer before reading the next.
//
// Strictly alternating, which is not a restriction but a description: every method this door
// carries is one request and one reply — hand it work, ask what it is, ask how the work went. The
// one method that streams is `watch`, and that is not on the list.
//
// Written the obvious way once that is noticed. It began as two io.Copy goroutines with a lock
// between them, because a refusal written here could land in the middle of an answer coming back
// — and a corrupted line is a broken reply to a request that was fine. With one goroutine there is
// no second writer to serialize, no half-close to sequence, and nothing to get wrong under load.
func doorCarry(in *bufio.Reader, out io.Writer, errOut io.Writer, conn net.Conn) int {
	enc := json.NewEncoder(conn)
	answers := bufio.NewReader(conn)
	for {
		line, err := in.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			var req daemon.Request
			switch {
			case json.Unmarshal(line, &req) != nil:
				if werr := writeJSONLine(out, daemon.Response{Err: "malformed request"}); werr != nil {
					return 1
				}
			case !doorAllows(req.Method):
				// Refused before it is forwarded, so a method this door does not carry never
				// reaches the daemon at all.
				if werr := writeJSONLine(out, daemon.Response{Err: "a fleet door carries " + doorMethods +
					" and nothing else; " + req.Method + " has to be asked for on that machine"}); werr != nil {
					return 1
				}
			default:
				if werr := enc.Encode(req); werr != nil {
					fmt.Fprintf(errOut, "magi: could not pass it on: %v\n", werr)
					return 1
				}
				answer, rerr := answers.ReadBytes('\n')
				if len(answer) > 0 {
					if _, werr := out.Write(answer); werr != nil {
						return 1
					}
				}
				if rerr != nil {
					// The daemon has gone. Said to the caller in its own vocabulary rather than by
					// closing the pipe, which reads as "the far machine is broken" — and if that
					// cannot be delivered either, the caller is gone too and there is nobody left
					// to tell, which is the same exit.
					if len(answer) == 0 {
						if werr := writeJSONLine(out, daemon.Response{
							Err: "that companion stopped answering"}); werr != nil {
							return relayNoDaemon
						}
					}
					return relayNoDaemon
				}
			}
		}
		if err != nil {
			return 0
		}
	}
}

// writeJSONLine answers the caller directly, in the shape its client already parses.
//
// The error is returned rather than dropped: this writes to the pipe the caller is reading, so a
// failure here is the crossing being gone — which is the one thing the loop above needs to know to
// stop talking to a daemon on behalf of somebody who left.
func writeJSONLine(out io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = out.Write(append(b, '\n'))
	return err
}

// publishedHere resolves a caller's socket to one this account actually published.
//
// The path is matched against the record list rather than dialled directly, which is the check
// `--relay` never had: with a forced command the socket is the only thing a caller still chooses,
// and without this it could choose any unix socket on the machine — another program's, or another
// account's if the permissions were ever loosened. Compared by base name as well as by path so a
// caller does not have to know where this account keeps its config directory.
func publishedHere(configDir, want string) (string, bool) {
	list, err := daemon.List(configDir)
	if err != nil {
		return "", false
	}
	base := filepath.Base(strings.TrimSpace(want))
	for _, in := range list {
		if in.Socket == want || filepath.Base(in.Socket) == base {
			return in.Socket, true
		}
	}
	return "", false
}
