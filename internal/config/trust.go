package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Which workspaces this machine will run things for.
//
// # What this is for
//
// A project's .magi/config.toml is committed, which is the point of it — the workflow travels with
// the repo — and which also means it arrives with a clone, written by whoever wrote the repo. Some
// of what it can say is a request (be read-only here, ask before everything) and is taken from
// anybody. The rest RUNS: a hook is a shell command on every tool event, an MCP server is a process
// the daemon spawns, base_url decides where the prompts go, and a [plugins.x] table can switch on a
// bundled plugin. Opening a directory was enough for all four.
//
// # Why a list and not a prompt
//
// A prompt on first open is the obvious shape and the one that fails: it arrives while somebody is
// trying to start work, it is the same yes/no every time, and the answer that gets clicked is yes.
// It also has no honest form in the two places this most matters — a daemon with no terminal, and a
// headless run — so it would have to default to something there anyway.
//
// A list is the same decision made where decisions belong: not in the way of the work, once per
// directory, in a file the operator can read and edit. It matches what the plugin loader already
// does — a workspace's plugins run when you NAME the directory — and gives that lever a memory.
//
// # Not a security boundary against yourself
//
// Nothing here stops a person from trusting a directory they should not have. It stops a directory
// from being trusted BY BEING OPENED, which is the step that took no decision at all.
const TrustFile = "trusted-workspaces"

// Trusted reports whether this workspace may run what its own config declares.
//
// Paths are compared after symlink resolution, the same way the daemon's socket path is derived, so
// /tmp and /private/tmp are one directory and not two — a trust granted through one spelling and
// checked through the other would silently mean nothing.
func Trusted(configDir, workdir string) bool {
	want := realPath(workdir)
	if want == "" {
		return false
	}
	for _, line := range trustLines(configDir) {
		if realPath(line) == want {
			return true
		}
	}
	return false
}

// Trust records a workspace, and reports whether it was already there.
//
// Appending rather than rewriting: this file is the operator's, comments and all, and a program
// that reformats what a person wrote is one they stop editing by hand.
func Trust(configDir, workdir string) (already bool, err error) {
	abs := realPath(workdir)
	if abs == "" {
		return false, fmt.Errorf("cannot resolve %s", workdir)
	}
	if Trusted(configDir, abs) {
		return true, nil
	}
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return false, err
	}
	f, err := os.OpenFile(filepath.Join(configDir, TrustFile),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return false, err
	}
	defer f.Close()
	_, err = fmt.Fprintln(f, abs)
	return false, err
}

// Untrust removes a workspace, and reports whether it was there to remove.
//
// The whole file is rewritten for this one, because there is no way to delete a line by appending.
// Comments and blank lines survive; only the matching entries go.
func Untrust(configDir, workdir string) (was bool, err error) {
	want := realPath(workdir)
	path := filepath.Join(configDir, TrustFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	var kept []string
	for _, line := range strings.Split(string(raw), "\n") {
		if t := strings.TrimSpace(line); t != "" && !strings.HasPrefix(t, "#") && realPath(t) == want {
			was = true
			continue
		}
		kept = append(kept, line)
	}
	if !was {
		return false, nil
	}
	return true, os.WriteFile(path, []byte(strings.Join(kept, "\n")), 0o600)
}

// TrustedList is every workspace on the list, for a person asking what they have allowed.
func TrustedList(configDir string) []string { return trustLines(configDir) }

func trustLines(configDir string) []string {
	f, err := os.Open(filepath.Join(configDir, TrustFile))
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out
}

// realPath is an absolute, symlink-resolved path, or "" when it cannot be one.
//
// A directory that does not exist still resolves to its absolute form: somebody may trust a
// workspace before creating it, and refusing that would be a rule about the order of two commands.
func realPath(p string) string {
	abs, err := filepath.Abs(strings.TrimSpace(p))
	if err != nil {
		return ""
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		return real
	}
	return abs
}
