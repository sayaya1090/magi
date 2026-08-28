package openai

import (
	"encoding/base64"
	"fmt"
	"os"
	"sync"

	"github.com/sayaya1090/magi/internal/core/model"
	"github.com/sayaya1090/magi/internal/core/session"
)

// imageBlock is an OpenAI content part carrying a picture. The url is a data: URI — magi's images
// live on this machine, and a file path means nothing to a model on the other side of an API.
type imageBlock struct {
	Type     string   `json:"type"` // "image_url"
	ImageURL imageURL `json:"image_url"`
}

type imageURL struct {
	URL string `json:"url"`
}

// catalogue is the static capability table: what magi ships knowing, before anything runs.
var catalogue = sync.OnceValue(func() *model.Registry { return model.NewRegistry() })

// seesImages says whether this model takes image input.
//
// It asks the injected reader first — the app's live table, which a plugin contributes to and the
// context-window probe writes into while magi runs. Reading a private copy of the static catalogue
// instead (the first version of this did) means a model somebody registered at runtime, declaring
// Vision, is still told it cannot see: the answer was frozen at process start.
//
// A model nobody has declared answers false. A request with an image_url block to a backend that
// cannot read one is either an error or, worse, silently dropped content the model is then asked
// about. The text line naming the file goes either way, so false costs the model a picture, never
// the fact that there was one.
func (c *Client) seesImages(modelID string) bool {
	if c.vision != nil {
		return c.vision(modelID)
	}
	return catalogue().Get(modelID).Vision
}

// What one request may carry in pictures, in bytes and in count.
//
// A conversation accumulates them: reviewing a deck is dozens of renders, and re-sending every one
// on every turn would grow the request without bound and re-bill it each time. Worse, the window
// accounting cannot see them — magi estimates a request's size from its text (compact.go's
// estimateTokens), and a picture is thousands of tokens that estimate knows nothing about. So the
// bound is deliberately small and BOTH kinds: four pictures and four megabytes, whichever runs out
// first, counted from the END of the conversation.
//
// The bytes counted are what goes on the WIRE, not what is on disk: a picture is base64 there, four
// bytes for every three. Counting the file made the real request a third larger than the budget it
// was checked against.
//
// The most recent are the ones being talked about. An older one keeps its line of text, which
// names the file and its type — the same line a model that cannot see pictures at all reads.
const (
	imageBudget    = 4 << 20
	imagesPerReply = 4
	// How far back to look for them. Bounds the work, not the choice: every candidate costs a
	// stat, and a long session holds hundreds. Anything older than this is history in text.
	imageScanDepth = 64
)

// onWire is what a file of n bytes costs inside the request: base64, four characters per three
// bytes, rounded up to the padded quantum.
func onWire(n int) int { return (n + 2) / 3 * 4 }

// riders are the pictures of one tool call that go on this request, and how many of that call's
// pictures did not.
type riders struct {
	refs []session.ImageRef
	left int
}

// pickImages decides which pictures ride, walking BACKWARDS from the end of the conversation.
//
// Backwards because a conversation grows and a request cannot. The render just made is what the
// next question is about; the one from twenty turns ago is history, and its line of text is what
// history needs. Forwards would have pinned the oldest picture in every request for the life of the
// session and dropped the newest.
//
// Two things this gets right that the first version did not, both of which only show at deck size:
//
//   - A picture too big for the remaining budget is SKIPPED, not a full stop. Returning at the
//     first one that did not fit meant a single large render at the end of a conversation blanked
//     every smaller picture behind it, for as long as it stayed in the window.
//   - The FIRST picture rides even when it alone is over budget. It is bounded already (the daemon
//     refuses to keep one over its own cap), and the alternative is a render that is on disk, named
//     in the log, and permanently invisible to the only reader that could look at it.
func pickImages(msgs []session.Message) map[string]riders {
	picked := map[string]riders{}
	bytes, count, looked := 0, 0, 0
	for i := len(msgs) - 1; i >= 0 && looked < imageScanDepth && count < imagesPerReply; i-- {
		if msgs[i].Role != session.RoleTool {
			continue
		}
		for _, p := range msgs[i].Parts {
			if p.Kind != session.PartToolResult || p.ToolResult == nil || len(p.ToolResult.Images) == 0 {
				continue
			}
			r := picked[p.ToolResult.CallID]
			for _, ref := range p.ToolResult.Images {
				looked++
				fi, err := os.Stat(ref.Path)
				if err != nil {
					continue // gone; its line of text still names it, and that is not an error
				}
				cost := onWire(int(fi.Size()))
				switch {
				case count >= imagesPerReply:
					r.left++
				case count > 0 && bytes+cost > imageBudget:
					r.left++
				default:
					r.refs = append(r.refs, ref)
					bytes += cost
					count++
				}
			}
			if len(r.refs) > 0 || r.left > 0 {
				picked[p.ToolResult.CallID] = r
			}
		}
	}
	return picked
}

// encodeImages turns the chosen references into content blocks. Whatever fails to read here is
// counted as left behind rather than failing the request: the result's own text still names it.
//
// Read from disk at this moment rather than carried in the log: the log holds references precisely
// so that it stays small, and the bytes are only needed while a request is being built.
func encodeImages(r riders) (blocks []any, left int) {
	left = r.left
	for _, ref := range r.refs {
		raw, err := os.ReadFile(ref.Path)
		if err != nil {
			left++
			continue
		}
		mime := ref.MIME
		if mime == "" {
			mime = "application/octet-stream"
		}
		blocks = append(blocks, imageBlock{
			Type:     "image_url",
			ImageURL: imageURL{URL: "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(raw)},
		})
	}
	return blocks, left
}

// imageMessage is the user message that carries a tool call's pictures.
//
// It is a USER message and not part of the tool result, because the API has no room for a picture
// in a tool result: role "tool" takes a string. So the pictures follow the result they belong to,
// with one line saying whose they are — without it the model is handed an image out of nowhere,
// after a tool result that said a file was written.
func imageMessage(callID string, blocks []any, left int) wireMessage {
	said := fmt.Sprintf("The images from tool call %s:", callID)
	if left > 0 {
		said += fmt.Sprintf(" (%d more were left out — too large for one request, "+
			"and named in the result above)", left)
	}
	content := append([]any{textBlock{Type: "text", Text: said}}, blocks...)
	return wireMessage{Role: "user", Content: content}
}
