package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/platform"
	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// ⚠ **이 둘은 진짜 셸에 진짜 명령을 먹인다** — 그게 요점이다(결함이 이음매에 있었고, 가드만 단위로
// 재면 고침 전에도 초록이었다). 그래서 픽스처가 **플랫폼의 셸로** 말해야 한다. 윈도우의 `bash` 도구는
// `powershell -NoProfile -Command` 이고(`builtin.Shell`), Windows PowerShell 5.1 에는 `&&` 가 없다 —
// 픽스처가 POSIX 로 적혀 있어서 이 기계에서는 셸의 **구문 오류**를 재고 있었다(실측 2026-09-13:
// `'&&' 토큰은 이 버전에서 올바른 문 구분 기호가 아닙니다`).
//
// 번역기를 쓰지 않고 **쓰는 모양마다 두 벌**을 적는다. 일반 번역은 이 시험이 안 쓰는 경우까지 떠안고,
// 틀리면 제품이 아니라 그 번역을 재게 된다.
//
// 되돌리기 왕복이 **바이트까지 같은 상태로** 돌아가야 이 시험이 성립한다(가드가 내용을 견준다).
// `Get-Content`/`Set-Content` 는 줄 끝을 CRLF 로 통일하지만, 그 통일이 양쪽 방향에 같이 걸리므로
// A→B→A 는 처음 바이트열로 정확히 돌아온다. 백업 사본은 바이트 복사라 그쪽도 그대로다.
type shellSays struct{ posix, powershell string }

func (s shellSays) cmd() string {
	if runtime.GOOS == "windows" {
		return s.powershell
	}
	return s.posix
}

// swap is the one shape both tests lean on: an in-place substitution, written so that the two
// directions of a restore loop are DIFFERENT command texts — an identical text is not a new mutation
// at all and never reaches the content comparison the tests are about.
func swap(file, from, to string) shellSays {
	return shellSays{
		posix:      "sed -i.tmp 's/" + from + "/" + to + "/' " + file + " && rm -f " + file + ".tmp",
		powershell: "(Get-Content " + file + ") -replace '" + from + "','" + to + "' | Set-Content " + file,
	}
}

// The other shapes, each written twice for the same reason.
func copyFile(from, to string) shellSays {
	return shellSays{posix: "cp " + from + " " + to, powershell: "Copy-Item " + from + " " + to}
}

func writeLine(text, file string) shellSays {
	return shellSays{
		posix:      "printf '" + text + "\\n' > " + file,
		powershell: "Set-Content " + file + " '" + text + "'",
	}
}

// removeTree names a path that is NOT there — that is the whole subject of the second test. The
// PowerShell spelling has to say so out loud: without -ErrorAction the cmdlet writes an error for a
// missing path, which is fine for the assertion but noise in the log of a test about saying nothing.
func removeTree(path string) shellSays {
	return shellSays{
		posix:      "rm -rf " + path,
		powershell: "Remove-Item -Recurse -Force -ErrorAction SilentlyContinue " + path,
	}
}

// TestBashRestoreLoopKeepsTheProgressWindowClimbing drives REAL bash commands through executeTool,
// because the guard machinery this relies on was already correct and merely unreachable from the
// bash path — a unit test on the guard alone would have passed before the fix too. The shape is the
// one observed live: back up, edit, restore, edit, restore. The net effect of each restore is a
// file state the turn already held, so it must not buy the run a fresh progress window.
func TestBashRestoreLoopKeepsTheProgressWindowClimbing(t *testing.T) {
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := closeAfter(t, New(store, nil, builtin.Default(), bus.New(), platform.New(), Config{Permission: "allow"}))
	wd := t.TempDir()
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})
	if err := os.WriteFile(filepath.Join(wd, "heap.c"), []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	s := a.sessionInfo(ctx, sid)
	actor := event.Actor{Kind: event.ActorAgent, ID: "coder"}
	guard := newRunGuard(nil)
	run := func(cmd string) {
		t.Helper()
		args, _ := json.Marshal(map[string]string{"command": cmd})
		a.executeTool(ctx, s, AgentSpec{Name: "coder"}, 0, actor, &session.ToolCall{
			CallID: "c_" + newID(), Name: "bash", Args: args,
		}, guard, "")
	}

	run(copyFile("heap.c", "heap.c.bak").cmd())
	if guard.mutationEpoch() == 0 {
		t.Fatal("precondition: a cp must register as a bash mutation")
	}
	// The first patch is REAL progress — a state this file has never held — so it earns a fresh
	// window. That is the baseline the loop then has to climb away from.
	run(swap("heap.c", "original", "patched").cmd())
	guard.mu.Lock()
	since0 := guard.sinceProgress
	guard.mu.Unlock()
	if since0 != 0 {
		t.Fatalf("precondition: a genuinely new version restarts the progress window, got %d", since0)
	}

	// Now the loop: restore→re-patch, over and over. Every command differs from the one before it,
	// so the command-text idempotence check inside mutated() cannot see it — each swing looked like
	// a brand-new deliverable version and zeroed both windows, which is how the run stayed one step
	// from the threshold forever and burned its whole budget here. The content read is what sees it,
	// so the windows must now CLIMB straight through the loop.
	for i := 0; i < 18; i++ {
		run(copyFile("heap.c.bak", "heap.c").cmd())
		run(swap("heap.c", "original", "patched").cmd())
	}

	guard.mu.Lock()
	since := guard.sinceProgress
	guard.mu.Unlock()
	if since == 0 {
		t.Error("the progress window must climb across a restore loop, got sinceProgress=0")
	}

	// The control, in the same run: a bash edit to a state the file has never held IS progress and
	// restarts the window, so this cannot be mistaken for "bash mutations stopped counting".
	run(swap("heap.c", "patched", "brand-new").cmd())
	guard.mu.Lock()
	since = guard.sinceProgress
	guard.mu.Unlock()
	if since != 0 {
		t.Errorf("a genuinely new version must restart the window, got sinceProgress=%d", since)
	}
}

// Driven through executeTool for the same reason as the test above: the defect is at the seam, and
// a unit test on noteEdit alone passes either way. Observed live on the restarted fix-ocaml-gc:
// `rm -rf _build && make world … || true` — _build never existed, so the before and after reads both
// came back empty and the result carried "[self-edit check] this write left the file byte-for-byte
// as it already was" about a command that neither wrote nor deleted a thing.
func TestRemovingAPathThatNeverExistedSaysNothing(t *testing.T) {
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := closeAfter(t, New(store, nil, builtin.Default(), bus.New(), platform.New(), Config{Permission: "allow"}))
	wd := t.TempDir()
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})
	ctx := context.Background()
	s := a.sessionInfo(ctx, sid)
	actor := event.Actor{Kind: event.ActorAgent, ID: "coder"}
	guard := newRunGuard(nil)
	run := func(cmd string) string {
		t.Helper()
		args, _ := json.Marshal(map[string]string{"command": cmd})
		a.executeTool(ctx, s, AgentSpec{Name: "coder"}, 0, actor, &session.ToolCall{
			CallID: "c_" + newID(), Name: "bash", Args: args,
		}, guard, "")
		evs, err := a.store.Read(ctx, sid, 0)
		if err != nil {
			t.Fatal(err)
		}
		var last string
		for _, e := range evs {
			var d event.PartAppendedData
			if e.Type != event.TypePartAppended || json.Unmarshal(e.Data, &d) != nil {
				continue
			}
			if d.Part.Kind == session.PartToolResult && d.Part.ToolResult != nil {
				last = string(d.Part.ToolResult.Content)
			}
		}
		return last
	}

	// A real file must still register, so the epoch is armed exactly as it was live. `echo … > file`
	// is the one line here that both shells read the same way, so it stays as it is.
	run("echo hi > kept.txt")
	if out := run(removeTree("_build").cmd()); strings.Contains(out, "self-edit check") {
		t.Errorf("removing a path that never existed is not a rewrite of anything:\n%s", out)
	}
	// The check still fires for what it exists to catch: a mutation whose net effect returns a file
	// to a state this turn already held. (An IDENTICAL command text is not a new mutation at all, so
	// it never reaches the content comparison — the swing has to be written two different ways.)
	run(writeLine("A", "f.txt").cmd())
	run(swap("f.txt", "A", "B").cmd())
	if out := run(swap("f.txt", "B", "A").cmd()); !strings.Contains(out, "self-edit check") {
		t.Errorf("a mutation that restores a state the turn already held must be reported:\n%s", out)
	}
}
