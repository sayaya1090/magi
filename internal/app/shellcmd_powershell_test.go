package app

import (
	"strings"
	"testing"
)

// **이 분류기들이 윈도우에서는 한 마디도 못 알아듣고 있었다.**
//
// 이 나무의 `bash` 도구는 윈도우에서 `powershell -NoProfile -Command` 이고(`builtin.Shell`), 시스템
// 프롬프트가 모델에게 그렇게 말한다 — 그래서 모델은 `Copy-Item`·`Set-Content`·`Remove-Item` 을 쓴다.
// 그 낱말이 어느 표에도 없었으므로:
//
//   - `mutatesFiles` 가 늘 거짓이었다 → **변이 에포크가 안 올라간다**. 그 결과는 `fileMutateVerbs` 의
//     주석이 이미 적어 둔 것이고(고칠 때마다 진전이 없다고 읽혀 세 번째 빌드가 막힌다), 윈도우에서는
//     그것이 예외가 아니라 **정상 경로**였다.
//   - `bashWritePaths` 가 빈 목록을 돌려줬다 → 내용 비교가 아예 안 돈다. 되돌리기를 알아보는 검사가
//     이 플랫폼에서 죽어 있었다.
//
// 실측 2026-09-13: `Copy-Item heap.c heap.c.bak` 로 `mutationEpoch()` 가 0.
//
// 이 시험은 **순수 텍스트 판정**이라 어느 플랫폼에서나 돈다 — 그것이 표를 `runtime.GOOS` 로 가르지
// 않은 이유이기도 하다.

func TestPowerShellFileWritesRegisterAsMutations(t *testing.T) {
	for _, cmd := range []string{
		"Copy-Item heap.c heap.c.bak",
		"copy-item heap.c heap.c.bak", // PowerShell 은 대소문자를 안 가린다
		"COPY-ITEM heap.c heap.c.bak",
		"Move-Item a.txt b.txt",
		"Remove-Item -Recurse -Force _build",
		"Set-Content f.txt 'A'",
		"(Get-Content f.txt) -replace 'A','B' | Set-Content f.txt",
		"Add-Content log.txt 'line'",
		"Out-File -FilePath out.txt -InputObject 'x'",
		"New-Item -ItemType File x.txt",
		"del old.txt",
		"xcopy src dst /E",
	} {
		if !mutatesFiles(cmd) {
			t.Errorf("파일을 쓰는데 변이로 안 읽힌다: %q", cmd)
		}
	}
	// 반대쪽: 묻기만 하는 것은 변이가 아니다. 이것이 없으면 위 목록을 아무 낱말이나 넣어 통과시킬 수 있고,
	// 그러면 순수한 조회가 진전 창을 새로 열어 준다(`queryingSubcommands` 가 같은 이유로 있다).
	for _, cmd := range []string{
		"Get-Content f.txt",
		"Get-ChildItem -Recurse",
		"Get-Item x.txt | Select-Object Length",
		"Test-Path _build",
	} {
		if mutatesFiles(cmd) {
			t.Errorf("묻기만 하는데 변이로 읽힌다: %q", cmd)
		}
	}
}

func TestPowerShellWritePathsNameTheFileAndNothingElse(t *testing.T) {
	for _, c := range []struct {
		cmd  string
		want []string
	}{
		// 값은 경로가 아니다. `Set-Content f.txt 'A'` 의 둘째 조각은 **쓰이는 내용**이므로, 그것을
		// 경로로 집으면 내용 비교가 `'A'` 라는 파일을 읽는다.
		{"Set-Content f.txt 'A'", []string{"f.txt"}},
		{"(Get-Content f.txt) -replace 'A','B' | Set-Content f.txt", []string{"f.txt"}},
		{"Set-Content -Value 'A' -Path f.txt", []string{"f.txt"}},
		{"Out-File -FilePath out.txt -InputObject 'x'", []string{"out.txt"}},
		{"Add-Content -Path log.txt -Value 'line'", []string{"log.txt"}},
		// 목적지는 둘째 피연산자. 원본은 여기 오지 않는다(그쪽은 bashMoveSources 의 질문이다).
		{"Copy-Item heap.c heap.c.bak", []string{"heap.c.bak"}},
		{"Move-Item -Destination b.txt -Path a.txt", []string{"b.txt"}},
		// ⚠ **매개변수의 값을 경로로 읽지 않는다.** 이것이 이 파일에서 가장 값이 큰 한 줄이다:
		// `-ErrorAction SilentlyContinue` 를 피연산자로 읽으면 `SilentlyContinue` 라는 파일의 내용을
		// 견주게 되고, 그건 「엉뚱한 파일을 비교한다」 — `bashWritePaths` 가 스스로 금지한 것이다.
		{"Remove-Item -Recurse -Force -ErrorAction SilentlyContinue _build", []string{"_build"}},
		{"Remove-Item -Path stale.txt", []string{"stale.txt"}},
		// 디렉터리는 견줄 내용이 없다 — POSIX 쪽에 `mkdir` 이 없는 것과 같은 이유로 아무것도 안 낸다.
		{"New-Item -ItemType Directory build", nil},
		// 여러 원본을 한 디렉터리로: 파일별 목적지를 알 수 없으므로 짐작하지 않는다.
		{"Copy-Item a.txt b.txt dst", nil},
		// 묻기만 하는 것은 쓴 것이 없다.
		{"Get-Content f.txt", nil},
	} {
		got := bashWritePaths(c.cmd)
		if strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Errorf("bashWritePaths(%q) = %v, want %v", c.cmd, got, c.want)
		}
	}
}

func TestPowerShellMoveSourcesNameWhatIsNowMissing(t *testing.T) {
	for _, c := range []struct {
		cmd  string
		want []string
	}{
		{"Move-Item a.txt b.txt", []string{"a.txt"}},
		{"Move-Item -Path a.txt -Destination b.txt", []string{"a.txt"}},
		{"Remove-Item -Force stale.txt", []string{"stale.txt"}},
		{"Remove-Item -Recurse -Force -ErrorAction SilentlyContinue _build", []string{"_build"}},
		// 복사는 원본을 가져가지 않는다.
		{"Copy-Item a.txt b.txt", nil},
		{"Get-ChildItem", nil},
	} {
		got := bashMoveSources(c.cmd)
		if strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Errorf("bashMoveSources(%q) = %v, want %v", c.cmd, got, c.want)
		}
	}
}
