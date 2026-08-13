package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"

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
	return parseGitStatus(string(res.Stdout)), nil
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
