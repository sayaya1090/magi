// Package mcpserve answers MCP over stdin and stdout, so one companion can be asked what it knows.
//
// # Why every companion is a server rather than one board being one
//
// The alternative was a shared store everybody posts into. It has a central part that can be down,
// it separates knowledge from the companion that earned it, and it needs somebody to decide what is
// worth publishing before anybody has asked. Serving instead means the knowledge stays where it was
// written and the question goes to it — which is also what happens between people.
//
// # Why stdio, and why that is the whole security story
//
// The MCP client this tree already has speaks stdio and HTTP. HTTP would mean a listening port with
// no authentication on it, which is the direction magi-web has refused from the start (it binds
// loopback and says so). stdio means the caller runs a process:
//
//	command = "magi --mcp --to design"                 # another companion on this machine
//	command = "ssh buildbox magi --mcp --to design"    # …or on another one
//
// So reaching a companion requires being able to run a program as somebody who can read their
// files. There is no port, and crossing a machine boundary is ssh's job, which is a thing operators
// already know how to reason about.
//
// # This is no longer read-only, and that is worth saying out loud
//
// For most of its life this package only READ: it answered questions about a companion out of its
// experience store, and the sentence above could end "nothing new is exposed". The ear changes
// that. A caller can now put a message into another companion's conversation, which makes that
// daemon do something.
//
// It is not a new privilege. Anything that can start `magi --mcp` can dial the same daemon socket
// directly, and the socket is owner-only — so the boundary is unchanged and it is the same boundary
// as before: the user who owns these files. What changed is what this package's own claim is worth,
// so the claim is rewritten rather than left standing while the code walked out from under it.
//
// The delivery itself is NOT written here. Server takes an Ear closure from whoever wired it up:
// dialling a daemon belongs to a package this one does not import, and a server constructed without
// one does not advertise the tool at all.
//
// # Four tools, because four different things get asked
//
// `ask` says something to the companion, now, while both are working. `about` answers "what are you
// for, and what can you be asked to do". `knows` answers "what have you got on X" with one line
// each. `detail` returns one entry whole.
//
// The last two are one question split in half deliberately: a search that returned full bodies
// would put four pages of somebody else's notes into the asker's context to answer something a
// title might have settled, and the caller cannot know which until it has seen the titles.
//
// `ask` is the one that makes a companion something you can work WITH rather than only read from.
// Its recipient is the tool name — `mcp__design__ask` — which is not a convenience: a name in an
// argument is a name a model can invent, and the recorded failure here is a request addressed to a
// companion called "ssh", which does not exist. A name that is part of the tool being called cannot
// be got wrong.
//
// What it deliberately does NOT do is wait. The reply is not this call's result; it is the other
// companion saying something back through the ear on this side. So the same door carries the
// question, the answer, and whatever follows, in both directions, and nothing blocks for the
// minutes that answer takes. Starting a piece of work is a different act with different rules, and
// stays in hand_off — that one refuses a chain, refuses to land inside a running turn, and knows
// when the other side finishes.
//
// `about` is the one that was missing, and its absence had a shape. A companion advertised a store
// and nothing else — so a model deciding whether to ask design or api had a role clause buried in
// a tool description and no way to learn that design has three written procedures and can reach the
// design tooling. What it can be asked to do is public; the COMMAND it would run to do it is not
// (see about()).
package mcpserve

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	expgit "github.com/sayaya1090/magi/internal/adapter/experience/git"
	"github.com/sayaya1090/magi/internal/core/embed"
	"github.com/sayaya1090/magi/internal/core/rank"
	"github.com/sayaya1090/magi/internal/core/text"
	"github.com/sayaya1090/magi/internal/port"
)

// Server answers for one companion.
type Server struct {
	// Name is what this companion is called, so an answer says who it came from. A model reading
	// three of these back to back has no other way to tell them apart.
	Name string
	// Role is what it is for, one line. It goes in the server's own description because the first
	// question about an unfamiliar tool is whether it is the right one to call.
	Role string
	// Dir is the experience store to answer from — a companion's own workspace tier.
	Dir string
	// Now is injectable so a test can assert on what is rendered rather than on today's date.
	Now func() string
	// Embed is optional. With it, a search finds a note about the invoice job when the question was
	// about billing; without it, the search is lexical and says so. Never fatal: an endpoint that
	// is absent, refusing or slow costs the semantic half and nothing else.
	Embed *embed.Client

	// What this companion is, beyond its notes — see the `about` tool.
	Team    string
	Hub     bool
	Workdir string
	// Skills are the named procedures written down in this companion's workspace: the closest
	// thing there is to a list of what it can be asked to do.
	Skills []port.Skill
	// Reach names the external tool servers this companion is configured to talk to. NAMES ONLY,
	// and that is a rule rather than a convenience — see about().
	Reach []string

	// Ear puts a message into this companion's conversation, and Caller is who is putting it there.
	//
	// Supplied rather than built here: delivering means dialling a daemon, which belongs to a
	// package this one does not import. Both nil/empty on a server that only answers questions
	// about a companion, and then the ear is not advertised at all — a tool in the list that
	// always refuses is worse than one that is not there.
	Ear    func(from, text string) error
	Caller string
}

// entry is one thing this companion wrote down, flattened out of the store's two kinds.
type entry struct {
	ID       string
	Kind     string // "rule" or "fact"
	Title    string
	Body     string
	Observed int
	Seen     string
}

func (s *Server) load(ctx context.Context) ([]entry, error) {
	inv, err := expgit.New(s.Dir).Inventory(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]entry, 0, len(inv))
	for _, e := range inv {
		kind := "rule"
		if e.Kind == "memory" {
			kind = "fact"
		}
		title := strings.TrimSpace(e.Description)
		if title == "" {
			title = strings.TrimSpace(firstLine(e.Body))
		}
		seen := e.FirstSeen
		if e.LastSeen != "" && e.LastSeen != e.FirstSeen {
			seen = e.FirstSeen + " → " + e.LastSeen
		}
		out = append(out, entry{
			ID: e.Name, Kind: kind, Title: title, Body: e.Body,
			Observed: e.Observed, Seen: seen,
		})
	}
	return out, nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// knows searches, and answers in lines rather than JSON.
//
// The reader is a model deciding whether to ask for the whole of something. That decision is made
// on the title, how often the thing has been seen, and how recently — not on a schema.
func (s *Server) knows(ctx context.Context, about string, limit int) string {
	all, err := s.load(ctx)
	if err != nil {
		return "cannot read what " + s.Name + " has written down: " + err.Error()
	}
	if len(all) == 0 {
		return s.Name + " has written nothing down."
	}
	docs := make([]string, len(all))
	for i, e := range all {
		// Title and body both: a note whose title is "the staging database" and whose body names
		// the table somebody asked about should be findable by the table.
		docs[i] = e.Title + " " + e.Body
	}
	lexical := rank.ByIDF(about, docs)
	order, how := s.fuse(ctx, about, docs, lexical)
	if len(order) == 0 {
		// Named, so the caller can tell "nothing on this" from "wrong companion". Without it the
		// two look identical and the caller asks the same question again somewhere else.
		return fmt.Sprintf("%s has %d entries and none of them mention %q.", s.Name, len(all), about)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s on %q — %d of %d entries, %s, best first. Ask `detail` with an id for the whole thing.\n",
		s.Name, about, min(len(order), limit), len(all), how)
	for i, at := range order {
		if i == limit {
			fmt.Fprintf(&b, "\n(+%d weaker matches — narrow the topic)\n", len(order)-limit)
			break
		}
		e := all[at]
		fmt.Fprintf(&b, "\n%s · %s · %s", e.Kind, e.ID, e.Title)
		if e.Observed > 1 {
			fmt.Fprintf(&b, " · seen %d×", e.Observed)
		}
		if e.Seen != "" {
			fmt.Fprintf(&b, " · %s", e.Seen)
		}
	}
	b.WriteString("\n")
	return b.String()
}

// fuse blends the lexical ranking with a semantic one, and says which the caller got.
//
// Said out loud because the two answer differently and a reader has to know which they are holding:
// a lexical search that found nothing means those words are not in the store, and a semantic one
// that found nothing means rather more than that. A search that silently became worse — the model
// endpoint went away — is a search whose results nobody can interpret.
func (s *Server) fuse(ctx context.Context, about string, docs []string, lexical []rank.Hit) ([]int, string) {
	lex := make([]int, len(lexical))
	for i, h := range lexical {
		lex[i] = h.Index
	}
	if !s.Embed.Available() {
		return lex, "by wording"
	}
	// The documents and the query in one request; the documents are cached after the first search,
	// so the steady-state cost is one embedding for the query.
	vecs, err := s.Embed.Embed(ctx, append(append([]string{}, docs...), about))
	if err != nil || len(vecs) != len(docs)+1 {
		// Reported in the answer rather than swallowed. The caller asked a question and got the
		// lexical answer to it, which is a fine answer as long as nobody thinks it was the other.
		return lex, "by wording only — no embeddings (" + errWord(err) + ")"
	}
	q := vecs[len(vecs)-1]
	type sim struct {
		at    int
		score float64
	}
	var sims []sim
	for i := range docs {
		if c := embed.Cosine(vecs[i], q); c > 0 {
			sims = append(sims, sim{i, c})
		}
	}
	sort.SliceStable(sims, func(a, b int) bool { return sims[a].score > sims[b].score })
	sem := make([]int, len(sims))
	for i, sm := range sims {
		sem[i] = sm.at
	}
	return embed.Fuse(lex, sem), "by wording and meaning"
}

func errWord(err error) string {
	if err == nil {
		return "the backend answered with the wrong number of vectors"
	}
	return text.Clip(err.Error(), 120)
}

// detail returns one entry whole.
func (s *Server) detail(ctx context.Context, id string) string {
	all, err := s.load(ctx)
	if err != nil {
		return "cannot read what " + s.Name + " has written down: " + err.Error()
	}
	for _, e := range all {
		if strings.EqualFold(e.ID, id) {
			return fmt.Sprintf("%s · %s · %s\nfrom %s\n\n%s", e.Kind, e.ID, e.Title, s.Name,
				strings.TrimSpace(e.Body))
		}
	}
	// The ids that exist, because a caller who mistyped one has no other way to find out and
	// guessing again is the only thing left to it.
	ids := make([]string, 0, len(all))
	for _, e := range all {
		ids = append(ids, e.ID)
	}
	sort.Strings(ids)
	return fmt.Sprintf("%s has nothing called %q. It has: %s",
		s.Name, id, text.Clip(strings.Join(ids, ", "), 600))
}

// about says what this companion is for and what it can be asked to do.
//
// # Why this is not the same question as `knows`
//
// `knows` answers "what have you written down about X" — a search of a store. It cannot answer
// "should I be asking you at all", and that is the question a model has first. Before this, the
// only trace of it was the role clause inside the other two tools' descriptions, so a companion
// with three skills and a connection to the билling database advertised none of it: from outside it
// was a name with a store behind it.
//
// # Names only, and that is the boundary
//
// The reachable servers are listed by NAME. Not the command, not the arguments, not a URL, and
// never an environment value. An [mcp] entry is a command this process would run, and join.go
// refuses to copy one across for exactly that reason — "the companion I joined told me to" is not
// a sentence anybody should find in an incident report. Advertising is the same act at a distance,
// so it obeys the same rule: what design can be ASKED to do is public; what design would RUN to do
// it is design's own.
//
// The same reasoning covers a URL, which is not obviously executable and is worse in one way: it
// can carry an internal host, and often a token in a query string.
func (s *Server) about() string {
	var b strings.Builder
	b.WriteString(s.Name)
	if s.Role != "" {
		b.WriteString(" — " + s.Role)
	}
	b.WriteString("\n")
	if s.Team != "" {
		b.WriteString("team: " + s.Team)
		if s.Hub {
			// Worth saying: addressing the team reaches the hub, so a caller that has this one has
			// already reached whoever answers for the rest.
			b.WriteString(" (answers for the team)")
		}
		b.WriteString("\n")
	}
	if s.Workdir != "" {
		b.WriteString("workspace: " + s.Workdir + "\n")
	}
	if len(s.Skills) > 0 {
		b.WriteString("\nWhat it has written procedures for:\n")
		for _, sk := range s.Skills {
			b.WriteString("  " + sk.Name)
			if d := strings.TrimSpace(sk.Description); d != "" {
				b.WriteString(" — " + text.Clip(oneLine(d), 160))
			}
			b.WriteString("\n")
		}
	}
	if len(s.Reach) > 0 {
		b.WriteString("\nExternal tool servers it can reach: " + strings.Join(s.Reach, ", ") + "\n" +
			"Names only. That says what it can be asked to do, not what you can run — to use one\n" +
			"of those yourself you configure it in your own workspace, on purpose.\n")
	}
	if len(s.Skills) == 0 && len(s.Reach) == 0 {
		b.WriteString("\nIt declares no skills and no external tool servers. What it has is what it\n" +
			"has written down — ask `knows`.\n")
	}
	return b.String()
}

// oneLine flattens a description so a multi-line first paragraph cannot break the list's shape.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// tools is what this server advertises.
func (s *Server) tools() []map[string]any {
	who := s.Name
	if s.Role != "" {
		who += " (" + s.Role + ")"
	}
	out := []map[string]any{}
	if s.Ear != nil {
		// The recipient is the TOOL NAME. That is the whole reason this is a tool per companion
		// rather than one tool with a `to` field: a name in a field is a name a model can invent,
		// and this tree has a recorded failure of exactly that — a request addressed to a
		// companion called "ssh", which does not exist. A name that is part of the tool it is
		// calling cannot be got wrong.
		out = append(out, map[string]any{
			"name": "ask",
			"description": "Say something to " + who + " right now, while you are both working: a " +
				"question you need answered to carry on, the answer to something they asked you, or " +
				"anything else in an exchange already under way. It goes into their conversation " +
				"and returns at once — they answer by saying something back to you the same way, " +
				"so do not wait here for a reply. To START a piece of work with them, use hand_off " +
				"instead: that one waits for their result and knows when they finish.",
			"inputSchema": json.RawMessage(`{"type":"object","properties":{
				"ask":{"type":"string","description":"what to say, standing on its own — they cannot see your conversation"}
			},"required":["ask"],"additionalProperties":false}`),
		})
	}
	return append(out, []map[string]any{
		{
			// A third tool per peer is not free — the tool list is paid on every prompt, and four
			// companions make this twelve entries rather than eight. It is here rather than folded
			// into the other two descriptions because a DESCRIPTION is paid every prompt while an
			// ANSWER is paid when somebody asks, and this answer is several lines long.
			"name": "about",
			"description": "What " + s.Name + " is for, and what it can be asked to do: its role, " +
				"its skills, and the external tool servers it can reach. Read this before asking " +
				"it for work.",
			"inputSchema": json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
		},
		{
			"name": "knows",
			"description": "Search what " + who + " has written down — its lessons and remembered " +
				"facts — and get one line each, best match first. This is what it RECORDED, not " +
				"what it is doing right now.",
			"inputSchema": json.RawMessage(`{"type":"object","properties":{
				"about":{"type":"string","description":"the topic, in a few words"},
				"limit":{"type":"integer","description":"how many lines at most (default 8)"}
			},"required":["about"],"additionalProperties":false}`),
		},
		{
			"name":        "detail",
			"description": "Get one of " + who + "'s entries in full, by the id `knows` printed.",
			"inputSchema": json.RawMessage(`{"type":"object","properties":{
				"id":{"type":"string","description":"the id from a knows result"}
			},"required":["id"],"additionalProperties":false}`),
		},
	}...)
}

// defaultLimit is how many lines come back when the caller does not say. Enough to see a topic's
// shape, few enough to read — and this lands in somebody's context, where every line costs.
const defaultLimit = 8

// Serve reads JSON-RPC from in and writes answers to out, until in ends.
//
// One message per line, which is what the client on the other side writes. Errors in a request are
// answered as errors rather than logged and dropped: a client waiting on an id it will never be
// answered for hangs, and the reason is invisible from over there.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	sc := bufio.NewScanner(in)
	// A body is a note somebody wrote; the default 64KB line limit would truncate one silently.
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	enc := json.NewEncoder(out)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			continue // not a message; there is nobody to tell, since we do not know the id
		}
		// A notification has no id and takes no answer. Replying to one is a protocol error the
		// client is entitled to complain about.
		if len(req.ID) == 0 {
			continue
		}
		result, rpcErr := s.handle(ctx, req.Method, req.Params)
		reply := map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(req.ID)}
		if rpcErr != "" {
			reply["error"] = map[string]any{"code": -32601, "message": rpcErr}
		} else {
			reply["result"] = result
		}
		if err := enc.Encode(reply); err != nil {
			return err
		}
	}
	return sc.Err()
}

func (s *Server) handle(ctx context.Context, method string, params json.RawMessage) (any, string) {
	switch method {
	case "initialize":
		return map[string]any{
			// The version the client asks for. Answering with a different one is how two
			// implementations that would have worked decide not to talk.
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "magi:" + s.Name, "version": "1"},
		}, ""
	case "tools/list":
		return map[string]any{"tools": s.tools()}, ""
	case "tools/call":
		var p struct {
			Name string          `json:"name"`
			Args json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return errResult("could not read the call: " + err.Error()), ""
		}
		return s.call(ctx, p.Name, p.Args), ""
	default:
		return nil, "no such method: " + method
	}
}

// call runs one tool. A tool that refuses answers with isError rather than a JSON-RPC error: the
// call reached the server and was understood, and the difference matters to a model deciding
// whether to try a different argument or a different tool.
func (s *Server) call(ctx context.Context, name string, args json.RawMessage) map[string]any {
	switch name {
	case "ask":
		if s.Ear == nil {
			return errResult(s.Name + " cannot be reached this way from here")
		}
		var p struct {
			Ask string `json:"ask"`
		}
		if err := json.Unmarshal(args, &p); err != nil {
			return errResult("could not read the arguments: " + err.Error())
		}
		if strings.TrimSpace(p.Ask) == "" {
			return errResult("say something. An empty message is a turn spent on nothing at both ends")
		}
		if err := s.Ear(s.Caller, p.Ask); err != nil {
			return errResult("could not reach " + s.Name + ": " + err.Error())
		}
		return okResult("Said to " + s.Name + ". They will see it at their next step and answer the " +
			"same way if it needs one — carry on; nothing arrives as the result of this call.")
	case "about":
		return okResult(s.about())
	case "knows":
		var a struct {
			About string `json:"about"`
			Limit int    `json:"limit"`
		}
		if err := json.Unmarshal(args, &a); err != nil {
			return errResult("could not read the arguments: " + err.Error())
		}
		if strings.TrimSpace(a.About) == "" {
			return errResult("say what the topic is. Asking for everything returns the whole " +
				"store, which is not an answer")
		}
		if a.Limit <= 0 || a.Limit > 40 {
			a.Limit = defaultLimit
		}
		return okResult(s.knows(ctx, a.About, a.Limit))
	case "detail":
		var a struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(args, &a); err != nil {
			return errResult("could not read the arguments: " + err.Error())
		}
		if strings.TrimSpace(a.ID) == "" {
			return errResult("say which entry — the id is in a `knows` result")
		}
		return okResult(s.detail(ctx, a.ID))
	default:
		return errResult("no such tool: " + name + ". This server has " + strings.Join(s.toolNames(), ", "))
	}
}

func okResult(s string) map[string]any {
	return map[string]any{"content": []map[string]any{{"type": "text", "text": s}}}
}

func errResult(s string) map[string]any {
	return map[string]any{"isError": true, "content": []map[string]any{{"type": "text", "text": s}}}
}

// toolNames is what this server actually advertises, for the refusal that names them. Read off
// tools() rather than written beside it: a list kept by hand is one that goes on naming a tool
// after it is gone, which sends a caller to try it again.
func (s *Server) toolNames() []string {
	out := make([]string, 0, 4)
	for _, t := range s.tools() {
		if n, _ := t["name"].(string); n != "" {
			out = append(out, n)
		}
	}
	return out
}
