//go:build windows

package builtin

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/sayaya1090/magi/internal/port"
	"github.com/sayaya1090/magi/internal/wintext"
)

// **셸이 제 말로 하는 말을 모델이 읽을 수 있는가** — 윈도우에서 실물로 잰다.
//
// 이 플랫폼의 `bash` 도구는 `powershell -NoProfile -Command` 이고, PowerShell 5.1 은 **콘솔의 코드
// 페이지로** 씁니다 — UTF-8 이 아니라. 한국어 설치에서는 CP949 이고, 그 바이트가 JSON 을 지나면서
// 전부 U+FFFD 가 되어 모델은 `위치 줄:1` 대신 `??ġ ??:1` 을 읽습니다. 구문 오류와 없는 파일을
// 구분하지 못하고, 읽어야 했던 사유는 아무도 보기 전에 사라집니다(실측 2026-09-13, `internal/wintext`).
//
// ⚠ **기대값을 로케일에 박지 않습니다.** 「CP949 바이트가 한글이 된다」로 적으면 영어 윈도우에서
// 고장을 보고합니다. 그래서 이 기계의 셸에게 **무엇을 쓸 수 있는지 먼저 묻고**, 그 글자로 잽니다 —
// 코드 페이지를 시험이 정하지 않으므로 「어느 코드 페이지를 골라야 하나」도 함께 재입니다.

// throughBash runs one command the way the model does, and returns what the model would read.
func throughBash(t *testing.T, command string) string {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"command": command})
	res, err := Bash{}.Execute(context.Background(), b, port.ToolEnv{Workdir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	var s string
	if json.Unmarshal(res.Content, &s) != nil {
		s = string(res.Content)
	}
	return s
}

// aStringThisShellCanSay asks PowerShell which of these its own output encoding round-trips, so the
// subject of the test is chosen by the machine rather than by me. Non-ASCII is the whole point: an
// ASCII answer would pass with the fix removed.
func aStringThisShellCanSay(t *testing.T) string {
	t.Helper()
	const probe = `$e=[Console]::OutputEncoding; ` +
		`foreach ($s in @('위치 줄:1 문자:27','日本語の行','Grüße, Straße')) { ` +
		`if ($e.GetString($e.GetBytes($s)) -eq $s) { [Console]::Out.Write($s); break } }`
	// Straight through the shell the product uses, but NOT through the tool — asking the tool to
	// pick the subject would hide a broken decode inside the question itself.
	name, args := Shell(probe)
	out := runProbe(t, name, args)
	// 전제를 세운다 — 위 주석의 같은 이유. 변환이 안 걸리면 이 답부터 읽히지 않는다.
	if !utf8.ValidString(out) {
		t.Fatalf("전제가 안 섰다 — 탐침의 답이 읽히지 않는다(변환이 안 걸렸다): %q", out)
	}
	if strings.TrimSpace(out) == "" {
		t.Skip("이 셸의 출력 인코딩으로는 위 문자열 중 어느 것도 못 쓴다 — UTF-8 콘솔이거나 그 밖")
	}
	return strings.TrimSpace(out)
}

func TestWhatTheShellSaysInItsOwnCodePageIsReadableToTheModel(t *testing.T) {
	want := aStringThisShellCanSay(t)
	t.Logf("이 기계의 셸이 쓸 수 있는 글: %q", want)

	// 1. 셸이 스스로 쓴 글. `Write-Output` 은 콘솔의 인코딩으로 나가므로 이것이 실물 모양이다.
	if got := throughBash(t, "Write-Output '"+want+"'"); !strings.Contains(got, want) {
		t.Errorf("셸이 쓴 글이 모델에게 안 닿는다:\n 원했던 것: %q\n 읽은 것: %q", want, got)
	}

	// 2. 그리고 **셸이 화내는 말**. 이쪽이 값이 큰 자리다 — 모델이 다음에 무엇을 할지 정하는 글이고,
	//    깨지면 이유 없이 재시도한다. 무엇이라고 할지는 로케일마다 다르므로 문구를 박지 않고,
	//    「읽을 수 있는 글인가」만 묻는다: 이 시험의 결함 모양은 U+FFFD 로 덮인 출력이다.
	said := throughBash(t, "Get-Item '"+want+"-없는파일'")
	if strings.Contains(said, "\uFFFD") {
		t.Errorf("셸의 오류 메시지가 깨져서 도착한다 (U+FFFD 가 섞였다):\n%s", said)
	}
}

// runProbe runs one command directly and returns its output as text, using the same conversion the
// tool path uses — the question here is only WHICH string to test with, and a probe that could not
// read its own answer would pick none.
func runProbe(t *testing.T, name string, args []string) string {
	t.Helper()
	out, _ := exec.Command(name, args...).CombinedOutput()
	return string(wintext.ToUTF8(out))
}
