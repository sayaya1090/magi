package arch

import (
	"path"
	"sort"
	"strings"
	"testing"
)

// The console (cmd/magi-web) releases on its own clock — a web-v* tag ships it alone, on a
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
	// 콘솔이 내놓는 파일들과 정적 데모의 목 — 콘솔의 주기로 나가는 것이 맞다: 매니페스트도
	// 워커도 아이콘 스프라이트도 데모의 답도 전부 "이 콘솔이 무엇으로 보이는가"에 대한 것이고,
	// 두 콘솔(옛 것과 새 것)이 같은 바이트를 내야 하므로 코어가 아니라 여기 공용 자리에 산다.
	"internal/webassets":    true,
	"internal/webdemo":      true,
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
	const root = "cmd/magi-web"
	if len(pkgImports[root]) == 0 {
		t.Fatal("cmd/magi-web imports nothing from the module — the walk is broken, so this asserts nothing")
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
			t.Errorf("cmd/magi-web now (transitively) depends on %s — a web release re-ships that "+
				"package on the console's own lifecycle. If that is the right call, add it to "+
				"consoleSurface and say why in the commit.", dep)
		}
	}
	for _, dep := range sortedSet(consoleSurface) {
		if !reached[dep] {
			t.Errorf("cmd/magi-web no longer depends on %s — drop it from consoleSurface so the "+
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
