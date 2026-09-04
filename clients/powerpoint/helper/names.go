// magi-ppt — PowerPoint 애드인의 헬퍼. 머신(사용자)당 하나다.
//
// 얼굴이 셋이다(DESIGN.md §5.1·§5.5·§5.7).
//
//   - magi 데몬 쪽으로는 **MCP 서버**다. 도구 호출이 Streamable HTTP 로 들어온다.
//   - 애드인 쪽으로는 **페이지를 내주는 https 서버이자 원격 손을 부리는 쪽**이다. 애드인이
//     붙어서 연결을 열어 두면 도구 호출이 그 연결을 거슬러 내려간다.
//   - 그리고 그 연결을 **반대 방향으로 한 번 더** 써서 데몬의 대화를 애드인으로 흘린다.
//
// 이 파일은 그중 아무것도 안 하고 **이름만** 든다. 이름이 여기 모여 있는 이유는 §5.5 의 ⚠ 다:
// 매니페스트의 주소, 헬퍼가 바인드하는 주소, 인증서의 SAN, 애드인이 전송에서 부르는 URL —
// 넷이 **같은 문자열**이어야 하는데 어긋나면 증상이 「애드인이 안 붙는다」 하나로 뭉쳐 나온다.
// 넷 중 어디가 틀렸는지는 화면에 안 나오고, `localhost` 와 `127.0.0.1` 은 사람 눈에 같아 보인다.
package main

import (
	"fmt"
	"net/url"

	"github.com/sayaya1090/magi/internal/version"
)

// helperVersion 은 핸드셰이크의 `serverInfo` 에 실린다. magi 는 그 답을 안 읽지만(§4.4) 다른
// 클라이언트는 읽고, 무엇보다 사람이 로그에서 어느 빌드였는지를 묻는다.
var helperVersion = version.Version

const (
	// Host 는 그 하나의 이름이다. `localhost` 가 아니라 IP 리터럴인 이유는 오리진이 문자열로
	// 견줘지기 때문이다 — 페이지를 `localhost` 로 내주고 전송이 `127.0.0.1` 을 부르면 자기
	// 오리진이 아니게 되어 §5.5 가 없앴다고 적은 벽 둘이 그대로 돌아온다. 설계 본문이 줄곧
	// 이 문자열로 적었고, 목업 매니페스트만 `localhost` 였다(§5.5 ⚠). 매니페스트를 고쳤다.
	Host = "127.0.0.1"

	// DefaultPort 는 **기본값이지 정설이 아니다**(§5.5.1 마지막 항목). 출하 값은 배포하는 쪽이
	// 정하고, 정한 값을 매니페스트와 헬퍼 양쪽에 같이 적는다. 목업이 3000 을 쓰고 있어서
	// 그것을 기본으로 둔다 — 새 숫자를 지어내면 이미 사이드로드해 둔 매니페스트가 깨진다.
	DefaultPort = 3000

	// ServerName 은 door 에 넘기는 이름이고, 도구는 `mcp__ppt__set_text` 로 등록된다(§5.0.6).
	//
	// **고정이다.** PID·창 번호·덱 이름을 안 넣는다: 모든 MCP 도구가 danger tool 이라(§4.4 ②)
	// 그것을 면하는 유일한 수단이 오퍼레이터의 allow 룰인데, 그 룰의 도구 자리에는 와일드카드가
	// 없어서 이름이 실행마다 바뀌면 규칙이 재시작마다 무효가 된다.
	ServerName = "ppt"
)

// Origin 은 애드인 페이지의 자기 오리진이자 데몬이 dial 하는 곳의 앞부분이다.
func Origin(port int) string { return fmt.Sprintf("https://%s:%d", Host, port) }

// PageURL 은 매니페스트의 <SourceLocation> 이 가리켜야 하는 자리다.
func PageURL(port int) string { return Origin(port) + "/taskpane.html" }

// MCPURL 은 `mcp-attach` 에 넘기는 URL 이다(§5.0.1 — door 는 URL 만 받고 커맨드라인은 안 받는다).
// **주소가 덱을 나른다.**
//
// MCP 호출에는 「어느 대화가 불렀는지」가 안 실려 온다 — 등록이 이름당 한 벌이고 데몬 전체를
// 덮는다. 그래서 덱마다 자기 데몬을 두고, 그 데몬이 붙는 **주소에** 덱을 적는다. 그러면 그
// 데몬에서 온 호출은 `document` 를 생략해도 어느 덱인지가 정해진다.
//
// 이 자리가 없으면 증상은 이렇다(2026-09-04 실물): 창 둘 중 새 덱에 만들라고 했는데 모델이 읽은
// 것은 옆 덱이었다. 그때는 아직 읽기만 했지만, 한 호출만 늦었어도 남의 덱에 장이 생겼다.
func MCPURL(port int, deck string) string {
	at := Origin(port) + "/mcp"
	if deck == "" {
		return at
	}
	return at + "?deck=" + url.QueryEscape(deck)
}

// BindAddr 는 리슨하는 자리. **루프백만** 이다(§8) — 라우팅 가능한 주소로는 안 연다.
func BindAddr(port int) string { return fmt.Sprintf("%s:%d", Host, port) }
