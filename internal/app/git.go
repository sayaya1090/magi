package app

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// What git says about the workspace, for a screen that is showing it.
//
// # Why this is not a tool
//
// The registry is what the MODEL sees. Adding a tool called git_status would put it in front of
// every agent on every turn — more names to read, one more thing to reach for — to answer a
// question nobody asked it. The console is asking, so the console gets a method.
//
// # Why it is not the shell either
//
// `magi --shell` exists and would do this in one line, and that is exactly the door this must not
// use: a shell takes a string, and a string that arrives from a page is a command somebody else
// composed. Every call here is a FIXED argv — a program name and constant arguments — with the
// workspace as the working directory and nothing from the request in it. There is nothing to
// inject into, which is a property of the shape rather than of the escaping.
//
// # Why porcelain v2
//
// It is the format git documents as stable for programs, it carries the branch and the
// ahead/behind counts in the same answer as the file list, and it distinguishes staged from
// unstaged without a second call. Parsing `git status` prose is how a screen ends up wrong in
// somebody's locale.

// GitState is the branch a workspace is on and what has not been committed.
type GitState struct {
	// Repo is false for a workspace that is not a checkout at all, which is not an error: a
	// companion can perfectly well work in a directory nobody put under version control, and a
	// screen should say nothing rather than show an empty change list.
	Repo bool `json:"repo"`
	// Branch is the branch name, or "" when HEAD is detached — in which case Head carries the
	// commit. A screen that printed a short sha under the word "branch" would be teaching somebody
	// that a detached head is a branch, which is the state where that misunderstanding costs work.
	Branch string `json:"branch,omitempty"`
	Head   string `json:"head,omitempty"`
	// Upstream is the branch this one tracks, with how far apart they are. Zero and zero on a
	// branch with an upstream is worth showing — "in step" is news to somebody about to pull.
	Upstream string `json:"upstream,omitempty"`
	Ahead    int    `json:"ahead,omitempty"`
	Behind   int    `json:"behind,omitempty"`
	// Changes is every path git would mention, with what kind of change it is.
	Changes []GitChange `json:"changes"`
	// Branches is what this checkout could switch to, current one included. Local only: a screen
	// offering to switch to a remote branch is offering to create a tracking branch under a name
	// somebody did not choose, which is a different act with the same word on it.
	Branches []string `json:"branches,omitempty"`
}

// GitChange is one path and what has happened to it.
type GitChange struct {
	Path string `json:"path"`
	// Kind is "staged", "unstaged", "both", "untracked" or "conflict" — the five a person acts on
	// differently. Not the two-letter XY code: that is git's alphabet, and a console that showed
	// "RM" would be asking its reader to know it.
	Kind string `json:"kind"`
}

// GitFacts reads the workspace's git state, or reports that there is none.
func (a *App) GitFacts(ctx context.Context, workdir string) (GitState, error) {
	if a.plat == nil {
		return GitState{}, fmt.Errorf("platform unavailable")
	}
	res, err := a.plat.Exec(ctx, port.Cmd{
		Path: "git",
		// --porcelain=v2 for the machine-readable form, --branch for the header lines,
		// --untracked-files=normal so a new file appears without listing every file inside a new
		// directory, and -z is deliberately NOT used: paths with newlines in them are rare enough
		// that the simpler parse is the better trade for a pane that lists them.
		Args:      []string{"status", "--porcelain=v2", "--branch", "--untracked-files=normal"},
		Dir:       workdir,
		MaxOutput: gitCaptureCap,
	})
	if err != nil || res.ExitCode != 0 {
		// Not a checkout, no git on the machine, or a repository this account may not read. All
		// three are "there is nothing to show here", and none of them is a reason to fail the
		// screen that asked.
		return GitState{}, nil
	}
	st := parseGitStatus(string(res.Stdout))
	// The branches, in a second call because git says nothing about them in a status. Sorted by
	// how recently each was committed to, which is the order every IDE lists them in and the order
	// somebody is actually looking for: the one you were on yesterday is near the top.
	if b, berr := a.plat.Exec(ctx, port.Cmd{
		Path:      "git",
		Args:      []string{"for-each-ref", "--sort=-committerdate", "--format=%(refname:short)", "refs/heads/"},
		Dir:       workdir,
		MaxOutput: gitCaptureCap,
	}); berr == nil && b.ExitCode == 0 {
		for _, name := range strings.Split(strings.TrimSpace(string(b.Stdout)), "\n") {
			if name = strings.TrimSpace(name); name != "" {
				st.Branches = append(st.Branches, name)
			}
		}
	}
	return st, nil
}

// gitCaptureCap bounds the answer. A tree with a hundred thousand untracked files — a node_modules
// nobody ignored — would otherwise be read into memory to fill a pane that can show forty rows.
const gitCaptureCap = 1 << 20

func parseGitStatus(out string) GitState {
	st := GitState{Repo: true, Changes: []GitChange{}}
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "# branch.head "):
			name := strings.TrimSpace(strings.TrimPrefix(line, "# branch.head "))
			// git says "(detached)" here, which is a state and not a name.
			if name != "(detached)" {
				st.Branch = name
			}
		case strings.HasPrefix(line, "# branch.oid "):
			st.Head = shortSHA(strings.TrimSpace(strings.TrimPrefix(line, "# branch.oid ")))
		case strings.HasPrefix(line, "# branch.upstream "):
			st.Upstream = strings.TrimSpace(strings.TrimPrefix(line, "# branch.upstream "))
		case strings.HasPrefix(line, "# branch.ab "):
			st.Ahead, st.Behind = aheadBehind(strings.TrimPrefix(line, "# branch.ab "))
		case strings.HasPrefix(line, "? "):
			st.Changes = append(st.Changes, GitChange{Path: strings.TrimPrefix(line, "? "), Kind: "untracked"})
		case strings.HasPrefix(line, "u "):
			// Unmerged: the fields differ from an ordinary entry's, and the path is last.
			if f := strings.Fields(line); len(f) > 0 {
				st.Changes = append(st.Changes, GitChange{Path: f[len(f)-1], Kind: "conflict"})
			}
		case strings.HasPrefix(line, "1 "), strings.HasPrefix(line, "2 "):
			// "1 XY sub mH mI mW hH hI path", and a rename has one field more — the similarity
			// score — before "path\tfrom". Split to the field count that entry has, or the score
			// arrives welded to the front of the path and the pane lists a file called
			// "R100 cmd/…". Found by a test written from git's own documented format.
			fields := 9
			if line[0] == '2' {
				fields = 10
			}
			f := strings.SplitN(line, " ", fields)
			if len(f) < fields {
				continue
			}
			path := f[fields-1]
			if i := strings.IndexByte(path, '\t'); i >= 0 {
				path = path[:i] // a rename names both; the new path is the one to open
			}
			st.Changes = append(st.Changes, GitChange{Path: path, Kind: kindOf(f[1])})
		}
	}
	return st
}

// kindOf reads git's XY pair as the three states a person acts on differently: it is staged, it is
// not, or it is both — which is the one worth seeing, because committing now would commit half of
// what is on screen.
func kindOf(xy string) string {
	if len(xy) < 2 {
		return "unstaged"
	}
	staged, working := xy[0] != '.', xy[1] != '.'
	switch {
	case staged && working:
		return "both"
	case staged:
		return "staged"
	default:
		return "unstaged"
	}
}

func aheadBehind(s string) (int, int) {
	var ahead, behind int
	for _, f := range strings.Fields(s) {
		if len(f) < 2 {
			continue
		}
		n, err := strconv.Atoi(f[1:])
		if err != nil {
			continue
		}
		switch f[0] {
		case '+':
			ahead = n
		case '-':
			behind = n
		}
	}
	return ahead, behind
}

func shortSHA(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

// LookOver asks the model what it makes of a file somebody is editing, without writing anything.
//
// # Why this is not a turn
//
// A turn is the agent working: it has the session's history, its tools, its plan, and everything it
// does lands in the log. A half-typed buffer is none of that — it is a person asking "does this
// look right" over their own shoulder, twenty times in a minute, about a file that is not on disk
// yet. Putting that in the session would fill the context with drafts and leave the agent
// answering questions about work nobody has done.
//
// So it is one call to the same model with a small prompt and no tools, nothing recorded, nothing
// started. What comes back goes on the screen of the person who asked and nowhere else.
//
// # Why it says nothing rather than something
//
// A reviewer that always finds three things teaches people to stop reading it. The prompt asks for
// silence when there is nothing worth saying, and an empty answer is drawn as nothing at all — no
// panel, no "looks good", no green tick that has to be earned back.
func (a *App) LookOver(ctx context.Context, sid session.SessionID, path, text string) (string, error) {
	if strings.TrimSpace(text) == "" {
		return "", nil
	}
	// Bounded. A person can open a 40,000-line file in the console, and the buffer travels on every
	// pause in typing — an unbounded prompt here is somebody's context window and their bill.
	if len(text) > lookOverCap {
		text = text[:lookOverCap] + "\n… (the rest of this file was not sent)"
	}
	// The companion's own agent and model, so what answers is the thing the person is working with
	// — not a second opinion from whatever this process's default happens to be.
	s := a.sessionInfo(ctx, sid)
	agent := a.agentFor(s)
	// The lines arrive prefixed with their real (absolute) line number and a tab: the console sends
	// the region around the caret, not the whole file, and numbers it so a finding anchors to an
	// actual line rather than the model's count. The reply keeps that shape — `<line><TAB><clause>`,
	// one per finding — so the console can draw each remark against the line it is about instead of
	// a paragraph over the top of the file. See LookOver's caller (cmd/magi-web) and app_state's
	// lookLang for the language cache.
	sys := "A person is editing " + path + " and has not saved it. The lines below are the region " +
		"around where they are working; each is prefixed with its line number and a tab. Report " +
		"ONLY what is wrong, missing or about to break: a bug, a typo in an identifier, a case not " +
		"handled, something that contradicts the rest. At most three findings. Answer with each " +
		"finding on its own line as the line number, a tab, and one short clause and NOTHING else — " +
		"`<line-number><TAB><what is wrong>`, using the numbers shown. If there is nothing worth " +
		"saying, answer with an empty message — do not summarise, do not praise, do not suggest " +
		"style, do not add any line that is not a numbered finding."
	// The clause in the reader's language, the numbers untouched — the language directive keeps code,
	// identifiers and paths as-is, and a line number is none of those.
	if dir := a.lookLangDirective(ctx, sid); dir != "" {
		sys = dir + "\n\n" + sys
	}
	req := port.ChatRequest{
		Model:  s.Model.Model,
		System: sys,
		Messages: []session.Message{{
			Role:  session.RoleUser,
			Parts: []session.Part{{Kind: session.PartText, Text: text}},
		}},
	}
	stream, serr := a.providerFor(agent).StreamChat(ctx, req)
	if serr != nil {
		return "", serr
	}
	out, _ := drainStream(stream)
	return strings.TrimSpace(out), nil
}

// lookLangDirective returns the "reply in the reader's language" directive for LookOver, detected
// from this session's genuine user prompts and cached (see sessionState.lookLang for why this one
// caches where the turn loop's language lock does not). Empty means English/undetermined.
func (a *App) lookLangDirective(ctx context.Context, sid session.SessionID) string {
	a.mu.Lock()
	if st := a.stateLocked(sid); st.lookLangSet {
		dir := st.lookLang
		a.mu.Unlock()
		return dir
	}
	a.mu.Unlock()
	evs, _ := a.store.Read(ctx, sid, 0)
	dir := langDirective(lastUserPromptText(evs))
	a.mu.Lock()
	st := a.stateLocked(sid)
	st.lookLang, st.lookLangSet = dir, true
	a.mu.Unlock()
	return dir
}

// DraftCommit asks the companion's model for a commit message for what is staged.
//
// A draft, not a commit: nothing is written, nothing is run, and the person who asked gets text
// they can rewrite or throw away. The same shape as LookOver above, and for the same reason —
// this travels outside the session so a discarded draft does not become part of the conversation.
//
// The last few subjects in this repository go with it. A commit message is written in a house
// style — what goes in the first word, whether there is a scope, how long the subject runs — and
// a model shown five real examples matches it, where one told "be conventional" invents a
// convention. They are evidence from the repository rather than a rule somebody typed here.
func (a *App) DraftCommit(ctx context.Context, sid session.SessionID, workdir, rules string) (string, error) {
	diff, err := a.GitDiffOf(ctx, workdir, "", true, false)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(diff) == "" {
		// Nothing staged is not a failure: it is the answer, and a screen says it better than a
		// model would.
		return "", nil
	}
	if len(diff) > lookOverCap {
		diff = diff[:lookOverCap] + "\n… (the rest of this diff was not sent)"
	}
	house := ""
	if a.plat != nil {
		res, lerr := a.plat.Exec(ctx, port.Cmd{
			Path: "git", Args: []string{"log", "-n", "5", "--pretty=%s"}, Dir: workdir, MaxOutput: 4 << 10})
		if lerr == nil && res.ExitCode == 0 && strings.TrimSpace(string(res.Stdout)) != "" {
			house = "\n\nThe last five subjects in this repository, so the draft is written the way " +
				"this project writes them:\n" + strings.TrimSpace(string(res.Stdout))
		}
	}
	// The person's own house rules ([templates] commit), if any. Additive to the base prompt and
	// placed LAST so they win where they conflict — this is where "layer the commits", a required
	// trailer, or a banned word lives. The draft screen may hand a ONE-OFF override for this draft
	// only (rules); non-empty, it stands in for the saved template without changing it.
	tmpl := a.cfg.Templates.Commit
	if strings.TrimSpace(rules) != "" {
		tmpl = rules
	}
	ruleBlock := ""
	if r := strings.TrimSpace(tmpl); r != "" {
		ruleBlock = "\n\nThis project's own rules for commit messages, which take precedence over the " +
			"instructions above where they differ:\n" + r
	}
	s := a.sessionInfo(ctx, sid)
	agent := a.agentFor(s)
	req := port.ChatRequest{
		Model: s.Model.Model,
		System: "Write the commit message for the staged diff below. Answer with the message and " +
			"nothing else: no preamble, no code fences, no quotation marks. A subject line in the " +
			"imperative under 72 characters, then — only if the change needs it — a blank line and " +
			"a short body saying WHY. Describe only what the diff shows: no issue numbers, no " +
			"names, nothing you cannot see here." + house + ruleBlock,
		Messages: []session.Message{{
			Role:  session.RoleUser,
			Parts: []session.Part{{Kind: session.PartText, Text: diff}},
		}},
	}
	stream, serr := a.providerFor(agent).StreamChat(ctx, req)
	if serr != nil {
		return "", serr
	}
	out, _ := drainStream(stream)
	// A model that answers in a fence anyway is a model whose text is still usable; the fence is
	// not part of the message and would land in the log verbatim.
	return strings.Trim(strings.TrimSpace(out), "`\n "), nil
}

// PRState is what a pull request from this branch would carry: which branch it goes onto, what
// commits are on this one that are not on that one, and the whole difference between them.
//
// The three questions somebody writing a pull request has, answered before they start typing.
// Without them the console's PR box was a title and a body over nothing — the same shape the
// commit message had before the workbench, and it produced the same message: "update".
type PRState struct {
	// Repo is false where there is no checkout, and Base is empty where this console cannot tell
	// what the request would go onto — no remote, no origin/HEAD, nothing named main or master.
	// Both are said rather than guessed: opening a request against the wrong branch is not a thing
	// to find out afterwards.
	Repo    bool        `json:"repo"`
	Branch  string      `json:"branch,omitempty"`
	Base    string      `json:"base,omitempty"`
	Commits []GitCommit `json:"commits,omitempty"`
	// Diff is base...HEAD — what this branch changed, not what has happened on the base since it
	// left. Three dots on purpose: two would show somebody else's work as part of the request.
	Diff string `json:"diff,omitempty"`
	// Pushed is whether the branch is on the remote already. It changes what the button does — a
	// branch that has never been pushed is pushed by opening the request — and a screen that did
	// not say so surprised somebody.
	Pushed bool `json:"pushed,omitempty"`
}

// GitCommit is one commit on the way up: what it is called and what it says.
type GitCommit struct {
	SHA     string `json:"sha"`
	Subject string `json:"subject"`
	When    string `json:"when,omitempty"`
}

// PRFacts reads what a pull request from this workspace would be.
func (a *App) PRFacts(ctx context.Context, workdir string) (PRState, error) {
	if a.plat == nil {
		return PRState{}, fmt.Errorf("platform unavailable")
	}
	git := func(args ...string) (string, bool) {
		res, err := a.plat.Exec(ctx, port.Cmd{Path: "git", Args: args, Dir: workdir, MaxOutput: diffCap})
		if err != nil || res.ExitCode != 0 {
			return "", false
		}
		return strings.TrimSpace(string(res.Stdout)), true
	}
	head, ok := git("rev-parse", "--abbrev-ref", "HEAD")
	if !ok {
		return PRState{}, nil // not a checkout, which is not an error — see GitFacts
	}
	out := PRState{Repo: true, Branch: head}
	// What this request would go onto, asked in the order that is most likely to be right: what the
	// remote itself says its default is, then the two names that are that default nearly everywhere.
	for _, guess := range []string{"origin/HEAD", "origin/main", "origin/master"} {
		if name, ok := git("rev-parse", "--abbrev-ref", guess); ok && name != "" && name != head {
			out.Base = name
			break
		}
	}
	if out.Base == "" {
		return out, nil // nothing to compare against; the screen says so
	}
	if _, ok := git("rev-parse", "--verify", "--quiet", "origin/"+head); ok {
		out.Pushed = true
	}
	if log, ok := git("log", "--pretty=%h\x1f%s\x1f%ad", "--date=short", out.Base+"..HEAD"); ok && log != "" {
		for _, line := range strings.Split(log, "\n") {
			bits := strings.Split(line, "\x1f")
			if len(bits) < 2 {
				continue
			}
			c := GitCommit{SHA: bits[0], Subject: bits[1]}
			if len(bits) > 2 {
				c.When = bits[2]
			}
			out.Commits = append(out.Commits, c)
		}
	}
	if diff, ok := git("diff", "--no-color", out.Base+"...HEAD"); ok {
		if len(diff) >= diffCap {
			diff += "\n… (this diff is longer than the console will carry; the rest is not shown)"
		}
		out.Diff = diff
	}
	return out, nil
}

// DraftPR asks the model for the title and body of a pull request for this branch.
//
// The same shape as DraftCommit and for the same reasons — outside the session, nothing written,
// the repository's own recent subjects as the house style. What differs is the evidence: a request
// is about a BRANCH, so what goes over is its commits and the whole difference against the base,
// not what happens to be staged right now.
func (a *App) DraftPR(ctx context.Context, sid session.SessionID, workdir, rules string) (string, error) {
	st, err := a.PRFacts(ctx, workdir)
	if err != nil {
		return "", err
	}
	if !st.Repo || st.Base == "" || (len(st.Commits) == 0 && strings.TrimSpace(st.Diff) == "") {
		return "", nil // nothing on this branch to describe
	}
	var b strings.Builder
	b.WriteString("BRANCH: " + st.Branch + " onto " + st.Base + "\n\nCOMMITS ON IT\n")
	for _, c := range st.Commits {
		b.WriteString("  " + c.SHA + " " + c.Subject + "\n")
	}
	diff := st.Diff
	if len(diff) > lookOverCap {
		diff = diff[:lookOverCap] + "\n… (the rest of this diff was not sent)"
	}
	b.WriteString("\nTHE WHOLE DIFFERENCE AGAINST " + st.Base + "\n" + diff)

	// House rules for pull requests ([templates] pr), additive and last so they win on a conflict.
	// The PR screen may hand a one-off override (rules) for this draft only; non-empty, it stands in
	// for the saved template without changing it.
	tmpl := a.cfg.Templates.PR
	if strings.TrimSpace(rules) != "" {
		tmpl = rules
	}
	ruleBlock := ""
	if r := strings.TrimSpace(tmpl); r != "" {
		ruleBlock = "\n\nThis project's own rules for pull requests, which take precedence over the " +
			"instructions above where they differ:\n" + r
	}
	s := a.sessionInfo(ctx, sid)
	agent := a.agentFor(s)
	req := port.ChatRequest{
		Model: s.Model.Model,
		System: "Write the pull request for the branch below. Answer with the request and nothing " +
			"else: no preamble, no code fences. The FIRST line is the title, in the imperative and " +
			"under 72 characters. Then a blank line and the body: what changed and why, what a " +
			"reviewer should look at first, and anything you can see is still open. Describe only " +
			"what the commits and the diff show — no issue numbers, no names, nothing you cannot " +
			"see here." + ruleBlock,
		Messages: []session.Message{{
			Role:  session.RoleUser,
			Parts: []session.Part{{Kind: session.PartText, Text: b.String()}},
		}},
	}
	stream, serr := a.providerFor(agent).StreamChat(ctx, req)
	if serr != nil {
		return "", serr
	}
	out, _ := drainStream(stream)
	return strings.Trim(strings.TrimSpace(out), "`\n "), nil
}

// OpenPR pushes this branch and opens a pull request for it.
//
// Two commands and a tool that may not be installed, which is why it is not one of GitDo's closed
// list: that list is fixed argv for git itself, and this shells out to `gh`. Still the same rules
// — the title and the body travel as arguments, never as part of a command line — and the same
// duty afterwards: the companion is told, because a pull request is a fact about the branch it is
// reasoning about.
//
// The push comes first and sets the upstream. `gh pr create` will offer to push interactively and
// there is nobody here to answer it, so a branch that exists only locally would fail with a
// question. A branch already pushed makes this a no-op that costs one round trip.
func (a *App) OpenPR(ctx context.Context, sid session.SessionID, workdir, title, body string, ask bool) (string, error) {
	if a.plat == nil {
		return "", fmt.Errorf("platform unavailable")
	}
	if strings.TrimSpace(title) == "" {
		return "", fmt.Errorf("a pull request needs a title")
	}
	if res, err := a.plat.Exec(ctx, port.Cmd{
		Path: "gh", Args: []string{"--version"}, Dir: workdir, MaxOutput: 4 << 10}); err != nil || res.ExitCode != 0 {
		// Said plainly rather than as a failed command: the answer is "install gh or open it in a
		// browser", and a screen repeating exec's own words would send somebody to the wrong place.
		return "", fmt.Errorf("this workspace has no gh, so the console cannot open a pull request — " +
			"install GitHub CLI and sign it in, or open the request in a browser")
	}
	push, perr := a.plat.Exec(ctx, port.Cmd{
		Path: "git", Args: []string{"push", "--set-upstream", "origin", "HEAD"},
		Dir: workdir, MaxOutput: diffCap})
	if perr != nil {
		return "", perr
	}
	if push.ExitCode != 0 {
		return "", fmt.Errorf("%s", strings.TrimSpace(string(push.Stderr)))
	}
	args := []string{"pr", "create", "--title", title}
	if strings.TrimSpace(body) != "" {
		args = append(args, "--body", body)
	} else {
		args = append(args, "--body", "")
	}
	res, err := a.plat.Exec(ctx, port.Cmd{Path: "gh", Args: args, Dir: workdir, MaxOutput: diffCap})
	if err != nil {
		return "", err
	}
	if res.ExitCode != 0 {
		return "", fmt.Errorf("%s", strings.TrimSpace(string(res.Stderr)))
	}
	// What gh prints is the URL, which is the whole answer somebody wants back.
	url := strings.TrimSpace(string(res.Stdout))
	if i := strings.LastIndex(url, "\n"); i >= 0 {
		url = strings.TrimSpace(url[i+1:])
	}
	if nerr := a.noteToSession(ctx, sid,
		"I opened a pull request from the console for this branch: "+url+" — titled "+title+
			". The branch is pushed, so what is on the remote is what you have committed.", ask); nerr != nil {
		return url, nerr
	}
	return url, nil
}

// lookOverCap is how much of a buffer travels. Big enough for the files people read on this screen,
// small enough that holding the key down does not become a bill.
const lookOverCap = 60 << 10

// GitDo is the handful of git commands a person runs from a screen, run where the workspace is.
//
// # The list is closed
//
// stage, unstage, discard, commit, push, stash, unstash. Each is a FIXED argv with the path as one
// argument — never a string a shell parses — so a filename with a space, a semicolon or a leading
// dash is a filename. Anything else somebody wants from git they have a terminal for.
//
// # Why not a tool for the agent
//
// It already has bash and knows git; a git_stage in the registry would be a name in front of every
// agent on every turn for the console's convenience. The registry is the model's vocabulary, not
// this screen's.
//
// # Every one of them is written into the companion's log
//
// This is the reason the whole path exists rather than a nicety on top of it. The agent is
// reasoning about a tree and a history it has read: what is uncommitted, what HEAD is, what it
// would push. A person committing, pushing or stashing from the console moves exactly those things
// under it — a stash CHANGES THE FILES the agent read, a commit moves HEAD so the agent's next
// commit means something different, a push changes what "unpushed" means, and staging changes what
// a `git commit` it runs would capture.
//
// So the rule is the simple one, because a rule with a carve-out is a rule somebody has to
// remember: every mutation this console makes is written where the agent will read it, in the
// person's own words, one line each.
func (a *App) GitDo(ctx context.Context, sid session.SessionID, workdir, what, path, message string, ask bool) (string, error) {
	if a.plat == nil {
		return "", fmt.Errorf("platform unavailable")
	}
	var args []string
	needsPath := true
	switch what {
	case "stage":
		args = []string{"add", "--", path}
	case "unstage":
		args = []string{"restore", "--staged", "--", path}
	case "discard":
		args = []string{"restore", "--", path}
	case "commit":
		if strings.TrimSpace(message) == "" {
			return "", fmt.Errorf("a commit needs a message")
		}
		// Only what is staged, which is what the screen showed as staged when the button was
		// pressed. -a would also take everything else in the tree, including a file somebody left
		// half-edited in the other pane.
		args, needsPath = []string{"commit", "-m", message}, false
	case "push":
		args, needsPath = []string{"push"}, false
	case "stash":
		// The whole working tree, including untracked files — a stash that leaves them behind is
		// one that does not do what the button says.
		args, needsPath = []string{"stash", "push", "--include-untracked"}, false
	case "unstash":
		args, needsPath = []string{"stash", "pop"}, false
	case "switch":
		// An existing branch. `switch` rather than `checkout`: it does one thing, and it refuses
		// rather than quietly creating something when the name is not a branch.
		if strings.TrimSpace(message) == "" {
			return "", fmt.Errorf("which branch")
		}
		args, needsPath = []string{"switch", "--", strings.TrimSpace(message)}, false
	case "new-branch":
		name := strings.TrimSpace(message)
		if name == "" {
			return "", fmt.Errorf("a branch needs a name")
		}
		// Rejected, not protected. The new-branch name is --create's argument, not a pathspec, so a
		// trailing -- (which every other case here uses) does not shield it — git would still read a
		// leading-dash name as an option. Every legal branch name that a person would type is
		// dash-free at the start (git itself forbids a component starting with -); so a name that
		// does is refused rather than handed to switch as a flag.
		if strings.HasPrefix(name, "-") {
			return "", fmt.Errorf("a branch name may not start with a dash")
		}
		args, needsPath = []string{"switch", "--create", name}, false
	case "pull":
		// --ff-only: a pull that merges is a pull that can leave a conflicted tree behind a button
		// press. If it will not fast-forward, git says so and the person decides in a terminal.
		args, needsPath = []string{"pull", "--ff-only"}, false
	case "fetch":
		args, needsPath = []string{"fetch"}, false
	default:
		return "", fmt.Errorf("%q is not one of the git commands this can run: %s", what, gitDoList)
	}
	if needsPath && strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("which file")
	}
	cmd := port.Cmd{Path: "git", Args: args, Dir: workdir, MaxOutput: gitCaptureCap}
	if what == "push" || what == "pull" || what == "fetch" {
		// Never a prompt. A push that needs a credential would sit there with a terminal question
		// nobody can see, holding the daemon goroutine until the timeout — and a console that
		// hangs is worse than one that says "this needs a credential I cannot ask you for".
		cmd.Env = []string{"GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=", "SSH_ASKPASS="}
	}
	res, err := a.plat.Exec(ctx, cmd)
	if err != nil {
		return "", err
	}
	out := strings.TrimSpace(string(res.Stdout) + "\n" + string(res.Stderr))
	if res.ExitCode != 0 {
		// git's own words. It is better at saying why a commit was refused than anything this
		// could write about it.
		return "", fmt.Errorf("%s", out)
	}
	if nerr := a.noteGit(ctx, sid, what, path, message, ask); nerr != nil {
		return out, fmt.Errorf("git did it and the note about it was not written: %w", nerr)
	}
	return out, nil
}

const gitDoList = "stage, unstage, discard, commit, push, stash, unstash, switch, new-branch, pull, fetch"

// noteGit tells the companion what a person just did to its repository, and what that means for
// what it is holding.
//
// The second half is the part that matters. "I stashed the working tree" is a fact; "the files you
// read are not what is on disk now" is the consequence, and the consequence is what a model acts
// on. Each of these says what changed under it in the terms the agent's own next step cares about.
func (a *App) noteGit(ctx context.Context, sid session.SessionID, what, path, message string, ask bool) error {
	subject := path
	if strings.TrimSpace(subject) == "" {
		subject = "this workspace"
	}
	var text string
	switch what {
	case "stage":
		text = "I staged " + subject + " from the console. A commit you make now would include it."
	case "unstage":
		text = "I unstaged " + subject + " from the console. A commit you make now would leave it out."
	case "discard":
		text = "I threw away the changes in " + subject + " from the console — it is back at the " +
			"last commit. What you have in context is the version that is gone; read it again."
	case "commit":
		text = "I committed the staged changes from the console: " + strings.TrimSpace(message) +
			". HEAD has moved, so nothing is left staged and a commit of yours would start from here."
	case "push":
		text = "I pushed this branch from the console. What was unpushed is on the remote now."
	case "stash":
		text = "I stashed the working tree from the console, untracked files included. The files " +
			"you have read are not what is on disk now — read anything you were working on again."
	case "unstash":
		text = "I put the stash back from the console. The working tree has changed under you; " +
			"read anything you were working on again."
	case "switch":
		text = "I switched this workspace to the branch " + strings.TrimSpace(message) +
			" from the console. Every file you have read may be a different version now."
	case "new-branch":
		text = "I made the branch " + strings.TrimSpace(message) +
			" from the console and this workspace is on it. Commits you make land there."
	case "pull":
		text = "I pulled from the console. Files you have read may have moved on; read anything " +
			"you were working on again."
	case "fetch":
		text = "I fetched from the console. Nothing in the working tree changed — what moved is " +
			"what the remote branches point at."
	default:
		text = "I ran git " + what + " from the console."
	}
	return a.noteToSession(ctx, sid, text, ask)
}

// GitDiffOf is what changed in ONE file, in git's own words.
//
// Beside GitDiff, which is the whole tree assembled through a throwaway index for the council's
// evidence gate — a different question with a different answer shape. This one is for a person
// looking at a file: it asks git for that path, and it asks the version of the question the caller
// named rather than folding staged and unstaged together the way an evidence diff has to.
//
// # Why the console does not compute it
//
// A diff is not a hard thing to produce and it is a very easy thing to produce DIFFERENTLY. git
// knows about renames, mode changes, binary files, submodules, CRLF and the .gitattributes that
// say what is text — a screen that reimplemented any of that would show a person something their
// repository does not agree with. So this runs git and passes what it says through.
//
// # Staged and unstaged are two different questions
//
// "What would a commit take" and "what have I changed since I staged" are not the same diff, and a
// screen that showed one while the reader thought they were looking at the other would be worse
// than showing neither. The caller says which.
//
// # An untracked file has no diff, and gets one anyway
//
// git will not diff a file it does not know about, so this asks it to compare the file with
// nothing: every line is an addition, which is exactly what a new file is. --no-index exits 1 to
// mean "they differ", which is the ordinary case here rather than a failure.
func (a *App) GitDiffOf(ctx context.Context, workdir, path string, staged, untracked bool) (string, error) {
	if a.plat == nil {
		return "", fmt.Errorf("platform unavailable")
	}
	// The untracked branch runs `git diff --no-index` on the path as a real filesystem path, which
	// reads any file the daemon user can — and this method is reached by the read-classified /diff
	// endpoint with no jail of its own, so a low-privilege console/relay/peer caller could ask for
	// /etc/passwd. Confine a named path to the workspace here, at the one seam every caller crosses.
	// The empty path (the whole-tree diff below) is the review of "everything staged" and is allowed.
	if strings.TrimSpace(path) != "" {
		if _, err := insideWorkdir(workdir, path); err != nil {
			return "", err
		}
	}
	args := []string{"diff", "--no-color"}
	switch {
	case untracked:
		args = append(args, "--no-index", "--", os.DevNull, path)
	case staged:
		args = append(args, "--cached", "--", path)
	default:
		args = append(args, "--", path)
	}
	if strings.TrimSpace(path) == "" && !untracked {
		// The whole tree, which is what a review of "everything about to be committed" wants.
		args = args[:len(args)-1]
	}
	res, err := a.plat.Exec(ctx, port.Cmd{Path: "git", Args: args, Dir: workdir, MaxOutput: diffCap})
	if err != nil {
		return "", err
	}
	// 0 is "no difference" and 1 is "they differ" for --no-index; for an ordinary diff 0 is the
	// only success. Anything else is git refusing, and it says why better than this could.
	if res.ExitCode > 1 || (res.ExitCode == 1 && !untracked) {
		return "", fmt.Errorf("%s", strings.TrimSpace(string(res.Stderr)))
	}
	out := string(res.Stdout)
	if len(out) >= diffCap {
		out += "\n… (this diff is longer than the console will carry; the rest is not shown)"
	}
	return out, nil
}

// diffCap bounds what travels. A diff of a lock file or a vendored bundle is a megabyte of lines
// nobody reads, and it would arrive at a pane that shows a few hundred.
const diffCap = 512 << 10
