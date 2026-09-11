package idebridge

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
)

// Waiting beats working. A turn blocked on a person IS running, so a rule that checked "is a tool
// reporting progress" first would answer "working" and leave the question standing with nothing on
// the screen pointing at it — the person waits for a companion that is waiting for them.
func TestWaitingBeatsWorking(t *testing.T) {
	got := activityOf(daemon.Status{
		Asking: &daemon.Waiting{Kind: "permission", What: "run `rm -rf build`"},
		Doing:  "compiling",
	})
	if got.State != Waiting {
		t.Fatalf("state = %q, want %q", got.State, Waiting)
	}
	if got.Asking != "run `rm -rf build`" {
		t.Errorf("asking = %q — the prompt itself is what the screen shows", got.Asking)
	}
}

// The prompt's own sentence first, then the reason, then the bare kind. A screen that only ever had
// "permission" to show would make every prompt look the same.
func TestAskingFallsBackToTheKindOnlyWhenNothingBetterWasSaid(t *testing.T) {
	got := activityOf(daemon.Status{Asking: &daemon.Waiting{Kind: "question"}})
	if got.Asking != "question" {
		t.Errorf("asking = %q, want the kind when nothing else was said", got.Asking)
	}
	withReason := activityOf(daemon.Status{Asking: &daemon.Waiting{Kind: "permission", Reason: "not on the allow list"}})
	if withReason.Asking != "not on the allow list" {
		t.Errorf("asking = %q, want the reason over the bare kind", withReason.Asking)
	}
}

// Whitespace is not a progress note. A tool that reported "  " would otherwise pin the screen on
// "working · " for the rest of the turn.
func TestBlankProgressIsNotWorking(t *testing.T) {
	if got := activityOf(daemon.Status{Doing: "   "}); got.State != Attached {
		t.Errorf("state = %q for a blank note, want %q", got.State, Attached)
	}
	if got := activityOf(daemon.Status{Doing: "running tests"}); got.State != Working || got.Doing != "running tests" {
		t.Errorf("state = %q doing = %q, want working with the note", got.State, got.Doing)
	}
}

// A key that was never said is left OUT, not set empty. A screen merging a new reading over an old
// one would otherwise overwrite what it knew with nothing — and `model` is genuinely absent whenever
// the request named no session, which is not the same as "no model".
func TestUnsaidSetupKeysAreAbsentRatherThanEmpty(t *testing.T) {
	got := activityOf(daemon.Status{Permission: "ask", Model: "  "})
	if _, ok := got.Setup["model"]; ok {
		t.Errorf("model is present for a blank value: %v", got.Setup)
	}
	if got.Setup["permission"] != "ask" {
		t.Errorf("permission = %q, want it carried", got.Setup["permission"])
	}
}

// No socket file is "nobody is there"; a socket that exists but refuses is, for the person, still
// nothing to talk to. Both must say so in the ONE vocabulary rather than as an exception — and both
// must carry a reason, because they are fixed in completely different ways.
func TestNoCompanionAnswersInTheVocabularyAndSaysWhy(t *testing.T) {
	// Short, like listen()'s: t.TempDir() under a long test name is itself past the address limit,
	// and the length check — which runs first, because a path that cannot be an address cannot be
	// dialled — would answer "unknown" about the test's own directory rather than about the socket.
	dir, err := os.MkdirTemp("", "idb")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	got := run(t, filepath.Join(dir, "nothing.sock"), `{"id":1,"method":"activity"}`)
	if got[0]["ok"] != true {
		t.Fatalf("activity failed rather than answering: %v", got[0])
	}
	if got[0]["state"] != NotRunning {
		t.Errorf("state = %v, want %q", got[0]["state"], NotRunning)
	}
	if why, _ := got[0]["why"].(string); why == "" {
		t.Error("no why: a client cannot tell which kind of nothing it found")
	}
}

// Past the address limit the OS refuses with "invalid argument", which names nothing. That is not
// "not running" — there may well be a companion — so it answers unknown and says what to change.
func TestAPathTooLongForTheAddressIsUnknownNotAbsent(t *testing.T) {
	long := "/" + strings.Repeat("x", 120) + ".sock"
	got := run(t, long, `{"id":1,"method":"activity"}`)
	if got[0]["state"] != Unknown {
		t.Fatalf("state = %v, want %q for an unusable path", got[0]["state"], Unknown)
	}
	why, _ := got[0]["why"].(string)
	if !strings.Contains(why, "MAGI_SOCKET_DIR") {
		t.Errorf("why does not name the way out: %q", why)
	}
}

// Measured on Windows 11 (2026-09-09): under %AppData% an AF_UNIX bind succeeds and every connect
// is refused with WSAEINVAL, at any length. The listener believes it is up, the caller is refused,
// and neither error says where to look — so the answer carries the sentence that does.
func TestAnAppDataSocketOnWindowsSaysWhereToLook(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the trap is a Windows one; the hint is only ever added there")
	}
	dir := filepath.Join(t.TempDir(), "AppData", "magi")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(dir, "d.sock")
	// A plain file is enough: reach() looks for something at the path, then fails to dial it.
	if err := os.WriteFile(sock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	got := run(t, sock, `{"id":1,"method":"activity"}`)
	why, _ := got[0]["why"].(string)
	if !strings.Contains(why, "MAGI_SOCKET_DIR") {
		t.Errorf("an AppData socket did not point at the escape hatch: %q", why)
	}
}

// The hint is for one platform's one trap. Adding it elsewhere would send somebody chasing a
// Windows problem on a machine that does not have it.
func TestTheAppDataHintIsNotAddedToUnrelatedPaths(t *testing.T) {
	if h := hint(filepath.Join("/home", "me", ".magi", "d.sock")); h != "" {
		t.Errorf("hint on an ordinary path: %q", h)
	}
}

// The daemon fills the model only when the request names a session. A poll that omits it gets no
// model, and a bridge that dropped the field would make that absence permanent.
func TestTheSessionReachesTheDaemon(t *testing.T) {
	d := listen(t, func(string) string { return `{"ok":true,"model":"opus","permission":"ask"}` })
	got := run(t, d.path, `{"id":1,"method":"activity","session":"s_01"}`)
	if got[0]["state"] != Attached {
		t.Fatalf("state = %v, want %q", got[0]["state"], Attached)
	}
	sent := d.requests()
	if len(sent) != 1 || !strings.Contains(sent[0], `"session":"s_01"`) {
		t.Fatalf("the daemon never saw the session: %q", sent)
	}
	setup, _ := got[0]["setup"].(map[string]any)
	if setup["model"] != "opus" {
		t.Errorf("setup = %v, want the model it named", setup)
	}
}

// 두 사본이 같은 낱말을 말하나.
//
// 이 패키지의 존재 이유가 「한 규칙이 화면마다 한 벌씩 적히는 것」을 끝내는 것인데, 옮기는
// 동안에는 사본이 **둘**이다(여기와 `clients/vscode/src/core/activity.ts`). 그리고 실제로
// 갈렸다 — 2026-09-09, 타입스크립트 쪽에서 「데몬이 안 말한 침묵」을 idle 로 그리던 것을 고쳤는데
// 이 패키지가 같은 낱말을 그대로 들고 있었다. 사람이 두 파일을 같이 여는 것에 기대면 안 되는
// 종류의 일이라, 시험이 진다.
//
// 낱말만 본다. 언제 어느 낱말을 고르는지는 각 쪽의 시험이 따로 지고(여기 위쪽, 저쪽
// `setup.test.ts`), 여기서 묻는 것은 **어휘가 하나인가** 뿐이다.
func TestBothCopiesSpeakOneVocabulary(t *testing.T) {
	ts := filepath.Join("..", "..", "..", "clients", "vscode", "src", "core", "activity.ts")
	body, err := os.ReadFile(ts)
	if err != nil {
		t.Fatalf("타입스크립트 사본을 못 읽었다 — 옮겨졌으면 이 시험부터 고칠 것: %v", err)
	}
	// `Name = 'value',` 에서 값만. 주석 안의 따옴표에 안 걸리도록 열거 항목 모양을 그대로 본다.
	re := regexp.MustCompile(`(?m)^\s{2}[A-Z]\w*\s*=\s*'([a-z-]+)',`)
	got := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(string(body), -1) {
		got[m[1]] = true
	}
	// 훑어서 「전부 같다」를 묻는 모양은 훑을 것이 없을 때도 초록이다.
	if len(got) < 3 {
		t.Fatalf("타입스크립트에서 낱말을 %d 개밖에 못 찾았다 — 스캔이 깨진 것이지 어휘가 맞는 게 아니다", len(got))
	}
	// The Go side is READ, not restated. A literal here could only ever hold the words somebody
	// remembered to type into it — see declaredWords for what that cost.
	want := map[string]bool{}
	for _, v := range declaredWords(t, "activity.go") {
		want[v] = true
	}
	if len(want) < 3 {
		t.Fatalf("activity.go 에서 낱말을 %d 개밖에 못 찾았다 — 스캔이 깨졌다", len(want))
	}
	for w := range want {
		if !got[w] {
			t.Errorf("이 패키지는 %q 를 말하는데 타입스크립트 사본에는 없다", w)
		}
	}
	for w := range got {
		if !want[w] {
			t.Errorf("타입스크립트 사본이 %q 를 말하는데 이 패키지에는 없다", w)
		}
	}
	t.Logf("낱말 %d 개를 두 사본에서 견줬다", len(want))
}
