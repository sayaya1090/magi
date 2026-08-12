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

// findLimit is how many results travel. A grep over a repository somebody checked a video into
// answers with tens of thousands of lines, and a pane 18rem wide can show a few dozen — so the cut
// happens here, once, and the answer says it was cut rather than trailing off.
const findLimit = 200

// find searches the workspace: file names, or what is inside them.
//
// Two kinds because they are two different questions and two different costs. A name search is a
// walk of the directory entries; a content search reads every file in the tree. The caller says
// which, rather than this guessing from the shape of the query — a guess would make the expensive
// one happen by accident.
func (s *server) find(w http.ResponseWriter, r *http.Request) {
	if s.forwarded(w, r, s.proxy) {
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, "find", findAnswer{Hits: []string{}})
		return
	}
	tool, args := "glob", map[string]string{}
	if r.URL.Query().Get("in") == "text" {
		// The pattern goes to the tool as it was typed: grep takes a regular expression, that is
		// what the agent's own searches take, and quietly escaping it here would make the console's
		// search a different search from the one in the transcript. A bad expression comes back as
		// the tool's own complaint.
		tool, args = "grep", map[string]string{"pattern": q}
	} else {
		// A name search is "contains", because that is what somebody typing three letters of a
		// filename means. ** so it reaches the whole tree rather than the root.
		args = map[string]string{"pattern": "**/*" + globQuote(q) + "*"}
	}
	raw, err := json.Marshal(args)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out, err := s.askCompanion(r, tool, raw)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var hits []string
	if uerr := json.Unmarshal([]byte(out), &hits); uerr != nil {
		http.Error(w, "the companion answered something this console could not read",
			http.StatusBadGateway)
		return
	}
	ans := findAnswer{Hits: hits}
	if len(hits) > findLimit {
		ans.Hits, ans.More = hits[:findLimit], len(hits)-findLimit
	}
	writeJSON(w, "find", ans)
}

type findAnswer struct {
	// Hits are paths for a name search and "path:line:text" for a content one — the tools' own
	// shapes, passed through. The page knows which it asked for.
	Hits []string `json:"hits"`
	// More is how many were cut off, so the pane can say "and 3,412 more" instead of implying the
	// list is all there is.
	More int `json:"more,omitempty"`
}

// globQuote makes the glob metacharacters in a typed query mean themselves.
//
// Somebody typing "page[1]" is naming a file, not writing a character class — and an unclosed
// bracket is refused by the tool, so without this a perfectly reasonable filename comes back as a
// syntax error. The wildcards this function does NOT escape are the ones the caller wrapped the
// query in.
func globQuote(q string) string {
	var b strings.Builder
	for _, r := range q {
		if strings.ContainsRune(`*?[]\`, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}
