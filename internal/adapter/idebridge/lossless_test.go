package idebridge

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
)

// **What does a row LOSE?**
//
// The contract for moving clients onto this fold is that the full body is the canonical thing and a
// one-line summary is a separate field — a client handed a clipped line cannot get the answer back
// ("richer is recoverable, collapsed is not", rows.go). This test asks the fold, for each place a
// body arrives, whether the body survives.
//
// It is written to FAIL where information is lost, so the answer is measured rather than argued.
func TestWhatARowLoses(t *testing.T) {
	long := strings.Repeat("가", 300)               // past clip's 100 UTF-16 units, and multi-byte
	lines := "first line\nsecond line\nthird line" // more than one line
	emoji := "🙂🙂🙂 " + strings.Repeat("x", 200)     // surrogate pairs at the bound
	evs := []event.Event{
		mk(1, "prompt.submitted", map[string]any{
			"messageId": "m1",
			"parts":     []any{map[string]any{"kind": "text", "text": lines}},
		}, map[string]any{"kind": "user", "id": "u1"}),
		mk(2, "part.appended", map[string]any{
			"messageId": "m1", "role": "assistant",
			"part": map[string]any{"kind": "text", "text": long},
		}, nil),
		mk(3, "part.appended", map[string]any{
			"messageId": "m1", "role": "assistant",
			"part": map[string]any{"kind": "reasoning", "text": lines + " " + emoji},
		}, nil),
		mk(4, "part.appended", map[string]any{
			"messageId": "m1", "role": "assistant",
			"part": map[string]any{"kind": "tool-call", "toolCall": map[string]any{
				"callId": "c1", "name": "bash", "args": map[string]any{"command": lines},
			}},
		}, nil),
		// A FAILED result: that is the one whose body the fold carries, and the comment beside it says
		// why — "the reason travels with the failure". A successful call's output is deliberately not
		// on the row (the screens draw the name and the arguments), so asking for it here would be
		// measuring a decision this test did not make.
		mk(5, "part.appended", map[string]any{
			"messageId": "m1", "role": "assistant",
			"part": map[string]any{"kind": "tool-result", "toolResult": map[string]any{
				"callId": "c1", "isError": true, "content": long + "\n" + lines,
			}},
		}, nil),
	}

	rows := Rows(evs)
	if len(rows) < 4 {
		t.Fatalf("expected the four bodies to become rows, got %d: %s", len(rows), show(t, rows))
	}
	whole := func(field, got, want string) {
		if got == want {
			return
		}
		t.Errorf("%s lost the body: %d chars became %d\n  had:  %.60q…\n  kept: %.60q…",
			field, len([]rune(want)), len([]rune(got)), want, got)
	}
	whole("the person's own words", rows[0].Text, lines)
	whole("the answer", rows[1].Text, long)
	whole("what it was thinking", rows[2].Text, lines+" "+emoji)
	// The tool row's text is the tool NAME (by design); its body is the args it asked with and the
	// result it got. The arguments arrive as the whole object, so the question is whether every value
	// that went in is still there — one representative field is a summary, not the arguments.
	tool := rows[3]
	if !strings.Contains(tool.Args, "first line") || !strings.Contains(tool.Args, "third line") {
		t.Errorf("the arguments lost the command: %q", tool.Args)
	}
	whole("what the failed call said", tool.Out, long+"\n"+lines)

	// **Every field of a call survives, not just the first one the picker would have chosen.**
	//
	// ⚠ This is the loss the reviewer named: `AskedFor` returned the first of path/command/… that was
	// present, so an edit's `{path, old_string, new_string}` arrived as the path alone — the two strings
	// that say what the edit WAS are exactly what a person reading it needs.
	edit := Rows([]event.Event{
		mk(1, "part.appended", map[string]any{
			"messageId": "m2", "role": "assistant",
			"part": map[string]any{"kind": "tool-call", "toolCall": map[string]any{
				"callId": "c9", "name": "edit", "args": map[string]any{
					"path": "a.go", "old_string": "before-text", "new_string": "after-text",
				},
			}},
		}, nil),
	})
	if len(edit) != 1 {
		t.Fatalf("the edit call did not become one row: %s", show(t, edit))
	}
	for _, want := range []string{"a.go", "before-text", "after-text"} {
		if !strings.Contains(edit[0].Args, want) {
			t.Errorf("the call lost %q — a representative field is a summary, not the arguments: %q",
				want, edit[0].Args)
		}
	}
	// And its one-line form still picks the field a person scans for.
	if edit[0].Summary != "edit a.go" {
		t.Errorf("the summary is %q, want the name and the representative field", edit[0].Summary)
	}

	// **A successful result's body is on the row too.** Whether a screen shows it is a different fact
	// (Ok, Note, Folded say that) — folding the two together left a migrated client unable to expand
	// what a successful call answered.
	okRun := Rows([]event.Event{
		mk(1, "part.appended", map[string]any{
			"messageId": "m3", "role": "assistant",
			"part": map[string]any{"kind": "tool-call", "toolCall": map[string]any{
				"callId": "c8", "name": "bash", "args": map[string]any{"command": "ls"},
			}},
		}, nil),
		mk(2, "part.appended", map[string]any{
			"messageId": "m3", "role": "tool",
			"part": map[string]any{"kind": "tool-result", "toolResult": map[string]any{
				"callId": "c8", "content": "a.go\nb.go",
			}},
		}, nil),
	})
	if len(okRun) != 1 {
		t.Fatalf("the successful call did not stay one row: %s", show(t, okRun))
	}
	whole("what the successful call answered", okRun[0].Out, "a.go\nb.go")
	if okRun[0].Ok == nil || !*okRun[0].Ok {
		t.Errorf("a successful call is not marked successful: %s", show(t, okRun[0]))
	}

	// **And an ADVISORY result — the third branch, and the one the old condition hid behind.**
	//
	// ⚠ An advisory result sets isError on purpose (a post-edit hook's complaint) while the work was
	// DONE. The old rule filled the body only for `isError && !advisory`, so this branch lost its text
	// twice over: it is not a failure, so nothing drew it, and it was not kept, so nothing could.
	// Its text is what the AGENT must act on — the one body a person is most likely to go looking for.
	advisory := Rows([]event.Event{
		mk(1, "part.appended", map[string]any{
			"messageId": "m4", "role": "assistant",
			"part": map[string]any{"kind": "tool-call", "toolCall": map[string]any{
				"callId": "c7", "name": "write", "args": map[string]any{"path": "a.go"},
			}},
		}, nil),
		mk(2, "part.appended", map[string]any{
			"messageId": "m4", "role": "tool",
			"part": map[string]any{"kind": "tool-result", "toolResult": map[string]any{
				"callId": "c7", "isError": true, "advisory": true,
				"content": "lint says x\nand also y",
			}},
		}, nil),
	})
	if len(advisory) != 1 {
		t.Fatalf("the advisory call did not stay one row: %s", show(t, advisory))
	}
	whole("what the advisory said", advisory[0].Out, "lint says x\nand also y")
	// The display facts are unchanged and still separate: the work succeeded, and the row is marked
	// as carrying a note rather than a failure.
	if advisory[0].Ok == nil || !*advisory[0].Ok {
		t.Errorf("an advisory result is drawn as a failure: %s", show(t, advisory[0]))
	}
	if !advisory[0].Note {
		t.Errorf("an advisory result is not marked as a note: %s", show(t, advisory[0]))
	}

	// And the one-line form is not lost — it is a field of its own, so a list has a line to draw and
	// the row still has the whole thing.
	for i, r := range rows {
		if r.Text == "" && r.Args == "" && r.Out == "" {
			continue
		}
		if r.Summary == "" {
			t.Errorf("[%d] %s: no one-line summary — a list has nothing to draw", i, r.Who)
			continue
		}
		if strings.Contains(r.Summary, "\n") {
			t.Errorf("[%d] %s: the summary is not one line: %q", i, r.Who, r.Summary)
		}
		if n := len([]rune(r.Summary)); n > 120 {
			t.Errorf("[%d] %s: the summary is %d runes — a list draws a wrapped paragraph", i, r.Who, n)
		}
	}
}

// mkBase is where the synthetic clock starts. A fixed instant, so these tests say the same thing on
// every machine and every day.
var mkBase = time.Date(2026, 9, 19, 9, 0, 0, 0, time.UTC)

// mk builds one event the way the daemon writes it.
func mk(seq int64, typ string, data map[string]any, actor map[string]any) event.Event {
	b, err := json.Marshal(data)
	if err != nil {
		panic(err)
	}
	// ⚠ **The synthetic events carried NO time, and that is why a real defect walked through here.**
	//
	// Rows carry the event's time since 2026-09-14. The fixture on disk was given timestamps the same
	// day — but these hand-built streams were not, so `At` was empty in every one of them and the
	// equalities this package rests on (replay ends where a live stream does; applying the differences
	// rebuilds the fold; the door's frames match a fresh window) **never compared a timestamp at all.**
	// Two halves of one measurement that never met.
	//
	// What walked through: replacing a streamed draft with its fact kept the row count the same, so the
	// stamping skipped it and a streamed answer's final row had no time while the same answer read
	// fresh did (#198, fixed in 632e0ea8 by somebody else). Every test here was green.
	//
	// So the clock is here now, derived from seq so a replay and a live stream of the same conversation
	// agree — and a fact built from a later event does NOT agree with the draft it replaced, which is
	// the difference that was invisible.
	e := event.Event{Seq: seq, Type: event.Type(typ), Data: b, TS: mkBase.Add(time.Duration(seq) * time.Second)}
	if actor != nil {
		e.Actor = event.Actor{Kind: event.ActorKind(str(actor, "kind")), ID: str(actor, "id")}
	}
	return e
}

// **A live stream and a replay of the same turn must end at the same rows.**
//
// They see different events. Live carries `part.delta` — the chunks, written with seq 0 because they
// are not facts — and then the `part.appended` fact that supersedes them. A replay from the log sees
// only the facts. If the two disagree, then re-opening a conversation shows something different from
// what the person watched happen, and the difference is invisible until somebody compares.
//
// This is the property the row door needs before any client can be moved onto it: the door serves
// replay today, and a client that also follows live must not end up with two pictures.
func TestALiveStreamEndsWhereAReplayDoes(t *testing.T) {
	answer := "Hello, this is the answer.\nIt has two lines."
	think := "let me look"
	prompt := mk(1, "prompt.submitted", map[string]any{
		"messageId": "m1",
		"parts":     []any{map[string]any{"kind": "text", "text": "fix it"}},
	}, map[string]any{"kind": "user", "id": "u1"})
	fact := func(seq int64, kind, text string) event.Event {
		return mk(seq, "part.appended", map[string]any{
			"messageId": "m1", "role": "assistant",
			"part": map[string]any{"kind": kind, "text": text},
		}, nil)
	}
	// Chunks carry seq 0: the daemon writes them as transitional, not as facts.
	delta := func(kind, text string) event.Event {
		return mk(0, "part.delta", map[string]any{"messageId": "m1", "kind": kind, "text": text}, nil)
	}
	done := mk(9, "turn.finished", map[string]any{}, nil)

	replay := []event.Event{prompt, fact(2, "reasoning", think), fact(3, "text", answer), done}
	live := []event.Event{
		prompt,
		delta("reasoning", "let me "), delta("reasoning", "look"),
		fact(2, "reasoning", think),
		delta("text", "Hello, this is "), delta("text", "the answer.\nIt has two lines."),
		fact(3, "text", answer),
		done,
	}

	same(t, "끝맺음까지 온 턴", replay, live)

	// ⚠ **위 한 짝만으로는 아무것도 안 재였다.** 초안을 치우는 기전이 **둘**이라(사실이 제 초안을
	// 덮는 것과, 끝맺음이 남은 초안을 쓸어내는 것) 하나를 지워도 다른 하나가 가려 준다 — 변이 둘이
	// 그렇게 살아남았다. 그래서 **각각만 적용되는 자리**로 가른다.
	//
	// 하나: 사실이 왔고 턴은 아직 안 끝났다(대화가 계속되는 흔한 상태). 여기서는 쓸어내기가 안 돌므로
	// 사실이 제 초안을 덮는 것이 유일한 길이다.
	same(t, "사실은 왔고 턴은 아직 안 끝났다",
		[]event.Event{prompt, fact(2, "text", answer)},
		[]event.Event{prompt, delta("text", "Hello, this is "), delta("text", "the answer.\nIt has two lines."),
			fact(2, "text", answer)})

	// 둘: 조각은 흘렀는데 **사실이 안 왔다**(스핀 가드가 버린 답·인터럽트·프로바이더 오류 — 코어에
	// 그런 갈래가 여럿이라고 이 파일이 적어 두었다). 로그에는 그 답이 없으므로 재생은 사용자 행만
	// 그린다. 실시간이 반쪽 답을 세워 두면, 붙어 있던 창 하나에만 그것이 서고 다른 창에는 없다.
	same(t, "조각만 흐르고 사실이 안 왔다",
		[]event.Event{prompt, done},
		[]event.Event{prompt, delta("text", "Hello, this is "), done})
}

// same 은 두 사건 열을 접어 최종 행이 같은지 본다.
func same(t *testing.T, what string, replay, live []event.Event) {
	t.Helper()
	a, b := Rows(replay), Rows(live)
	if len(a) != len(b) {
		t.Errorf("%s: 재생은 행 %d개, 실시간은 %d개 — 같은 턴을 두 그림으로 그린다\n  재생:  %s\n  실시간: %s",
			what, len(a), len(b), show(t, a), show(t, b))
		return
	}
	for i := range a {
		if show(t, a[i]) != show(t, b[i]) {
			t.Errorf("%s [%d]: 재생과 실시간이 갈린다 — 대화를 다시 열면 사람이 본 것과 다른 것이 선다\n  재생:  %s\n  실시간: %s",
				what, i, show(t, a[i]), show(t, b[i]))
		}
	}
}

// **Whitespace is content.**
//
// ⚠ The bodies were whole but still trimmed, and for a tool's output that is a loss with teeth:
// `"    return x\n"` arrived as `"return x"`, so the indentation — which in code output IS the
// content — was gone before any screen saw it. Trimming may decide WHETHER there is a body; it must
// not produce one.
//
// The blank case is the one deliberate exception: a body of nothing but spaces is nothing to draw, and
// the row says so by having no body at all.
func TestWhitespaceIsContent(t *testing.T) {
	code := "    return x\n"
	indented := "\tif err != nil {\n\t\treturn err\n\t}\n"

	rows := Rows([]event.Event{
		mk(1, "prompt.submitted", map[string]any{
			"messageId": "m1", "parts": []any{map[string]any{"kind": "text", "text": code}},
		}, map[string]any{"kind": "user", "id": "u1"}),
		mk(2, "part.appended", map[string]any{
			"messageId": "m1", "role": "assistant",
			"part": map[string]any{"kind": "text", "text": indented},
		}, nil),
		mk(3, "part.appended", map[string]any{
			"messageId": "m1", "role": "assistant",
			"part": map[string]any{"kind": "reasoning", "text": code},
		}, nil),
		mk(4, "part.appended", map[string]any{
			"messageId": "m1", "role": "assistant",
			"part": map[string]any{"kind": "tool-call", "toolCall": map[string]any{
				"callId": "c1", "name": "bash", "args": map[string]any{"command": "go build"},
			}},
		}, nil),
		mk(5, "part.appended", map[string]any{
			"messageId": "m1", "role": "tool",
			"part": map[string]any{"kind": "tool-result", "toolResult": map[string]any{
				"callId": "c1", "content": indented,
			}},
		}, nil),
	})
	if len(rows) != 4 {
		t.Fatalf("행이 %d 개다: %s", len(rows), show(t, rows))
	}
	exact := func(what, got, want string) {
		if got != want {
			t.Errorf("%s: 공백이 깎였다\n  들어간 것: %q\n  나온 것:   %q", what, want, got)
		}
	}
	exact("사람이 붙여 넣은 코드", rows[0].Text, code)
	exact("답의 들여쓰기", rows[1].Text, indented)
	exact("생각의 들여쓰기", rows[2].Text, code)
	exact("도구가 답한 것", rows[3].Out, indented)

	// 그리고 요약은 여전히 한 줄이다 — 정리는 거기서 한다.
	for i, r := range rows {
		if strings.Contains(r.Summary, "\n") || strings.HasPrefix(r.Summary, " ") {
			t.Errorf("[%d] 요약이 정리되지 않았다: %q", i, r.Summary)
		}
	}

	// ⚠ **공백만 있는 본문에서 두 갈래가 갈린다.**
	//
	// 본문이 곧 행인 자리(사람이 한 말, 답, 생각)에서는 공백뿐이면 **행을 안 만든다** — 그릴 것이
	// 없는 행을 세우는 것이므로. 행이 다른 이유로 이미 있는 자리(도구 결과는 **호출**의 행에
	// 앉는다)에서는 **그대로 싣는다**: 「이 파일에 공백 셋이 들어 있다」와 「도구가 아무것도 안
	// 답했다」는 다른 답이고, 둘을 한 글자로 합치는 것은 깎기와 같은 손실의 한 단계 작은 판이다
	// (리뷰가 짚었다, 2026-09-13 — 이 시험의 앞 판본이 그 합침을 못박고 있었다).
	blank := Rows([]event.Event{
		mk(1, "part.appended", map[string]any{
			"messageId": "m9", "role": "assistant",
			"part": map[string]any{"kind": "text", "text": "   \n\t "},
		}, nil),
		mk(2, "part.appended", map[string]any{
			"messageId": "m9", "role": "assistant",
			"part": map[string]any{"kind": "tool-call", "toolCall": map[string]any{
				"callId": "c9", "name": "bash", "args": map[string]any{"command": "true"},
			}},
		}, nil),
		mk(3, "part.appended", map[string]any{
			"messageId": "m9", "role": "tool",
			"part": map[string]any{"kind": "tool-result", "toolResult": map[string]any{
				"callId": "c9", "content": "  \n ",
			}},
		}, nil),
	})
	tools := 0
	for _, r := range blank {
		if r.Who == WhoAgent {
			t.Errorf("공백만 있는 답이 행이 됐다: %s", show(t, r))
		}
		if r.Who != WhoTool {
			continue
		}
		tools++
		if r.Out != "  \n " {
			t.Errorf("공백만 있는 결과가 그대로 안 실렸다: %q — 「공백뿐인 답」과 「빈 답」이 "+
				"한 글자로 합쳐지면 어느 화면도 둘을 못 가른다", r.Out)
		}
		// 그리고 그것이 **목록에 글자를 올리지는 않는다** — 보존과 기본 표시는 다른 결정이다.
		if r.Summary != "bash true" {
			t.Errorf("공백뿐인 본문이 요약에 올라왔다: %q", r.Summary)
		}
	}
	if tools != 1 {
		t.Fatalf("도구 행이 %d 개다 — 이 갈래를 재고 있지 않다", tools)
	}

	// **진짜 빈 답과는 다르다.** 같은 호출에 내용이 없으면 본문도 없다.
	empty := Rows([]event.Event{
		mk(1, "part.appended", map[string]any{
			"messageId": "m8", "role": "assistant",
			"part": map[string]any{"kind": "tool-call", "toolCall": map[string]any{
				"callId": "c8", "name": "bash", "args": map[string]any{"command": "true"},
			}},
		}, nil),
		mk(2, "part.appended", map[string]any{
			"messageId": "m8", "role": "tool",
			"part": map[string]any{"kind": "tool-result", "toolResult": map[string]any{
				"callId": "c8", "content": "",
			}},
		}, nil),
	})
	if len(empty) != 1 || empty[0].Out != "" {
		t.Errorf("빈 결과가 빈 본문이 아니다: %s", show(t, empty))
	}

	// **평결의 본문들도 같다** — 투표가 만든 행에 얹혀 오므로 온 대로 싣는다.
	verdict := Rows([]event.Event{
		mk(1, "council.verdict", map[string]any{
			"member": "alpha", "round": 1, "decision": " done ", "lens": " correctness ",
			"rationale": "    the diff keeps the guard\n", "cite": "    if err != nil {\n",
			"keep": "  the guard  ", "thought": "  weighing it  ",
		}, nil),
	})
	if len(verdict) != 1 {
		t.Fatalf("평결이 한 행이 아니다: %s", show(t, verdict))
	}
	v := verdict[0]
	for _, c := range []struct{ what, got, want string }{
		{"rationale", v.Text, "    the diff keeps the guard\n"},
		{"cite", v.Cite, "    if err != nil {\n"},
		{"keep", v.Keep, "  the guard  "},
		{"thought", v.Thought, "  weighing it  "},
	} {
		if c.got != c.want {
			t.Errorf("평결의 %s 가 깎였다: %q (원문 %q)", c.what, c.got, c.want)
		}
	}
	// 토큰은 반대다 — 「 done 」과 「done」은 같은 상태여야 한다.
	if v.Decision != "done" || v.Lens != "correctness" {
		t.Errorf("토큰이 깎이지 않았다: decision=%q lens=%q — 공백이 붙은 낱말을 다른 상태로 "+
			"읽으면 화면이 없는 판정을 그린다", v.Decision, v.Lens)
	}
	if strings.Contains(v.Summary, "\n") || strings.TrimSpace(v.Summary) != v.Summary {
		t.Errorf("요약이 한 줄이 아니거나 공백을 안고 있다: %q", v.Summary)
	}
}

// **Can a later frame NAME the row it changes?**
//
// The fold reaches back: a reply clears the bar on the prompt above it, a tool result lands on its
// call's row, a resurfaced interjection moves its original. Inside one process that is a pointer. On a
// wire it needs a name — and a live contract that sends changes instead of the whole list is
// impossible without one.
//
// So this asks the rows that exist today whether they carry one. It is written to report what IS
// rather than to assert a design: if two rows share a name, that is the finding, and the contract has
// to supply identity before the door can carry live changes.
func TestWhetherARowCanBeNamed(t *testing.T) {
	var events []event.Event
	read(t, "fold_events.json", &events)
	rows := Rows(events)
	if len(rows) < 10 {
		t.Fatalf("픽스처에서 행을 %d 개밖에 못 얻었다 — 잴 것이 없다", len(rows))
	}

	// Seq is the event that put the row there, which is the only candidate the row already carries.
	bySeq := map[int64][]int{}
	for i, r := range rows {
		bySeq[r.Seq] = append(bySeq[r.Seq], i)
	}
	clashes := 0
	for seq, at := range bySeq {
		if len(at) > 1 {
			clashes++
			kinds := make([]string, 0, len(at))
			for _, i := range at {
				kinds = append(kinds, string(rows[i].Who))
			}
			t.Logf("seq %d 를 행 %v 가 나눠 쓴다(%v)", seq, at, kinds)
		}
	}
	if clashes > 0 {
		t.Errorf("%d 개의 seq 가 여러 행에 걸린다 — seq 로는 뒤의 프레임이 고칠 행을 지목할 수 없다", clashes)
	}

	// And the live case the batch fixture cannot show: two drafts of ONE message. Deltas are written
	// with seq 0, so a reasoning draft and a text draft of the same message both claim 0.
	live := Rows([]event.Event{
		mk(0, "part.delta", map[string]any{"messageId": "m1", "kind": "reasoning", "text": "thinking"}, nil),
		mk(0, "part.delta", map[string]any{"messageId": "m1", "kind": "text", "text": "answering"}, nil),
	})
	if len(live) != 2 {
		t.Fatalf("두 초안이 두 행이 아니다: %s", show(t, live))
	}
	// seq 로는 못 가른다 — 그것이 이 칸이 있는 이유다. 이름으로 가른다.
	if live[0].Seq != live[1].Seq {
		t.Errorf("조각이 seq 를 갖게 됐다(%d, %d) — 그러면 이 규칙의 근거가 바뀐 것이니 여기부터 고칠 것",
			live[0].Seq, live[1].Seq)
	}
	if live[0].ID == live[1].ID || live[0].ID == "" {
		t.Errorf("한 메시지의 두 초안이 같은 이름을 쓴다(%q, %q) — 실시간에서 고칠 행을 지목할 수 없다",
			live[0].ID, live[1].ID)
	}

	// ⚠ **그리고 메시지가 둘일 때.** 이 규칙의 첫 판은 초안 행에 메시지 id 를 안 실어서 이름이
	// `d::text` 였다 — 한 메시지만 쓰는 위 짝으로는 통과하고, 두 메시지가 동시에 흐르면 남의 행을
	// 고친다. 시험이 한 메시지만 보면 그 구멍이 안 보인다.
	two := Rows([]event.Event{
		mk(0, "part.delta", map[string]any{"messageId": "m1", "kind": "text", "text": "one"}, nil),
		mk(0, "part.delta", map[string]any{"messageId": "m2", "kind": "text", "text": "two"}, nil),
	})
	if len(two) != 2 {
		t.Fatalf("두 메시지의 초안이 두 행이 아니다: %s", show(t, two))
	}
	if two[0].ID == two[1].ID {
		t.Errorf("두 메시지의 초안이 같은 이름을 쓴다(%q) — 이름에 메시지가 빠졌다", two[0].ID)
	}

	// 그리고 사실의 행들도 서로 다른 이름을 갖는다. 이름이 겹치면 뒤의 프레임이 남의 행을 고친다.
	seen := map[string]int{}
	for i, r := range rows {
		if r.ID == "" {
			t.Errorf("[%d] %s 행에 이름이 없다 — 뒤의 프레임이 이 행을 지목할 수 없다", i, r.Who)
			continue
		}
		if j, dup := seen[r.ID]; dup {
			t.Errorf("행 %d 와 %d 가 이름 %q 를 나눠 쓴다", j, i, r.ID)
		}
		seen[r.ID] = i
	}
}
