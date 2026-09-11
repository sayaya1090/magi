package arch

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// 소스가 §N 으로 가리키는 절은 실제로 있는 절이어야 한다.
//
// ⚠ **두 번호가 처음부터 허공을 가리키고 있었다.** 이 나무의 주석은 규칙을 `§5.5`·`§4.4` 처럼
// 절 번호로 가리킨다 — 44 개를 쓰고 그중 42 개는 어느 문서의 제목으로 정확히 풀린다. 나머지
// 둘, `§8.1`(턴 계량기)과 `§9.5`(크로스컴파일 불변식)는 **저장소 어느 문서에도, 역사의 어느
// 시점에도 없었다.** 2026-09-11 실측: `git log -S` 로 전 역사를 뒤져도 그 제목이 들어온 적이
// 없다. 2026-06-27 에 처음 쓰인 날부터 바깥의 무언가를 가리키고 있었고, 열여덟 자리로 불었다.
//
// 안 풀리는 인용은 없느니만 못하다. 읽는 사람은 그것을 찾아 나서고, 못 찾은 뒤에도 **규칙이
// 옮겨 간 것인지 애초에 없었던 것인지** 알 수 없다. 앞의 것이면 찾아야 하고 뒤의 것이면
// 그만두어야 하는데, 화면에 나오는 것은 같은 「없음」이다.
//
// 그래서 42 개가 지금 정직하다는 사실을 붙든다. 새 인용이 허공을 가리키면 여기서 운다.
//
// 인용이 문서를 대면 **그 문서에서** 묻는다.
//
// ⚠ **처음 판은 번호만 봤고, 그래서 엉뚱한 문서로 풀려도 통과했다.** 153 개 인용 중 31 개는
// `DESIGN.md §5.5`·`CLIENT_LIFECYCLE §5` 처럼 문서를 함께 댄다. 번호만 보는 검사는 그 이름을
// 버리므로, `DESIGN.md §11` 처럼 **그 문서에 없고 다른 문서에만 있는** 절을 가리켜도 초록이다 —
// 2026-09-11 실측: certs.go 의 `DESIGN.md §5.5` 를 `DESIGN.md §11` 로 바꿔도 이 시험이 통과했다
// (§11 은 excel MANUAL·CAPABILITIES 에 산다). 인용이 풀린다고 보고하면서 실제로는 다른 문서를
// 짚은 것이라, 이 시험이 잡으라고 있는 그 무늬 그대로다.
//
// 이름이 같은 문서가 여럿인 것은 흠이 아니다 — `DESIGN.md` 는 저장소에 여럿이고, 주석은 제
// 옆의 것을 뜻한다. 그래서 **이름이 맞는 문서 중 하나라도** 그 절을 가지면 통과다. 어느
// 디렉토리의 것인지까지 따지려면 인용이 경로를 대야 하고, 그것은 이 시험이 정할 규약이 아니다.
//
// 이름을 안 댄 122 개는 번호만으로 묻는다. 그것이 그 인용에 대해 물을 수 있는 가장 센 물음이다.
func TestEverySectionCitedFromSourceExists(t *testing.T) {
	root := repoRootDir(t)

	// 문서별로 제목을 든다. 「어디에든 있나」와 「그 문서에 있나」를 둘 다 물어야 하므로
	// 합집합만으로는 모자란다.
	heading := regexp.MustCompile(`^#{1,6}\s+(\d+(?:\.\d+)*)[.\s]`)
	byDoc := map[string]map[string]bool{} // 문서 경로 -> 절 집합
	anywhere := map[string]bool{}
	docs := 0
	walk(t, root, ".md", func(p string, body []byte) {
		docs++
		set := map[string]bool{}
		for _, line := range strings.Split(string(body), "\n") {
			if m := heading.FindStringSubmatch(line); m != nil {
				set[m[1]] = true
				anywhere[m[1]] = true
			}
		}
		byDoc[p] = set
	})
	if docs < 20 || len(anywhere) < 40 {
		t.Fatalf("문서 %d 개에서 번호 붙은 제목을 %d 개밖에 못 찾았다 — 스캔이 깨진 것이지 "+
			"인용이 맞는 게 아니다", docs, len(anywhere))
	}

	// 인용. 앞의 30자를 같이 잡아 문서 이름이 붙었는지 본다.
	//
	// 시험 파일은 뺀다. 이 파일의 설명문이 바로 그 번호들을 **예시로** 적고 있고, 그것까지 세면
	// 시험이 저를 물어 이유를 적어 두는 일을 벌한다 — wave 25 의 grep 가드가 같은 함정을 밟았다.
	cite := regexp.MustCompile(`(.{0,30})§(\d+(?:\.\d+)*)`)
	named := regexp.MustCompile(`([A-Z][A-Za-z_]*)(?:\.md)?\s*$`)
	type use struct{ doc, sec, file string }
	var uses []use
	walk(t, root, ".go", func(p string, body []byte) {
		if strings.HasSuffix(p, "_test.go") {
			return
		}
		for _, m := range cite.FindAllStringSubmatch(string(body), -1) {
			u := use{sec: m[2], file: p}
			if d := named.FindStringSubmatch(m[1]); d != nil {
				u.doc = d[1]
			}
			uses = append(uses, u)
		}
	})
	if len(uses) < 100 {
		t.Fatalf("소스에서 절 인용을 %d 개밖에 못 찾았다 — 스캔이 깨졌다", len(uses))
	}

	bad := map[string]bool{}
	strong := 0
	for _, u := range uses {
		if u.doc == "" {
			if !anywhere[u.sec] {
				bad["§"+u.sec+" ("+u.file+") — 그런 제목이 어느 문서에도 없다"] = true
			}
			continue
		}
		strong++
		// 이름이 맞는 문서 중 하나라도 그 절을 가지면 된다. 같은 이름의 문서가 여럿인 것은
		// 이 나무의 보통 모양이고(DESIGN.md 는 여럿이다), 주석은 제 옆의 것을 뜻한다.
		found, exists := false, false
		for p, secs := range byDoc {
			base := filepath.Base(p)
			base = strings.TrimSuffix(strings.TrimSuffix(base, ".md"), ".ko")
			if base != u.doc {
				continue
			}
			exists = true
			if secs[u.sec] {
				found = true
				break
			}
		}
		switch {
		case !exists:
			bad[u.doc+" §"+u.sec+" ("+u.file+") — 그 이름의 문서가 없다"] = true
		case !found:
			bad[u.doc+" §"+u.sec+" ("+u.file+") — 그 문서에 그 절이 없다"] = true
		}
	}

	var list []string
	for b := range bad {
		list = append(list, b)
	}
	sort.Strings(list)
	for _, b := range list {
		t.Errorf("인용이 풀리지 않는다: %s", b)
	}
	t.Logf("인용 %d 개를 견줬다 — 문서를 댄 %d 개는 그 문서에서, 나머지는 번호로. "+
		"문서 %d 개, 제목 %d 개.", len(uses), strong, docs, len(anywhere))
}

// walk hands every file under root with the given suffix to fn, skipping what is not ours.
func walk(t *testing.T, root, suffix string, fn func(path string, body []byte)) {
	t.Helper()
	skip := map[string]bool{"node_modules": true, ".git": true, "out": true, "build": true, "bin": true, "obj": true}
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if skip[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, suffix) {
			return nil
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		rel, _ := filepath.Rel(root, p)
		fn(filepath.ToSlash(rel), b)
		return nil
	})
	if err != nil {
		t.Fatalf("%s 를 훑다 실패: %v", root, err)
	}
}

// repoRootDir walks up to the directory holding go.mod.
func repoRootDir(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, serr := os.Stat(filepath.Join(dir, "go.mod")); serr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod 를 못 찾았다 — 저장소 뿌리를 모르면 이 시험은 아무것도 안 훑는다")
		}
		dir = parent
	}
}
