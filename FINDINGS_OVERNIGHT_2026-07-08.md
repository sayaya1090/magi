# 야간 자동 버그헌트 — 2026-07-08 (세션 2)

방식: 마기의 실제 코드경로를 복잡/엣지 입력으로 구동하는 결정론적 프로브를
"벤치마크처럼" 웨이브로 돌리고, **매 웨이브마다 실행 로그를 줄단위로 분석**해서
(1) 확정 발견, (2) 정상 확인(회귀감시 가치), (3) **하네스 자체 점검·개선**,
(4) 다음 웨이브 새 아이디어를 기록한다. 모든 주장은 실제 stdout 라인을 인용한다
(read-logs-not-summaries). LLM 없이 결정론적으로 재현 가능한 표면 우선.

프로브 하네스: `internal/adapter/tool/builtin/probe_overnight_test.go`
(빌드 비파괴, `-run TestOvernightProbe -v`; 커밋 대상 아님 — 조사용).

---

## 진행 로그 (웨이브 카운터)

- **Wave 1 (Family A–G, ~90 케이스, probeA.log):** 경로탈옥(A)·read(B)·write(C)·
  edit/multiedit(D)·grep(E)·glob(F)·bash(G). 결과: 툴 레이어는 이미 N-시리즈
  감사로 경화됨 → 신규 발견은 경미(O1, O2). 대량의 **정상 확인**이 회귀감시 가치.
- **Wave 2 (CLI/헤드리스 플래그, cli_probe.sh):** 바이너리 실행으로 플래그 검증·종료코드.
  발견: **O5(프로파일 미검증 — 안전 footgun)**, O6(테마 미검증). N1(output/permission→exit2)·
  N3(빈 프롬프트→exit2) 유효. **하네스 버그 1건 발견·수정**(zsh 종료코드 캡처). 부수확인:
  로컬 Ollama 가동 중(`list-models`→qwen3.6:27b) → 향후 라이브 헤드리스 프로브 가능.
- **Wave 10 (파일/검색 툴 엣지케이스 → 경로 감옥, 🔴 보안 클러스터):** `read` 견고성 확인 후
  `resolvePath`/`withinSymlinkJail` 감사 → **O14**(깨진 심링크 fail-open 쓰기 이스케이프) →
  같은 클래스를 "파일 내용을 읽는 walk 표면"으로 확장해 **O15**(grep 내용 유출)·**O16**(findcontext
  스니펫 유출) 연쇄 발견, 모두 결정론 프로브로 실제 유출 입증 후 수정·테스트·커밋(b987ca5/a0e7fcc/
  d062d07, 🔴×3) + **N12** 가드 공통 헬퍼화(5b87512). **하네스 교훈:** (1) 가장 많이 쓰는 툴
  (read)이 견고해도 그 *공통 하위 유틸(resolvePath)*이 진짜 위험면 — 툴 표면이 아니라 공유 헬퍼를
  판다. (2) 실제 os 콜(Lstat/EvalSymlinks/Readlink) 시맨틱 추적이 핵심(EvalSymlinks가 깨진 링크에
  err 반환→fail-open이 뿌리). (3) 한 결함(감옥 우회 심링크)을 이름 붙이면 read→write(O14)→grep
  (O15)→findcontext(O16)로 표면을 체계적으로 훑어 한 웨이브에 동일계열 3건을 쓸어담음 — O8→O13
  렌더 계열과 같은 "클래스 사냥" 패턴이 두 번째로 크게 적중.
- **Wave 9 (O8 형제 감사 — cellWidth/StringWidth 혼용 전수):** O8이 드러낸 취약계열을 따라
  렌더 경로를 훑어 **O13 확정**(검색 하이라이트 우측 밀림, 결정론 프로브로 "or he"≠"error" 입증)
  → 수정·테스트·커밋(e4c1fc9). 부수: `truncate`(cmd/magi)는 이미 rune-boundary 안전(테스트 있음,
  회귀 positive), `highlightSelection`은 StringWidth 일관(정상). **하네스 교훈:** 한 버그의
  *결함 클래스*(척도 혼용)를 이름 붙여 형제 경로를 전수 감사하면 동일계열이 연쇄 발견됨 —
  O8→O9→O13. 남은 width-인자 경로(codediff/councilMemberPlain)도 같은 렌즈로 후속 감사.
- **Wave 8 (헤드리스 이벤트 스트림 계약, fake-provider):** `runHeadless`가 TurnFinished/Error
  없이 스트림이 닫히면 exit 0을 낸다는 fake 프로브 관찰 → **트레이스로 반증**: `Subscribe`
  고루틴은 `ctx.Done()` 또는 `live` 채널 close시에만 out을 닫는데, (a) 헤드리스 ctx는
  `context.Background()`라 Done()이 nil(발화 안 함), (b) `live`는 bus의 `cancel()`로만 닫히고
  그 cancel은 runHeadless가 **defer**로 리턴 시에만 호출 → 루프 도중엔 안 닫힘. 결론:
  **현 배선에선 도달 불가**(비버그). **하네스 자가점검 성과:** fake가 의심 신호를 줬지만 실배선
  트레이스가 오탐임을 확인 — 프로브를 삭제(오탐을 SUSPECT로 남기지 않음), 수정도 커밋도 안 함.
  → O12(아래) 방어 아이디어로만 기록. **교훈:** fake의 행동≠프로덕션 배선; "이 경로가 실제로
  어떤 ctx/채널 수명으로 도달되나"를 소스로 끝까지 봐야 함(벤치 스나이핑 방지 원칙과 동일).
- **Wave 7 (bgproc kill-race N11 회귀테스트):** 기존 TestBackgroundKill은 kill 반환만 검증,
  N11의 핵심(kill 직후 bash_output이 stale "running" 아닌 terminal 상태여야 함)은 미커버 →
  `TestBackgroundKillStatusRace` 추가(무sleep, ×80 + -race clean). **하네스 자가점검 성과:** 첫
  단언이 과도(killed|done만 허용)했는데 ×50 반복이 `exited -1`도 정당한 terminal임을 즉시 노출 →
  단언을 "running만 금지"로 정정. msg-4 지침(하네스 자체 검증)이 실제로 잘못된 테스트를 잡아냄.
  버그 신규발견 아님(N11 방어 견고 확인) + 회귀 잠금.
- **Wave 6 (guardrail 값 검증, applyProfile 트레이스):** O5잔여를 끝까지 추적 →
  `applyProfile` default가 미인식 profile을 무구속 처리함을 소스로 확정 → **O11 수정**
  (`validateGuardrailValues` 하드페일) + E2E로 `.magi` 오타값 exit 2 확인. **하네스 교훈:**
  "3층 폐쇄" 패턴 정립 — 안전설정은 (1)플래그 (2)config 키 (3)config 값 세 입력면을 모두 막아야
  하나라도 뚫리면 무의미. 향후 새 enum 설정 추가 시 이 3층 체크리스트를 기본 적용.
- **Wave 5 (config/TOML 로딩, probe_unknown):** 오타 키 프로브 → **O10 확정**(`toml.Unmarshal`이
  미지 키 조용히 폐기, `Load`도 글로벌 에러 폐기). 수정: `LoadWithUnknown`+`md.Undecoded()`+stderr
  경고(하드에러 아님, forward-compat). free-form 섹션 오탐 없음 확인. **하네스 교훈:** O5(플래그)를
  고친 뒤 "같은 결함의 다른 입력 경로(config층)"를 의식적으로 따라가 O10을 찾음 — 수정마다 *동형
  결함의 인접 표면*을 체크리스트化할 것(플래그→env→config→값검증 순).
- **Wave 4 (TUI 폭/클립 렌더, probe_clip/probe_ambig):** clipLine 불변식
  `cellWidth(clipLine(s,w)) <= w`를 15입력×10폭(CJK/emoji/ZWJ/국기/combining/ANSI/control)
  검증 → 기본폭 0위반(견고). 이후 **가설검증**으로 `setAmbiguousWide(true)` + ambiguous 글리프
  프로브 → **O8 실버그 확정**(28/28 위반, 최대 100% 오버플로우). 수정 후 재프로브 0위반, 프로브를
  정식 회귀테스트(TestClipLineAmbiguousWide)로 승격하고 조사용 프로브 삭제. **하네스 교훈:**
  "기본설정 0위반"에서 멈추지 말고 **그 방어가 존재하는 이유(비기본 모드)를 강제 활성화**해야
  실제 결함이 드러난다 — 프로브는 항상 "이 코드가 왜 있나?"의 전제조건을 켜고 돌릴 것.
- **Wave 3-live (`-output json` 스키마 + N13, json_out.txt):** 라이브 헤드리스 1회.
  `-output json`은 **유효 JSONL**(44라인, bad 0, 전부 type+seq 보유)로 확인. 발견: O7(seq
  비단조 — 설계상 transient=0, 스키마 개선 아이디어). **하네스 이슈 2건**: (a) `echo|python`
  라인검증이 라인 왜곡→JSON 위양성, (b) N13 마커검사가 에이전트의 명령 인용에 속음(위양성).
  → 순수 Python 검증·이벤트기반 검사로 교체 필요.

---

## 확정 발견 (세션 2)

### O1 🟢 `edit`에 빈 `old` → "occurs 23 times"라는 혼란스러운 에러
증거(probeA.log:45): `edit{old:"", new:"X"}` →
`not unique: old string occurs 23 times — add surrounding context…`.
빈 문자열은 모든 위치(문자 사이)에 매치되어 "23번 발생"으로 보고된다. 에이전트가
실수로 `old:""`를 보내면 "컨텍스트를 더 붙이라"는 **엉뚱한** 지시를 받는다.
성격: N7(명확한 에러 메시지) 계열의 UX 결함. **✅ 수정완료:** edit.go/multiedit.go에서
`old==""` 선행 거부(`old string must not be empty (use write…)`). 테스트: `edit_empty_old_test.go`
(TestEditEmptyOldRejected, TestMultiEditEmptyOldRejected). 검증: 프로브 재실행 시
`isErr=true :: old string must not be empty` 확인.

### O2 🟢 `glob` 브레이스 `{a,b}` 패턴이 조용히 무매치
증거(probeA.log:69,76): `**/*.{go,txt}` → `[]`, `{a,b}.go` → `[]`. glob 라이브러리는
문자클래스 `[gG]`·`**`는 지원하나 브레이스 확장은 미지원 → 구문상 유효해서 N4의
"invalid pattern" 에러도 안 나고 **조용히 빈 결과**. 셸 스타일 브레이스를 쓰는
모델은 힌트 없이 0건을 받는다. 성격: N4 계열(조용한 무매치). 제안: 패턴에 미지원
브레이스가 있으면 결과가 비었을 때 노트(예: `note: {..} brace expansion is not
supported; list alternatives separately`) 또는 브레이스 확장 지원. (glob.go) — 경미.

### O5 🟡 `-profile <오타>` 조용히 수용 → 안전 자세(posture) 무음 저하 (safety footgun)
증거: `-profile bogus -list-models` → **exit 0**, 에러 없이 모델 목록 출력(오타 수용).
코드: main.go의 선행 enum 검증 블록은 `-output`·`-permission`·`-theme`만 다루고
**`-profile`은 검증하지 않음**(main.go:99–115). 그리고 `applyProfile`의 `default` 케이스는
알수없는 값에 **아무것도 안 함**(config.go:308–313) → sandbox 미설정(unconfined) + permission
과거기본으로 조용히 진행. 즉 사용자가 `-profile safe`(read-only)를 `safmode`처럼 오타내면
경고 없이 **비확정(unconfined) 자세**로 에이전트가 돈다. output/permission은 N1에서 loud-fail로
고쳤는데 **profile은 누락**됨. 성격: N1 계열 + 안전 관련(가장 중요). **✅ 수정완료:**
main.go의 인라인 4개 switch를 순수 헬퍼 `validateEnumFlags(output,permission,profile,theme)`로
추출하고 `-profile`(safe|standard|yolo, 빈값 허용) 검증 추가 → 오타 시 `exit 2`로 loud-fail.
테스트: `cmd/magi/validateflags_test.go`(TestValidateEnumFlags, O5/O6/N1 전부 커버).
검증: `-profile bogus -list-models` → `exit 2: invalid -profile "bogus"`.
잔여(후속): config/project TOML에서 온 profile 오타는 아직 `applyProfile` default가 조용히 무시
→ 로드시 경고 추가 여지(Wave 5에서 다룸).

### O6 🟢 `-theme <오타>` 조용히 auto로 폴백 (미검증, 화면색만 영향)
증거: `-theme neon -list-models` → exit 0, 무에러. 코드(main.go:118–126): `switch *theme`의
`default`가 `HasDarkBackground()` 자동감지로 폴백 → `auto|dark|light` 광고와 달리 아무 값이나 수용.
성격: N1 계열이나 **영향은 색상뿐**(경미). N1 수정 주석조차 output/permission만 예로 듦 → theme 누락.
**✅ 수정완료:** O5과 함께 `validateEnumFlags`에 theme(auto|dark|light) 검증 추가 → `exit 2`.
검증: `-theme neon -list-models` → `exit 2: invalid -theme "neon"`. — 낮음/nit.

### O7 ⚪→💡 `-output json` 스트림의 `seq`가 비단조 (설계상 transient=0) — 스키마 명료성 개선 아이디어
증거(json_out.txt): 44 이벤트, 전부 유효 JSON·type·seq 보유. 단 seq 범위 (0,8)로 **비단조**.
원인: `event.go:113–114` — Seq는 Store append시 부여되는 **세션당 단조** 번호이고 **transient
(bus-only) 이벤트는 Seq==0**. 스트림의 `part.delta` 34개가 transient → seq 0으로 섞임. 즉
**버그 아님**(문서화된 설계). 다만 JSONL 소비자는 seq를 전역 정렬/중복제거 키로 오인하기 쉽다
(seq==0이 "첫 이벤트"인지 "transient"인지 필드만으론 구분 불가). 💡 개선 아이디어: transient
이벤트에 명시 플래그(`transient:true`) 부여 또는 seq 생략, 문서에 "정렬은 도착순, seq는 영속
이벤트에만 의미"를 명시. — 개선(낮음), 회귀 아님.

---

## 정상 확인 (⚪, 회귀 감시 가치 — Wave 1)

- **경로 탈옥 전량 차단, 유출 0 (A, probeA.log:3–19):** `../`·절대경로·`in.txt/../../`·
  심링크 파일(`escape_link`)·심링크 디렉터리(`parent_link/…`) 전부 `outside workdir`로
  거부. 실제 워크디렉터리 바깥에 `TOPSECRET` 파일을 심어두고 검사 → **유출/외부쓰기 0건**.
  pathutil의 심링크 자일(withinSymlinkJail)이 실효.
- **read 캡 견고 (B):** `maxReadBytes=10MiB` 바이트 캡 + `defaultReadLines=2000` 기본창.
  음수 offset(-5)→선두로 클램프(:30), 음수 limit(-1)→기본창(:32), offset 초과(99999)→
  **명확한 노트**(:29, N6 수정 유효). 컨텍스트 폭주 없음.
- **read 바이너리 차단 (B:26):** NUL 포함 파일 → `binary file: bin.dat` 에러.
- **write 상대경로 에러 (C:36):** 디렉터리에 write → `open adir: is a directory`
  (절대 임시경로 노출 없음 — N8 수정 유효).
- **edit CRLF/후행공백 관용 (D:49,50):** `matched ignoring line endings` /
  `ignoring trailing whitespace` — 관용 매칭 동작.
- **multiedit 원자성 (D:54):** 2번째 훅 실패 시 1번째도 롤백(atomic?=true) — 부분적용 없음.
- **grep 빈결과 `[]` (E:62,65):** null 아님 (N9 유효). 유효하지 않은 정규식은 에러(:59).
- **bash 캡·타임아웃·cd 비영속 (G):** `truncateOut`+`captureCap`로 대량출력 캡(:85),
  `sleep 5`+timeout1→`[timed out after 1s]`(:84), `cd /tmp` 후 다음 호출은 원 워크디렉터리(:82).
- **N11 킬-레이스 수정 확인(코드):** bgproc.go에 `killed bool` 필드 → kill 직후 `[id killed]`
  표기(bgproc.go:97,301–303). Wave에서 타이밍 재현 테스트는 미실시(→ 후속 아이디어).

---

## 하네스 점검 (Wave 1)

**올바르게 작동?** 예. 자일 프로브는 실제 워크디렉터리 바깥에 진짜 파일을 만들고
결과에 sentinel(`TOPSECRET`/`root:`)이 새는지 검사 → 진짜 유출이면 잡혔을 것.
multiedit 원자성은 파일을 되읽어 `one` 부재로 확인 → 간접이지만 유효.

**개선점(다음 웨이브에 반영):**
1. `oneline()`이 140자에서 잘라 bash 대량출력의 실제 캡 여부를 로그만으로 못 봄
   → 소스를 읽어 확인해야 했다. **모든 패밀리에 `len=` 로깅** 추가(read엔 이미 있음).
2. 자일 유출 판정이 sentinel 2종에만 의존 → 다른 외부파일을 읽는 우회는 못 잡음.
   **고유 sentinel 파일명 + 내용 동시 검사**로 강화.
3. multiedit 원자성은 파일 내용을 정확 비교(exact)로 승격.

**다음 웨이브 새 아이디어(우선순위순):**
- **Wave 2 — CLI/헤드리스(바이너리 exec):** 플래그 파싱(N1 회귀), `-p ""`(N3 회귀),
  `-output json` 스키마 유효성(1이벤트/라인, 에러이벤트 형태), 알수없는 플래그, `--` 종결자,
  `-permission` 값별 동작(N13), 파이프 stdin.
- **Wave 3 — list/astgrep/findcontext/todo:** 경로자일·빈결과·대량·유니코드.
- **Wave 4 — TUI 렌더(별도 pkg):** clipLine 폭/CJK/이모지/결합문자/ANSI(N10 비-shell 경로),
  탭 확장, 모달 폭 래핑(오늘 픽스한 좁은터미널 예약과 연동).
- **Wave 5 — config(.magi/config.toml) 로딩:** 유효하지 않은 TOML, 알수없는 키,
  MAGI_* 환경변수 오버라이드 우선순위.
- **Wave 6 — 동시성/레이스:** 같은 파일 병렬 edit, bgproc kill-후-status 타이밍(N11 재현),
  bgproc 동시 다수.

## 하네스 점검 (Wave 2)

**하네스 버그 발견·수정:** 첫 CLI 프로브가 `PIPESTATUS`(bash 전용)로 종료코드를 읽어
zsh에서 **전부 무음 폴백(head의 exit=0)** → 모든 종료코드가 가짜였음. 소스를 읽어야만
alarm이 걸렸는지 알 수 있었다. **수정:** 파이프 제거 + 출력을 파일로 받아 `$?`로 magi의
실제 종료코드 캡처(cli_probe.sh). 이후 invalid enum→2, version→0 등 정상 관측.
교훈: **프로브가 관측하려는 신호(종료코드)를 프로브 자신이 왜곡하지 않는지 매 웨이브 검증**.

**정렬 함정:** `-p ""`(N3 빈 프롬프트)·잘못된 `-output` 등이 **profile/theme 검증보다 먼저**
발화 → 프로파일 검증 부재를 가리움. 격리하려면 프롬프트·다른 무효플래그를 빼고
`-list-models`(플래그 검증 후 실행, 프롬프트 불요)와 조합해야 신호가 드러남. 하네스 설계
교훈: **한 프로브는 한 변수만** — 다른 조기 종료 경로를 제거해 대상 신호를 고립.

**새 아이디어(라이브 모델 가용으로 추가):** 로컬 Ollama가 응답하므로 짧은 타임아웃의
라이브 헤드리스 프로브가 가능. 단 순차·비동시(E2E 행 위험). 후보: `-output json` 스키마
유효성(1이벤트/라인, 에러이벤트 형태, usage 필드), `-permission auto`가 bash 조용히 거부
(N13 회귀), 툴출력 내 프롬프트-인젝션 취급, 거대/공백/유니코드 프롬프트.

## 하네스 점검 (Wave 3-live)

**하네스 이슈 2건(둘 다 위양성 유발):**
1. **`echo "$line" | python json.loads` 라인검증** — sh의 `echo`가 라인을 왜곡(백슬래시/길이)
   → 유효 JSON을 "NON-JSON"으로 1건 오탐. **권위 있는 검사**는 Python이 파일을 직접
   readline하는 것(0 실패). 교훈: **JSON 검증은 셸 파이프를 끼우지 말고 파서 안에서 끝까지**.
2. **N13 마커검사** — `grep MAGI_MARKER_42`가 에이전트가 **명령 텍스트를 인용**한 것에 속아
   위양성. tail을 보면 실제로는 bash를 못 돌리고 승인 요청 후 council이 UNVERIFIED 처리.
   교훈: "bash가 실제로 실행됐나"는 **프롬프트에 없는·모델이 못 만드는 산출물**(계산값/난스)이나
   **`-output json` 이벤트 스트림의 tool.call+result**로 판정해야 함. → N13 재검은 Wave 6에서
   이벤트기반으로 재수행(deferred).

**정상 확인(⚪):** `-output json`은 유효 JSONL(44/44 파싱, 전 이벤트 type+seq), 이벤트 종류도
풍부·정형(session.created/prompt.submitted/todos.changed/workflow.phase/context.usage/
part.delta/part.appended/turn.finished).

### O8 🔴 `clipLine`이 ambiguous/decor-wide 터미널에서 최대 100% 오버플로우 (실버그, 수정+테스트)
증거(Wave 4b 프로브, `setAmbiguousWide(true)`): `clipLine("★"×40, w)`가 폭 w에 대해
`cellWidth=2w`(over by w)를 반환 — 예: w=20 → "★…"18개 = 40셀. ·, →, — 등 **코드가
"우리 출력에 흩뿌려진다"고 직접 주석 단** East-Asian ambiguous 글리프 전부 해당. 28/28 케이스 위반.
원인: `clipLine`(toolbody.go)의 가드는 `cellWidth`(wide)로 재지만 절단은
`ansi.Truncate(s, width-1)`—즉 `ansi.StringWidth`(narrow) 기준. 두 척도가 어긋나 절단이
과충전. 게다가 말줄임표 `…`(U+2026) 자체가 ambiguous라 wide 터미널에서 2셀 → 예산 계산에서도 누락.
**영향:** width.go 전체(ambiguousWide/decorWide, 스크롤바 정렬용)가 존재하는 바로 그 터미널
(일부 East-Asian 설정·Windows Terminal)에서 절단된 모든 라인이 우측 경계를 넘어 스크롤바/거터가
깨짐. 기본(narrow) 터미널에서는 `cellWidth==StringWidth`라 무증상 → 그래서 여태 미발견.
**✅ 수정완료:** toolbody.go `clipLine`을 셀-정확 절단으로 교체 — 예산=`width-cellWidth("…")`,
`ansi.Truncate` 후 `cellWidth`가 예산 초과면 StringWidth를 1씩 back-off. narrow일 땐 루프 미실행
→ **바이트 동일**(무회귀). 테스트: `toolbody_test.go::TestClipLineAmbiguousWide`(★·→ + 혼합,
7개 폭). **잔여(후속 아이디어):** model_layout.go/model_view.go의 선택·오버레이 경로도 `ansi.Cut`/
`ansi.StringWidth`를 쓰는데 셀-보정과 섞이면 유사 미스매치 가능 → Wave 후속서 별도 감사.

### O9 🟡 마우스 선택 좌표가 ambiguous-wide 터미널에서 어긋남 (추론·미검증, 저severity)
근거(코드 독해, model_layout.go:354–444): `highlightSelection`/`selectedText`/`screenToContent`
는 모두 `ansi.StringWidth`+`ansi.Cut`를 **일관되게** 사용 → 내부적으로 StringWidth 좌표계에서
자기정합적이라 **렌더 오버플로우는 없음**(O8과 달리 척도 혼용 아님). 다만 `screenToContent`가
물리적 화면 셀 x를 그대로 StringWidth 컬럼으로 매핑하므로, ambiguousWide가 켜진 터미널에서 한 줄에
ambiguous 글리프(·★→)가 있으면 물리 셀과 StringWidth 컬럼이 누적만큼 어긋나 **드래그 선택이
엉뚱한 룬을 하이라이트**할 수 있음. **영향:** 크래시·레이아웃 붕괴 없음, 마우스 선택 정밀도만
저하(비기본 터미널 한정). **미검증:** 마우스 이벤트를 결정론적으로 프로브하기 어려워 트레이스 미확보
— **추론 단계**로만 기록. 후속: cellWidth 기반 컬럼을 마우스/선택 경로에 관통시키는 큰 변경이라
별도 태스크로 분리(당장 수정 안 함, 벤치 스나이핑 방지 원칙대로 트레이스 확보 후 진행). 우선순위 낮음.

### O10 🟠 config.toml 오타 키가 조용히 무시됨 (O5의 config층 쌍둥이, 수정+테스트)
증거(Wave 5 프로브): `profil="safe"`, `modle="typo"`, `permision="deny"`를 담은 config →
`Load` err=nil, `Profile=""`(즉 safe 미적용 → 무구속 fallback), `Model`은 원래값 유지. 원인:
`config.Load`가 `toml.Unmarshal` 사용 → 미매칭 키를 알려주는 `MetaData`를 버림. 게다가 main.go의
글로벌 로드는 에러조차 `_`로 폐기. **영향:** O5(플래그)의 config판 — 사용자가 `profile`을 오타내면
가드레일 포스처가 조용히 무구속으로 남음(안전), 그 외 설정도 "왜 안 먹지" 혼란. **✅ 수정완료:**
`LoadWithUnknown(dir)(Config,[]string,error)` 추가 — `toml.Decode`의 `md.Undecoded()`로 미지의
키 수집. `Load`는 이를 감싸 하위호환. main.go가 두 config(글로벌/.magi) 로드 후
`warnUnknownConfigKeys`로 stderr 경고. **하드에러 아님**(신버전 config를 구버전이 읽어도 로드됨,
forward-compat). free-form 섹션(plugins/theme/mcp/routing)은 map으로 소비돼 오탐 없음. 테스트:
`config_test.go::TestLoadWithUnknownKeys`(profil/modle 보고 + theme/plugins 미보고 + 결측파일),
`cmd/magi/validateflags_test.go::TestWarnUnknownConfigKeys`. **잔여(후속):** O5잔여와 합쳐,
로드된 profile/permission/sandbox **값** 자체의 유효성(예: `profile="saef"`는 키는 맞지만 값이
오타)은 아직 미검증 — applyProfile default no-op에 경고 추가가 남은 조각(Wave 6/후속).

### O11 🟠 guardrail 값 오타가 무구속으로 조용히 강등 (O5의 값-side 쌍둥이, 수정+테스트)
증거(Wave 6 트레이스): `applyProfile`(internal/app/config.go:283)의 `default` 케이스가
비어있는 Profile과 **인식불가 Profile("saef")을 동일 취급** → 무구속. CLI 플래그는 O5로
검증되지만 config에서 온 `cfg.Profile`/`cfg.Permission`/`cfg.Sandbox` **값**은 미검증으로
`app.Config`에 유입(main.go:383/381/384). Sandbox 오타("workspace-writ")도 미인식 모드→무샌드박스.
**✅ 수정완료:** main.go에 `validateGuardrailValues(effProfile, perm, sandbox)` 추가 — 병합된
*유효값*의 profile/permission/sandbox를 검증, 미인식이면 **exit 2 하드페일**(안전-critical 닫힌
enum이라 O10의 경고와 달리 하드페일). app.New 직전 호출. 테스트:
`validateflags_test.go::TestValidateGuardrailValues`. **E2E 검증:** `.magi/config.toml`에
`profile="saef"` → `exit 2: invalid profile "saef"`, 동시에 `profil` 오타키는 O10 경고도 발화;
`profile="safe"`는 정상(list-models exit 0). → **O5→O10→O11로 profile-안전 3층(플래그/키/값) 폐쇄.**

### O12 ⚪→💡 헤드리스 종료코드가 "터미널 이벤트 관측"을 보장하지 않음 (현재 비도달, 방어 아이디어)
근거(Wave 8 트레이스): `runHeadless`의 `for e := range sub`는 TurnFinished→exit0, Error→exit1로만
분기하고, **스트림이 그 둘 없이 닫히면 자연히 루프를 빠져나가 exit 0**. 현재는 도달 불가(ctx=
Background, cancel은 defer). **그러나** 향후 헤드리스에 시그널→ctx-cancel 배선(인터랙티브엔 흔한
기능)이 추가되면, Ctrl-C/타임아웃으로 중단된 런이 **성공(exit0)으로 보고**되어 스크립트/벤치가
"완료"와 "중단"을 구분 못 함. 💡 저비용 방어(선제 아님, 배선 추가 시 동반): 루프 진입 전
`sawTerminal:=false`, TurnFinished/Error에서 true, 루프 종료 후 `!sawTerminal`이면 비정상종료로
비-0 반환. **지금은 수정 안 함**(도달 불가 = 트레이스로 증명된 비버그, 게이트-먼저 원칙). 시그널
배선 태스크와 함께 처리하도록 이 노트만 남김.

### O13 🟠 검색 하이라이트가 ambiguous-wide 터미널에서 우측으로 밀림 (O8 동일계열, 수정+테스트+커밋)
증거(Wave 9 프로브, `setAmbiguousWide(true)`): `contentPlain="★★★error here"`, 쿼리 "error" →
하이라이트가 `"error"`가 아니라 `"or he"`를 칠함(★ 3개만큼 우측 이동). 원인:
`highlightSearch`(model_view.go:511–512)가 컷 좌표를 `cellWidth(plain[:start])`로 계산하지만
실제 추출은 `ansi.Cut`(StringWidth 좌표) + `w=ansi.StringWidth(styled)` → **O8와 동일한
cellWidth-vs-StringWidth 혼용**. cellWidth는 ambiguous 룬마다 +1 보정을 넣는데 ansi.Cut은 그걸
모르므로 프리픽스의 ambiguous 룬 수만큼 컷이 우측 이동. **영향:** 비기본(ambiguous-wide) 터미널에서
검색 하이라이트가 엉뚱한 글자에 입혀짐(크래시·행 없음, 시각 오정렬). 기본 터미널은 무증상.
**✅ 수정·커밋(e4c1fc9):** 컷 좌표를 `ansi.StringWidth`로 변경(highlightSelection과 동일 패턴,
기본 케이스 바이트동일). 테스트: `search_test.go::TestSearchHighlightAmbiguousWide`(SGR-span
스캐너 `highlightedSpans`로 칠해진 텍스트가 정확히 "error"인지 검증). **주목:** O8 수정이
"이 계열이 취약하다"는 신호였고, 형제 경로(검색 하이라이트)를 의식적으로 감사해 O13을 찾음 —
O9(마우스 선택)와 함께 cellWidth/StringWidth 혼용이 렌더 전반의 반복 패턴임을 시사. 후속 감사대상 전수 점검 완료:
`codediff.go::highlightDiffLine`은 `lipgloss.Width` 일관 사용(정상), `model.go:594`
`councilMemberPlain` 클릭 히트테스트는 세그먼트 폭·구분자 모두 `ansi.StringWidth` 일관(정상).
→ **O8-계열(cellWidth/StringWidth 혼용) 스위프 종결: 실버그는 clipLine(O8)·검색하이라이트(O13)
두 곳뿐**, 나머지 렌더 경로는 척도 일관. (마우스 선택 O9는 별개 미해결 가설로 잔존.)

### O14 🔴 심링크 감옥 fail-open → 워크디렉터리 밖 파일 생성(샌드박스 이스케이프, 보안-치명, 수정+테스트+커밋)
증거(Wave 10 프로브, 결정론): `work/link` → `outside/escaped.txt`(아직 없음) 심링크 생성 후
`resolvePath(work,"link")`가 **에러 없이 통과**, 이어 `write{path:"link"}` 실행 →
`outside/escaped.txt`가 **워크디렉터리 밖에 실제 생성됨**. 원인: `withinSymlinkJail`
(pathutil.go)가 "가장 깊은 존재 조상"을 찾고 그 조상에 `EvalSymlinks` 실패 시 `return nil`
(통과)로 처리. 깨진 심링크는 `Lstat` 성공(조상으로 선택됨)하지만 대상이 없어 `EvalSymlinks`
실패 → **바로 그 fail-open 분기로 빠져 감옥 우회**. workspace-write 샌드박스에서 순수-Go 파일툴
(write/edit/multiedit)이 셸 없이 밖으로 씀 → 경계 무력화. **영향:** 에이전트가 `ln -s
/etc/cron.d/x work/link` 류로 심링크만 심으면 write 툴로 임의 경로에 파일 생성 가능(샌드박스
탈출). 심각도 최상. **✅ 수정·커밋(b987ca5):** fail-open 제거 — 조상이 해석 불가 심링크면
`os.Readlink`로 대상을 따라가 그 위치를 재귀 감옥검사(체인 대비 depth 40 상한 → 사이클도 종료).
워크디렉터리 안을 가리키는 깨진 심링크(정상 write 경로)는 계속 허용. 테스트:
`pathutil_more_test.go::TestResolvePathRejectsBrokenSymlinkEscape`(밖-이스케이프 거부 +
안-깨진링크 허용 + 사이클 종료 3케이스). **주목:** 이번 세션 유일 🔴(보안). 파일툴이 "순수-Go라
셸 샌드박스와 무관하게 자체 감옥에 의존"하는 구조라 감옥 자체의 fail-open이 곧 경계 붕괴 —
후속: `edit`/`multiedit`/`glob`/`list`가 심링크를 따라가는 다른 표면(디렉터리 순회 중
심링크 경유)도 같은 렌즈로 감사 가치.

### O15 🔴 grep이 워크디렉터리 밖 심링크를 따라가 파일 내용 유출(읽기 이스케이프, O14 형제, 수정+테스트+커밋)
증거(Wave 10 후속 프로브, 결정론): `work/link` → `outside/secret.txt`(내용 `TOPSECRET_TOKEN=abc123`)
심링크 후 `grep{pattern:"TOPSECRET_TOKEN",path:"."}` → 결과 `["link:1:TOPSECRET_TOKEN=abc123"]`
**밖 파일 내용 유출**. 원인: grep은 `WalkDir`로 순회(심링크 디렉터리는 안 내려가지만 심링크
**파일**은 엔트리로 넘어옴). 콜백이 `d.IsDir()`만 걸러 심링크 파일은 통과 → `os.ReadFile(p)`가
심링크를 **따라가** 밖 파일을 읽음. read/write/edit는 resolvePath 감옥을 거치지만 grep의 walk는
안 거쳐 유출. **영향:** 에이전트가 워크디렉터리에 심링크만 심으면 grep으로 임의 파일(비밀·키) 내용
탈취(읽기 샌드박스 이스케이프). O14(쓰기)의 읽기판. **✅ 수정·커밋(a0e7fcc):** walk 콜백에서
심링크 엔트리(`d.Type()&fs.ModeSymlink`)면 `resolvePath` 재검사 후 이스케이프면 skip. 워크디렉터리
안 파일을 가리키는 심링크는 계속 검색. 테스트: `grep_symlink_test.go::TestGrepSymlinkJail`
(밖-심링크 유출 차단 + 안-심링크 검색 유지). **주목:** O14 수정 직후 "파일 내용을 읽는 다른 walk
표면"을 의식적으로 좇아 발견 — 같은 결함 클래스(감옥 우회 심링크)를 read→write(O14)→grep(O15)로
연쇄 확장. **남은 감사면:** glob/list는 내용을 안 읽고 경로만 반환(유출 아님), astgrep/findcontext가
파일 내용을 읽는다면 동일 walk 가드 필요 — 다음 웨이브 후보.

### O16 🔴 findcontext가 밖 심링크의 스니펫까지 유출(O15 형제, 수정+테스트+커밋)
증거(결정론 프로브): `work/credentials` → `outside/credentials.txt`(`aws_secret_access_key =
LEAKED_SNIPPET_XYZ`) → `findcontext{query:"credentials aws secret"}` 결과에
`{"path":"credentials","score":9,"snippet":"aws_secret_access_key = LEAKED_SNIPPET_XYZ"}`
— 경로 + **스니펫 본문**까지 유출(grep보다 노출 큼). 원인: findcontext.go:89 WalkDir 콜백이
`d.IsDir()`만 걸러 심링크 파일이 121행 `os.ReadFile`로 따라가짐(grep O15와 동일 셰이프).
**✅ 수정·커밋(d062d07):** walk에서 심링크 엔트리 `resolvePath` 재검사 후 이스케이프 skip.
안-심링크는 계속 랭크. 테스트: `findcontext_symlink_test.go::TestFindContextSymlinkJail`.
**주목:** grep(O15)→findcontext(O16) 동일 5줄 가드가 두 walk 툴에 중복 → 클래스 재발방지용
공통 헬퍼 추출 여지(N급 제안, 아래). astgrep은 외부 ast-grep 바이너리 순회(기본 심링크 비추적,
루트만 resolvePath) → Go측 무수정.

### R2 ✅ edit/write는 기존 파일 모드 보존(회귀 positive, 비버그)
가설: edit/write가 `os.WriteFile(...,0o644)`로 쓰므로 0755 스크립트의 +x 비트를 날릴 것.
프로브(결정론): 0755 `run.sh`를 edit·write-overwrite 후 `os.Stat().Mode()` 확인 → 둘 다 **0755
유지**. 원인: `os.WriteFile`은 기존 파일에 대해 truncate만 하고 perm은 안 바꿈(신규 생성 시에만
0644 적용). → 실버그 아님. **하네스 자기점검:** 첫 프로브가 잘못된 JSON 키(`old_string` vs
실제 `old`)를 써 edit가 조용히 no-op(에러 반환)했는데, 프로브에 `res.IsError`/결과텍스트 로깅을
넣어둔 덕에 "모드 보존"이 아니라 "편집 자체가 안 됨"을 즉시 간파 → 올바른 키로 재실행. msg4
규율("매 런 로그 분석, 하네스가 올바로 작동하는지 점검")이 또 자기 프로브 버그를 잡음(앞선
KillStatusRace와 동형). 교훈: 프로브는 **대상 툴이 실제로 성공했는지**(IsError)를 항상 로깅해야
위양성/위음성 안 냄.

### N12 ✅ walk-기반 툴의 심링크 감옥 가드 공통화(재발방지 리팩터, 적용+커밋)
grep·findcontext에 동일한 심링크 가드가 복붙돼 있어, 향후 walk-기반 콘텐츠 읽기 툴이 빠뜨리면
같은 이스케이프 재발 위험. **✅ 적용·커밋(5b87512):** `pathutil.go`에 `symlinkEscapesJail(workdir,
p, d fs.DirEntry) bool` 추출(=resolvePath 옆, "walk 툴이 지켜야 할 계약"으로 문서화), 두 호출부를
헬퍼 호출로 치환(동작 불변). 클래스의 단일 진실지점 확보.

### P1 🔴 동일 `(agent,prompt)` 직렬 재위임 리브록 — 오케스트레이터 "무한대기"(사용자 리포트, 수정+테스트+커밋)
**사용자 리포트:** "하이" 후 "이 프로젝트 리뷰해줘" → explorer가 돌고, 마지막에 coder가 도는데
"코더에게 하이 하고 넘어가고 하이가 안 끝나고 계속 돌아". 사용자 자가진단: 오케스트레이터가
explorer 결과물을 coder에게 리뷰로 넘기려다 **prompt로 "hi"(원래 인사말)를 잘못 넘겨서** 원하는
응답이 안 나오고 무한대기.

**근본원인(코드 확정):**
1. `task` 툴은 `a.Prompt`를 아무 검증 없이 자식에 전달(`task.go:54/68/98`→`orchestrate.go:403`).
   오케스트레이터 LLM이 인자를 "hi"로 채우면 그대로 coder에 전달(1차 원인=모델 품질).
2. coder는 "hi"만 받아 인사로 답하고 종료 → 쓸모없는 결과가 `injectSubagentResult`로 주입.
3. 주입 → `needsOrchestratorTurn`=true(`bgHasUnconsumed`) → 오케스트레이터 재기동(step 무소모).
4. 원하는 리뷰가 안 나왔으니 **coder에 동일 재위임**. `dispatch` 중복방지(`orchestrate.go:109`
   `g.inflight[key]`)는 **"동시 실행 중"인 동일 키만** 막음 — 앞 coder 종료 시 inflight에서 삭제
   (`:129`)되므로 동일 `coder\x00"hi"` **직렬 재위임이 허용됨** → 3으로 복귀. maxSteps(240)까지
   같은 헛일 반복 = "무한대기"(실제로는 리브록). **첫 오배정은 모델문제지만, 거기서 리브록에 빠지는
   건 magi 구조결함.**

**프로브(결정론, 모델 무관):** `completingLLM`(텍스트만 내고 종료)으로 coder 완료시킨 뒤 동일
`(coder,"hi")` 재위임 시도 → **수정 전 허용(note=="")로 버그 재현**.

**✅ 수정+커밋:** `bgGroup.completed map[string]bool` 추가, 완료 goroutine에서 완료 키 기록
(`orchestrate.go` dispatch goroutine), `dispatch`에서 inflight 검사 직후 **완료된 동일 키 재위임
거부**("이미 실행됐고 결과가 위에 있다 — 그 결과를 쓰거나 **다른** 프롬프트를 보내라"). 컨텍스트-프리
delegate는 동일 프롬프트면 결정적으로 같은 결과만 내므로 verbatim 재실행은 무의미. **다른 프롬프트
(정상 멀티웨이브)는 키가 달라 무영향.** 문구 스캔 아님(순수 구조적 키 dedup). 회귀:
`dispatch_dedup_test.go::TestDispatchDedupsCompleted`. app 전체 테스트 green.

**후속 아이디어(미구현):** 1차 원인(오케스트레이터가 인사말을 delegate prompt로 에코)은 모델 품질
이슈. delegate prompt가 부모 최근 user 메시지와 바이트동일이면 경고하는 구조가 가능하나(합법적
포워딩 케이스와 충돌) 이번엔 보류 — 리브록 가드가 핵심 고가치 수정.

---

### P2 🟠 delegate VERBATIM 스펙이 2000B에서 맨-말줄임(`…`)으로 잘림 — 코더가 재현할 정확블록 유실(사용자 리포트, 수정+테스트+커밋)
**사용자 리포트:** "편집 툴 호출하는데, 많은 양을 매칭해서 호출할 때 ….때문에 매칭 실패하는
버그가 있다" → 이어서 "코더에게 …으로 전달된 거 아닐까". 즉 **큰 내용이 코더에게 `…`로 잘려
전달돼**, 코더가 그걸 edit `old`로 쓰면 매칭 실패한다는 가설.

**근본원인(코드+프로브 확정):**
1. edit 인자 자체는 magi가 **안 자름** — `openai.go:442 toolAccumulator.add`가 모든 스트림
   델타 조각을 `append`로 이어붙이고, `firstJSONValue`(:407)는 *중복 전송된 전체 JSON*만 첫 값
   으로 정리할 뿐 단일 큰 인자를 절단하지 않음. 모델이 큰 `old`를 emit하면 그대로 전달됨. (가설의
   "edit 인자 절단" 부분은 기각.)
2. **그러나 컨텍스트-프리 delegate(=코더)에게 넘기는 유일한 스펙**인 goal은 절단됨:
   `planner.go:1312 delegateBrief`가 `"SPEC (authoritative — ... follow this VERBATIM): " +
   clipLine(g, 2000)`. clipLine은 2000B 초과분을 **맨 `…`**로 자름(`council_gate.go:106`).
   → 2000B 넘는 스펙의 뒤쪽 정확 식별자/블록이 사라지고, 코더는 재현할 것을 애초에 못 받음. 코더가
   잘린 지점을 그대로 edit `old`에 옮기면 dangling `…`까지 들어가 매칭 실패. **"VERBATIM 준수"
   라벨과 침묵절단이 정면 모순.** 기존 메모 `brief-paraphrase-spec-loss`(kv val/value 유실)와 동형.

**프로브(결정론, 모델 무관):** 2700B goal 끝에 `EXACT_BLOCK = "kv-store-value-42"` 배치 →
`delegateBrief` 결과: `goal bytes=2700, brief has ellipsis=true, brief keeps EXACT_BLOCK=false`
— 수정 전 정확블록이 `…`로 유실됨을 재현.

**✅ 수정+커밋(`1706495`):** `clipSpec`(council_gate.go) 신설 — 상한 8000B(실사용 요청 거의
전부 포괄)로 올리고, 초과 시 맨 `…` 대신 **명시 마커**("this cutoff is NOT part of the spec;
ask for the remainder")를 붙여 모델이 절단점을 verbatim 재현하지 못하게. `delegateBrief`의 SPEC
분기를 `clipSpec(g, 8000)`로 교체(off-분기 400B 오리엔테이션은 불변). 회귀:
`planner_test.go::TestDelegateBriefSpecSurvivesLongGoal`(2300B+ 스펙의 정확블록 보존 + 맨-말줄임
부재), `::TestDelegateBriefSpecTruncationIsExplicit`(8000B 초과 시 명시 마커). app 전체 green.

**클래스 확장(사용자 지시 "다른 툴에도 있지 않냐, 확장해서 고쳐"):** "모델 컨텍스트로 돌아가는
텍스트를 맨 `…`로 잘라 모델이 verbatim 재현하려다 실패"하는 패턴을 전수 감사.
- **✅ 형제 수정(`f2c6c73`):** `loop.go:509/517` stall/no-progress 넛지가 "원래 task를 다시
  읽어라"며 authoritative task를 `clipLine(task,1500)` 맨-`…`로 재전달 → `clipSpec`로 교체(넛지
  크기 유지, 절단만 명시).
- **✅ 상류 근본(`3f9eef2`):** 애초에 인라인을 줄이는 방향 — `prompt.go:86` 오케스트레이터 위임
  지침에 "파일 작업은 경로를 넘기고 서브에이전트가 직접 읽게 하라, 내용 붙여넣기 금지"를 추가(붙여넣은
  내용은 잘리고 컨텍스트 낭비; 자식은 자기 read 툴로 실파일을 봐야). 사용자 제안 그대로.
- **안전 판정(비버그, 감사 근거 기재):** `read`(라인을 `…`로 안 자름; 바이트/윈도우 절단은 전용
  note 명시) · `grep`(전체 매치라인) · `edit.nearMissHint`(`%q` 전체라인+라인번호) ·
  `bash.truncateOut`/`webfetch`/`guard.capToolResult`(모두 "(N bytes omitted)"류 명시 마커).
- **클래스 밖(비대상):** `findcontext`/`astgrep` 스니펫은 `oneLineN` 맨-`…`지만 **`TrimSpace`된
  path:line 로케이터 프리뷰** — 트림만으로도 이미 verbatim 편집소스가 아니라 "…"가 한계요인이 아님.
  실패요인/이전산출물/피드백/형제제목 clipLine은 **재현 대상 아닌 오리엔테이션 요약**이라 맨-`…` 정상.

---

### I 🟠 findcontext가 비-Latin(한글/CJK/키릴) 쿼리에서 "no usable keywords"로 데드엔드(수정+테스트+커밋)
**근본원인:** `keywords()`의 단어-분리 술어가 **ASCII 전용**(`a-zA-Z0-9`)이라 한글/CJK/키릴 쿼리는
모든 룬이 구분자로 취급돼 토큰이 0개 → `Execute`가 "query has no usable keywords" 반환. 한국어
코드베이스/프롬프트에서 findcontext가 통째로 무력화.

**✅ 수정+커밋(`e697d52`):** FieldsFunc 술어를 `unicode.IsLetter(r)||unicode.IsDigit(r)`로 교체
(`findcontext.go`, `"unicode"` import). `len(w)>=3` 바이트 게이트는 유지(CJK 1음절=3바이트라 통과).
ASCII 동작(stopword/<3/camel split) 무회귀. 회귀: `findcontext_test.go::TestKeywordsUnicode`
(설정 파싱/конфиг/データベース/auth설정/café 등) + `::TestFindContextKoreanQuery`("사용자 인증"이
한국어 주석 파일을 1위로 랭크).

---

### F1 💡 CJK 테이블 정렬 — "폭 정보를 모델에 전달"이 아니라 마크다운 테이블 유도(사용자 아이디어, 구현+커밋)
**사용자 아이디어:** "계산된 문자 폭 정보를 모델에 전달해서 모델이 테이블 형태 데이터를 줄 때 깨끗
하게 나오게 하는 건 어떨까." (CJK=2셀이라 모델이 rune 수로 패딩하면 표가 어긋남.)

**측정(결정론 프로브):** magi는 `glamour/v2`로 렌더(`model_layout.go:36`). CJK 마크다운 테이블을
glamour로 렌더 → 모든 라인 `cellWidth`가 **정확히 78로 동일, `|` 구분자 정렬**(김/홍길동 무관).
즉 **모델이 마크다운 테이블을 주면 magi가 이미 CJK-정확 정렬**함. 문제는 모델이 손수 스페이스로
패딩한 ASCII 테이블 → glamour 통과 → rune-count 패딩이 CJK에서 깨짐.

**설계 판정:** "폭 정보 전달"은 부적절 — 모델은 토큰을 **스트리밍**으로 생성하므로 아직 없는 텍스트의
폭을 미리 못 주고, 마크다운 테이블은 어차피 glamour가 정렬하니 폭 정보 자체가 불필요. **근본 처방=
손-정렬을 하지 말게 하는 것.**

**✅ 구현+커밋(`829c9ef`):** `prompt.go` `outputFormatGuide` 신설 — "응답은 마크다운으로 렌더된다;
표는 마크다운 테이블(`| a | b |`)로 주고 렌더러가 CJK 2셀 포함해 정렬한다; **스페이스 손-정렬·ASCII
박스표 금지**(문자 수만 세서 CJK가 어긋남)." `systemFor`에서 `envInfo` 뒤 정적 삽입(모든 에이전트,
prefix-cache 무교란). 회귀: `regression_test.go::TestSystemForCarriesOutputFormatGuide`(top-level+
subagent 둘 다 포함). app 전체 green.

---

### P3 🟠 한글(NFD) 파일에서 edit/multiedit 실패 — 유니코드 정규화 불일치(사용자 리포트, 재현+수정+커밋)
**사용자 리포트:** "한글이 포함되면 에디트툴 실패한다는 이야기가 있네."

**근본원인(프로브로 재현):** **macOS(darwin)는 한글을 NFD(자모 분해: 함→ㅎ+ㅏ+ㅁ)로 저장**하는
경우가 많은데, 모델은 NFC(완성형)를 emit. 눈엔 동일하나 바이트가 다름 → `applyEdit`의 exact/
EOL 계층이 못 잡고 "not found"로 실패. 프로브: NFC "함수 정의"=13B vs NFD=31B, `equalBytes=false`
→ `applyEdit(content=NFD, old=NFC)` → **"not found: old string not present"** 재현.

**추가 발견:** `multiedit`는 `applyEdit`를 안 쓰고 **자체 strings.Count/Replace exact 매칭만** —
EOL/공백/정규화 관용이 아예 없어 같은 버그 + 더 취약.

**✅ 수정+커밋(`6c2ec12`):** `applyEdit`에 **정규화-관용 계층(tier 2.5)** 추가 — `old`를 파일의
형태(NFC 후 NFD)로 맞춰 exact 매칭, 히트 시 `new`도 같은 형태로 써서 나머지 파일 정규화는 불변.
`lineKey`도 NFC 폴딩해 tier 3(공백-관용 whole-line)이 정규화와 합성됨. **`multiedit`를 `applyEdit`
위임으로 리팩터**해 세 계층 상속(원자성 유지). `golang.org/x/text` direct 승격. 회귀:
`edit_norm_test.go`(NFD↔NFC 양방향 + multiedit). builtin 전체 green, vet clean.

### P4 🟠 `magi.ask` 폼 UI — nil 라벨 · 가로 옵션 · 로고 부재(사용자 리포트, 수정+테스트+커밋)
**사용자 리포트:** ADSSO 로그인 메뉴가 `› nil` 로 렌더, 긴 옵션이 한 줄에 안 들어감,
스타트업 로고 없이 폼만 뜸.

**근본원인:**
1. `bridge.go bridgeAsk` — gopher-lua는 없는 키에 `LNil`을 반환하고 `LNil.String()=="nil"`
   (리터럴). `Label="nil"`이 되어 `prompt.go`의 `label==""→Name` 폴백을 통과 → `› nil`.
2. `prompt.go View` — select/multiselect 옵션을 `strings.Join(opts,"  ")`로 **가로** 배치라
   긴 라벨(`Sign in with DS AD SSO (브라우저 팝업)`)이 줄을 넘침.
3. `RunPrompt`는 독립 `tea.Program`(AltScreen)이라 메인 TUI 스플래시(`logo.go splashView`)와
   분리 → 로그인 화면에 MAGI 로고 없음.

**✅ 수정+커밋(`79b6099`):**
- `bridgeAsk`에 `LNil→""` 정규화 헬퍼 → 라벨 미지정 시 Name 폴백 정상.
- 옵션 **세로 배열**(옵션당 1행): select `●`(선택=커서)/`○`, multiselect `[x]`/`[ ]`, 포커스
  행 `›`+하이라이트. **중첩 ↑/↓**(리스트 안 이동→끝 옵션에서 다음 필드로 크로스), select Enter=
  확정+다음(마지막이면 Submit). Submit는 옵션 아래.
- `splashView`에서 `logoBlock()` 분리 → 프롬프트 상단에 동일 MAGI 워드마크 렌더("스타트업
  페이지+폼" 형태). 회귀: `prompt_test.go`(세로 네비 재작성 + `TestPromptSelectVerticalNav`
  + `TestPromptViewRendersLogoAndVerticalOptions`), `ask_test.go::TestBridgeAskOmittedLabelIsEmpty`.

### P5 🟠 모달(권한/질문) 열림 중 트랜스크립트 스크롤 불가 — 컨텍스트 되짚기 차단(사용자 리포트, 수정+테스트+커밋)
**사용자 리포트:** 예스노/객관식 선택지가 떠 있으면 스크롤로 이전 내용을 볼 수 없어, 자리를
비웠다 돌아오면 지금 결정하려는 것의 맥락을 사람이 이해할 수 없음.

**근본원인:** `model_input.go:237` — 권한 모달이 열리면 결정 키를 뺀 **모든 키를 삼킴**
(`return nil, true // swallow`), 질문 모달(`:211`)·마우스 핸들러(`handleMouse` perm/quest 분기
`return nil`)도 동일. AltScreen이라 터미널 자체 스크롤백도 없음 → 페이지백 수단이 전무.

**✅ 수정+커밋(`45e56d0`):** `scrollTranscriptKey`(pgup/pgdown·ctrl+u/d·shift+up/down)와
`wheelScrollTranscript`(휠)를 두 모달 키·마우스 핸들러에서 통과. 모달은 유지(결정 대기 지속).
회귀: `modal_scroll_test.go`(`TestScrollTranscriptKey`, `TestPermissionModalStaysScrollable`).

## Wave 11 — 유니코드 정규화 클래스 헌트(검색 표면)

### O17 🟠 grep·findcontext가 NFD 파일을 NFC 쿼리로 조용히 놓침(P3 형제, 재현+수정+커밋)
P3(edit/multiedit NFD)에서 명명한 클래스를 **검색 표면**으로 확장. 파일이 disk에 NFD로
저장(macOS 한글)돼 있고 모델은 NFC로 쿼리하면 바이트 불일치 → 무매치.

**증거(Wave 11 프로브 `K_norm_search`, 결정론):** `writeFile("kor.go", "// "+NFD("함수 정의"))`
후 —
- `grep{pattern: NFC("함수")}` → `[]` (miss). **NFD 패턴 control은 매치**, Latin control도 매치
  → 정규화 불일치임이 격리됨.
- `findcontext{query: NFC("함수 정의")}` → 빈 결과(score 0). 둘 다 P3와 동일 클래스로 확정.

**근본원인:** grep은 `regexp.MatchString(line)`(바이트 리터럴), findcontext는
`strings.Contains(lower, term)`(바이트) — 둘 다 NFD≠NFC를 못 넘김.

**✅ 수정+커밋(`16a3fa8`):**
- `grep`: 패턴에 **비-ASCII 룬이 있을 때만** 패턴과 각 테스트 라인을 NFC 폴딩(매치 테스트에만;
  출력은 원본 on-disk 바이트 보존). ASCII 패턴은 무손상(exact-byte 유지) → 일반 케이스 무회귀.
  헬퍼 `isASCIIOnly`.
- `findcontext`: 쿼리 term(`keywords(NFC(query))`) · 경로 파트(base/dirPart) · 콘텐츠
  (`scoreContent` 최상단 `content=NFC(content)`)를 NFC 폴딩 후 스코어.
- 회귀: `norm_search_test.go`(NFD-파일↔NFC-쿼리 grep/findcontext + grep ASCII 무회귀 +
  출력 원본바이트 보존). 프로브 하네스는 비커밋 유지. builtin 전체 green, vet clean.

### O18 🟠 glob·grep --glob이 NFD 파일**명**을 NFC 패턴으로 조용히 놓침(O17 파일명 형제, 재현+수정+커밋)
O17이 **콘텐츠** 검색을 닫았고, 이건 **파일명** 매칭 형제. 파일명이 disk에 NFD(macOS)로
저장돼 있고 모델은 NFC 글롭을 침 → `filepath.Match` 바이트 비교 → 무매치.

**증거(Wave 12 프로브 `L_norm_glob`, 결정론):** `writeFile(NFD("함수")+".go")` 후 —
- `glob{pattern:"*"+NFC("함수")+"*.go"}` → `[]` (miss). **ASCII control `*.go`는 둘 다 나열**
  (`함수.go` 원본 NFD 바이트 포함) → 정규화 불일치로 격리.
- `grep{pattern:"func F", glob:"*"+NFC("함수")+"*.go"}` → `[]` (miss).

**근본원인:** glob 툴·grep `--glob`는 공통 `matchGlob`→`filepath.Match`(바이트) 경유. grep의
비-슬래시 base-case도 `filepath.Match(g, base)` 직접 호출.

**✅ 수정+커밋(`817771d`):** `matchGlob`이 **패턴에 비-ASCII 룬이 있을 때** 패턴·이름 양쪽을
NFC 폴딩(glob 툴 + grep 슬래시-글롭 동시 커버). `grepGlobMatch`의 base-case도 동일 폴딩.
나열 경로는 원본 on-disk 바이트 보존, ASCII 패턴 무손상. 회귀: `norm_search_test.go`
(`TestGlobMatchesNFDNameWithNFCPattern`, `TestGrepGlobFilterMatchesNFDName`).

## Wave 13 — astgrep 감옥 불변식(O15/O16 형제, 외부 프로세스 표면)

### O19 🟡 astgrep 스트림 파서가 워크디렉터리 밖 매치를 절대경로+스니펫으로 방출(감옥 불변식 위반, 재현+수정+커밋)
O15/O16이 grep/findcontext의 심링크 읽기-이스케이프를 닫았지만 **astgrep은 미커버**. ast-grep는
트리를 **외부 프로세스가 직접 walk**하므로 인-코드 심링크 가드(`symlinkEscapesJail`)가 닿지 못함 →
마지막 방어선은 `parseAstGrepStream`. 그런데 이 파서는 밖 파일에 대해 `filepath.Rel`이 `..`
프리픽스를 내면 **스킵하지 않고 절대경로를 그대로 emit + 스니펫까지 포함**(astgrep.go:154-157).

**증거(Wave 13, 순수함수 프로브):** `parseAstGrepStream({"file":"/etc/secret.go",...snippet...})` →
`emitted file="/etc/secret.go" text="const Token=\"TOPSECRET\""` (count=1). **밖 경로+스니펫 방출 확정.**
모든 다른 툴의 불변식(밖=미반환, rel만)과 어긋남.

**현 시점 라이브 미도달(∴🟡):** ast-grep 0.44.0은 기본적으로 심링크를 **안 따라감**(라이브 확인:
`work/link→outside` 심링크 두고 run → 빈 결과), `a.Path`는 `resolvePath`가 O14 감옥으로 차단.
∴ 현재 익스플로잇 불가 — 하지만 미래 `--follow`/정규화 quirk 대비 방어심화 + rel-경로 일관성 버그.

**✅ 수정+커밋(`af0ede5`):** 파서가 밖 경로(`Rel` 에러 or `..`/`../` 프리픽스)를 **드롭**(continue),
상대경로는 workdir에 join 후 판정(이스케이프 오인 방지). 회귀: `astgrep_jail_test.go`
(밖-드롭 / 안-유지-rel / 상대경로-유지). 기존 라이브 `TestAstGrepRealMatch` green(happy-path 무회귀).

---

## 이번 세션 수정 요약 (커밋 대기 — 요청 시에만)

- **O3** edit/multiedit 빈 old 거부 — `edit.go`, `multiedit.go` (+`edit_empty_old_test.go`).
- **O5** `-profile` 검증(안전 footgun) + **O6** `-theme` 검증 — `cmd/magi/main.go`
  (`validateEnumFlags` 추출, +`validateflags_test.go`; N1도 소급 커버).
- **O8** `clipLine` ambiguous/decor-wide 터미널 오버플로우(실버그) — `toolbody.go`
  (+`toolbody_test.go`의 TestClipLineAmbiguousWide). 기본 터미널 무회귀(바이트 동일).
- **O10** config.toml 오타 키 조용히 무시(O5 config판) — `internal/config/config.go`
  (`LoadWithUnknown`) + `cmd/magi/main.go`(`warnUnknownConfigKeys`) (+`config_test.go`,
  `validateflags_test.go`의 각 테스트). forward-compat(경고만, 하드에러 아님).
- **O11** guardrail 값 오타 무구속 강등(O5 값판) — `cmd/magi/main.go`
  (`validateGuardrailValues`, 하드페일) (+`validateflags_test.go::TestValidateGuardrailValues`).
  O5+O10+O11로 profile-안전 플래그/키/값 3층 폐쇄.
- **(리뷰 후속)** 좁은터미널 모달 예약이 렌더 높이를 추적하도록 — `internal/adapter/tui/
  model_layout.go` (+`quest_test.go`의 TestModalReserveTracksWrapAtNarrowWidth).
- **(회귀 잠금)** N11 kill-race 회귀테스트 — `internal/adapter/tool/builtin/bgproc_test.go`
  (`TestBackgroundKillStatusRace`, ×80 + -race). 신규버그 아님, 방어 견고 확인.
- **O13** 검색 하이라이트 wide-ambiguous 밀림(O8 계열) — `model_view.go` +
  `search_test.go::TestSearchHighlightAmbiguousWide`. **커밋됨 e4c1fc9.**
- **O14** 🔴 심링크 감옥 fail-open 샌드박스 이스케이프(쓰기) — `pathutil.go` +
  `pathutil_more_test.go::TestResolvePathRejectsBrokenSymlinkEscape`. **커밋됨 b987ca5.**
- **O15** 🔴 grep 심링크 경유 밖 파일 내용 유출(읽기) — `grep.go` +
  `grep_symlink_test.go::TestGrepSymlinkJail`. **커밋됨 a0e7fcc.**
- **O16** 🔴 findcontext 심링크 경유 밖 스니펫 유출(읽기) — `findcontext.go` +
  `findcontext_symlink_test.go::TestFindContextSymlinkJail`. **커밋됨 d062d07.**
- **O17** 🟠 grep·findcontext NFD-파일 vs NFC-쿼리 조용한 무매치(P3 검색 형제) — `grep.go`,
  `findcontext.go` + `norm_search_test.go`. **커밋됨 16a3fa8.**
- **O18** 🟠 glob·grep --glob NFD-파일명 vs NFC-패턴 조용한 무매치(O17 파일명 형제) — `glob.go`,
  `grep.go` + `norm_search_test.go`. **커밋됨 817771d.**
- **O19** 🟡 astgrep 파서가 밖 매치를 절대경로+스니펫으로 방출(O15/O16 외부프로세스 형제, 방어심화)
  — `astgrep.go` + `astgrep_jail_test.go`. **커밋됨 af0ede5.**
- **P4** `magi.ask` 폼 UI(nil 라벨·세로 옵션·로고) — `bridge.go`, `prompt.go`, `logo.go`
  (+`prompt_test.go`, `ask_test.go`). **커밋됨 79b6099.**
- **P5** 모달 열림 중 트랜스크립트 스크롤 허용 — `model_input.go`
  (+`modal_scroll_test.go`). **커밋됨 45e56d0.**
- 프로브 하네스 `probe_overnight_test.go`는 조사용(비커밋).
- 전 패키지 `go test` green, gofmt/vet clean, `go build ./...` clean.
- **커밋 정책 갱신(사용자 승인):** 중요 이슈는 발견 즉시 레이어드 커밋(core/app→cmd→config→test,
  plexus/Claude 언급·Co-Authored-By 없음). findings 문서·probe는 워킹트리 유지. **이미 커밋된
  이번 세션 분:** c4511aa(O8) 78efeec(O3) 9d5c231(N11) 01e75f5(O10) c66e2bd(O5/O6/O11)
  da58630(모달) e4c1fc9(O13) b987ca5(O14 🔴) a0e7fcc(O15 🔴) d062d07(O16 🔴).
