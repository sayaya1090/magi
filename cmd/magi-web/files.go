package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/app"
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
// treeDirCap bounds one request. A tree with more open directories than this is not a tree
// somebody is reading; the cap keeps a crafted or runaway request from turning one HTTP call into
// an unbounded walk of the workspace.
const treeDirCap = 64

// files answers one directory — or, when the caller names several, all of them in ONE reply.
//
// The tree used to ask per directory: the page walked the open ones and awaited each in turn, so a
// reader with eight directories open paid eight HTTP round trips AND eight acquisitions of the
// companion's pooled connection, which is held across a whole round trip (see withClient — a call
// behind a running model was measured at 2.7s against 0.6ms idle). The listings themselves are one
// os.ReadDir each and were never the cost. Reported from a work machine as "the file browser is
// slow"; on a fast local disk the same shape is invisible, which is why it lasted.
//
// So the client sends the subtree it has open and gets it back keyed by path, having taken the
// lock once. A path that cannot be read answers with its own empty list rather than failing the
// batch: one unreadable directory must not blank the tree around it.
func (s *server) files(w http.ResponseWriter, r *http.Request) {
	if s.forwarded(w, r, s.proxy) {
		return
	}
	// "." rather than "" so a request with no path means the workspace root, which is what a tree
	// asks for first. The tool resolves it against the workdir and refuses anything above it.
	want := []string{}
	seen := map[string]bool{}
	for _, p := range r.URL.Query()["path"] {
		p = strings.TrimSpace(p)
		if p == "" {
			p = "."
		}
		if !seen[p] && len(want) < treeDirCap {
			seen[p] = true
			want = append(want, p)
		}
	}
	if len(want) == 0 {
		want = []string{"."}
	}
	dirs := make(map[string]json.RawMessage, len(want))
	var firstErr error
	err := s.browse(r, func(cl *daemon.Client, _ session.SessionID) error {
		for _, p := range want {
			args, merr := json.Marshal(map[string]string{"path": p})
			if merr != nil {
				return merr
			}
			out, terr := cl.ReadOnlyTool("list", args)
			if terr != nil {
				// Remembered, not returned: the FIRST failure decides the status when nothing at
				// all could be read, and a later directory that reads fine still travels.
				if firstErr == nil {
					firstErr = terr
				}
				dirs[p] = json.RawMessage("[]")
				continue
			}
			if strings.TrimSpace(out) == "" {
				out = "[]"
			}
			// The tool answers with JSON already — [{"name":…,"isDir":…}] — so it is carried as it
			// came rather than decoded and re-encoded into a shape this file would then own.
			dirs[p] = json.RawMessage(out)
		}
		return nil
	})
	if err == nil && firstErr != nil && len(want) == 1 {
		err = firstErr
	}
	if err != nil {
		// The companion's own words, at the status of a request that named something wrong: a path
		// that is not there, or one outside the workspace. Both are the caller's mistake and both
		// are already said in a sentence a person can act on.
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	body, merr := json.Marshal(map[string]any{"dirs": dirs})
	if merr != nil {
		http.Error(w, merr.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if _, werr := w.Write(body); werr != nil {
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

// save writes a file in the workspace, through the companion, and only for somebody who may.
//
// # Why this exists at all, when the reading side argued against writing
//
// It argued against writing WITHOUT A RECORD. A console that quietly put bytes into a workspace
// would be doing the agent's work with none of the agent's account of it: the next turn would
// overwrite the change or build on it, and nothing anywhere would say a person had been in there.
// The companion writes the edit into its own log as the person's own words before this answers, so
// the change is in the transcript and in front of the model on its next turn. That is what makes it an edit rather than a surprise.
//
// # Why `shell` and not a capability of its own
//
// Because a new capability here would be a smaller power with a bigger name. Anybody with `shell`
// can already write any file in that workspace — `echo … > file` is one command — so putting this
// behind the same gate widens nothing, while inventing `edit` would leave two capabilities that
// permit the same act and a roles file where granting one and refusing the other means nothing.
func (s *server) save(w http.ResponseWriter, r *http.Request) {
	if postOnly(w, r) {
		return
	}
	if s.forwarded(w, r, s.proxy) {
		return
	}
	path := strings.TrimSpace(r.FormValue("path"))
	text := r.FormValue("text")
	if path == "" {
		http.Error(w, "which file", http.StatusBadRequest)
		return
	}
	// Whether the companion should answer, rather than only be told. The person's call, per action:
	// a console that always answered would spend a turn on every click of a stage button, and one
	// that never did would make somebody wanting a second opinion go and type "look at it".
	ask := r.FormValue("ask") != ""
	// A PATCH when the page could make one, and the whole file otherwise.
	//
	// The patch is the smaller thing to send for a large file, and it is also the only version of
	// this that can refuse: it carries the context around each change, so a file the agent has
	// edited since the person opened it no longer matches and git says so. A whole-file save has
	// nothing to disagree with — the last writer simply wins, silently.
	//
	// The page falls back to the whole file where a patch means nothing: a new file, or a workspace
	// that is not a checkout and so has no git to apply one.
	if patch := r.FormValue("patch"); strings.TrimSpace(patch) != "" {
		if derr := s.withClient(r, func(cl *daemon.Client, _ session.SessionID) error {
			return cl.PatchFile(path, patch, ask)
		}); derr != nil {
			// 409, not 400: this is not a malformed request, it is two people having changed the
			// same thing — and the page draws it as that rather than as a failure to save.
			http.Error(w, derr.Error(), http.StatusConflict)
			return
		}
		writeText(w, "")
		return
	}
	args, err := json.Marshal(map[string]string{"path": path, "content": text})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var out string
	if derr := s.withClient(r, func(cl *daemon.Client, _ session.SessionID) error {
		text, terr := cl.WriteTool("write", args, ask)
		out = text
		return terr
	}); derr != nil {
		http.Error(w, derr.Error(), http.StatusBadRequest)
		return
	}
	writeText(w, out)
}

// git answers what git makes of the companion's workspace: the branch, and what is not committed.
//
// Read-only and fixed: what runs on the far side is `git status --porcelain=v2 --branch` with the
// workspace as its directory and nothing from this request anywhere in it.
func (s *server) git(w http.ResponseWriter, r *http.Request) {
	if s.forwarded(w, r, s.proxy) {
		return
	}
	var out string
	if derr := s.browse(r, func(cl *daemon.Client, _ session.SessionID) error {
		text, gerr := cl.Git()
		out = text
		return gerr
	}); derr != nil {
		// A daemon too old to answer, or one whose workspace is not a checkout, is not an error
		// page: the pane shows nothing rather than a failure about a section nobody asked for.
		writeJSON(w, "git", app.GitState{})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if strings.TrimSpace(out) == "" {
		out = "{}"
	}
	if _, werr := w.Write([]byte(out)); werr != nil {
		log.Printf("magi-web: writing the git state: %v", werr)
	}
}

// prFacts is what a pull request from this companion would carry — the base, the commits and the
// difference — so the screen can show it while somebody writes the request.
func (s *server) prFacts(w http.ResponseWriter, r *http.Request) {
	if s.forwarded(w, r, s.proxy) {
		return
	}
	var out string
	if derr := s.browse(r, func(cl *daemon.Client, _ session.SessionID) error {
		said, ferr := cl.PRFacts()
		out = said
		return ferr
	}); derr != nil {
		http.Error(w, derr.Error(), http.StatusBadGateway)
		return
	}
	// Passed through as the JSON the daemon produced: this console does not know the shape and has
	// no reason to learn it.
	w.Header().Set("Content-Type", "application/json")
	if strings.TrimSpace(out) == "" {
		out = "{}"
	}
	if _, werr := w.Write([]byte(out)); werr != nil {
		log.Printf("magi-web: writing what a pull request would carry: %v", werr)
	}
}

// prMsg is the model's draft of that request. It opens nothing.
func (s *server) prMsg(w http.ResponseWriter, r *http.Request) {
	if postOnly(w, r) {
		return
	}
	if s.forwarded(w, r, s.proxy) {
		return
	}
	rules := r.FormValue("rules") // a one-off house-rules override for this draft only
	var out string
	if derr := s.alone(r, func(cl *daemon.Client, _ session.SessionID) error {
		said, merr := cl.DraftPR(rules)
		out = said
		return merr
	}); derr != nil {
		http.Error(w, derr.Error(), http.StatusBadGateway)
		return
	}
	writeText(w, out)
}

// gitPR pushes the branch and opens a pull request, answering with the URL it got back.
func (s *server) gitPR(w http.ResponseWriter, r *http.Request) {
	if postOnly(w, r) {
		return
	}
	if s.forwarded(w, r, s.proxy) {
		return
	}
	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		http.Error(w, "a pull request needs a title", http.StatusBadRequest)
		return
	}
	var url string
	if derr := s.alone(r, func(cl *daemon.Client, _ session.SessionID) error {
		out, perr := cl.OpenPR(title, r.FormValue("body"))
		url = out
		return perr
	}); derr != nil {
		// The refusals here are the interesting half — no gh, a rejected push, a request that
		// already exists — so they are passed through rather than replaced with a status line.
		http.Error(w, derr.Error(), http.StatusConflict)
		return
	}
	writeText(w, url)
}

// gitMsg is a commit message drafted from what is staged. It commits nothing.
//
// Beside look() rather than beside the git routes, because it is the same kind of thing: the
// companion's own model, asked a question outside the session, answering to the person who asked.
func (s *server) gitMsg(w http.ResponseWriter, r *http.Request) {
	if postOnly(w, r) {
		return
	}
	if s.forwarded(w, r, s.proxy) {
		return
	}
	rules := r.FormValue("rules") // a one-off house-rules override for this draft only
	var out string
	if derr := s.alone(r, func(cl *daemon.Client, _ session.SessionID) error {
		said, merr := cl.DraftCommit(rules)
		out = said
		return merr
	}); derr != nil {
		http.Error(w, derr.Error(), http.StatusBadGateway)
		return
	}
	writeText(w, out)
}

// look asks the companion's model what it makes of a file somebody is editing.
//
// A POST because it carries a buffer, and behind `prompt` because it spends the model: it is not a
// change to anything, but it is work asked of the backend on somebody's behalf, which is what that
// capability is about. Read-only in every other sense — nothing is written, nothing is recorded,
// and no turn is started.
func (s *server) look(w http.ResponseWriter, r *http.Request) {
	if postOnly(w, r) {
		return
	}
	if s.forwarded(w, r, s.proxy) {
		return
	}
	path := strings.TrimSpace(r.FormValue("path"))
	text := r.FormValue("text")
	if strings.TrimSpace(text) == "" {
		writeText(w, "")
		return
	}
	var out string
	if derr := s.alone(r, func(cl *daemon.Client, _ session.SessionID) error {
		said, lerr := cl.LookOver(path, text)
		out = said
		return lerr
	}); derr != nil {
		http.Error(w, derr.Error(), http.StatusBadGateway)
		return
	}
	writeText(w, out)
}

// complete answers inline completion text at the cursor in a file the person is editing.
//
// The same relay as look: this process holds no model (main.go builds its reader with a nil
// provider), so the completion is one call forwarded to the companion daemon, on the fast profile
// the daemon routes it to. Through s.alone, like /look and /git-msg, so a slow generation does not
// hold the pooled connection. path in the query is the file; prefix and suffix are the buffer either
// side of the cursor.
func (s *server) complete(w http.ResponseWriter, r *http.Request) {
	if postOnly(w, r) {
		return
	}
	if s.forwarded(w, r, s.proxy) {
		return
	}
	path := strings.TrimSpace(r.FormValue("path"))
	prefix := r.FormValue("prefix")
	suffix := r.FormValue("suffix")
	if strings.TrimSpace(prefix)+strings.TrimSpace(suffix) == "" {
		writeText(w, "")
		return
	}
	var out string
	var why app.CompleteReason
	if derr := s.alone(r, func(cl *daemon.Client, _ session.SessionID) error {
		said, reason, cerr := cl.CompleteCode(path, prefix, suffix)
		out, why = said, reason
		return cerr
	}); derr != nil {
		http.Error(w, derr.Error(), http.StatusBadGateway)
		return
	}
	// WHICH empty this is, for an editor that wants to say so. The body is unchanged — the ghost
	// text is the body and an empty one still means "insert nothing" — so a console that ignores
	// this header behaves exactly as before. It carries the distinction the daemon now makes:
	// "unrouted" is a configuration mistake that will never produce a completion, and it used to be
	// indistinguishable from a model that simply had nothing to suggest. A completion that FAILED
	// is not here at all; it is the 502 above.
	if why != app.CompleteProduced {
		w.Header().Set("X-Magi-Complete", string(why))
	}
	writeText(w, out)
}

// openFile tells the companion which file the editor has open and its unsaved buffer, so the agent's
// next turn sees it as ambient context (app.SetOpenFile). Nothing is generated or recorded — it
// stores a string — so it goes through the pooled client rather than s.alone. An empty buffer clears
// it, which is how the editor says a file was closed.
func (s *server) openFile(w http.ResponseWriter, r *http.Request) {
	if postOnly(w, r) {
		return
	}
	if s.forwarded(w, r, s.proxy) {
		return
	}
	path := strings.TrimSpace(r.FormValue("path"))
	text := r.FormValue("text")
	if derr := s.withClient(r, func(cl *daemon.Client, _ session.SessionID) error {
		return cl.SetOpenFile(path, text)
	}); derr != nil {
		http.Error(w, derr.Error(), http.StatusBadGateway)
		return
	}
	writeText(w, "")
}

// suggest answers the composer's ghost text: how the person is likely to finish the instruction
// they are typing. Same relay as /complete — this process holds no model — through s.alone so a slow
// suggestion does not hold the pooled connection. prefix is what they have typed so far.
func (s *server) suggest(w http.ResponseWriter, r *http.Request) {
	if postOnly(w, r) {
		return
	}
	if s.forwarded(w, r, s.proxy) {
		return
	}
	prefix := r.FormValue("prefix")
	var out string
	if derr := s.alone(r, func(cl *daemon.Client, _ session.SessionID) error {
		said, serr := cl.Suggest(prefix)
		out = said
		return serr
	}); derr != nil {
		http.Error(w, derr.Error(), http.StatusBadGateway)
		return
	}
	writeText(w, out)
}

// diff answers what changed in one file, as git wrote it.
//
// `which` says WHICH question: what a commit would take (staged), what has changed since it was
// staged (the default), or a file git does not know about yet (untracked), which is compared with
// nothing so that every line reads as the addition it is.
func (s *server) diff(w http.ResponseWriter, r *http.Request) {
	if s.forwarded(w, r, s.proxy) {
		return
	}
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	which := strings.TrimSpace(r.URL.Query().Get("which"))
	var out string
	if derr := s.browse(r, func(cl *daemon.Client, _ session.SessionID) error {
		text, gerr := cl.GitDiff(path, which)
		out = text
		return gerr
	}); derr != nil {
		http.Error(w, derr.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, "diff", map[string]string{"path": path, "which": which, "text": out})
}

// fileDo makes, moves or removes a file in the workspace.
//
// Behind `shell` like the other two that change something there, and for the same reason: anybody
// who may run a command in that workspace can already do all of this with one line, so this widens
// nothing — and a second capability permitting the same act would make granting either meaningless.
func (s *server) fileDo(w http.ResponseWriter, r *http.Request) {
	if postOnly(w, r) {
		return
	}
	if s.forwarded(w, r, s.proxy) {
		return
	}
	what := strings.TrimSpace(r.FormValue("do"))
	path := strings.TrimSpace(r.FormValue("path"))
	to := strings.TrimSpace(r.FormValue("to"))
	ask := r.FormValue("ask") != ""
	if derr := s.withClient(r, func(cl *daemon.Client, _ session.SessionID) error {
		return cl.FileDo(what, path, to, ask)
	}); derr != nil {
		http.Error(w, derr.Error(), http.StatusBadRequest)
		return
	}
	writeText(w, "")
}

// gitDo runs one of the four git commands the pane offers, in the companion's workspace.
//
// Behind `shell` for the reason saving is: anybody who may run a command in that workspace can
// already run these, so this widens nothing — and it is the capability whose name already means
// "acts in the workspace outside the approval policy a tool call goes through".
func (s *server) gitDo(w http.ResponseWriter, r *http.Request) {
	if postOnly(w, r) {
		return
	}
	if s.forwarded(w, r, s.proxy) {
		return
	}
	what := strings.TrimSpace(r.FormValue("do"))
	path := strings.TrimSpace(r.FormValue("path"))
	message := strings.TrimSpace(r.FormValue("message"))
	var out string
	if derr := s.withClient(r, func(cl *daemon.Client, _ session.SessionID) error {
		said, gerr := cl.GitDo(what, path, message, r.FormValue("ask") != "")
		out = said
		return gerr
	}); derr != nil {
		// git's own words, at the status of a request that asked for something it would not do.
		http.Error(w, derr.Error(), http.StatusBadRequest)
		return
	}
	writeText(w, out)
}

// askCompanion runs one read-only tool on the companion this request names.
// browse runs a READ-ONLY look at a companion — the tree, the git state, a file, a search — on a
// connection of its own.
//
// The pooled client holds one mutex across a whole round trip, so on that connection every card of
// the workspace pane queues behind every other one AND behind whatever the companion is doing:
// measured in withClient, a listing that costs 0.6ms idle took 2.7s with a model in flight. The
// daemon already serves each connection in its own goroutine, so the cards genuinely run at the
// same time once they stop sharing one — which is what makes the tree and the git card two
// parallel requests rather than two halves of one wait.
//
// Read-only by rule: nothing that WRITES goes here. A write wants the pooled client's reconnect
// (a socket the daemon has replaced) and wants to be serialized against the other writes; a look
// wants neither, and a look that fails is answered by the next poll a moment later.
func (s *server) browse(r *http.Request, do func(*daemon.Client, session.SessionID) error) error {
	return s.alone(r, do)
}

func (s *server) askCompanion(r *http.Request, tool string, args json.RawMessage) (string, error) {
	var out string
	err := s.browse(r, func(cl *daemon.Client, _ session.SessionID) error {
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
