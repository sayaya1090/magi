package openai

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
)

func withImage(t *testing.T, dir, name string, size int) session.ImageRef {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, make([]byte, size), 0o600); err != nil {
		t.Fatal(err)
	}
	return session.ImageRef{Path: p, MIME: "image/png"}
}

// A call and the result that answered it. A result on its own is an orphan the wire repair demotes
// to plain user text, so the fixture has to be the real pair.
func called(callID, text string, imgs ...session.ImageRef) []session.Message {
	body, _ := json.Marshal(text)
	return []session.Message{
		{Role: session.RoleAssistant, Parts: []session.Part{{
			Kind:     session.PartToolCall,
			ToolCall: &session.ToolCall{CallID: callID, Name: "render", Args: json.RawMessage(`{}`)},
		}}},
		{Role: session.RoleTool, Parts: []session.Part{{
			Kind:       session.PartToolResult,
			ToolResult: &session.ToolResult{CallID: callID, Content: body, Images: imgs},
		}}},
	}
}

// The tool message among them, with its pictures behind it if any rode along.
func afterTheCall(out []wireMessage) []wireMessage {
	for i, m := range out {
		if m.Role == "tool" {
			return out[i:]
		}
	}
	return nil
}

// A model that cannot read pictures still hears that there was one: the tool result's own text
// names the file. Losing both would put back the symptom this whole path exists to remove — a call
// that did its work and reported nothing.
func TestAModelWithoutVisionStillGetsTheLine(t *testing.T) {
	dir := t.TempDir()
	msgs := called("c1", "[image: image/png at /x.png]", withImage(t, dir, "a.png", 10))
	out := afterTheCall(convertMessages(msgs, false))
	if len(out) != 1 {
		t.Fatalf("wanted the tool message alone, got %d: %+v", len(out), out)
	}
	if !strings.Contains(out[0].Content.(string), "[image:") {
		t.Error("the line naming the picture is gone — that line is all a text-only model gets")
	}
}

// A model that can read them gets them after the result, as a user message: the API gives a tool
// result nowhere to put a picture.
func TestPicturesFollowTheResultTheyBelongTo(t *testing.T) {
	dir := t.TempDir()
	msgs := called("c1", "rendered", withImage(t, dir, "a.png", 10))
	out := afterTheCall(convertMessages(msgs, true))
	if len(out) != 2 {
		t.Fatalf("wanted result + pictures, got %d", len(out))
	}
	if out[1].Role != "user" {
		t.Errorf("pictures rode as %q — role tool takes a string", out[1].Role)
	}
	blocks, ok := out[1].Content.([]any)
	if !ok || len(blocks) != 2 {
		t.Fatalf("content is %T with %d blocks — want one line of text and one picture", out[1].Content, len(blocks))
	}
	if _, isText := blocks[0].(textBlock); !isText {
		t.Error("the first block should say whose pictures these are")
	}
	img, isImg := blocks[1].(imageBlock)
	if !isImg || !strings.HasPrefix(img.ImageURL.URL, "data:image/png;base64,") {
		t.Errorf("second block is %+v — a path means nothing to a model on an API", blocks[1])
	}
	if !strings.Contains(blocks[0].(textBlock).Text, "c1") {
		t.Error("the line does not name the call — an image out of nowhere follows a result about a file")
	}
}

// The newest pictures ride; older ones keep their line. Forwards would have pinned the first render
// of a long session in every request and dropped the one just made.
func TestTheNewestPicturesAreTheOnesThatRide(t *testing.T) {
	dir := t.TempDir()
	var msgs []session.Message
	for i := 0; i < imagesPerReply+2; i++ {
		name := string(rune('a'+i)) + ".png"
		msgs = append(msgs, called("c"+string(rune('0'+i)), "rendered", withImage(t, dir, name, 10))...)
	}
	out := convertMessages(msgs, true)
	var carried []string
	for _, m := range out {
		if m.Role == "user" {
			carried = append(carried, m.Content.([]any)[0].(textBlock).Text)
		}
	}
	if len(carried) != imagesPerReply {
		t.Fatalf("%d requests carried pictures, want %d", len(carried), imagesPerReply)
	}
	last := "c" + string(rune('0'+imagesPerReply+1))
	if !strings.Contains(strings.Join(carried, " "), last) {
		t.Errorf("the newest call %s did not carry its picture: %v", last, carried)
	}
	if strings.Contains(strings.Join(carried, " "), "call c0") {
		t.Errorf("the oldest call kept its place in the request: %v", carried)
	}
}

// A picture whose file is gone is not a failed request: the line of text still names it.
func TestAMissingFileIsNotAFailedRequest(t *testing.T) {
	msgs := called("c1", "rendered", session.ImageRef{Path: "/nonexistent/gone.png", MIME: "image/png"})
	out := afterTheCall(convertMessages(msgs, true))
	if len(out) != 1 {
		t.Fatalf("got %d messages — a reference that does not resolve should leave the result alone", len(out))
	}
}

// Vision is read from the catalogue, and a model nobody catalogued cannot be assumed to read
// pictures: an image_url block to a backend that does not know one is an error or, worse, content
// silently dropped and then asked about.
func TestAnUnknownModelIsNotAssumedToSee(t *testing.T) {
	if canSeeImages("some-local-model-nobody-catalogued") {
		t.Error("an unknown model was treated as able to read pictures")
	}
	if !canSeeImages("gpt-4o") {
		t.Error("a catalogued vision model was treated as unable — the flag has a reader now")
	}
}
