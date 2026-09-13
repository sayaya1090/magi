//go:build windows

package platform

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/sayaya1090/magi/internal/port"
)

// `bash` 도구만 셸을 부르는 것이 아니다 — **이 문도 있다**: `!` 인라인 셸(`App.RunShell`), 워크플로
// 단계, `git`. 같은 결함이 같은 플랫폼에서 같은 모양으로 나타나므로, 같은 변환을 여기서도 잰다.
//
// ⚠ 기대값을 로케일에 박지 않는다 — 무엇을 쓸 수 있는지 이 기계에게 먼저 묻는다
// (`internal/adapter/tool/builtin/nativetext_windows_test.go` 와 같은 방식, 같은 이유).
func TestOutputInTheMachinesCodePageComesBackAsText(t *testing.T) {
	run := func(command string) port.ExecResult {
		t.Helper()
		res, err := OS{}.Exec(context.Background(), port.Cmd{
			Path: "powershell", Args: []string{"-NoProfile", "-Command", command}, Dir: t.TempDir(),
		})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}

	const probe = `$e=[Console]::OutputEncoding; ` +
		`foreach ($s in @('위치 줄:1 문자:27','日本語の行','Grüße, Straße')) { ` +
		`if ($e.GetString($e.GetBytes($s)) -eq $s) { [Console]::Out.Write($s); break } }`
	want := strings.TrimSpace(string(run(probe).Stdout))
	if want == "" {
		t.Skip("이 셸의 출력 인코딩으로는 위 문자열 중 어느 것도 못 쓴다")
	}
	// 전제를 세운다. 변환이 아예 안 걸렸으면 이 답 자체가 읽히지 않는 바이트열인데, 그것을 그대로
	// 다음 명령에 넣으면 「깨진 것과 깨진 것을 견주는」 시험이 된다 — 실패는 하지만 이유를 안 말한다.
	if !utf8.ValidString(want) {
		t.Fatalf("전제가 안 섰다 — 탐침의 답이 읽히지 않는다(변환이 안 걸렸다): %q", want)
	}
	t.Logf("이 기계의 셸이 쓸 수 있는 글: %q", want)

	if got := strings.TrimSpace(string(run("Write-Output '" + want + "'").Stdout)); got != want {
		t.Errorf("표준 출력이 깨져서 돌아온다:\n 원했던 것: %q\n 읽은 것: %q", want, got)
	}
	// 그리고 표준 오류. 이쪽이 `!` 를 친 사람이 읽는 글이다.
	said := run("[Console]::Error.Write('" + want + "')")
	if got := strings.TrimSpace(string(said.Stderr)); got != want {
		t.Errorf("표준 오류가 깨져서 돌아온다:\n 원했던 것: %q\n 읽은 것: %q", want, got)
	}
}
