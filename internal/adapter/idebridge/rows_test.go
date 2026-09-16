package idebridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
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

// transcriptKT is the Kotlin copy's enum, read the same way and for the same reason.
//
// ⚠ **This copy was OUTSIDE the guard while the guard existed**, and it was the copy the decision
// was made about: Go and TypeScript held each other to eight while Kotlin quietly kept six, with one
// `Info` doing the work of three. A guard that covers two of three copies is a guard whose whole
// subject can drift.
func transcriptKT(t *testing.T) string {
	t.Helper()
	path := filepath.Join("..", "..", "..", "clients", "jetbrains", "plugin", "core", "src", "main",
		"kotlin", "dev", "sayaya", "magi", "ide", "usecase", "Rows.kt")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("코틀린 사본을 못 읽었다 — 옮겨졌으면 이 시험부터 고칠 것: %v", err)
	}
	return string(body)
}

// `enum class Who { … }` is read as a set of words, lower-cased to meet the wire's spelling.
func TestTheRowVocabularyMatchesTheKotlinCopy(t *testing.T) {
	body := transcriptKT(t)
	decl := regexp.MustCompile(`enum class Who \{([^}]+)\}`).FindStringSubmatch(body)
	if decl == nil {
		t.Fatal("Rows.kt 에서 `enum class Who` 를 못 찾았다 — 스캔이 깨진 것이지 어휘가 맞는 게 아니다")
	}
	got := map[string]bool{}
	for _, w := range strings.Split(decl[1], ",") {
		if n := strings.ToLower(strings.TrimSpace(w)); n != "" {
			got[n] = true
		}
	}
	if len(got) < 3 {
		t.Fatalf("코틀린에서 낱말을 %d 개밖에 못 찾았다 — 스캔이 깨졌다", len(got))
	}
	mine := map[string]bool{}
	for _, w := range Vocabulary() {
		mine[w] = true
	}
	for w := range mine {
		if !got[w] {
			t.Errorf("코틀린 사본에 `%s` 가 없다 — 한 사실을 두 낱말로 적으면 안 재지는 쪽이 갈린다", w)
		}
	}
	for w := range got {
		if !mine[w] {
			t.Errorf("코틀린 사본에만 `%s` 가 있다 — 이쪽이 모르는 낱말은 화면에 안 그려진다", w)
		}
	}
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
	// The constants are READ from rows.go, not restated here. Restating them made this a check of
	// one hand-written list against another: a Who constant added to the file and forgotten in
	// Vocabulary() was absent from BOTH sides and so agreed perfectly. See declaredWords.
	declared := []string{}
	for name, v := range declaredWords(t, "rows.go") {
		if strings.HasPrefix(name, "Who") {
			declared = append(declared, v)
		}
	}
	if len(declared) < 5 {
		t.Fatalf("rows.go 에서 Who 상수를 %d 개밖에 못 찾았다 — 스캔이 깨졌다", len(declared))
	}
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
	t.Logf("선언된 낱말 %d 개를 Vocabulary() 와 견줬다", len(declared))
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

	// 이 문에만 있는 칸. **사유와 함께 적고, 사유가 낡으면 운다** — 젯브레인 쪽 와이어 가드가 쓰는
	// 그 모양이다(선언만 해 두고 아무도 안 채우면 그 사본은 늘 빈 칸을 나른다).
	goOnly := map[string]string{
		"id": "이 문의 칸이다. 뒤의 프레임이 고칠 행을 지목할 이름 — 접기가 뒤로 손을 뻗는 것을 " +
			"전선으로 나르려면 필요하고, 한 프로세스 안에서는 포인터가 그 일을 한다. 이쪽 사본은 " +
			"제 셰이퍼가 제 목록을 들고 있어 이름이 필요 없다 — 문으로 옮겨 가는 날 필요해진다.",
		"at": "시간. **타입스크립트 사본이 안 나르는 사실이고, 코틀린 사본은 모든 행에 나른다** — " +
			"그래서 이 칸은 이 문이 지어낸 것이 아니라 세 사본 중 둘이 빠뜨린 것이다. 이 사본이 " +
			"문으로 옮겨 오는 날 그리기 시작하면 이 줄을 지운다.",
		"evidence": "멤버들이 판단 근거로 받은 것 전부(task·plan·report·actions·changes). 코틀린 " +
			"사본은 이것을 나르고 타입스크립트 사본은 안 나른다 — 같은 이유로 여기 적는다.",
		"summary": "이 문의 칸이다. 본문을 통째로 나르기로 하면서(전문이 정본) 한 줄 요약이 갈 곳이 " +
			"필요해졌고, 얕은 클라이언트(슬라이드 애드인·상태 줄)가 그것을 읽는다. 이쪽 사본의 " +
			"셰이퍼는 본문을 직접 그리므로 채울 이유가 없다 — 이 사본이 문으로 옮겨 가는 날 " +
			"셰이퍼가 사라지고 이 줄도 사라진다.",
	}
	for f := range mine {
		if got[f] {
			continue
		}
		if why, ok := goOnly[f]; ok {
			if why == "" {
				t.Errorf("%q 가 사유 없이 면제돼 있다", f)
			}
			continue
		}
		t.Errorf("Row 가 %q 를 싣는데 타입스크립트 사본에는 그 필드가 없다", f)
	}
	// ★ **면제도 늙는다.** 사본이 그 칸을 선언하기 시작하면 이 줄은 거짓이 되고, 무엇보다 그때부터
	// **두 사본이 갈려도 아무도 안 운다** — 면제가 조용히 면제를 넓히는 것이다.
	for f := range goOnly {
		if got[f] {
			t.Errorf("%q 가 이제 타입스크립트 사본에도 있다 — 면제를 지울 것(면제가 남으면 그 칸의 드리프트를 아무도 안 잡는다)", f)
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

// **And the Kotlin copy's fields, which nothing was comparing.**
//
// ⚠ **The field guard above reads two of the three copies.** It holds this Row against the TypeScript
// one both ways, and that is exactly half the job: the Kotlin shaper carries facts neither of the
// others does, so a fact this door drops is invisible — TypeScript does not carry it either, and the
// guard sees two copies agreeing. Measured 2026-09-14, moving a client onto this door was about to
// lose the event TIME, which the Kotlin copy puts on every row and nothing here had a place for.
//
// The asymmetry is not itself a defect — three shapers are being replaced by one, and a client shaper
// may legitimately hold something the door does not. What must not happen is that nobody KNOWS.
func TestTheRowFieldsMatchTheKotlinCopy(t *testing.T) {
	body := transcriptKT(t)
	m := regexp.MustCompile(`(?ms)^data class Row\((.*?)^\)`).FindStringSubmatch(body)
	if m == nil {
		t.Fatal("Rows.kt 에서 `data class Row(` 를 못 찾았다 — 스캔이 깨진 것이지 모양이 맞는 게 아니다")
	}
	// Declaration lines only (`    val name: Type…`). The doc comments between them quote these very
	// names, which is why this reads declarations rather than text — the same reason the TypeScript
	// scan above matches on the shape of a declaration.
	got := map[string]bool{}
	for _, f := range regexp.MustCompile(`(?m)^    val ([a-zA-Z_][a-zA-Z0-9_]*)\s*:`).FindAllStringSubmatch(m[1], -1) {
		got[f[1]] = true
	}
	if len(got) < 10 {
		t.Fatalf("코틀린 Row 에서 필드를 %d 개밖에 못 찾았다 — 스캔이 깨졌다", len(got))
	}

	mine := map[string]bool{}
	rt := reflect.TypeOf(Row{})
	for i := 0; i < rt.NumField(); i++ {
		name := strings.Split(rt.Field(i).Tag.Get("json"), ",")[0]
		if name != "" && name != "-" {
			mine[name] = true
		}
	}

	// 이 문에만 있는 칸, 사유와 함께. 위 표와 같은 규칙이다.
	doorOnly := map[string]string{
		"id": "뒤의 프레임이 고칠 행을 지목할 이름. 이쪽 셰이퍼는 제 목록을 들고 있어 필요 없다 — " +
			"문으로 옮겨 가는 날 필요해진다.",
		"summary": "얕은 클라이언트가 읽는 한 줄. 이 창은 본문을 마크다운으로 그리므로 채울 이유가 없다.",
		"folded": "기본으로 접을지 — 이 문이 정하고 클라이언트가 따르는 결정. 이쪽 셰이퍼는 그리는 " +
			"코드가 종류를 보고 스스로 정한다(생각·도구 본문).",
		"seq": "행을 만든 사건. 이쪽 셰이퍼는 목록의 자리로 같은 일을 한다.",
		"readOnly": "라운드가 판단하는 턴이 파일을 안 고쳤다는 사실. 이쪽 사본은 그것을 " +
			"`evidence` 문장 안에 넣는다 — 같은 사실, 다른 모양.",
		"fileNav":  "도구 계약에서 추출한 구조화된 파일·줄 이동 정보. VS Code 클라이언트에서 먼저 도입되었으며 젯브레인 변경은 이번 범위에서 제외됐다.",
		"rawArgs":  "도구 호출의 전체 원문 인자. 요약(args)과 분리하여 웹뷰에서 상세 펼치기를 제공한다.",
		"outputId": "확정된 답변이나 도구 결과의 읽기 전용 가상 문서 식별자. VS Code 클라이언트에서 먼저 도입되었으며 젯브레인 변경은 이번 범위에서 제외됐다.",
	}
	// 이쪽 사본에만 있는 칸도 사유와 함께. **이 절반이 없어서 시간이 사라질 뻔했다.**
	copyOnly := map[string]string{
		"tool": "도구 이름을 따로 든다. 이 문은 그것을 `text` 에 싣는다(도구 행의 본문이 이름이다) — " +
			"사실은 가 있고 모양만 다르다.",
		"why": "카운슬의 결론 행에서 `continue` 를 붙들고 있는 반대. 이 문은 그것을 결론 행의 " +
			"`text` 에 이어 붙인다 — 사실은 가 있고, 따로 뽑아 그릴지는 화면의 결정이다.",
	}
	for f := range mine {
		if got[f] {
			continue
		}
		if why, ok := doorOnly[f]; ok {
			if strings.TrimSpace(why) == "" {
				t.Errorf("%q 가 사유 없이 면제돼 있다", f)
			}
			continue
		}
		t.Errorf("이 문이 %q 를 싣는데 코틀린 사본에는 그 필드가 없다", f)
	}
	for f := range got {
		if mine[f] {
			continue
		}
		if why, ok := copyOnly[f]; ok {
			if strings.TrimSpace(why) == "" {
				t.Errorf("코틀린 사본이 %q 를 싣는데 이 문에는 없고, 사유도 없다 — 이 창을 문으로 "+
					"옮기면 그 사실이 사라진다", f)
			}
			continue
		}
		t.Errorf("코틀린 사본이 %q 를 싣는데 이 문에는 그 칸이 없다 — 이 창을 문으로 옮기면 "+
			"그 사실이 사라진다", f)
	}
	// ★ 면제도 늙는다, 양쪽 다.
	for f := range doorOnly {
		if got[f] {
			t.Errorf("%q 가 이제 코틀린 사본에도 있다 — 면제를 지울 것", f)
		}
	}
	for f := range copyOnly {
		if mine[f] {
			t.Errorf("%q 가 이제 이 문에도 있다 — 면제를 지울 것", f)
		}
	}
}
