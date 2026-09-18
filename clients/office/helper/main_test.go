package office

import (
	"bytes"
	"strings"
	"testing"
)

// TestAnswersGoToStdoutAndDiagnosticsToStderr 는 질의성 출력(stdout)과 진단 로그(stderr)의 스트림 분리 여부를 검증합니다.
//
// [2026-09-04 실측 결함 방지]
// 이전 구현에서는 `run`이 stderr 단일 스트림으로만 출력하여, 매뉴얼 §7 안내에 따라
// `magi-word -allow-rules > config.toml`로 리다이렉션 시 빈 파일이 생성되고
// 모든 읽기 도구 호출마다 권한 확인 팝업이 발생하는 결함이 있었습니다(§2.1).
// 사용자가 요청한 데이터 출력(버전, 허용 규칙, 인증서 힌트 등)은 stdout으로,
// 기동 배너 및 진단 메시지는 stderr로 명확히 분리하여 파이프라인 연동 무결성을 보장합니다.
func TestAnswersGoToStdoutAndDiagnosticsToStderr(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		flag string
		want string
	}{
		{"-version", "magi office"},
		{"-allow-rules=word", "mcp__word__list_paragraphs(**)"},
		{"-allow-rules=xl", "mcp__xl__list_sheets(**)"},
		{"-allow-rules=ppt", "mcp__ppt__list_slides(**)"},
		{"-cert-hint", "office-helper-cert.pem"},
	}
	for _, c := range cases {
		var out, logw bytes.Buffer
		if code := Run([]string{c.flag, "-config-dir", dir}, &out, &logw); code != 0 {
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
