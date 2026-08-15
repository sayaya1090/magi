// Package embed turns text into vectors, so a search can find "billing" when somebody asked about
// invoices.
//
// # Why lexical ranking is not enough, and why it still stays
//
// IDF ranking (internal/core/rank) is honest about what it does: it finds documents that share
// WORDS with the query, and weights the rare ones. It cannot find a note about the invoice job when
// the question was about billing, because those two strings have nothing in common. Every real
// question somebody asks another companion is phrased in their own words, not in the words the
// answer happened to use.
//
// So both, fused. Not embeddings alone: an embedding is a similarity judgement made by a model, and
// it is confidently wrong often enough that a rare exact token — a file name, an error code, an
// identifier — must not be able to lose to something that merely feels related. The lexical half is
// the floor; the semantic half is what gets you to the answer when you did not know its vocabulary.
//
// # What happens when there is no model
//
// Nothing breaks and nothing pretends. An endpoint that is absent, refusing, or slow leaves the
// search lexical, and the caller is told which it got — a search that silently became worse is a
// search whose results nobody can interpret.
package embed

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Client turns text into vectors through an OpenAI-compatible /v1/embeddings endpoint.
//
// The same endpoint the rest of magi talks to, because a second backend to configure is a second
// thing to be wrong. Nothing here is magi-specific: Ollama, vLLM, LiteLLM and the hosted APIs all
// answer this shape.
type Client struct {
	BaseURL string
	APIKey  string
	Model   string
	HTTP    *http.Client
	// Warn reports something that did not stop the answer but will cost the next one. Optional; a
	// caller that sets nothing gets silence, which is right for a test and wrong for a daemon.
	Warn func(string)
	// CacheDir holds one file per (model, text) pair. An entry is embedded once and then read from
	// disk for the life of the machine: the text is content-addressed, so an edited note is a new
	// key and a stale vector cannot be served for it.
	CacheDir string

	warnedMu sync.Mutex
	warned   map[string]bool
}

// Available reports whether this client has enough to try. Callers check it rather than discovering
// the answer through a failed request per query.
func (c *Client) Available() bool {
	return c != nil && strings.TrimSpace(c.BaseURL) != "" && strings.TrimSpace(c.Model) != ""
}

// warn reports at most once per distinct message. A cache directory that cannot be written to
// fails on every entry of every search, and the same line a hundred times is not a hundred facts.
func (c *Client) warn(msg string) {
	if c.Warn == nil {
		return
	}
	c.warnedMu.Lock()
	defer c.warnedMu.Unlock()
	if c.warned == nil {
		c.warned = map[string]bool{}
	}
	if c.warned[msg] {
		return
	}
	c.warned[msg] = true
	c.Warn(msg)
}

func (c *Client) key(text string) string {
	sum := sha256.Sum256([]byte(c.Model + "\x00" + text))
	return hex.EncodeToString(sum[:])
}

// readCached returns a vector already on disk, or nil.
func (c *Client) readCached(text string) []float32 {
	if c.CacheDir == "" {
		return nil
	}
	b, err := os.ReadFile(filepath.Join(c.CacheDir, c.key(text)))
	if err != nil || len(b) == 0 || len(b)%4 != 0 {
		return nil
	}
	out := make([]float32, len(b)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return out
}

func (c *Client) writeCached(text string, v []float32) {
	if c.CacheDir == "" || len(v) == 0 {
		return
	}
	if err := os.MkdirAll(c.CacheDir, 0o700); err != nil {
		return
	}
	b := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(f))
	}
	if err := os.WriteFile(filepath.Join(c.CacheDir, c.key(text)), b, 0o600); err != nil {
		// A failed write costs a re-embed next time and nothing else — the answer this call is
		// about is already correct — but a cache that never lands turns every search into a paid
		// round trip, and a search that got slower for no visible reason is the kind of thing
		// somebody spends an afternoon on.
		c.warn("cannot cache an embedding in " + c.CacheDir + ": " + err.Error())
	}
}

// Embed returns one vector per input, in order, reading from the cache where it can.
//
// Everything not cached goes in ONE request. A note store is a few dozen entries and one round trip
// is the difference between a search that answers and a search somebody stops waiting for.
func (c *Client) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	var missing []string
	var where []int
	for i, t := range texts {
		if v := c.readCached(t); v != nil {
			out[i] = v
			continue
		}
		missing = append(missing, t)
		where = append(where, i)
	}
	if len(missing) == 0 {
		return out, nil
	}
	body, err := json.Marshal(map[string]any{"model": c.Model, "input": missing})
	if err != nil {
		return nil, err
	}
	url := strings.TrimRight(c.BaseURL, "/") + "/embeddings"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	cl := c.HTTP
	if cl == nil {
		// Bounded. This sits in front of somebody's question, and an endpoint that hangs must cost
		// the search its semantic half rather than cost the caller the whole answer.
		cl = &http.Client{Timeout: 20 * time.Second}
	}
	resp, err := cl.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var parsed struct {
		Data []struct {
			// A POINTER so an omitted index stays distinct from an explicit 0: a plain int decoded a
			// missing field to 0, and the loop below then mapped EVERY vector onto slot 0 (leaving the
			// rest nil and caching the wrong vector under text[0]'s key). See the useIndex guard.
			Index     *int      `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("embeddings answered %s: %w", resp.Status, err)
	}
	if parsed.Error != nil {
		return nil, fmt.Errorf("embeddings: %s", parsed.Error.Message)
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("embeddings answered %s", resp.Status)
	}
	if len(parsed.Data) != len(missing) {
		// Positional, so a short answer would silently pair vectors with the wrong texts. Refused
		// rather than trusted: a search ranked on somebody else's vector is wrong in a way nobody
		// reading the results could detect.
		return nil, fmt.Errorf("asked for %d embeddings and got %d", len(missing), len(parsed.Data))
	}
	// Indexed by the response's own index — but only when EVERY row carries one. A backend that omits
	// index (some OpenAI-compatible servers do) would otherwise collapse the whole batch onto slot 0.
	// If any row is missing it, the field is not being given; fall back to positional order (the count
	// already matched above), which is the order the request was sent in.
	useIndex := true
	for i := range parsed.Data {
		if parsed.Data[i].Index == nil {
			useIndex = false
			break
		}
	}
	for pos, d := range parsed.Data {
		at := pos
		if useIndex && *d.Index >= 0 && *d.Index < len(missing) {
			at = *d.Index
		}
		out[where[at]] = d.Embedding
		c.writeCached(missing[at], d.Embedding)
	}
	return out, nil
}

// Cosine is the similarity of two vectors, in [-1, 1]. Zero for a length mismatch or an empty one,
// which is what a caller wants: no opinion rather than a wrong one.
func Cosine(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		x, y := float64(a[i]), float64(b[i])
		dot += x * y
		na += x * x
		nb += y * y
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// Fuse merges two rankings of the same documents into one, by reciprocal rank.
//
// # Why rank and not score
//
// An IDF score and a cosine are not comparable numbers — one is a sum of logs with no ceiling, the
// other is bounded at 1 — so any weighted sum of them needs a normalisation that is invented, tuned
// on whatever corpus happened to be at hand, and wrong on the next one. Reciprocal Rank Fusion uses
// only the ORDER each side put things in, which is the part both are actually asserting.
//
// score(d) = Σ 1/(k + rank(d)), k = 60, the constant from the paper this comes from. It is there so
// the top of a list does not dominate absolutely: without it, first place in one ranking beats
// second place in both, and agreement between the two methods is the signal worth most.
func Fuse(lists ...[]int) []int {
	const k = 60.0
	score := map[int]float64{}
	// First appearance, so ties break the way the first list ordered them rather than by map order.
	var order []int
	for _, list := range lists {
		for rank, id := range list {
			if _, seen := score[id]; !seen {
				order = append(order, id)
			}
			score[id] += 1 / (k + float64(rank+1))
		}
	}
	at := make(map[int]int, len(order))
	for i, id := range order {
		at[id] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		if score[order[a]] != score[order[b]] {
			return score[order[a]] > score[order[b]]
		}
		return at[order[a]] < at[order[b]]
	})
	return order
}
