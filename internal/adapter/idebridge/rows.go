package idebridge

// The shape of a conversation row, decided in ONE place.
//
// This is the third of the eight (docs/IDE_BRIDGE §3) and the largest: "행을 짓는 규칙은 한 벌"
// is the invariant, and it was the one this repository had most visibly struggled to hold. The two
// older copies were 806 lines of Kotlin and 808 of TypeScript, and measured 2026-09-10 they did
// not agree on what a row even was:
//
//	TypeScript  user · agent · tool · thinking · council · system · error · image   (eight)
//	Kotlin      User · Agent · Tool · Thinking · Council · Info                     (six)
//
// One image attachment was `image` carrying a bare path on one screen and `Info` carrying
// "🖼 <path>" on the other; an error was `error` on one and `Info` + ⚠ on the other. That was
// the same defect as the activity vocabulary one layer up — two copies, one fact, two words —
// and it is why this file exists before any of the rule does.
//
// **Eight, not six.** A client that wants six can fold `system`, `error` and `image` into one; a
// client handed six cannot get the picture back. Richer is recoverable, collapsed is not.
// Decided 2026-09-10, and Kotlin was brought into alignment to eight on 2026-09-12 (ee9176ff) and held by
// TestTheRowVocabularyMatchesTheKotlinCopy.

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

// FileNav is the structured file and line navigation target for a tool call.
type FileNav struct {
	Path string `json:"path"`
	Line *int   `json:"line,omitempty"`
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
	// At is when the event that made this row happened, as the log wrote it.
	//
	// ⚠ **The Kotlin copy has put this on every row since it had rows, and this door had nowhere for
	// it.** Nothing noticed, because the cross-copy field guard compared this struct against the
	// TypeScript one — which does not carry a time either — so two copies agreed and the third was not
	// read (TestTheRowFieldsMatchTheKotlinCopy now reads it, 2026-09-14). A client moved onto these
	// rows would have lost every timestamp it draws today.
	//
	// First stamp wins: a row that is MOVED later — a resurfaced question — keeps the time it was
	// asked, which is the fact a person is reading when they scroll back to it.
	At string `json:"at,omitempty"`

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

	// Args is what the call was ASKED to do — the whole arguments.
	//
	// The name alone is not a row: a turn that runs thirty commands draws thirty rows reading
	// "bash ✓" — same glyph, same word, nothing saying which command or which file. The transcript
	// is where a person answers "what did it just do".
	//
	// ⚠ **It used to be one representative field.** The rendering picked the first of
	// path/command/pattern/… that the call had, so an edit's `{path, old_string, new_string}` arrived
	// as the path alone — and the two strings that say what the edit WAS are the reason somebody opens
	// that row. One field is a summary; Summary carries it.
	Args string `json:"args,omitempty"`

	// RawArgs is the full unabridged arguments for a tool call.
	RawArgs string `json:"rawArgs,omitempty"`

	// FileNav is the structured file and line navigation target for a tool call.
	FileNav *FileNav `json:"fileNav,omitempty"`

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
	// Evidence is what the members were given to judge — the whole of it, in the order the core sent
	// it: the task, the plan, the report, the actions, the changes.
	//
	// ⚠ **Not a selection.** Picking which of those to carry would turn "what the members saw" into
	// "what we decided to show", and a verdict is checkable only against the first. This door carried
	// none of it until 2026-09-14: the task became the row's text and the other four were dropped, so
	// the one thing that makes a council's answer auditable did not reach a screen at all.
	//
	// ⚠ **Whether it is drawn open is not said here, and Folded is not the place either.** Folded marks
	// a row whose WHOLE content is secondary (reasoning); this row's text is the task being judged and
	// belongs on screen. A body a screen shuts until asked is the shape a tool result already has, and
	// this fold does not mark those folded either — the clients fold both by kind, and the Kotlin copy
	// already folds this one ("전사는 흐르는 화면이라 증거가 펼쳐진 채 서면 대화를 덮는다"). Saying it
	// with the one flag there is would hide the task as well, which is a different claim.
	Evidence string `json:"evidence,omitempty"`
	// Cite is the fragment of the record this verdict rests on, or NO-EVIDENCE. It is CHECKABLE,
	// and an empty one on a "done" is itself worth seeing.
	//
	// Kept exactly as it came, whitespace and all: it is a QUOTE, and a trimmed quote no longer
	// matches the thing it is quoting — which is the one property this field exists for.
	Cite string `json:"cite,omitempty"`
	// Keep is what this member says a revision must preserve — emitted regardless of the decision,
	// because an approving member's keep is what a rewrite forced by somebody else would drop.
	Keep string `json:"keep,omitempty"`
	// Thought is what this member was thinking before it answered — the provider's reasoning
	// stream, never parsed and never a vote.
	//
	// It matters most where there is nothing else: a member whose reply arrived as reasoning ALONE
	// is recorded silent, and without this the surface draws a considered shrug over thousands of
	// characters of work. Folded like every other reasoning row — and kept whole like one, since a
	// reasoning stream one layer up is not trimmed either.
	Thought string `json:"thought,omitempty"`
	// Confidence is how sure the member was, 0..1, self-reported and WEIGHED: the tally is a
	// confidence-weighted sum, so a done at 0.2 and a done at 0.95 do not count the same. A
	// pointer because absent and 0 are different.
	Confidence *float64 `json:"confidence,omitempty"`

	// Queued is a prompt the core PARKED: typed while a turn was running, to run as its own turn
	// when this one ends. Distinct from Pending, which means asked and not answered yet — both
	// draw a bar, and without the difference a parked message looks like one being worked on.
	Queued bool `json:"queued,omitempty"`

	// Out is what the tool answered, as the tool said it — for every result, not only failures.
	//
	// ⚠ **Preserving the body and showing it are two different decisions**, and they used to be one:
	// this was filled only for a real failure, so a client reading rows could never expand what a
	// successful call returned — the output was in the log and not on the row, and the screen would
	// have to go parse the log again. That is the drift this package exists to end.
	//
	// What a screen draws by default is said by the fields around it, and none of that changed: Ok
	// tells failure from success, Note marks an advisory result (whose text is what the AGENT must act
	// on, so drawing it like a failure would colour a successful write as a broken one), and Folded
	// marks bodies shut until somebody asks.
	//
	// ⚠ **Including a body with no words in it.** A result of `"  \n "` is carried as `"  \n "`: this
	// row belongs to a CALL, so nothing here has to earn it by being non-empty, and "the file holds
	// three spaces" is a different answer from "the tool returned nothing". Summary is empty for such a
	// body, which is how a list says there is nothing to read without the fact being gone.
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

	// OutputID is the read-only output document ID for finalized assistant text or tool result.
	OutputID string `json:"outputId,omitempty"`

	// Summary is this row as ONE line, for a screen that draws a list.
	//
	// ⚠ **It exists because the bodies are now whole.** The arguments a call was made with and the
	// reason a call failed used to arrive clipped — first line, 100 UTF-16 units — and that is a loss
	// no reader can undo: a client handed the first line of a three-line command cannot get the
	// command back. It is the same argument the vocabulary above makes ("richer is recoverable,
	// collapsed is not"), applied to the text rather than to the words.
	//
	// So the clip did not disappear, it moved here. A shallow client (a slide add-in, a status line)
	// draws this; a client that shows the conversation draws the bodies. Neither has to parse anything
	// to get the other, which is the whole point of deciding it once.
	Summary string `json:"summary,omitempty"`

	// ID names this row so a later frame can say which row it changed.
	//
	// ⚠ **Seq cannot do it.** The fold reaches BACKWARDS — a reply clears the bar on the prompt above
	// it, a tool result lands on its call's row, a resurfaced interjection moves its original — and
	// inside one process that is a pointer. On a wire it needs a name, or a live contract can only
	// resend the whole list. Seq is the obvious candidate and it is almost enough: measured over the
	// fixture, every fact has its own. But a streaming chunk is NOT a fact and is written with seq 0,
	// so one message's reasoning draft and text draft both claim 0 — the two rows a screen is most
	// actively redrawing are exactly the two seq cannot tell apart
	// (TestWhetherARowCanBeNamed, 2026-09-13).
	//
	// So: a fact's row is named by its seq, and a draft by the message and kind it is a draft OF —
	// which is the key the fold already uses internally to find it (`drafts`). One rule, and the name a
	// client holds is the name the fold knows the row by.
	//
	// **Decided 2026-09-13: a fact does NOT inherit its draft's name.** The question was whether the
	// arriving fact should keep `d:m1:text` so a live frame could say "that row became this" instead of
	// "drop that, add this" — the Kotlin copy states the intent as "조각은 새 줄이 아니라 같은 줄의
	// 고쳐 쓰기". It must not, and the reason is that a name has to be a function of the ROW, never of
	// the path a client took to it: the same row would then be called `d:m1:text` by a client that
	// watched it stream and `3` by one that opened the window afterwards — and by the FIRST client
	// again after any reconnect, which resets from the fold. A name that changes on reconnect produces
	// exactly the duplicated row that inheriting it was meant to prevent.
	//
	// So the transition is said as what it is (live.go): the draft is dropped and the fact added. What
	// a screen loses by that is not the row, it is the row's own UI state — a reasoning draft somebody
	// had expanded folds shut when the fact lands. Closing THAT needs the fold to say which draft a
	// fact supersedes (a `replaces` on the add), which needs the fact row to carry its message id.
	// Unbuilt on purpose: no client draws rows live yet, and a field nothing draws is the shape this
	// tree has paid for four times.
	ID string `json:"id,omitempty"`
}
