package mcp

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/core/session"
)

// imageCap bounds one decoded image. A tool result is trimmed at 64KB (guard.go) and a picture is
// not trimmable — half a PNG is not half a picture — so images ride beside the text instead, with
// their own bound. 8MB is a full-page render at print resolution and two orders above a screenshot;
// past it the answer is a path the caller can fetch, not bytes in a conversation.
const imageCap = 8 << 20

// How far over the cap a base64 string may measure before it is refused unread. Padding and line
// breaks make the encoded form measure larger than the picture inside it: at 76-column wrapping
// that is about 1.3%, so this is room enough for any legal encoding of a picture at the cap and far
// short of the runaway the pre-check exists to stop.
const imageSlack = 1 << 20

// The largest single JSON-RPC frame a transport will read. A picture at the cap arrives base64'd
// (four bytes per three) inside a JSON message, so the frame is bigger than the file it carries.
const sseFrameCap = imageCap*4/3 + 1<<20

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
		// Say it. A host with nowhere to put pictures is a fact the model needs: an answer that was
		// only a picture is otherwise the empty string again, which is the thing this whole path
		// exists to stop. Reported once, not once per block — the reason is the same for all of them.
		for _, c := range blocks {
			if c.Type == "image" && c.Data != "" {
				return nil, []string{"[image: this daemon keeps no images, so it was not saved]"}
			}
		}
		return nil, nil
	}
	for i, c := range blocks {
		if c.Type != "image" || c.Data == "" {
			continue
		}
		// Sized before decoding, because base64 of a 100MB image is 133MB of string and decoding it
		// to find out it is too big means holding both. But DecodedLen is an UPPER bound on a
		// string that may carry padding and line breaks — it counts both as picture — so measuring
		// the cap with it rejected a picture of exactly the cap, and rejected line-wrapped base64
		// (76 columns, which MIME does and the spec allows) a good deal under it. The pre-check
		// keeps its purpose with room to spare; the cap itself is applied to the decoded bytes.
		if base64.StdEncoding.DecodedLen(len(c.Data)) > imageCap+imageSlack {
			notes = append(notes, fmt.Sprintf("[image %d: about %d bytes, over the %d cap — dropped]",
				i+1, base64.StdEncoding.DecodedLen(len(c.Data)), imageCap))
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
		// Named by what is IN it, not by which call made it.
		//
		// The name was <tool>-<index>, which is the same path every time that tool is called: render
		// slide 3, then slide 9, and the log line about slide 3 resolves to slide 9's picture
		// (measured). A reference that will not resolve is bad; one that resolves to the WRONG
		// picture is worse, because nothing about it looks wrong. Two calls that produced the same
		// bytes now share one file, which is the only case where sharing is honest.
		sum := sha256.Sum256(raw)
		name := fmt.Sprintf("%s-%s%s", safePart(callID), hex.EncodeToString(sum[:6]), extFor(c.MimeType))
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

// How long a picture stays on disk. The turn that made it is over in minutes; the log naming it is
// kept forever, and a deck review can write tens of megabytes in an afternoon. Neither "delete when
// the turn ends" (a viewer opens that log tomorrow) nor "keep everything" (nothing ever removes an
// 8MB render) is the right answer, so pictures outlive the turn by a season and no longer.
//
// What an older log loses is the picture, not the fact: the tool result still carries the line
// naming the file and its type, every reader of a missing file already treats it as absent rather
// than as an error, and the model was never shown pictures from that far back anyway.
const imageLifetime = 30 * 24 * time.Hour

// SweepImages removes pictures older than imageLifetime, and any session folder left empty by that.
// Called once at startup: the daemon writes these all day and nothing else would ever take them
// away. Errors are the caller's to report or ignore — a sweep that cannot read its own directory is
// not a reason to refuse to start.
func SweepImages(dir string, now time.Time) (removed int, freed int64) {
	if dir == "" {
		return 0, 0
	}
	root := filepath.Join(dir, "images")
	sessions, err := os.ReadDir(root)
	if err != nil {
		return 0, 0
	}
	for _, s := range sessions {
		if !s.IsDir() {
			continue
		}
		home := filepath.Join(root, s.Name())
		files, err := os.ReadDir(home)
		if err != nil {
			continue
		}
		left := 0
		for _, f := range files {
			info, err := f.Info()
			if err != nil {
				left++
				continue
			}
			if now.Sub(info.ModTime()) < imageLifetime {
				left++
				continue
			}
			if err := os.Remove(filepath.Join(home, f.Name())); err != nil {
				left++
				continue
			}
			removed++
			freed += info.Size()
		}
		if left == 0 {
			// Only ever an empty directory — a non-empty one refuses, which is the guard here. A
			// folder that will not go is not a failure of the sweep: its files are already gone.
			//
			// The continue is inert today because nothing follows it. Anything added below this
			// line has to run for a folder that would not go, so move this check when that happens.
			if err := os.Remove(home); err != nil {
				continue
			}
		}
	}
	return removed, freed
}
