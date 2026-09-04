package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// mcpTool adapts a remote MCP tool to port.Tool. name is the NAMESPACED name the agent calls (so it
// cannot shadow a builtin or another server's tool — the registry replaces by name); remote is the
// server-side name the call is forwarded to.
type mcpTool struct {
	// byOwner is one hand per conversation. The empty key is the daemon-wide hand — a server
	// declared in config, or attached without naming a session, which is what every attach meant
	// before conversations could own one.
	//
	// **A map rather than one client because the registry is keyed by name.** Two PowerPoint decks
	// attach the same server name; registering twice would replace the first tool object and the
	// first deck would silently lose its hand. So the second attach MERGES here, and the call
	// picks the hand by the session it came from.
	mu          sync.Mutex
	byOwner     map[string]*Client
	name        string
	remote      string
	description string
	schema      json.RawMessage
	// imageDir is where a picture this tool returns is kept — the daemon's data directory. Empty
	// when the host gave none, and then images are reported as dropped rather than written
	// somewhere that disappears.
	imageDir string
}

// VisibleTo answers port.Owned. A daemon-wide hand shows to everyone; otherwise only the
// conversation that attached one.
func (t *mcpTool) VisibleTo(session string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, all := t.byOwner[""]; all {
		return true
	}
	_, mine := t.byOwner[session]
	return mine
}

// adopt takes another attach's hand under this name. **Merging, not replacing** — see byOwner.
func (t *mcpTool) adopt(owner string, c *Client) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.byOwner[owner] = c
}

// release drops that conversation's hand and answers how many are left. Zero means the tool has
// nobody behind it and the caller takes it out of the registry.
func (t *mcpTool) release(owner string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.byOwner, owner)
	return len(t.byOwner)
}

// handFor picks the hand for that conversation: its own, else the daemon-wide one.
func (t *mcpTool) handFor(session string) *Client {
	t.mu.Lock()
	defer t.mu.Unlock()
	if c, ok := t.byOwner[session]; ok {
		return c
	}
	return t.byOwner[""]
}

func (t *mcpTool) Name() string            { return t.name }
func (t *mcpTool) Description() string     { return t.description }
func (t *mcpTool) Schema() json.RawMessage { return t.schema }

// Execute forwards the call to the MCP server and flattens its content blocks into the tool
// result: the text as text, and any images to disk with references beside it.
//
// The flattening used to be text-only, and an image block carries no text — so a server that
// answered with a rendered slide returned the empty string and the model was told the call had
// worked and produced nothing.
func (t *mcpTool) Execute(ctx context.Context, args json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	// **광고를 막는 것으로는 모자란다.** 이름을 이미 아는 모델은 그냥 부른다 — 앞 대화에서 봤거나,
	// 사람이 적어 줬거나, 지어냈거나. 그러면 그 호출은 남의 대화에 붙은 손으로 가고, PowerPoint
	// 라면 **사람이 보고 있지도 않은 덱**이 고쳐진다.
	hand := t.handFor(string(env.SessionID))
	if hand == nil {
		b, _ := json.Marshal("this tool belongs to another conversation — it was attached for a " +
			"different session and this call was not run")
		return session.ToolResult{Content: b, IsError: true}, nil
	}
	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	res, err := hand.CallTool(cctx, t.remote, args)
	if err != nil {
		b, _ := json.Marshal(err.Error())
		return session.ToolResult{Content: b, IsError: true}, nil
	}

	kept, notes := keepImages(t.imageDir, string(env.SessionID), t.name, res.Content)
	var sb strings.Builder
	for _, c := range res.Content {
		if c.Type == "image" {
			continue // 그림은 글이 아니다 — 아래에서 한 줄로 말하고, 파일은 kept가 든다
		}
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(c.Text)
	}
	// The text says a picture came back, because the text is what every reader gets: a model whose
	// backend cannot take images, a terminal, a log somebody greps a week later.
	for _, k := range kept {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString("[image: " + k.MIME + " at " + k.Path + "]")
	}
	for _, n := range notes {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(n)
	}
	b, _ := json.Marshal(sb.String())
	return session.ToolResult{Content: b, IsError: res.IsError, Images: kept}, nil
}
