package idebridge

// The shape of a conversation row, decided in ONE place.
//
// This is the third of the eight (docs/IDE_BRIDGE §3) and the largest: "행을 짓는 규칙은 한 벌"
// is the invariant, and it is the one this repository has most visibly failed to hold. The two
// existing copies are 806 lines of Kotlin and 808 of TypeScript, and measured 2026-09-10 they do
// not agree on what a row even IS:
//
//	TypeScript  user · agent · tool · thinking · council · system · error · image   (eight)
//	Kotlin      User · Agent · Tool · Thinking · Council · Info                     (six)
//
// One image attachment is `image` carrying a bare path on one screen and `Info` carrying
// "🖼 <path>" on the other; an error is `error` on one and `Info` + ⚠ on the other, which the
// Kotlin source says out loud. That is the same defect as the activity vocabulary one layer up —
// two copies, one fact, two words — and it is why this file exists before any of the rule does.
//
// **Eight, not six.** A client that wants Kotlin's six can fold `system`, `error` and `image` into
// one; a client handed six cannot get the picture back. Richer is recoverable, collapsed is not.
// Decided 2026-09-10.

// Who is the kind of speaker a row carries, and the whole set of them.
//
// Constants rather than an iota enum: these cross a wire as words, and a word a newer build does
// not recognise must stay readable rather than becoming whichever member happens to be zero — the
// same reason Activity keeps an unknown state word instead of folding it.
const (
	// WhoUser is what a person said.
	WhoUser = "user"
	// WhoAgent is what the companion said.
	WhoAgent = "agent"
	// WhoTool is a tool call and, once it lands, its result.
	WhoTool = "tool"
	// WhoThinking is reasoning the model showed. Folded shut by default.
	WhoThinking = "thinking"
	// WhoCouncil is one seat's verdict in one round.
	WhoCouncil = "council"
	// WhoSystem is the session saying something about itself — a fold, a restart, a note.
	WhoSystem = "system"
	// WhoError is something that failed, kept apart from WhoSystem on purpose: a screen colours
	// them differently and a person scanning for trouble is scanning for this one.
	WhoError = "error"
	// WhoImage is an attached picture. The row carries the path and nothing else — where the
	// Kotlin copy put a glyph in the text, which is a decision that belongs to whatever draws it.
	WhoImage = "image"
)

// Vocabulary is every Who this bridge can put on a row.
//
// Named rather than left implicit so the cross-copy guard has something to compare, exactly as
// Methods() gives `about` one list to advertise from.
func Vocabulary() []string {
	return []string{WhoUser, WhoAgent, WhoTool, WhoThinking, WhoCouncil, WhoSystem, WhoError, WhoImage}
}

// Row is one line of a conversation as a screen shows it.
//
// Every field is `omitempty` and that is load-bearing: absent and zero are different facts all over
// this struct. An absent Ok means a tool call still running, where false means one that failed; an
// absent Confidence means a member said nothing about how sure they were, where 0 would read as a
// member sure of the opposite.
//
// Draw shallowly (invariant 0-2): what to SAY is the daemon's decision. A client that parses
// payloads to compose sentences composes them once per client, and there are six clients.
type Row struct {
	// Seq is the event that put this row here, so a later frame can find it again.
	Seq int64 `json:"seq"`

	// Draft marks a row built from streaming chunks and not yet written as a fact.
	//
	// It exists so the conversation moves WHILE the model answers: the chunks arrive as deltas and
	// the appended fact is only written once the stream ends, so without this a long answer on a
	// local model is a frozen panel above a status that says "working".
	Draft bool `json:"draft,omitempty"`

	Who  string `json:"who"`
	Text string `json:"text"`

	// CallID is the tool call this row is about, so its result can land on it.
	CallID string `json:"callId,omitempty"`

	// Args is what the call was ASKED to do, in one line.
	//
	// The name alone is not a row: a turn that runs thirty commands draws thirty rows reading
	// "bash ✓" — same glyph, same word, nothing saying which command or which file. The transcript
	// is where a person answers "what did it just do".
	Args string `json:"args,omitempty"`

	// Ok is a tool result. Absent means still running, which is why it is a pointer.
	Ok *bool `json:"ok,omitempty"`

	// Note marks a result that DID the work and left something the agent must read.
	//
	// A post-edit hook's output, a language server's complaint about the file just written. Those
	// set isError on purpose — that is what makes the model stop and act on them — and isError is
	// also what a screen draws its glyph from, so a file that was written and then linted drew as
	// a write that FAILED. "Did the work happen" and "is there something to read" are two
	// questions.
	Note bool `json:"note,omitempty"`

	// Round is which council round this verdict belongs to. Three rounds of three members is nine
	// rows that look alike without it.
	Round int `json:"round,omitempty"`
	// Decision is how the member voted: done, continue, abstain.
	Decision string `json:"decision,omitempty"`
	// Silent says the verdict was never given — backend down, deadline, an unreadable reply.
	//
	// It rides beside an abstain so a surface can say "no answer" where a member never spoke,
	// rather than reporting a failure as a considered abstention.
	Silent bool `json:"silent,omitempty"`
	// Lens is the judging seat this member holds: correctness, verification, completeness. It is
	// WHY a council has three seats — without it three verdicts are interchangeable names.
	Lens string `json:"lens,omitempty"`
	// Rule is the round's own threshold (majority, unanimous). Kept apart from Lens, which belongs
	// to a SEAT: the rule governs how the seats add up.
	Rule string `json:"rule,omitempty"`
	// Opened marks the row that OPENS a round — the convened row, not a verdict.
	Opened bool `json:"opened,omitempty"`
	// ReadOnly says the turn this round judges changed no files. On the opening row because it
	// says what KIND of turn is being weighed.
	ReadOnly bool `json:"readOnly,omitempty"`
	// Cite is the fragment of the record this verdict rests on, or NO-EVIDENCE. It is CHECKABLE,
	// and an empty one on a "done" is itself worth seeing.
	Cite string `json:"cite,omitempty"`
	// Keep is what this member says a revision must preserve — emitted regardless of the decision,
	// because an approving member's keep is what a rewrite forced by somebody else would drop.
	Keep string `json:"keep,omitempty"`
	// Confidence is how sure the member was, 0..1, self-reported and WEIGHED: the tally is a
	// confidence-weighted sum, so a done at 0.2 and a done at 0.95 do not count the same. A
	// pointer because absent and 0 are different.
	Confidence *float64 `json:"confidence,omitempty"`

	// Queued is a prompt the core PARKED: typed while a turn was running, to run as its own turn
	// when this one ends. Distinct from Pending, which means asked and not answered yet — both
	// draw a bar, and without the difference a parked message looks like one being worked on.
	Queued bool `json:"queued,omitempty"`

	// Out is why a tool failed, as the tool said it. Kept only for a real failure: an advisory
	// result's text is what the AGENT must act on, and putting it here would draw a successful
	// write in the colours of a broken one.
	Out string `json:"out,omitempty"`

	// Pending is a prompt with no answer yet — the screen draws a bar beside it.
	Pending bool `json:"pending,omitempty"`
	// Abandoned is a prompt whose turn was cancelled.
	//
	// ⚠ Without it the bar never comes down: Pending is cleared by an assistant part or by the
	// turn finishing, and an interrupted prompt gets neither, so the row goes on claiming "waiting
	// for an answer" about a request that was stopped.
	Abandoned bool `json:"abandoned,omitempty"`
	// MsgID is the prompt's own id, so a later event can find its row.
	MsgID string `json:"msgId,omitempty"`

	// Member is which council member said it.
	Member string `json:"member,omitempty"`
	// Folded marks rows shut by default — reasoning, tool bodies.
	Folded bool `json:"folded,omitempty"`
}
