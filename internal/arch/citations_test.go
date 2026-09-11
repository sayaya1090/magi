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
// 번호만 본다 — 어느 문서의 §5.5 인지까지는 묻지 않는다. 주석이 문서 이름을 같이 적는 경우가
// 드물고, 그것을 요구하면 이 시험이 재는 것이 「인용이 풀리는가」에서 「인용을 어떻게 적는가」로
// 바뀐다. 뒤엣것은 이 시험이 정할 일이 아니다.
func TestEverySectionCitedFromSourceExists(t *testing.T) {
	root := repoRootDir(t)

	// 모든 마크다운의 번호 붙은 제목.
	heading := regexp.MustCompile(`^#{1,6}\s+(\d+(?:\.\d+)*)[.\s]`)
	have := map[string]bool{}
	docs := 0
	walk(t, root, ".md", func(_ string, body []byte) {
		docs++
		for _, line := range strings.Split(string(body), "\n") {
			if m := heading.FindStringSubmatch(line); m != nil {
				have[m[1]] = true
			}
		}
	})
	if docs < 20 || len(have) < 40 {
		t.Fatalf("문서 %d 개에서 번호 붙은 제목을 %d 개밖에 못 찾았다 — 스캔이 깨진 것이지 "+
			"인용이 맞는 게 아니다", docs, len(have))
	}

	// 소스가 쓰는 인용.
	cite := regexp.MustCompile(`§(\d+(?:\.\d+)*)`)
	where := map[string]map[string]bool{}
	// 시험 파일은 뺀다. 이 파일의 설명문이 바로 그 두 번호를 **예시로** 적고 있고, 그것까지
	// 세면 시험이 저를 물어 이유를 적어 두는 일을 벌한다 — wave 25 의 grep 가드가 같은 함정을
	// 밟았다. 산문은 인용의 사용이 아니다.
	walk(t, root, ".go", func(path string, body []byte) {
		if strings.HasSuffix(path, "_test.go") {
			return
		}
		for _, m := range cite.FindAllStringSubmatch(string(body), -1) {
			if where[m[1]] == nil {
				where[m[1]] = map[string]bool{}
			}
			where[m[1]][path] = true
		}
	})
	if len(where) < 30 {
		t.Fatalf("소스에서 절 인용을 %d 개밖에 못 찾았다 — 스캔이 깨졌다", len(where))
	}

	var bad []string
	for sec := range where {
		if !have[sec] {
			files := make([]string, 0, len(where[sec]))
			for f := range where[sec] {
				files = append(files, f)
			}
			sort.Strings(files)
			bad = append(bad, "§"+sec+" ("+strings.Join(files, ", ")+")")
		}
	}
	sort.Strings(bad)
	for _, b := range bad {
		t.Errorf("이 절을 가리키는 주석이 있는데 그런 제목이 어느 문서에도 없다: %s", b)
	}
	t.Logf("소스가 인용하는 절 %d 개를 문서 %d 개의 제목 %d 개와 견줬다", len(where), docs, len(have))
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
