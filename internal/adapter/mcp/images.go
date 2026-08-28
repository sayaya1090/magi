package mcp

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sayaya1090/magi/internal/core/session"
)

// imageCap bounds one decoded image. A tool result is trimmed at 64KB (guard.go) and a picture is
// not trimmable — half a PNG is not half a picture — so images ride beside the text instead, with
// their own bound. 8MB is a full-page render at print resolution and two orders above a screenshot;
// past it the answer is a path the caller can fetch, not bytes in a conversation.
const imageCap = 8 << 20

// keepImages writes the image blocks of one answer to disk and returns references to them.
//
// The bytes cannot ride in the log: an event file is read whole by every viewer that opens the
// session, and one slide render would make it the size of the conversation it describes. So the log
// keeps a path and the file keeps the picture — the arrangement ImageRef was defined for.
//
// dir is where they live and how long: the daemon's data directory, beside the sessions, NOT the
// turn's scratch (which is removed when the turn ends, and these outlive it — a viewer opens the
// log tomorrow). A caller with no directory gets no images rather than a temp file that disappears:
// a reference that will not resolve is worse than saying the picture did not survive.
func keepImages(dir, sessionID, callID string, blocks []contentBlock) ([]session.ImageRef, []string) {
	var kept []session.ImageRef
	var notes []string
	if dir == "" {
		return nil, nil
	}
	for i, c := range blocks {
		if c.Type != "image" || c.Data == "" {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(c.Data))
		if err != nil {
			notes = append(notes, fmt.Sprintf("[image %d: not base64 — %v]", i+1, err))
			continue
		}
		if len(raw) > imageCap {
			notes = append(notes, fmt.Sprintf("[image %d: %d bytes, over the %d cap — dropped]",
				i+1, len(raw), imageCap))
			continue
		}
		home := filepath.Join(dir, "images", safePart(sessionID))
		if err := os.MkdirAll(home, 0o700); err != nil {
			notes = append(notes, fmt.Sprintf("[image %d: %v]", i+1, err))
			continue
		}
		name := fmt.Sprintf("%s-%d%s", safePart(callID), i+1, extFor(c.MimeType))
		path := filepath.Join(home, name)
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			notes = append(notes, fmt.Sprintf("[image %d: %v]", i+1, err))
			continue
		}
		kept = append(kept, session.ImageRef{Path: path, MIME: c.MimeType})
	}
	return kept, notes
}

// extFor is the file's suffix, chosen from what the server said it sent. Unknown types keep .bin:
// the MIME is carried beside the path, so the suffix is a convenience for whoever opens the
// directory by hand, not a fact anything reads back.
func extFor(mime string) string {
	switch strings.ToLower(strings.TrimSpace(mime)) {
	case "image/png":
		return ".png"
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/svg+xml":
		return ".svg"
	default:
		return ".bin"
	}
}

// safePart keeps an id from becoming a path. Session and call ids come from this process, but they
// are the kind of value that grows a separator the day something upstream changes.
func safePart(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "unnamed"
	}
	return b.String()
}
