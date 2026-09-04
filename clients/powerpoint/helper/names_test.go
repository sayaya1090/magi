package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
)

// addinDir 은 애드인 소스가 사는 곳. 한 자리에 모아 두는 이유는 아래 시험들이 전부 그 트리를
// 훑기 때문이고, 옮기는 날 고칠 곳이 하나이기 때문이다.
const addinDir = "../addin"

// 이름 넷이 같은 문자열인가(DESIGN.md §5.5 ⚠, §9 마지막 항목).
//
// 매니페스트의 <SourceLocation>·<AppDomain>, 헬퍼가 바인드하는 주소, 인증서의 SAN, 애드인이
// 전송에서 부르는 URL. 어긋나면 증상이 「애드인이 안 붙는다」 하나로 뭉쳐 나오고 넷 중 어디가
// 틀렸는지는 화면에 안 나온다. 그래서 산문이 아니라 시험이 진다.
//
// **몇 개를 어디서 찾았는지 같이 적는다**(§9 「초록을 읽는 법」). 훑어서 「전부 같다」를 묻는
// 모양은 훑을 것이 없을 때도 초록이라, 찾은 수를 세지 않으면 「하나도 안 틀렸다」와 「볼 것이
// 없었다」가 같은 글자로 찍힌다.
func TestTheFourNamesAreOneString(t *testing.T) {
	body, err := os.ReadFile(filepath.Join(addinDir, "manifest.xml"))
	if err != nil {
		t.Fatalf("매니페스트를 못 읽었다: %v", err)
	}
	// 루프백을 가리키는 URL 만 본다. github.com 을 가리키는 SupportUrl·LearnMoreUrl 은 이
	// 규칙의 대상이 아니다 — 우리 오리진이 아니라 남의 주소다.
	loopback := regexp.MustCompile(`https?://(?:localhost|127\.0\.0\.1|\[::1\])(?::\d+)?`)
	found := loopback.FindAllString(string(body), -1)
	if len(found) == 0 {
		t.Fatal("매니페스트에서 루프백 주소를 하나도 못 찾았다 — 이건 '다 같다'가 아니라 '볼 것이 없었다'다")
	}
	want := Origin(DefaultPort)
	for _, got := range found {
		if got != want {
			t.Errorf("매니페스트가 %q 로 적혀 있다. 하나의 이름은 %q 다", got, want)
		}
	}
	t.Logf("매니페스트에서 루프백 주소 %d 개를 찾아 %q 와 견줬다", len(found), want)

	// 그리고 그 URL 들이 실제로 서 있어야 하는 두 자리가 있는지 본다. 위 스캔은 「찾은 것이
	// 전부 같다」만 묻지 「있어야 할 것이 있다」는 안 묻는다.
	for _, must := range []string{
		"<AppDomain>" + want + "</AppDomain>",
		`<SourceLocation DefaultValue="` + PageURL(DefaultPort) + `"`,
	} {
		if !strings.Contains(string(body), must) {
			t.Errorf("매니페스트에 이 줄이 없다: %s", must)
		}
	}
}

// 애드인은 자기 오리진을 **쓰지 적지 않는다**(§5.5 — 페이지를 헬퍼가 내주므로 주소는 자기
// 오리진이다). 소스에 주소를 박으면 위 시험이 세는 넷이 다섯이 되고, 다섯째는 아무도 안 본다.
func TestTheAddinDoesNotWriteTheOriginDown(t *testing.T) {
	literal := regexp.MustCompile(`https?://(?:localhost|127\.0\.0\.1|\[::1\])`)
	scanned := 0
	root := filepath.Join(addinDir, "src")
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".js") {
			return err
		}
		scanned++
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, hit := range literal.FindAllString(string(b), -1) {
			t.Errorf("%s 가 오리진을 적어 뒀다(%s). 애드인은 location.origin 을 쓴다", path, hit)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("애드인 소스를 못 훑었다: %v", err)
	}
	if scanned == 0 {
		t.Fatalf("%s 에서 .js 를 하나도 못 찾았다 — 훑을 것이 없었다", root)
	}
	t.Logf("애드인 소스 %d 개를 훑었다", scanned)
}

// 서버 이름은 **자기 자신으로 다듬어져야** 한다(§5.0.6). 코어가 `sanitizeToolPart` 로
// [A-Za-z0-9_-] 밖을 `_` 로 바꾸는데, 다듬은 뒤 달라지는 이름을 고르면 사람이 목록에서 보는
// 이름과 allow 룰에 적어야 하는 이름이 갈린다. 그 함수는 unexported 라 여기서 못 부르므로,
// **그 함수가 손대지 않는 문자만 쓴다**는 더 강한 조건을 건다.
func TestTheServerNameSanitizesToItself(t *testing.T) {
	safe := regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	if !safe.MatchString(ServerName) {
		t.Fatalf("서버 이름 %q 에 다듬기가 손댈 문자가 있다", ServerName)
	}
	if ServerName == "" {
		t.Fatal("빈 이름은 다듬기가 'x' 로 바꾼다")
	}
}

// 그리고 그 이름이 **내장 도구 이름이면 attach 자체가 거절된다**(코어의 `App.AttachToolServer`:
// "%q is the name of a tool this companion already has"). 산문으로 「안 겹친다」고 적어 두면
// 내장 도구가 하나 늘어난 날 조용히 틀린다. 그러니 목록을 세지 말고 목록에게 묻는다.
func TestTheServerNameIsNotABuiltinToolName(t *testing.T) {
	tools := builtin.Default().List()
	if len(tools) == 0 {
		t.Fatal("내장 도구를 하나도 못 받았다 — 볼 것이 없었다")
	}
	for _, tl := range tools {
		if tl.Name() == ServerName {
			t.Fatalf("서버 이름 %q 가 내장 도구와 같다", ServerName)
		}
	}
	t.Logf("내장 도구 %d 개와 견줬다", len(tools))
}

// 매니페스트의 자식 순서가 스키마를 지키는가.
//
// `VersionOverridesV1_0` 의 순서는 Description? → **Requirements?** → Hosts → Resources 다.
// 한동안 이 트리의 매니페스트는 `<Requirements>` 를 `<Hosts>` **뒤에** 두고 있었고, 스키마를
// 어긴 매니페스트를 Office 는 **통째로 버린다** — 그때 증상은 「리본 단추가 없다」가 아니라
// **「애드인이 아예 없다」**이고, 어디에도 사유가 안 뜬다. 사이드로드해 보고서야 보이는 자리라
// 시험이 없으면 다음 사람이 같은 하루를 다시 쓴다.
func TestTheManifestKeepsTheSchemaOrder(t *testing.T) {
	body, err := os.ReadFile(filepath.Join(addinDir, "manifest.xml"))
	if err != nil {
		t.Fatalf("매니페스트를 못 읽었다: %v", err)
	}
	// **주석을 먼저 걷어낸다.** 이 매니페스트의 주석은 자기가 지키는 규칙을 설명하느라 요소
	// 이름을 그대로 적는데, 그러면 스캐너가 산문을 마크업으로 센다 — 처음 이 시험을 돌렸을 때
	// 실제로 그랬다(순서는 맞는데 빨갛게 떴다). 자기 바늘에 걸리는 스캐너를 예외로 빼는 대신
	// 보는 것을 좁힌다.
	text := xmlComment.ReplaceAllString(string(body), "")
	vo := strings.Index(text, "<VersionOverrides")
	if vo < 0 {
		t.Fatal("VersionOverrides 가 없다 — 볼 것이 없었다")
	}
	req := strings.Index(text[vo:], "<Requirements>")
	hosts := strings.Index(text[vo:], "<Hosts>")
	res := strings.Index(text[vo:], "<Resources>")
	if req < 0 || hosts < 0 || res < 0 {
		t.Fatalf("VersionOverrides 안에서 셋 중 하나를 못 찾았다: req=%d hosts=%d res=%d", req, hosts, res)
	}
	if !(req < hosts && hosts < res) {
		t.Errorf("VersionOverrides 의 순서가 Requirements → Hosts → Resources 가 아니다 "+
			"(req=%d hosts=%d res=%d). Office 는 이 매니페스트를 통째로 버리고 아무 말도 안 한다",
			req, hosts, res)
	}
}

// xmlComment 는 `<!-- … -->` 하나. `(?s)` 로 줄바꿈을 넘긴다.
var xmlComment = regexp.MustCompile(`(?s)<!--.*?-->`)

// **주소가 덱을 나른다** — 덱마다 자기 데몬을 둘 때, 그 데몬에서 온 호출은 어느 덱인지가 정해진다.
func TestMCPURLCarriesTheDeck(t *testing.T) {
	if got := MCPURL(3000, ""); got != Origin(3000)+"/mcp" {
		t.Errorf("덱을 모르면 옛 주소 그대로여야 한다: %s", got)
	}
	got := MCPURL(3000, "doc-1-2")
	if got != Origin(3000)+"/mcp?deck=doc-1-2" {
		t.Errorf("덱이 주소에 안 실렸다: %s", got)
	}
	// **이름을 그대로 붙이지 않는다.** 덱 이름에 `&` 나 공백이 들어오면 질의가 갈라진다.
	if q := MCPURL(3000, "a b&c=d"); !strings.Contains(q, "deck=a+b%26c%3Dd") {
		t.Errorf("덱 이름을 안 감쌌다: %s", q)
	}
}
