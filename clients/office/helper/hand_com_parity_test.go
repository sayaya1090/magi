package office

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// 재는 자리와 도는 자리가 갈리면 초록이 거짓이 된다. COM 손(hand-com/src/Hand.cs)의 Known 은 이 헬퍼의
// catalogue 와 같은 이름들이어야 한다 — 여기 도구가 하나 늘면 저쪽이 「모른다」로 거절하고, 저쪽에만 있는
// 이름은 아무도 못 부른다. 그 소스를 여기서 읽어 양쪽 차집합이 비어 있는지 잰다.
func TestTheComHandKnowsExactlyTheCatalogue(t *testing.T) {
	src, err := os.ReadFile("../hand-com/src/Hand.cs")
	if err != nil {
		t.Skip("hand-com 이 옆에 없다: ", err)
	}
	s := string(src)
	i := strings.Index(s, "Known = new HashSet<string> {")
	if i < 0 {
		t.Fatal("Hand.cs 에 Known 집합이 없다")
	}
	j := strings.Index(s[i:], "};")
	known := map[string]bool{}
	for _, m := range regexp.MustCompile(`"([a-z_]+)"`).FindAllStringSubmatch(s[i:i+j], -1) {
		known[m[1]] = true
	}
	mine := map[string]bool{}
	for _, x := range PPT.Catalogue(false) {
		mine[x.Name] = true
	}
	var onlyHere, onlyThere []string
	for n := range mine {
		if !known[n] {
			onlyHere = append(onlyHere, n)
		}
	}
	for n := range known {
		if !mine[n] {
			onlyThere = append(onlyThere, n)
		}
	}
	sort.Strings(onlyHere)
	sort.Strings(onlyThere)
	if len(onlyHere) > 0 || len(onlyThere) > 0 {
		t.Fatalf("COM 손과 catalogue 가 어긋난다 — 헬퍼에만: %v · COM 손에만: %v", onlyHere, onlyThere)
	}
	if len(known) != 48 {
		t.Fatalf("도구가 %d개다 — 문서(48)를 같이 고쳐라", len(known))
	}
}
