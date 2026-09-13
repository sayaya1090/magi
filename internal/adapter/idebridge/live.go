package idebridge

import (
	"encoding/json"
	"strings"
)

// How a conversation changes, said once.
//
// `Rows` folds a WHOLE log. That is the right shape for opening a window and the wrong shape for
// staying open: a person watching a model answer would get the entire conversation resent per chunk.
// So a live surface needs a way to say "this row changed", and the hard part is that this fold
// reaches BACKWARDS — a reply clears the bar on the prompt above it, a tool result lands on its
// call's row, an inline answer pulls its question down the list. An incremental fold cannot be had by
// folding the tail (rows.go and the `rows` door both say why), so the shape here is different:
//
//	fold the whole log → diff against what the client last saw → send the difference
//
// The fold stays the one rule. The difference is derived from its output, so a live surface and a
// replaying surface cannot drift: there is no second fold to keep in step.
//
// # Where the words came from
//
// Measured, not invented (2026-09-13). Every prefix of the canonical fixture was folded and compared
// with the prefix before it, and so were the three paths that reach backwards — a draft becoming a
// fact, a resurfaced interjection, a queued question answered inline. What actually happens is:
//
//	append 18 · patch 4 · drop 2   (the fixture, 22 events)
//	move  1                        (a queued question answered inline, with a row after it)
//
// ⚠ **The first measurement said there were three words.** `move` was missing because neither the
// fixture nor the first synthetic case ever had a row BETWEEN the moved question and the end of the
// list, so "moved to the bottom" and "was already at the bottom" looked the same. A case that cannot
// tell two outcomes apart reports the one it can see. TestTheLiveWordsAreMeasured now names the case
// that forces each word, and fails if a word stops being exercised.
const (
	// OpReset is the whole list. The first frame, and the safety valve.
	//
	// ⚠ **A contract with no way to start over fails WRONG rather than failing loudly.** If two rows
	// ever share a name, a patch aimed at one of them would silently edit the other; Diff answers a
	// reset instead. It is also what the `rows` door already sends — a replay IS a reset — so a client
	// implements one path and uses it for both.
	OpReset = "reset"
	// OpAdd is a row that was not there. `After` says which row it follows ("" = the top).
	OpAdd = "add"
	// OpGrow appends text to a row that is already drawn — the streaming case, said cheaply.
	//
	// ⚠ **This is the one word that exists for its cost.** A draft grows by one chunk at a time, and a
	// patch carrying the whole row would resend the entire answer per chunk: a 60KB answer arriving in
	// 1500 chunks is 45MB of socket traffic to deliver 60KB. Diff emits this only when the sole change
	// is text that GREW — the old text is a prefix of the new one and every other field is equal — so
	// it is a strict shorthand for that patch and nothing else. A client that implements it as
	// `row.text += op.text` is exactly right.
	OpGrow = "grow"
	// OpPatch is a row whose content changed in any other way. Carries the whole row.
	//
	// Whole rather than a field delta: a field delta needs a second vocabulary (the field names) kept
	// in step across six clients, to save bytes on the changes that are RARE. The frequent one is
	// OpGrow, which is already cheap.
	OpPatch = "patch"
	// OpDrop is a row that is gone — a draft superseded by its fact, a question that moved under a
	// new name.
	OpDrop = "drop"
	// OpMove is a row that is still there, in a different place. `After` says which row it now
	// follows ("" = the top).
	OpMove = "move"
)

// Op is one change to a list of rows.
//
// Applied IN ORDER: a frame is a sequence, not a set. Drops go first, then moves put the survivors in
// their final order, then the changed rows, then the new ones — so an added row can name a row added
// earlier in the same frame as the one it follows.
type Op struct {
	Op string `json:"op"`
	// ID is which row this is about. Empty on a reset.
	ID string `json:"id,omitempty"`
	// Row is the row itself, on add and patch. A pointer so an absent row is absent rather than an
	// empty one, the same reason Row.Ok is.
	Row *Row `json:"row,omitempty"`
	// Rows is the whole list, on reset.
	Rows []Row `json:"rows,omitempty"`
	// Text is what OpGrow appends.
	Text string `json:"text,omitempty"`
	// After is the row this one now follows, on add and move. "" means the top of the list.
	//
	// ⚠ **Named, not numbered.** An index means "the list you had when I sent this", and a client that
	// missed a frame would edit the wrong line with no way to notice. A name that a client does not
	// know is the one forgiving rule in this contract: put the row at the END and carry on — a row in
	// the wrong place is recoverable by the next reset, a row silently REPLACING another is not.
	After string `json:"after,omitempty"`
}

// Diff says what changed between two foldings of the same conversation.
//
// The guarantee is exact, and TestApplyingTheWordsRebuildsTheFold measures it on every prefix of the
// fixture and of the backwards-reaching paths: Apply(before, Diff(before, after)) equals after,
// field for field. That is the same promise TestALiveStreamEndsWhereAReplayDoes makes about the fold
// itself, one layer up — a live screen and a screen that just opened show the same conversation.
func Diff(before, after []Row) []Op {
	// ⚠ Names must be unique on BOTH sides or a patch cannot be aimed. Answer the whole list rather
	// than a patch that would edit some other row: this is the one place where being expensive is
	// the correct behaviour.
	posB, okB := index(before)
	posA, okA := index(after)
	if !okB || !okA {
		return []Op{{Op: OpReset, Rows: clone(after)}}
	}

	var ops []Op

	// 1. Gone.
	for _, r := range before {
		if _, still := posA[r.ID]; !still {
			ops = append(ops, Op{Op: OpDrop, ID: r.ID})
		}
	}

	// 2. The survivors, in the order the new list puts them. Greedy rather than a minimal edit
	// script: the measured case needs one move, and a clever algorithm that emitted a different
	// correct answer would still have to be checked by Apply, which is what actually holds this up.
	have := make([]string, 0, len(before))
	for _, r := range before {
		if _, still := posA[r.ID]; still {
			have = append(have, r.ID)
		}
	}
	want := make([]string, 0, len(after))
	for _, r := range after {
		if _, had := posB[r.ID]; had {
			want = append(want, r.ID)
		}
	}
	for i, id := range want {
		if i < len(have) && have[i] == id {
			continue
		}
		at := -1
		for j := i; j < len(have); j++ {
			if have[j] == id {
				at = j
				break
			}
		}
		if at < 0 {
			continue
		}
		have = append(have[:at], have[at+1:]...)
		have = append(have[:i], append([]string{id}, have[i:]...)...)
		prev := ""
		if i > 0 {
			prev = want[i-1]
		}
		ops = append(ops, Op{Op: OpMove, ID: id, After: prev})
	}

	// 3. Changed.
	for i := range after {
		r := after[i]
		j, had := posB[r.ID]
		if !had {
			continue
		}
		old := before[j]
		if rowsEqual(old, r) {
			continue
		}
		if grown, ok := grew(old, r); ok {
			ops = append(ops, Op{Op: OpGrow, ID: r.ID, Text: grown})
			continue
		}
		row := r
		ops = append(ops, Op{Op: OpPatch, ID: r.ID, Row: &row})
	}

	// 4. New, in the order the new list has them, each naming the row it follows — which, being
	// earlier in that list, either survived or was added by an op already emitted here.
	for i := range after {
		r := after[i]
		if _, had := posB[r.ID]; had {
			continue
		}
		prev := ""
		if i > 0 {
			prev = after[i-1].ID
		}
		row := r
		ops = append(ops, Op{Op: OpAdd, ID: r.ID, Row: &row, After: prev})
	}
	return ops
}

// Apply is the client side of the contract, written here so both copies of it have something to be
// checked against — the same reason Rows lives in this package at all.
func Apply(rows []Row, ops []Op) []Row {
	out := clone(rows)
	for _, op := range ops {
		switch op.Op {
		case OpReset:
			out = clone(op.Rows)
		case OpDrop:
			if i := at(out, op.ID); i >= 0 {
				out = append(out[:i], out[i+1:]...)
			}
		case OpMove:
			i := at(out, op.ID)
			if i < 0 {
				continue
			}
			row := out[i]
			out = append(out[:i], out[i+1:]...)
			out = insert(out, row, op.After)
		case OpGrow:
			if i := at(out, op.ID); i >= 0 {
				out[i].Text += op.Text
			}
		case OpPatch:
			if i := at(out, op.ID); i >= 0 && op.Row != nil {
				out[i] = *op.Row
			}
		case OpAdd:
			if op.Row == nil || at(out, op.ID) >= 0 {
				continue
			}
			out = insert(out, *op.Row, op.After)
		}
	}
	return out
}

// insert puts a row after the named one. An unknown name means the end — see Op.After.
func insert(rows []Row, row Row, after string) []Row {
	if after == "" {
		return append([]Row{row}, rows...)
	}
	if i := at(rows, after); i >= 0 {
		return append(rows[:i+1], append([]Row{row}, rows[i+1:]...)...)
	}
	return append(rows, row)
}

func at(rows []Row, id string) int {
	for i := range rows {
		if rows[i].ID == id {
			return i
		}
	}
	return -1
}

// index maps each row's name to its place, and says whether the names were unique.
func index(rows []Row) (map[string]int, bool) {
	m := make(map[string]int, len(rows))
	for i, r := range rows {
		if r.ID == "" {
			return m, false
		}
		if _, dup := m[r.ID]; dup {
			return m, false
		}
		m[r.ID] = i
	}
	return m, true
}

// grew says whether the only difference is text that got longer — the streaming case.
func grew(old, now Row) (string, bool) {
	if old.Text == now.Text || !strings.HasPrefix(now.Text, old.Text) {
		return "", false
	}
	probe := now
	probe.Text = old.Text
	if !rowsEqual(old, probe) {
		return "", false
	}
	return now.Text[len(old.Text):], true
}

// rowsEqual compares every field, including ones added later.
//
// ⚠ Through JSON rather than field by field: a comparison that names the fields is a list that ages,
// and the field somebody adds next year is exactly the one a live screen would then stop updating.
// Row has no unexported or unserialisable fields — the whole struct crosses a wire.
func rowsEqual(a, b Row) bool {
	x, err1 := json.Marshal(a)
	y, err2 := json.Marshal(b)
	if err1 != nil || err2 != nil {
		return false
	}
	return string(x) == string(y)
}

func clone(rows []Row) []Row {
	if rows == nil {
		return nil
	}
	out := make([]Row, len(rows))
	copy(out, rows)
	return out
}
