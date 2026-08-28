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
// A model nobody has declared, AND whose family nobody has declared, answers false. The table
// answers a ":tag" variant from its family — how "qwen3-vl:8b" gets the facts of "qwen3-vl", and
// how every other field in it works — so a variant of a vision model is taken to see. That is the
// right answer for a quantisation and the wrong one for a distill that dropped the encoder; the
// registry cannot tell them apart, and a name nobody has declared at all is still false.
//
// False is the safe direction. A request with an image_url block to a backend that cannot read one
// is either an error or, worse, silently dropped content the model is then asked about. The text
// line naming the file goes either way, so false costs the model a picture, never the fact that
// there was one.
func (c *Client) seesImages(modelID string) bool {
	if c.vision != nil {
		return c.vision(modelID)
	}
	return catalogue().Get(modelID).Vision
}

// What one request may carry in pictures, in bytes and in count.
//
// A conversation accumulates them: reviewing a deck is dozens of renders, and re-sending every one
// on every turn would grow the request without bound and re-bill it each time. And a picture is
// thousands of tokens that a text estimate knows nothing about, so the bound is deliberately small
// and BOTH kinds: four pictures and four megabytes, whichever runs out first, counted from the END
// of the conversation.
//
// The bound is not the whole answer to that blindness, only a ceiling on it. The output-cap fit
// charges what rides (ridingTokens, below), because there an under-count is a refused request.
// The COMPACTION trigger is still blind — internal/app's estimateTokens counts text and cannot
// import this package — and there an under-count only means compacting a little late, which the
// overflow-and-retry path already survives.
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

// imageTokens is what one riding picture is charged against the context window.
//
// A flat number, and deliberately a high one. What a backend really charges depends on the
// picture's DIMENSIONS, which the bytes on disk do not give: a PNG's size is its compression, not
// its pixels. The two published formulas bracket the answer — OpenAI counts 85 plus 170 a tile,
// and Anthropic counts width×height/750 up to a 1568² ceiling, which is where ~1600 comes from.
// So this over-charges a small icon several times over and under-charges nothing.
//
// Over-charging is the safe direction here, and cheaply: it can only LOWER an output cap, by at
// most four pictures' worth, and only in a window already nearly full. Under-charging is how the
// request goes over the window and the turn dies — the failure fitMaxTokens exists to prevent.
const imageTokens = 1600

// ridingTokens is what the pictures on this request cost the window.
//
// Counted from pickImages rather than from the parts, so the estimate charges for exactly the
// pictures that will be SENT: the budget drops some, a repeat of the same file rides once, and a
// picture whose file has gone rides not at all. That is a second walk of the tail of the
// conversation, and a second stat of each candidate, which is the price of the two numbers being
// about the same request — the alternative is an estimate of a body nobody built.
func ridingTokens(msgs []session.Message) int {
	n := 0
	for _, r := range pickImages(msgs) {
		n += len(r.refs)
	}
	return n * imageTokens
}

// riders are the pictures of one tool call that go on this request, and what happened to the ones
// that did not. Two counters, not one total, because the model is told the reason and the two
// reasons ask for different things of it: a picture left behind for SIZE invites a smaller render,
// and one left behind because the request already carries four does not.
type riders struct {
	refs    []session.ImageRef
	tooBig  int
	tooMany int
}

func (r riders) left() int { return r.tooBig + r.tooMany }

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
	// Which files are already riding. keepImages names a picture by its CONTENT, so two calls that
	// produced the same bytes share one file — which is what re-rendering a deck after changing one
	// slide produces, every time. Counted per ref, the same picture took two of the four places and
	// two thirds of the budget, and the model was sent it twice. A repeat is not "left out" either:
	// the request already carries it, and the result's text names the same path.
	seen := map[string]bool{}
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
				if seen[ref.Path] {
					continue
				}
				fi, err := os.Stat(ref.Path)
				if err != nil {
					continue // gone; its line of text still names it, and that is not an error
				}
				cost := onWire(int(fi.Size()))
				switch {
				case count >= imagesPerReply:
					r.tooMany++
				case count > 0 && bytes+cost > imageBudget:
					r.tooBig++
				default:
					r.refs = append(r.refs, ref)
					seen[ref.Path] = true
					bytes += cost
					count++
				}
			}
			if len(r.refs) > 0 || r.left() > 0 {
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
func encodeImages(r riders) (blocks []any, left riders) {
	left = riders{tooBig: r.tooBig, tooMany: r.tooMany}
	for _, ref := range r.refs {
		raw, err := os.ReadFile(ref.Path)
		if err != nil {
			left.tooBig++ // it was chosen and could not be read; the honest half of "not here"
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
func imageMessage(callID string, blocks []any, left riders) wireMessage {
	said := fmt.Sprintf("The images from tool call %s:", callID)
	// Why they were left out, not just how many. Saying "too large" about pictures that were left
	// behind by the COUNT tells the model to render smaller, which does not help and costs it a
	// round: at four pictures the size of each one is not what ran out.
	switch {
	case left.tooBig > 0 && left.tooMany > 0:
		said += fmt.Sprintf(" (%d more were left out — %d too large for one request and %d over the "+
			"limit of %d pictures; all are named in the result above)",
			left.left(), left.tooBig, left.tooMany, imagesPerReply)
	case left.tooBig > 0:
		said += fmt.Sprintf(" (%d more were left out — too large for one request, "+
			"and named in the result above)", left.tooBig)
	case left.tooMany > 0:
		said += fmt.Sprintf(" (%d more were left out — one request carries at most %d pictures, "+
			"not because of their size; all are named in the result above)", left.tooMany, imagesPerReply)
	}
	content := append([]any{textBlock{Type: "text", Text: said}}, blocks...)
	return wireMessage{Role: "user", Content: content}
}
