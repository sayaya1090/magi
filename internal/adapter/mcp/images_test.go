package mcp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// A picture near the cap arrives over SSE as one enormous line. The scanner used to stop at 1MB,
// which put the real ceiling at about 750KB of picture — well under the 8MB the code says it
// allows, and reported as a scanner error rather than as a picture that was too big.
func TestAPictureBiggerThanAMegabyteSurvivesTheWire(t *testing.T) {
	const size = 3 << 20 // over the old 1MB line limit, under imageCap
	picture := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x89}, size))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req request
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if req.Method != "tools/call" {
			r.Body = io.NopCloser(bytes.NewReader(body)) // the handshake still needs to read it
			fakeHTTPServer().ServeHTTP(w, r)
			return
		}
		result, _ := json.Marshal(callToolResult{
			Content: []contentBlock{{Type: "image", Data: picture, MimeType: "image/png"}},
		})
		event, _ := json.Marshal(message{JSONRPC: jsonRPCVersion, ID: &req.ID, Result: result})
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\n\n", event)
	}))
	defer srv.Close()

	client := newHTTPClient(srv.URL, nil, nil)
	defer client.Close()
	ctx := context.Background()
	if err := client.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	res, err := client.CallTool(ctx, "http_echo", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("a %dMB picture did not survive the wire: %v", size>>20, err)
	}
	if len(res.Content) != 1 || res.Content[0].Data != picture {
		t.Fatalf("the picture came back different: %d blocks", len(res.Content))
	}
}

// Nothing else ever removes a picture: the turn ends in minutes and the log naming it is kept
// forever. What the sweep must not do is take the ones a session is still likely to be about.
func TestOldPicturesAreSweptAndRecentOnesAreNot(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "images", "sess-1")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(home, "render-aaaaaa.png")
	recent := filepath.Join(home, "render-bbbbbb.png")
	for _, p := range []string{old, recent} {
		if err := os.WriteFile(p, make([]byte, 1024), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	stale := now.Add(-imageLifetime - time.Hour)
	if err := os.Chtimes(old, stale, stale); err != nil {
		t.Fatal(err)
	}

	removed, freed := SweepImages(dir, now)
	if removed != 1 || freed != 1024 {
		t.Fatalf("swept %d files (%d bytes), want 1 and 1024", removed, freed)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Error("the old picture is still there")
	}
	if _, err := os.Stat(recent); err != nil {
		t.Errorf("a picture from this week was swept: %v", err)
	}
	// The folder stays while something is in it…
	if _, err := os.Stat(home); err != nil {
		t.Errorf("the session's folder went with it: %v", err)
	}
	// …and goes when nothing is.
	if err := os.Chtimes(recent, stale, stale); err != nil {
		t.Fatal(err)
	}
	SweepImages(dir, now)
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Error("an empty session folder was left behind")
	}
}

// A host with no image directory has nothing to sweep, and a directory that was never written to is
// not an error to report at startup.
func TestSweepingNothingIsQuiet(t *testing.T) {
	if n, _ := SweepImages("", time.Now()); n != 0 {
		t.Error("sweeping an unset directory did something")
	}
	if n, _ := SweepImages(t.TempDir(), time.Now()); n != 0 {
		t.Error("sweeping a directory with no images did something")
	}
}

// The cap is measured on the picture, not on the string carrying it. Base64 counts padding and line
// breaks as picture, so measuring the cap with the encoded length refused a picture of exactly the
// cap — and refused a line-wrapped one (76 columns, which MIME does) well under it.
func TestAPictureAtTheCapIsKeptHoweverItIsEncoded(t *testing.T) {
	dir := t.TempDir()
	raw := bytes.Repeat([]byte{0x89}, imageCap)
	flat := base64.StdEncoding.EncodeToString(raw)

	var wrapped strings.Builder
	for i := 0; i < len(flat); i += 76 {
		end := min(i+76, len(flat))
		wrapped.WriteString(flat[i:end] + "\n")
	}

	for name, data := range map[string]string{"flat": flat, "wrapped": wrapped.String()} {
		kept, notes := keepImages(dir, "sess-"+name, "render",
			[]contentBlock{{Type: "image", Data: data, MimeType: "image/png"}})
		if len(kept) != 1 {
			t.Errorf("%s base64 of a picture AT the cap was refused: %v", name, notes)
		}
	}

	// And one genuinely over it still is.
	over := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x89}, imageCap+1))
	kept, notes := keepImages(dir, "sess-over", "render",
		[]contentBlock{{Type: "image", Data: over, MimeType: "image/png"}})
	if len(kept) != 0 || len(notes) != 1 {
		t.Errorf("a picture over the cap was kept anyway (%d kept, notes %v)", len(kept), notes)
	}
}
