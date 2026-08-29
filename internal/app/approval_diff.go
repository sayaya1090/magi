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
// the truth for a new file), unreadable args, a path that leaves the workdir lexically or through
// a symlink (the write itself will be refused; previewing foreign bytes helps nobody), or a file
// past the read cap. Empty content is a real write — truncation — and gets its all-removed diff.
func writeApprovalDiff(workdir string, args json.RawMessage) (string, bool) {
	var a struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if json.Unmarshal(args, &a) != nil || a.Path == "" || workdir == "" {
		return "", false
	}
	// The tool's own jail (resolvePath) is mirrored: the lexical half below, and the symlink half
	// with EvalSymlinks after the file is known to exist — a symlink inside the workdir pointing
	// outside used to be READ here, before anyone approved anything, putting jail-refused bytes
	// on every status viewer as the preview of a write the real jail was going to refuse anyway.
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
	realBase, berr := filepath.EvalSymlinks(base)
	realAbs, aerr := filepath.EvalSymlinks(abs)
	if berr != nil || aerr != nil {
		return "", false
	}
	if rrel, rerr := filepath.Rel(realBase, realAbs); rerr != nil || rrel == ".." || strings.HasPrefix(rrel, ".."+string(filepath.Separator)) {
		return "", false
	}
	old, rerr := os.ReadFile(abs)
	if rerr != nil || len(old) > approvalReadCap {
		// Re-checked after the read: the size seen by Stat is not the size read a moment later,
		// and the byte cap below must never be fed more than it was promised.
		return "", false
	}
	// An identical rewrite answers a SENTENCE, not "": both wire fields carrying the diff are
	// omitempty, so an empty authoritative answer arrives identical to "no diff computed" and
	// viewers fall back to the args view — the very all-added rendering this exists to replace.
	d := change.LineDiff(string(old), a.Content)
	if d == "" {
		return "(this write matches the file exactly — no change)", true
	}
	return change.CapDiffBytes(d), true
}

// writeApprovalDiffFor resolves the session's workdir and delegates. Called WITHOUT a.mu held —
// sessionInfo takes the lock itself, and the read underneath is file IO.
func (a *App) writeApprovalDiffFor(ctx context.Context, sid session.SessionID, args json.RawMessage) (string, bool) {
	return writeApprovalDiff(a.sessionInfo(ctx, sid).Workdir, args)
}
