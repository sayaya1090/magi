package idebridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"testing"
)

// The row vocabulary and the row shape are checked against the TypeScript copy, both ways.
//
// This is the same guard TestBothCopiesSpeakOneVocabulary gives the activity words, aimed at the
// thing that has already gone wrong twice as badly. The two existing copies disagree today about
// what a row IS — TypeScript has eight kinds and Kotlin six, and the same image attachment is a
// picture on one screen and an info line on the other — so a third copy written without a guard
// would not drift LATER, it would land divergent.
//
// The TypeScript file is the one read because it is the one this package's vocabulary was decided
// from (docs/IDE_BRIDGE §3, 2026-09-10: eight, not six). When the editor moves onto the bridge this
// guard is what says the move was complete; until then it is what says the two agree.

func transcriptTS(t *testing.T) string {
	t.Helper()
	path := filepath.Join("..", "..", "..", "clients", "vscode", "src", "core", "transcript.ts")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("타입스크립트 사본을 못 읽었다 — 옮겨졌으면 이 시험부터 고칠 것: %v", err)
	}
	return string(body)
}

// The union `export type Who = 'a' | 'b' | …` is read as a set of words.
func TestTheRowVocabularyMatchesTheTypeScriptCopy(t *testing.T) {
	body := transcriptTS(t)
	decl := regexp.MustCompile(`(?m)^export type Who\s*=\s*([^;]+);`).FindStringSubmatch(body)
	if decl == nil {
		t.Fatal("transcript.ts 에서 `export type Who` 를 못 찾았다 — 스캔이 깨진 것이지 어휘가 맞는 게 아니다")
	}
	got := map[string]bool{}
	for _, m := range regexp.MustCompile(`'([a-z-]+)'`).FindAllStringSubmatch(decl[1], -1) {
		got[m[1]] = true
	}
	// A scan that found nothing agrees with everything.
	if len(got) < 3 {
		t.Fatalf("타입스크립트에서 낱말을 %d 개밖에 못 찾았다 — 스캔이 깨졌다", len(got))
	}

	mine := map[string]bool{}
	for _, w := range Vocabulary() {
		mine[w] = true
	}
	for w := range mine {
		if !got[w] {
			t.Errorf("이 패키지는 행 종류 %q 를 말하는데 타입스크립트 사본에는 없다", w)
		}
	}
	for w := range got {
		if !mine[w] {
			t.Errorf("타입스크립트가 행 종류 %q 를 말하는데 Vocabulary() 에 없다 — 그 행은 여기서 못 지어진다", w)
		}
	}
}

// Vocabulary() must name every constant the package declares, or `about`-style advertisement lies.
func TestTheVocabularyListNamesEveryWordTheRowsUse(t *testing.T) {
	declared := []string{WhoUser, WhoAgent, WhoTool, WhoThinking, WhoCouncil, WhoSystem, WhoError, WhoImage}
	got := append([]string(nil), Vocabulary()...)
	want := append([]string(nil), declared...)
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Vocabulary() = %v, 선언된 낱말은 %v — 목록과 상수가 갈리면 광고가 거짓말이 된다", got, want)
	}
	seen := map[string]bool{}
	for _, w := range Vocabulary() {
		if seen[w] {
			t.Errorf("Vocabulary() 에 %q 가 두 번 있다", w)
		}
		seen[w] = true
	}
}

// The wire NAMES of the row fields, both ways.
//
// A field this side spells differently is not an error on either end: encoding/json writes the key
// nobody reads and the screen draws a blank, which is the same silent-default trap the wire tests
// exist for one level down.
func TestTheRowFieldsMatchTheTypeScriptCopy(t *testing.T) {
	body := transcriptTS(t)
	m := regexp.MustCompile(`(?ms)^export interface Row \{(.*?)^\}`).FindStringSubmatch(body)
	if m == nil {
		t.Fatal("transcript.ts 에서 `export interface Row` 를 못 찾았다 — 스캔이 깨진 것이지 모양이 맞는 게 아니다")
	}
	// Declaration lines only: `  name?: type;`. Prose above them talks about these names, so the
	// shape of the declaration is what is read — the same reason the activity guard stopped
	// scanning whole files.
	got := map[string]bool{}
	for _, f := range regexp.MustCompile(`(?m)^  ([a-zA-Z_][a-zA-Z0-9_]*)\??:`).FindAllStringSubmatch(m[1], -1) {
		got[f[1]] = true
	}
	if len(got) < 5 {
		t.Fatalf("타입스크립트 Row 에서 필드를 %d 개밖에 못 찾았다 — 스캔이 깨졌다", len(got))
	}

	mine := map[string]bool{}
	rt := reflect.TypeOf(Row{})
	for i := 0; i < rt.NumField(); i++ {
		tag := rt.Field(i).Tag.Get("json")
		if tag == "" || tag == "-" {
			t.Errorf("Row.%s 에 json 태그가 없다", rt.Field(i).Name)
			continue
		}
		name := tag
		if c := regexp.MustCompile(`^([^,]*)`).FindStringSubmatch(tag); c != nil {
			name = c[1]
		}
		mine[name] = true
	}

	for f := range mine {
		if !got[f] {
			t.Errorf("Row 가 %q 를 싣는데 타입스크립트 사본에는 그 필드가 없다", f)
		}
	}
	for f := range got {
		if !mine[f] {
			t.Errorf("타입스크립트 Row 에 %q 가 있는데 이쪽 Row 에는 없다 — 그 사실은 편집기까지 못 간다", f)
		}
	}
}

// Absent and zero are different facts on this struct, and omitempty is what keeps them apart.
//
// Ok absent is "still running" where false is "it failed"; Confidence absent is "the member said
// nothing about it" where 0 would read as a member sure of the opposite. Both are pointers for
// that reason, and a plain bool/float64 here would quietly start reporting the zero as a fact.
func TestAnEmptyRowCarriesOnlyWhatWasSaid(t *testing.T) {
	line, err := json.Marshal(Row{Seq: 8, Who: WhoAgent, Text: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	if err := json.Unmarshal(line, &back); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"seq", "who", "text"} {
		if _, ok := back[k]; !ok {
			t.Errorf("%q 가 빠졌다: %s", k, line)
		}
	}
	if len(back) != 3 {
		t.Errorf("아무도 말하지 않은 것이 실렸다: %s", line)
	}

	no := false
	if line, err = json.Marshal(Row{Seq: 9, Who: WhoTool, Text: "bash", Ok: &no}); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(line, &back); err != nil {
		t.Fatal(err)
	}
	if v, ok := back["ok"]; !ok || v != false {
		t.Errorf("실패한 도구 호출의 ok=false 가 사라졌다 — 도는 중과 구별이 안 된다: %s", line)
	}
}
