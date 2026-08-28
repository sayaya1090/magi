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

// modelFacts is the capability table, built once. Vision is the only field read here.
var modelFacts = sync.OnceValue(func() *model.Registry { return model.NewRegistry() })

// canSeeImages says whether this model takes image input.
//
// Until now nothing read that flag: seven models declared Vision and no code anywhere asked, because
// the pictures never reached the wire. This is the reader it was waiting for. A model the table does
// not know answers false — a request with an image_url block to a backend that cannot read one is
// either an error or, worse, silently dropped content the model is then asked about.
func canSeeImages(modelID string) bool {
	return modelFacts().Get(modelID).Vision
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
// The most recent are the ones being talked about. An older one keeps its line of text, which
// names the file and its type — the same line a model that cannot see pictures at all reads.
const (
	imageBudget    = 4 << 20
	imagesPerReply = 4
)

// imagesFor turns the pictures attached to a tool result into content blocks, newest first, until
// the budget is spent. Returns what fitted and how many were left out.
//
// Read from disk here rather than carried in the log: the log holds references precisely so that it
// stays small, and the bytes are only needed at the moment a request is built.
func imagesFor(refs []session.ImageRef, budget, count int) (blocks []any, left int, spent int) {
	for _, ref := range refs {
		if len(blocks) >= count {
			left++
			continue
		}
		raw, err := os.ReadFile(ref.Path)
		if err != nil {
			// A picture that no longer resolves is not worth a failed request: the tool result's
			// own text already names it, and that line is what the model reads instead.
			left++
			continue
		}
		if len(raw) > budget-spent {
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
		spent += len(raw)
	}
	return blocks, left, spent
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

// recentImages picks the tool calls whose pictures ride on this request: walking backwards from the
// end of the conversation until the budget is spent.
//
// Backwards because a conversation grows and a request cannot. The render just made is what the
// next question is about; the one from twenty turns ago is history, and its line of text is what
// history needs. Forwards would have pinned the oldest picture in every request for the life of the
// session and dropped the newest.
func recentImages(msgs []session.Message) map[string]bool {
	picked := map[string]bool{}
	bytes, count := 0, 0
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != session.RoleTool {
			continue
		}
		for _, p := range msgs[i].Parts {
			if p.Kind != session.PartToolResult || p.ToolResult == nil || len(p.ToolResult.Images) == 0 {
				continue
			}
			for _, ref := range p.ToolResult.Images {
				fi, err := os.Stat(ref.Path)
				if err != nil {
					continue // gone; its line of text still names it
				}
				if count >= imagesPerReply || bytes+int(fi.Size()) > imageBudget {
					return picked
				}
				bytes += int(fi.Size())
				count++
				picked[p.ToolResult.CallID] = true
			}
		}
	}
	return picked
}
