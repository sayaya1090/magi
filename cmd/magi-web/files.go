package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/core/session"
)

// The workspace a companion works in, read through the companion.
//
// # Why this process does not open the files
//
// It very often cannot. A console is read over an ssh tunnel from another machine as a matter of
// routine, and a peer's companions are on a third one; the path in a row is a path on THEIR
// filesystem, and resolving it here would open whatever this machine happens to have at the same
// place — which, on two machines set up by one person, is frequently a real file, the wrong one.
//
// Where it could, it still should not. The daemon already confines every path to the workspace,
// jails symlinks that point out of it, numbers the lines and cuts off a file too big to show. A
// second implementation here would be a second answer that can disagree with the one the agent
// gets, and it would have to learn all of that again.
//
// So both routes are one call to the companion's own read-only tools — see daemon.ToolReader. The
// allowlist that keeps them read-only is on the daemon's side, not here: there is more than one
// thing in front of a daemon and only one of it.
//
// # Reading, and nothing else
//
// There is no route here that writes. Editing a workspace is what the companion is for, and a
// console that quietly did it itself would be doing the agent's work without the agent's log,
// its approval policy, or its account of why.

// files answers what is in one directory of the workspace: name and whether it is a directory.
func (s *server) files(w http.ResponseWriter, r *http.Request) {
	if s.forwarded(w, r, s.proxy) {
		return
	}
	// "." rather than "" so a request with no path means the workspace root, which is what a tree
	// asks for first. The tool resolves it against the workdir and refuses anything above it.
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		path = "."
	}
	args, err := json.Marshal(map[string]string{"path": path})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out, err := s.askCompanion(r, "list", args)
	if err != nil {
		// The companion's own words, at the status of a request that named something wrong: a path
		// that is not there, or one outside the workspace. Both are the caller's mistake and both
		// are already said in a sentence a person can act on.
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// The tool answers with JSON already — [{"name":…,"isDir":…}] — so it goes out as it came,
	// rather than being decoded and re-encoded into a shape this file would then own. Written
	// straight rather than through writeJSON, which would encode the string and deliver a quoted
	// blob the page would have to parse twice.
	w.Header().Set("Content-Type", "application/json")
	if strings.TrimSpace(out) == "" {
		out = "[]"
	}
	if _, werr := w.Write([]byte(out)); werr != nil {
		log.Printf("magi-web: writing a directory listing: %v", werr)
	}
}

// file answers with the contents of one file, as the companion's own read tool renders it.
func (s *server) file(w http.ResponseWriter, r *http.Request) {
	if s.forwarded(w, r, s.proxy) {
		return
	}
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		http.Error(w, "which file", http.StatusBadRequest)
		return
	}
	args, err := json.Marshal(map[string]string{"path": path})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out, err := s.askCompanion(r, "read", args)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// Line-numbered exactly as the agent sees it. A console that stripped the numbers to look
	// tidier would leave a person and their companion pointing at different line 40s.
	writeJSON(w, "file", map[string]string{"path": path, "text": out})
}

// askCompanion runs one read-only tool on the companion this request names.
func (s *server) askCompanion(r *http.Request, tool string, args json.RawMessage) (string, error) {
	var out string
	err := s.withClient(r, func(cl *daemon.Client, _ session.SessionID) error {
		text, terr := cl.ReadOnlyTool(tool, args)
		if terr != nil {
			return terr
		}
		out = text
		return nil
	})
	return out, err
}
