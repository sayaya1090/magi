package arch

import (
	"path"
	"sort"
	"strings"
	"testing"
)

// The console (clients/web/server) releases on its own clock — a web-v* tag ships it alone, on a
// lifecycle separate from the core's v* releases. That separation only stays real while the
// console's reach into this module stays deliberate: every package it pulls in is core code a
// web release silently re-ships, and the day the surface should shrink to a wire-and-files
// contract, this list is the work order.
//
// So the transitive closure is FROZEN, in both directions, the way appReachesIntoAdapters is:
// a new dependency must be added here by someone deciding the console should carry it, and one
// that drops out must be removed so the list cannot rot into fiction. Computed statically over
// every production file (all GOOSes at once), not via `go list`, so no build tag hides an edge.
var consoleSurface = map[string]bool{
	"internal/adapter/daemon":         true,
	"internal/adapter/experience/git": true,
	"internal/adapter/fleet":          true,
	"internal/adapter/identity":       true,
	"internal/adapter/platform":       true,
	"internal/adapter/provider":       true,
	"internal/adapter/store/jsonl":    true,
	"internal/adapter/tool/builtin":   true,
	"internal/app":                    true,
	"internal/atomicfile":             true,
	// 콘솔이 내놓는 파일들 — 콘솔의 주기로 나가는 것이 맞다: 매니페스트도 워커도 아이콘
	// 스프라이트도 전부 "이 콘솔이 무엇으로 보이는가"에 대한 것이다. 데모의 목은 이 목록에서
	// 빠졌다: 이제 답하는 것이 Go 문자열이 아니라 콘솔과 같이 컴파일되는 모듈(clients/web/ui/demo-ui)
	// 이고, 그러니 코어가 실어 나를 것이 아니다.
	"internal/webassets": true,
	// Windows 에서 콘솔 없는 데몬이 검은 창을 남기지 않게 하는 조각. platform·tool/builtin·app 이
	// 이미 실어 나르므로 콘솔이 데몬을 다시 띄우는 길에 같이 간다(2026-09-06, daemon(Windows) 커밋).
	"internal/quietconsole": true,
	// 경로가 «뿌리에서 시작하는가»를 두 플랫폼의 철자 모두로 답하는 잎 하나. 표준 라이브러리의
	// `IsAbs`/`IsLocal` 은 도는 플랫폼의 답을 주는데, 판정 대상은 부르는 쪽이 «어느 OS인지 생각하지
	// 않고» 쓴 글자다 — 그래서 같은 가드가 윈도우에서만 거절하고 리눅스에서는 통과했다(2026-09-10).
	// 콘솔이 이미 싣는 `tool/builtin` 과 `app` 이 이것을 쓰므로 같이 간다. 의존이 없는 잎이라
	// 표면이 넓어지는 것은 이름 하나뿐이고, 사본을 두 벌 두는 쪽이 더 비싸다(이 저장소가 그것을
	// 이미 한 번 치렀다 — 되돌릴 수 없는 삭제의 문이 제 사본을 들고 반만 고쳐져 있었다).
	// pid 하나가 살아 있는지를 두 플랫폼의 철자로 답하는 잎. **새 코드가 아니다** — 이 두 파일은
	// `adapter/daemon` 안에 있었고 콘솔은 이미 그것을 싣고 있었다. 밖으로 뺀 이유는 업데이트
	// 트랜잭션도 같은 물음을 해야 하기 때문이고(검토 R9: 감시하던 세대가 살아 있는가), 두 벌이면
	// 한쪽이 산 프로세스를 죽었다고 결론 내린다 — 그 결론 위의 행동이 양쪽 다 파괴적이다(청구를
	// 지우거나, 멀쩡한 빌드를 되돌리거나). 표면이 넓어지는 것은 이름 하나뿐이고 실리는 코드는
	// 그대로다.
	"internal/procalive":    true,
	"internal/pathx":        true,
	"internal/config":       true,
	"internal/core/auth":    true,
	"internal/core/bus":     true,
	"internal/core/change":  true,
	"internal/core/cluster": true,
	"internal/core/command": true,
	"internal/core/council": true,
	"internal/core/cron":    true,
	"internal/core/embed":   true,
	"internal/core/event":   true,
	"internal/core/lang":    true,
	"internal/core/meeting": true,
	"internal/core/model":   true,
	"internal/core/rank":    true,
	"internal/core/report":  true,
	"internal/core/session": true,
	"internal/core/text":    true,
	"internal/core/webpush": true,
	"internal/envflag":      true,
	"internal/port":         true,
	"internal/version":      true,
}

func TestTheConsolesDependencySurfaceIsFrozen(t *testing.T) {
	// package dir -> module-internal imports of its production files, over the whole repo.
	pkgImports := map[string]map[string]bool{}
	for _, f := range goFiles(t) {
		dir := path.Dir(f)
		m := pkgImports[dir]
		if m == nil {
			m = map[string]bool{}
			pkgImports[dir] = m
		}
		for _, imp := range imports(t, f) {
			m[imp] = true
		}
	}

	// Transitive closure from the console package.
	const root = "clients/web/server"
	if len(pkgImports[root]) == 0 {
		t.Fatal("clients/web/server imports nothing from the module — the walk is broken, so this asserts nothing")
	}
	reached := map[string]bool{}
	queue := []string{root}
	for len(queue) > 0 {
		dir := queue[0]
		queue = queue[1:]
		for imp := range pkgImports[dir] {
			if imp == root || reached[imp] {
				continue
			}
			reached[imp] = true
			queue = append(queue, imp)
		}
	}

	for _, dep := range sortedSet(reached) {
		if !consoleSurface[dep] {
			t.Errorf("clients/web/server now (transitively) depends on %s — a web release re-ships that "+
				"package on the console's own lifecycle. If that is the right call, add it to "+
				"consoleSurface and say why in the commit.", dep)
		}
	}
	for _, dep := range sortedSet(consoleSurface) {
		if !reached[dep] {
			t.Errorf("clients/web/server no longer depends on %s — drop it from consoleSurface so the "+
				"frozen surface stays the measured one.", dep)
		}
	}
}

func sortedSet(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		if strings.HasPrefix(k, "internal/") || strings.HasPrefix(k, "cmd/") {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}
