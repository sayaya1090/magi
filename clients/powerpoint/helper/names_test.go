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
const addinDir = "../mockup"

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
