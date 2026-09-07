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
	// 자리는 `clients/powerpoint/hand-com` 이다. 2026-09-07 까지 여기가 `../hand-com` 이라 **이 시험은 한 번도
	// 안 돌았다** — `t.Skip` 은 화면에서 초록과 구별이 안 된다(TESTING §9.1 의 「초록인데 안 재고 있던 모양」).
	// 그래서 못 읽으면 건너뛰지 않고 실패한다: 이 파일이 옮겨지면 그 사실이 바로 보여야 한다.
	src, err := os.ReadFile("../../powerpoint/hand-com/src/Hand.cs")
	if err != nil {
		t.Fatalf("COM 손의 소스를 못 읽었다(%v) — 자리가 바뀌었으면 이 경로를 같이 고친다", err)
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
		if x.Local != nil {
			continue // 헬퍼가 답하는 도구는 손에 안 간다 — 손이 몰라도 된다(list_documents)
		}
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
