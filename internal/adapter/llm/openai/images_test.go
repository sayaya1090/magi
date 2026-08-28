package openai

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/model"
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

// Which calls carried pictures, named by the line that says whose they are.
func whoCarried(out []wireMessage) []string {
	var carried []string
	for _, m := range out {
		if m.Role == "user" {
			if blocks, ok := m.Content.([]any); ok {
				carried = append(carried, blocks[0].(textBlock).Text)
			}
		}
	}
	return carried
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
	carried := whoCarried(convertMessages(msgs, true))
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
	var c Client // no injected table: the static catalogue answers, and it must not panic
	if c.seesImages("some-local-model-nobody-catalogued") {
		t.Error("an unknown model was treated as able to read pictures")
	}
	if !c.seesImages("gpt-4o") {
		t.Error("a catalogued vision model was treated as unable — the flag has a reader now")
	}
}

// The table magi actually runs on is filled in while it runs: a plugin contributes models, and the
// context-window probe registers what it learns. An adapter holding its own copy of the built-in
// catalogue answers "cannot see" for every one of them, forever — the answer was frozen at start.
func TestAModelRegisteredWhileRunningCanBeSeenToSee(t *testing.T) {
	live := model.NewRegistry()
	live.Register(model.Info{ID: "local-vlm", ContextWindow: 32000, Vision: true})
	c := Client{}
	WithVision(func(id string) bool { return live.Get(id).Vision })(&c)

	if !c.seesImages("local-vlm") {
		t.Error("a model registered at runtime is still told it cannot read pictures")
	}
	if c.seesImages("another-one-nobody-declared") {
		t.Error("the injected table turned unknown into yes — the safe direction is no")
	}
}

// A picture too large for what is left of the budget is SKIPPED, and the walk keeps going. It used
// to stop there, so one oversized render blanked every smaller picture behind it — for as long as
// it stayed in the window, which is every turn of a review.
func TestAPictureThatDoesNotFitIsSkippedNotAFullStop(t *testing.T) {
	dir := t.TempDir()
	var msgs []session.Message
	msgs = append(msgs, called("older", "rendered", withImage(t, dir, "older.png", 500<<10))...)
	msgs = append(msgs, called("middle", "rendered", withImage(t, dir, "middle.png", 2<<20))...)
	msgs = append(msgs, called("newest", "rendered", withImage(t, dir, "newest.png", 2<<20))...)

	carried := strings.Join(whoCarried(convertMessages(msgs, true)), " ")
	if !strings.Contains(carried, "newest") {
		t.Errorf("the newest render did not ride: %s", carried)
	}
	if strings.Contains(carried, "middle") {
		t.Errorf("a picture over the remaining budget rode anyway: %s", carried)
	}
	if !strings.Contains(carried, "older") {
		t.Errorf("the walk stopped at the one that did not fit: %s", carried)
	}
}

// …and the first one rides even when it alone is over budget. It is bounded already — the daemon
// refuses to keep a picture over its own cap — and the alternative is a render that is on disk,
// named in the log, and invisible to the only reader that could look at it.
func TestTheOnePictureBeingTalkedAboutAlwaysRides(t *testing.T) {
	dir := t.TempDir()
	msgs := called("huge", "rendered", withImage(t, dir, "huge.png", imageBudget+1))
	if len(whoCarried(convertMessages(msgs, true))) != 1 {
		t.Error("a single over-budget render was left invisible; its bytes are on disk and nothing can look at them")
	}
}

// The count is still a count. Ten small pictures do not all ride because they are small.
func TestTheLimitIsStillALimit(t *testing.T) {
	dir := t.TempDir()
	var msgs []session.Message
	for i := 0; i < 10; i++ {
		msgs = append(msgs, called(fmt.Sprintf("c%d", i), "rendered",
			withImage(t, dir, fmt.Sprintf("i%d.png", i), 100))...)
	}
	if got := len(whoCarried(convertMessages(msgs, true))); got != imagesPerReply {
		t.Errorf("%d calls carried pictures, want %d", got, imagesPerReply)
	}
}

// The budget is one budget for the request, not one per tool call. Handing every chosen call the
// whole allowance again sent seven pictures under a limit of four — and the limit exists because
// the window accounting cannot see them at all.
func TestTheBudgetIsNotHandedOutAgainToEveryCall(t *testing.T) {
	dir := t.TempDir()
	var msgs []session.Message
	for i := 0; i < 3; i++ {
		msgs = append(msgs, called(fmt.Sprintf("one%d", i), "rendered",
			withImage(t, dir, fmt.Sprintf("one%d.png", i), 100))...)
	}
	four := []session.ImageRef{
		withImage(t, dir, "m1.png", 100), withImage(t, dir, "m2.png", 100),
		withImage(t, dir, "m3.png", 100), withImage(t, dir, "m4.png", 100),
	}
	msgs = append(msgs, called("many", "rendered", four...)...)

	sent := 0
	for _, m := range convertMessages(msgs, true) {
		if m.Role != "user" {
			continue
		}
		for _, b := range m.Content.([]any) {
			if _, isImg := b.(imageBlock); isImg {
				sent++
			}
		}
	}
	if sent > imagesPerReply {
		t.Errorf("%d pictures went on the wire under a limit of %d", sent, imagesPerReply)
	}
	// And the ones that rode are the newest: the last call is what the next question is about.
	if !strings.Contains(strings.Join(whoCarried(convertMessages(msgs, true)), " "), "many") {
		t.Error("the newest call did not carry its pictures — backwards walking was undone by forward emitting")
	}
}
