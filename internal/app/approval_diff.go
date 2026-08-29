package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/sayaya1090/magi/internal/core/change"
	"github.com/sayaya1090/magi/internal/core/session"
)

// approvalReadCap bounds the file read behind a write-approval diff. Past it the args view
// stands — LineDiff would summarize a file this size to one line anyway, and an approval prompt
// must never stall on a giant file.
const approvalReadCap = 1 << 20

// writeApprovalDiff renders what a `write` would actually change, against the file as it is RIGHT
// NOW — the one moment that read is honest, because the write has not happened yet.
//
// EditDiff cannot do this: core/change is a pure function of the call's arguments (that is what
// lets three viewers recompute it from a log without a filesystem), so its write branch shows the
// whole content as additions. Live-QA read that as the lie it looks like: a one-line addition to
// a 40-line file arrived as 41 added lines, and the person approving could not find the change.
// The approval prompt is the one caller with the file still on disk in front of it, so the real
// diff is computed here and CARRIED, and the pure fallback remains for everything else.
//
// ok=false means "no authoritative answer — keep the args view": absent file (all-additions is
// the truth for a new file), unreadable args, a path that leaves the workdir (the write itself
// will be refused; previewing foreign bytes helps nobody), or a file past the read cap.
func writeApprovalDiff(workdir string, args json.RawMessage) (string, bool) {
	var a struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if json.Unmarshal(args, &a) != nil || a.Path == "" || a.Content == "" || workdir == "" {
		return "", false
	}
	// The tool's own jail (resolvePath) is mirrored lexically. The symlink half is deliberately
	// not: this path only READS, for a preview shown to the daemon's own operator, and the write
	// that follows still goes through the real jail.
	base := filepath.Clean(workdir)
	abs := filepath.Clean(a.Path)
	if !filepath.IsAbs(abs) {
		abs = filepath.Clean(filepath.Join(base, a.Path))
	}
	rel, err := filepath.Rel(base, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	if st, serr := os.Stat(abs); serr != nil || !st.Mode().IsRegular() || st.Size() > approvalReadCap {
		return "", false
	}
	old, rerr := os.ReadFile(abs)
	if rerr != nil {
		return "", false
	}
	// An identical rewrite answers ("", true): the truthful preview of a no-op is no diff, and
	// the caller must not fall back to a view that shows every line as added.
	return change.LineDiff(string(old), a.Content), true
}

// writeApprovalDiffFor resolves the session's workdir and delegates. Called WITHOUT a.mu held —
// sessionInfo takes the lock itself, and the read underneath is file IO.
func (a *App) writeApprovalDiffFor(ctx context.Context, sid session.SessionID, args json.RawMessage) (string, bool) {
	return writeApprovalDiff(a.sessionInfo(ctx, sid).Workdir, args)
}
