package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestAnswersGoToStdoutAndDiagnosticsToStderr 는 **물어본 것에 답하는 출력이 파이프로 받아지는가**를
// 잰다.
//
// 실물에서 나왔다(2026-09-04). `run` 이 한 곳(stderr)에만 쓰고 있어서, 매뉴얼 §7 이 시키는 대로
// `magi-word -allow-rules > config.toml` 을 하면 **빈 파일이 조용히 생겼다.** 화면에는 규칙이
// 보이므로 사람은 받은 줄 알고, 그 컴패니언은 읽기 도구마다 사람에게 물어보게 된다 — §2.1 이
// 「안 뜰 이유를 없앤다」고 적어 둔 스냅샷이 제일 먼저 무너지는 자리다.
//
// 가르는 축은 길이가 아니라 **「사람이 물어본 것인가」**다. 그래서 기동 배너는 여기서 안 잰다.
func TestAnswersGoToStdoutAndDiagnosticsToStderr(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		flag string
		want string
	}{
		{"-version", "magi-word"},
		{"-allow-rules", "mcp__word__list_paragraphs(**)"},
		{"-cert-hint", "word-helper-cert.pem"},
	}
	for _, c := range cases {
		var out, logw bytes.Buffer
		if code := run([]string{c.flag, "-config-dir", dir}, &out, &logw); code != 0 {
			t.Errorf("%s: 종료 코드 %d", c.flag, code)
		}
		if !strings.Contains(out.String(), c.want) {
			t.Errorf("%s: stdout 에 %q 가 없다 — 파이프로 못 받는다.\nstdout=%q\nstderr=%q",
				c.flag, c.want, out.String(), logw.String())
		}
		if logw.Len() > 0 {
			t.Errorf("%s: stderr 에 %q 가 나왔다 — 답은 stdout 한 곳이어야 한다",
				c.flag, logw.String())
		}
	}
}
