package mcp

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
)

// A picture comes back as base64 in a block with no text at all. Before this, the whole answer
// flattened to the empty string: a call that had done its work reported nothing.
func TestImageBlocksAreKeptAndNamed(t *testing.T) {
	dir := t.TempDir()
	png := []byte("\x89PNG\r\n\x1a\nnot really a png, but bytes")
	blocks := []contentBlock{
		{Type: "text", Text: "rendered slide 3"},
		{Type: "image", Data: base64.StdEncoding.EncodeToString(png), MimeType: "image/png"},
	}
	kept, notes := keepImages(dir, "s_1", "mcp__ppt__render", blocks)
	if len(notes) != 0 {
		t.Fatalf("nothing should have gone wrong: %v", notes)
	}
	if len(kept) != 1 {
		t.Fatalf("kept %d images, want 1", len(kept))
	}
	if kept[0].MIME != "image/png" {
		t.Errorf("mime %q — the type the server sent is what a viewer opens it with", kept[0].MIME)
	}
	if !strings.HasSuffix(kept[0].Path, ".png") {
		t.Errorf("path %q has no .png — the suffix is for whoever opens the directory by hand", kept[0].Path)
	}
	on, err := os.ReadFile(kept[0].Path)
	if err != nil {
		t.Fatalf("the reference does not resolve: %v", err)
	}
	if string(on) != string(png) {
		t.Error("the file is not the bytes the server sent")
	}
	// Under the data directory, not the turn's scratch: a viewer opens the log tomorrow.
	if !strings.HasPrefix(kept[0].Path, filepath.Join(dir, "images")) {
		t.Errorf("kept at %q — images live beside the sessions", kept[0].Path)
	}
}

// Two calls of the same tool are two pictures. The name used to be <tool>-<index>, which is the
// same path every time — render slide 3, then slide 9, and the log line about slide 3 resolved to
// slide 9's picture. Measured on the first version of this file.
func TestTwoCallsOfOneToolDoNotOverwriteEachOther(t *testing.T) {
	dir := t.TempDir()
	one := keepOne(t, dir, "mcp__ppt__render", []byte("SLIDE-3-RENDER"))
	two := keepOne(t, dir, "mcp__ppt__render", []byte("SLIDE-9-RENDER"))
	if one.Path == two.Path {
		t.Fatalf("both calls wrote %q — the second answer overwrote the first", one.Path)
	}
	back, err := os.ReadFile(one.Path)
	if err != nil {
		t.Fatalf("the first reference no longer resolves: %v", err)
	}
	if string(back) != "SLIDE-3-RENDER" {
		t.Errorf("the first reference resolves to %q — a reference to the wrong picture is worse "+
			"than one that fails", string(back))
	}
}

// The same bytes twice are one file. Sharing is honest only when the content is identical.
func TestTheSamePictureTwiceIsOneFile(t *testing.T) {
	dir := t.TempDir()
	a := keepOne(t, dir, "mcp__ppt__render", []byte("SAME"))
	b := keepOne(t, dir, "mcp__ppt__render", []byte("SAME"))
	if a.Path != b.Path {
		t.Errorf("identical pictures were written twice: %q and %q", a.Path, b.Path)
	}
}

func keepOne(t *testing.T, dir, tool string, raw []byte) session.ImageRef {
	t.Helper()
	kept, notes := keepImages(dir, "s_1", tool, []contentBlock{
		{Type: "image", Data: base64.StdEncoding.EncodeToString(raw), MimeType: "image/png"},
	})
	if len(kept) != 1 {
		t.Fatalf("kept %d images (notes %v), want 1", len(kept), notes)
	}
	return kept[0]
}

// A host with nowhere to put them says so. Writing to a temp file that disappears would leave a
// reference in the log that never resolves, which reads as data loss rather than as a limit.
func TestNoDirectoryKeepsNothing(t *testing.T) {
	kept, notes := keepImages("", "s_1", "call", []contentBlock{
		{Type: "image", Data: base64.StdEncoding.EncodeToString([]byte("x")), MimeType: "image/png"},
	})
	if len(kept) != 0 {
		t.Fatalf("kept %v — a host with no image directory keeps none", kept)
	}
	// And SAYS so: an answer that was only a picture is otherwise the empty string, which is the
	// symptom this whole path exists to stop.
	if len(notes) != 1 || !strings.Contains(notes[0], "keeps no images") {
		t.Fatalf("notes %v — silence here is the old bug wearing a different hat", notes)
	}
}

// Bad base64 and oversized images are REPORTED. The model is told what happened to the picture it
// asked for; silence would leave it believing the call answered in full.
func TestBrokenAndOversizedImagesAreReported(t *testing.T) {
	dir := t.TempDir()
	big := base64.StdEncoding.EncodeToString(make([]byte, imageCap+1))
	kept, notes := keepImages(dir, "s_1", "call", []contentBlock{
		{Type: "image", Data: "!!!not base64!!!", MimeType: "image/png"},
		{Type: "image", Data: big, MimeType: "image/png"},
	})
	if len(kept) != 0 {
		t.Fatalf("kept %d — neither of those should be on disk", len(kept))
	}
	if len(notes) != 2 {
		t.Fatalf("notes %v — both failures are the model's business", notes)
	}
	if !strings.Contains(notes[0], "base64") || !strings.Contains(notes[1], "cap") {
		t.Errorf("notes do not say what went wrong: %v", notes)
	}
}

// An id that grew a separator must not become a path.
func TestIdsCannotEscapeTheDirectory(t *testing.T) {
	dir := t.TempDir()
	kept, _ := keepImages(dir, "../../etc", "../passwd", []contentBlock{
		{Type: "image", Data: base64.StdEncoding.EncodeToString([]byte("x")), MimeType: "image/png"},
	})
	if len(kept) != 1 {
		t.Fatalf("kept %d, want 1", len(kept))
	}
	if !strings.HasPrefix(kept[0].Path, dir) {
		t.Errorf("wrote outside its directory: %q", kept[0].Path)
	}
}
