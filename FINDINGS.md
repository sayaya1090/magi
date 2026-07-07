# magi 셀프리뷰 발견사항

사용자 부재 중 자율 감사. 각 항목: **심각도**, 구체적 **증거**(file:line + 검증 방법 — 검증 안 된 주장 금지), **상태**(이번 세션 수정 / 미해결 / 개선), 그리고 제안 수정안.

원칙: 코드를 읽고, 저렴한 경우 프로브나 테스트로 직접 검증한 항목만 기재한다. 여기 있는 것 중 추측은 없다. 라이브 모델 E2E는 의도적으로 회피했다 — Ollama가 떠 있고 벤치와 충돌할 수 있기 때문(E2E는 동시 실행 시 행에 걸림).

범례: 🔴 버그(잘못된 동작) · 🟡 잠재/엣지 · 🟢 개선/다듬기.

---

## 이번 세션 수정 완료

### 🔴 F1 — magi 종료 시 background/`&` 프로세스가 함께 죽음 (서버 띄우고 채점하는 벤치 실패)
- **위치:** `internal/adapter/tool/builtin/bgproc.go`(경로 A, `background=true`) 및 `bash.go`(경로 B, 동기 `server &`).
- **근본 원인(추적 + 재현):** 자식의 stdout/stderr가 `*os.File`이 아닌 writer에 연결되어 os/exec가 내부적으로 `os.Pipe`를 삽입한다. magi가 종료하면 read-end가 닫히고 자식은 다음 write에서 SIGPIPE로 죽는다. 경로 B의 `CombinedOutput`은 하나의 파이프를 공유하고 `&` 자식이 이를 상속하므로, `WaitDelay`가 이를 닫으면 약 2초 내에 자식이 죽는다.
- **수정:** 두 경로 모두 이제 결합 출력을 실제 임시 파일(`os.CreateTemp`)에 캡처하고, 백그라운드 프로세스는 `Setsid` 아래에서 실행된다(자체 세션/프로세스그룹, controlling tty 없음). `bash_kill`은 이제 프로세스 그룹 전체에 시그널을 보낸다(`killGroup`, `Kill(-pid, SIGKILL)`); TUI는 인터랙티브 종료 시 남은 bg 프로세스를 정리(`KillBackgroundProcesses`)하는 반면, 헤드리스 `-p`는 채점을 위해 띄운 서버를 의도적으로 생존시킨다.
- **검증:** `TestBashBackgroundChildSurvives`(실제 Bash 도구: `(sleep 0.4; touch marker) &`가 즉시 반환되고 이후 marker가 나타남 — 옛 파이프 커플링에서는 실패했을 것), `TestBackgroundDetachesSession`(자식 pgid ≠ magi pgid; 세션 리더 pgid==pid). darwin+linux에서 gofmt/vet/build clean; windows 비테스트 빌드 clean.

### 🔴 F2 — bash_output이 대용량 백그라운드 출력을 조용히 건너뜀 (30KB–256KB 버스트 유실)
- **위치:** `internal/adapter/tool/builtin/bgproc.go`, `BashOutput.Execute` + `readLogSince`.
- **버그:** `readLogSince`는 `maxBgBuf`(당시 256KB)까지 읽고 소비 오프셋을 반환한 바이트만큼 정확히 전진시켰지만, `BashOutput`은 그 텍스트를 `truncateOut`(30KB 표시 캡)에 통과시켰다. 그래서 30KB보다 큰 버스트에서 오프셋은 약 256KB 점프하는데 첫 30KB만 표시됨 — 30KB–256KB 중간이 소비되고 절대 노출되지 않았다.(옛 `syncBuffer` 경로도 동일한 잠재 불일치가 있었다.)
- **수정:** `maxBgBuf = 30KB`(== `truncateOut` 캡)로 설정해 read-advance == 표시 바이트가 되도록 함; 큰 버스트는 이제 연속된 `bash_output` 호출로 페이징된다. 풀캡 read 시 `…(more output buffered — call bash_output again)` 힌트 추가.
- **검증:** 구성상의 추론 + 기존 bgproc 테스트 여전히 green.(전용 대용량 버스트 페이징 테스트 추가 가치 있음 — I3 참조.)

### 🟢 F3 — 클릭 가능한 컨트롤이 색상 텍스트와 시각적으로 구분됨
- **위치:** `internal/adapter/tui/styles.go`, `model_view.go`, `toolbody.go`, `render.go`.
- **변경:** `‹ back` 브레드크럼이 평범한 강조-볼드 텍스트여서 클릭 불가 상태 칩(`◈ plan:`, `⚖ council`)과 동일했다. 브레드크럼에 `styleClickable`(채워진 강조 알약), 트랜스크립트 내 펼침/접힘 토글(추론 블록 및 넘치는 도구 호출 본문)에 `styleFoldChip`(저강조 컨테이너 알약)을 추가. 모달 Save/Submit는 이미 채워진 알약을 썼고, 이제 모든 클릭 가능 요소가 버튼으로 읽히는 반면 상태/키보드 힌트 텍스트는 평평하게 유지된다.
- **검증:** tui 테스트 green(테스트 고정 부분문자열 `ctrl+t` / `+N more lines` / `collapse` 보존; 접힌 추론은 한 줄 유지).

### 🔴 O1 — 터미널이 2칸으로 그리는 장식 글리프의 폭 오산 (Windows, 사용자 보고) — 이번 세션 수정
- **위치:** `internal/adapter/tui/width.go`(`cellWidth`/`ambiguousExtra`) + `render.go`/`model_view.go`의 글리프(`✻ ✦ ⚖ ⇅ ‹ ›`).
- **증거(측정, go-runewidth + x/ansi 대상 scratchpad 프로브):**
  | 글리프 | ansi.StringWidth | narrowRW | wideRW | ambiguous? |
  |---|---|---|---|---|
  | `‹` `›` (브레드크럼) | 1 | 1 | 1 | **아니오** |
  | `✦` (브랜드) | 1 | 1 | 1 | **아니오** |
  | `✻` (thought) | 1 | 1 | 1 | **아니오** |
  | `⚖` (council) | 1 | 1 | 1 | **아니오** |
  | `⇅` (스크롤 미터) | 1 | 1 | 1 | **아니오** |
  | `·` `…` `◈` `⛐` `★` `│` | 1 | 1 | 2 | 예 |
- **깨지는 이유:** `ambiguousExtra` 보정은 East-Asian *ambiguous*(narrow 1 / wide 2)로 분류된 룬에만 발동한다. `✻ ✦ ⚖ ⇅` 같은 글리프는 ambiguous가 **아니어서** — 어디서나 1로 측정됨 — 그런데도 Windows Terminal은 이들 중 여럿을 2칸 폭으로 렌더링한다. 이를 보정할 수단이 없어, 그런 글리프를 담은 줄(예: 접힌 추론 줄 `✻ thought · …`)은 글리프당 1칸씩 부족하게 측정됨 → `padOrTruncate`가 덜 패딩/덜 자름 → 사용자가 본 정렬 깨짐. 시작 시 프로브는 `│`(ambiguous임)를 쓰므로 이 별개 클래스를 감지할 수 없다.
- **수정(적용):** 시작 시 CPR(커서 위치 보고) 프로브를 추가해, magi가 실제 그리는 6개 장식 글리프 각각을 실제 터미널에 출력하고 커서가 몇 칸 전진했는지 측정한다(`probe_unix.go`의 `probeDecorWidths`; Windows는 `probe_windows.go`에서 Console API `GetConsoleScreenBufferInfo`의 커서-X 델타 사용). 2칸으로 그려지는 글리프는 `decorWide` 오버라이드 맵에 등록되고, `cellWidth`가 ambiguous-클래스 프로브와 **독립적으로** 이를 반영한다. macOS/Linux는 1로 측정 → 보정 없음 → 회귀 제로; Windows는 2로 측정 → 보정됨. 수동 오버라이드/검증용 `MAGI_DECOR_WIDTH=wide|narrow` 환경변수 추가.
- **검증:** `TestCellWidthDecorWide` — ambiguous 보정만으로는 이 6개 글리프가 움직이지 않음을 확인(narrow=1/ambWide=1, 클래스 갭 자체를 증명)하고, decor 오버라이드가 켜지면 2칸 글리프마다 정확히 1칸씩 더해짐을 검증(`‹ back`→1, `✦ magi ⚖`→2, `‹›✦✻⚖⇅`→6, `·—→`→0). gofmt/build/vet/`GOOS=windows` 크로스빌드/tui 테스트 모두 green.

### 🟡 O2 — 백그라운드 로그 파일이 디스크에서 무한히 증가 — 이번 세션 수정
- **위치:** `internal/adapter/tool/builtin/bgproc.go`, `start()` / `readLogSince` / `pruneLocked`.
- **증거:** 프로세스별 임시 로그(`magi-bg-*.log`)는 자식이 append만 할 뿐, 잘라내거나 로테이트하는 게 없다. 프로세스가 pruned(레지스트리 > 완료 32개)되거나 kill될 때만 삭제된다. 장수하는 수다스러운 서버 하나(요청마다 로깅하는 dev 서버)가 세션 수명 내내 `/tmp`에 무한히 쓴다. `maxBgBuf`는 *읽기*를 캡할 뿐 파일 크기가 아니다.
- **영향:** 긴 인터랙티브 세션 / 긴 벤치 컨테이너에서 느린 디스크 채움. podman 디스크 채움 위양성 이력(메모리 `bench-env-podman-disk` 참조)을 감안하면 경계할 가치가 있다.
- **수정(적용):** 로그를 8MiB(`hardLogCap`)로 캡. 로그 파일을 `O_APPEND`로 열어(비-append fd를 제자리 truncate하면 자식의 오래된 오프셋에 NUL 스파스 구멍이 생김 — append면 truncate 후 자식의 다음 write가 EOF=0으로 seek해 깔끔히 재시작), `bash_output` 처리 시 `rotateIfHuge`가 크기가 캡을 넘으면 `os.Truncate(path,0)` + 리더 오프셋 리셋을 수행하고 `…(log exceeded 8 MiB — rotated, older output dropped)` 알림을 1회 붙인다.
- **검증:** `TestRotateIfHuge`(캡 초과 시 truncate→크기 0, 리더 오프셋 0으로 되감김; 캡 미만은 no-op). builtin 테스트 green.

### 🟡 O3 — `runCapture` 임시 파일이 Windows에서 잔존할 수 있음 — 이번 세션 수정(시작 시 청소)
- **위치:** `internal/adapter/tool/builtin/bash.go`, `runCapture`(`defer os.Remove(name)`).
- **가설(추적):** POSIX에서는 생존한 `&` 자식이 아직 연 파일을 unlink해도 무해(write는 unlink된 inode로 감). Windows에서는 라이브 핸들이 있는 파일 삭제가 모든 핸들이 `FILE_SHARE_DELETE`로 열렸는지에 의존 — 상속된 자식 핸들이 remove를 막아 임시 파일을 누출할 수 있다. 실무상 `&` 백그라운드 구문은 PowerShell이 아니므로 노출은 좁지만, `Start-Process`류 detach는 걸릴 수 있다.
- **수정(적용):** 정상 경로의 `defer os.Remove`에 더해, 시작 시 `SweepStaleTempLogs`가 temp 디렉터리에서 magi 자신의 잔존 로그(`magi-bg-*.log`, `magi-bash-*.log`) 중 24h 경과분만 회수한다(`cmd/magi/main.go` 시작부에서 1회 호출). 연령 게이트라 생존한 헤드리스 서버가 아직 쓰는 로그는 절대 건드리지 않는다 — Windows 누출과 헤드리스 고아 로그를 함께 안전하게 처리한다.
- **검증:** `TestSweepStaleTempLogs`(연령 초과 magi 로그만 삭제; 신선한 magi 로그와 무관 파일은 보존). darwin+linux에서 확인; Windows 실호스트 최종 확인은 V3와 함께 남김.

### ✅ O4 — `clipLine`이 셀 폭이 아닌 룬 수로 도구 본문을 잘라 CJK 언더클립 — 이번 세션 수정
- **위치:** `internal/adapter/tui/toolbody.go`, `clipLine`(`if r := []rune(s); len(r) > width`). 모든 본문 렌더러(bash/read/grep/glob/list/text)가 사용.
- **증거(코드):** 여기서 `width`는 **셀** 예산(`m.bodyWidth()-2`)인데, 가드는 이를 `len([]rune(s))` — **룬** 수 — 와 비교한다. CJK/wide 40자 줄은 40룬이지만 약 80셀; `40 > width(60)`은 false라 `clipLine`은 그 줄을 **자르지 않고 `…`도 없이** 반환한다. 리포에는 이미 올바른 측정(`width.go`의 `cellWidth`/`padOrTruncate`)이 있어 이 호출 지점은 나머지 레이아웃과 불일치했다.
- **보통은 레이아웃을 *깨지 않는* 이유:** 최종 `composeBox` 패스가 모든 조합 줄을 `padOrTruncate`(셀 정확)에 통과시켜, 과폭 본문 줄은 래핑 대신 뷰포트 가장자리에서 하드컷된다. **하지만** 그 컷은 `…` 없이, `clipLine`이 고르지 않은 위치에서 일어난다 — 한글/CJK 도구 출력(한글 파일 `read`, 한글 `grep` 히트)이 잘림 표시 없이 끝부분을 급작스레 잃는다. 좁은 창에서는 눈에 띄는 오절단.
- **관련성:** O1과 동일한 wide-글리프 측정 계열이며, 사용자 본인의 (한글) 콘텐츠에 직접 영향.
- **수정(적용):** `clipLine`이 이제 `cellWidth`로 측정하고 `ansi.Truncate(s, width-1, "") + "…"`로 자름(`padOrTruncate`를 반영), `[]rune` 슬라이스 대신. `toolbody.go`에 `x/ansi` import 추가.
- **검증:** 새 `TestClipLine` CJK 케이스 — `clipLine("가"×40, 20)`가 ≤ 20셀로 측정되고 `…`로 끝남; 예산 내 CJK 줄은 변경 없이 반환; 기존 ASCII(`a`×100→10룬) 및 탭 확장 단언 여전히 통과. `go build ./...`, `go vet`, `MAGI_E2E_OLLAMA_BASE=disabled go test ./internal/adapter/tui/...` 모두 green; gofmt clean.

### 🔴 O6 — `retractProgress`가 `progressSinceNudge`를 안 지워 진동이 D18a stall-converge 수렴을 회피 — 런타임 트레이스로 발견·수정
- **위치:** `internal/app/guard.go`, `retractProgress`(옛 260-265) vs `mutated`(250) / `shouldNudge`(673) 수렴 조건.
- **증거(런타임 트레이스, `internal/app/guard_trace_test.go` — 라이브 모델 없이 loop 사용 패턴대로 guard 구동):** 순수 no-progress(converge ON)는 **호출 24**에서 force-stop하는데, implement↔revert 진동(A→B→A→B, converge ON)은 **호출 50**에서야 stop — 정확히 2배로 수렴이 안 걸림.
  ```
  순수:  [step23] since=24 lastStallAt=12 stallNudges=1  >>> stop="stall"   (호출 24)
  진동(수정 전): stallNudges 1→2→3 전부 발화, [step49] stop="stall"        (호출 50)
  ```
- **근본 원인:** 진동의 매 swing마다 `mutated()`가 `progressSinceNudge=true`로 설정(250). `retractProgress()`는 self-revert가 churn임을 알고 `sinceProgress`/`lastStallAt`는 되돌리지만 `progressSinceNudge`는 안 건드렸다. 그래서 re-arm 지점마다 `g.stallConverge && stallNudges>=1 && !progressSinceNudge`의 `!progressSinceNudge`가 false → collapse가 스킵됨. 되돌린 mutation을 "전진"으로 카운트해, 수렴 가속기가 하필 그것이 가장 값진 churn 케이스에서 무력화됐다. (stall 자체는 retractProgress의 `sinceProgress` 복원 덕에 여전히 착지하므로 치명적 무한루프는 아니고, "수렴이 안 됨" 결함.)
- **수정(적용):** `retractProgress`에 `g.progressSinceNudge = false` 한 줄 추가 — 되돌린 mutation은 전진이 아니므로 re-arm collapse를 막지 않는다.
- **검증(재트레이스):** 진동이 이제 **호출 26**에서 stop(순수 24와 동일 스케줄, 첫 swing이 revert가 아니라 +2). `TestGuardTraceOscillation`에 `calls > 32면 실패` 회귀 경계 추가. `internal/app` 전체 테스트 green.

### 🟡 O5 — `read`가 파일 전체를 캡 없이 반환(기본 줄 제한 없음, 크기 가드 없음) — 이번 세션 수정
- **위치:** `internal/adapter/tool/builtin/read.go` — 옛 코드는 `offset>0 || limit>0`일 때만 슬라이스; 그 외엔 파일 전체 반환. `os.ReadFile(abs)`가 `info.Size()` 확인 없이 파일 전체를 메모리에 로드(이미 `os.Stat`의 `info`를 손에 쥐고 있었는데도).
- **증거(코드 + 도구 간 비교):** *다른 모든* 빌트인은 출력을 캡한다 — `astgrep`(첫 50매치), `webfetch`(`s[:max]+"…(truncated)"`), `findcontext`(`info.Size() <= 1<<20` 가드), `lspnav`(`truncateOut`). `read`만 없었다: 맨 `read {"path":"big.log"}`(제한 없음)가 모든 줄에 번호를 매겨 반환. 그래서 수 MB 로그나 큰 생성 파일이 (a) RAM에 완전 버퍼링되고 (b) 모델 컨텍스트/트랜스크립트에 그대로 덤프됨 — 다른 도구들이 막는 바로 그 컨텍스트-플러드 + OOM 계열.
- **영향:** 제한 없이 큰 파일을 읽는 약한 모델이 한 호출로 컨텍스트 창을 날릴 수 있음(및 비용), 또는 거대 파일에서 프로세스 메모리 급증. 나머지 도구 세트와 에이전트 read 도구의 관례(기본 ~2000줄 창)와 불일치.
- **수정(적용):** (1) `os.ReadFile`을 `readCapped`(첫 10MiB만 `io.LimitReader`로 읽고 초과 시 truncation 노트)로 교체해 메모리 바운드; (2) `limit`이 명시되지 않았을 때 기본 줄 캡(2000) 적용, 재개 지점을 알려주는 `…(N more lines — read with offset=X to continue)` 푸터를 붙임. 명시 `limit`은 호출자의 선택이므로 그 경우 푸터를 붙이지 않음.
- **검증:** `TestReadDefaultLineCap`(2500줄 파일의 맨 read가 정확히 2000줄 + "500 more lines" + `offset=2001` 푸터 반환), `TestReadSmallFileNoFooter`(캡 이내 파일은 전체 반환, 헛푸터 없음), `TestReadExplicitLimitNoFooter`(명시 limit엔 기본-캡 푸터 없음). 기존 read 테스트(`TestRead`/`TestReadLocatesByBasename`/`TestReadOffsetPastEOF` 등) 여전히 green. gofmt/vet/build clean.

---

## 개선 아이디어 (I1~I4 전부 반영 완료)

- 🟢 **I1 — 동기 bash 전체 출력 버퍼링 캡 (반영).** `runCapture`가 로그 전체를 `os.ReadFile`하던 것을 `readHeadTail(name, captureCap=256KiB)`로 교체 — 수백 MB를 뿜는 명령(`cat huge`, 폭주 빌드)도 head+tail만 유지하고 중간은 `…[N bytes omitted]…`로 생략해 메모리를 고정 바운드. 아울러 `truncateOut`을 head-only(첫 30KB)에서 **head+tail(¾/¼)**로 개선 — 빌드/테스트 실패의 실제 에러·최종 상태는 대개 끝에 있으므로, head-only 절단은 바로 그 유용한 부분을 버렸다. 룬 경계 절단으로 항상 유효 UTF-8. **검증:** `TestTruncateOutHeadTail`(끝의 `FATAL…` 보존 + 마커 + 유효 UTF-8), `TestReadHeadTailBounds`(4MiB 파일이 ~cap로 바운드; cap 이내는 전체 반환).
- 🟢 **I2 — bg 로그 로테이션 (반영).** O2 수정으로 반영됨(`hardLogCap`/`rotateIfHuge`, `TestRotateIfHuge`).
- 🟢 **I3 — bash_output 대용량 버스트 페이징 테스트 (반영).** `TestBashOutputPagesLargeBurst`: 100KB 버스트를 로그에 쓰고 `readLogSince`로 드레인해 바이트 단위 재구성 + 오프셋이 반환 바이트만큼만 전진(무-점프) + 한 read가 `maxBgBuf` 초과 안 함을 단언 — F2(read-advance == 표시 바이트)를 직접 고정.
- 🟢 **I4 — 폭 오버라이드 (반영).** O1 수정으로 반영됨: 하드코딩 테이블 대신 시작 시 CPR 실측 프로브(`probeDecorWidths`)로 구현해 어떤 터미널에서도 correct-by-measurement, 이식성이 높다. `MAGI_DECOR_WIDTH`로 수동 오버라이드.

---

## 라이브 검증 필요 (벤치가 안 도는 때 수행 — 동시 실행 시 Ollama E2E 행)

- **V1 — 실제 서버 태스크에서 F1 엔드투엔드.** Terminal-Bench "서버 띄우고 채점" 태스크를 수정된 바이너리로 헤드리스 실행; 띄운 서버가 magi 종료를 넘어 verifier 단계까지 생존하는지 확인. 수정 전/후 대조.
- **V2 — 인터랙티브 정리.** TUI에서 `background=true` dev 서버를 띄우고 magi 종료, `KillBackgroundProcesses`가 회수했는지(고아 없음) 확인 — 그리고 *헤드리스* 실행은 살려두는지 확인.
- **V3 — Windows Terminal에서 O1/O3.** 접힌 추론 줄(`✻ thought …`)과 헤더(`✦ magi … ⚖ council`)를 Windows Terminal에 렌더; 실제 컬럼 드리프트가 CPR 프로브로 0이 되는지 검증. O3의 `magi-bash-*.log` 잔존/청소도 실호스트에서 확인.

---

## 감사 커버리지 (이번 세션 검토 범위)

- **검토 후 견고 판정(발견 없음):** `pathutil.go`(workdir jail: 어휘적 + `EvalSymlinks` 조상 확인, WalkDir는 심링크 미추적 — 견고), `edit.go`(정확→EOL→공백관용→근접; 빈-`old`/후행개행 엣지 안전), `multiedit.go`(진짜 원자적: 인메모리 적용, 단일 write, 빠른 실패), `write.go`(jailed, 부모 생성, 설계상 덮어쓰기), `width.go`/`padOrTruncate`(셀 정확 최종 조합).
- **검토 후 발견 산출:** `bgproc.go`/`bash.go`(F1, F2, O2, O3, I1), `styles.go`/`toolbody.go`/`render.go`/`model_view.go`(F3, O1, O4), `read.go`(O5), `guard.go`/`loop.go`(O6).
- **런타임 감사 완료(벤치 미실행 확인 후):** `internal/app/loop.go` + `guard.go`의 stall-가드 / step-budget / re-arm 로직을, 라이브 모델 없이 guard 상태기계를 loop 사용 패턴대로 결정론적으로 구동하는 트레이스 하니스(`guard_trace_test.go`)로 감사했다. 5개 시나리오(순수 no-progress converge on/off, 조기 mutation 후 stall, implement↔revert 진동, 다양 파일-authoring)를 실행해 상태 전이를 라인 단위로 관찰 — 이 과정에서 **O6**(진동이 수렴 회피)을 실증·수정. 나머지 4개 시나리오는 설계대로 동작 확인(순수 no-progress는 호출 24에 착지; converge OFF는 48; 조기 mutation은 창을 올바로 재시작 후 착지; 다양 파일-authoring은 by-design으로 stall 안 함 — 리뷰 노트로 기록). council 게이트 런타임 감사는 다음 대상.

---

_방법 노트: 위 발견은 코드 검증(읽기 + scratchpad 폭 프로브 + green 단위 테스트)에 기반한다. 공유 Ollama의 벤치 충돌을 피하려 밤새 라이브 모델 실행은 하지 않았다. 런타임 증거가 필요한 항목은 사실로 단정하지 않고 "라이브 검증 필요"에 격리했다._
