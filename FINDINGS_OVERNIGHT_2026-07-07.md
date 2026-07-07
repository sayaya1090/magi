# 야간 자동 감사 — 2026-07-07

목표: 복잡/엣지 케이스로 magi를 벤치처럼 반복 실행하며 버그·개선점을 발굴해 기록.
방법: (A) 모델 비의존 결정적 프로브(CLI/플래그/설정/에러경로 + 내부 툴 적대적 입력), (B) 로컬 gpt-oss:20b 대상 라이브 헤드리스 런(타임아웃 방어). 모든 발견은 **실제 출력 인용**으로 뒷받침(read-logs 원칙). 요약 전파 금지.

범례: 🔴 버그 · 🟡 잠재/엣지 · 🟢 개선 · ⚪ 정상 확인(무결)

바이너리: `magi dev (commit 635075c)` @ 2026-07-07 05:02 (이 세션 UI 수정 포함).

---

## 진행 로그 (probe 카운터)

- **Wave 1 (P1–P10, CLI/플래그/에러경로, 모델 비의존):** 대부분 정상(⚪). `--nope`→exit2+usage, unreachable base→깔끔한 `error[provider]` exit1, `-doctor`/`-list-models` 정상. 발견 3건 아래(N1~N3).
- **Wave 2 (R/W/E/M/G/GL/L/S 40여 프로브, 내부 툴 적대적 입력, `audit_probe_test.go`):** 보안 핵심은 전부 견고(⚪) — 경로 감옥 `../../etc/passwd`·절대경로·심링크→/etc 모두 차단, 바이너리 NUL 감지, multiedit 원자성 확인. 발견 N4~N9 아래.
- **Wave 3 (B1–B18, bash/bgproc 런타임, `audit_probe2_test.go`):** timeout(`[timed out after 1s]`)·detachTTY(`/dev/tty: Device not configured` 즉시 실패, 무행)·거대출력 절단·exit코드·유령 id 클린 에러·`&` 자식 생존(B14 marker PRESENT, F1 확증)·bg lifecycle(start/output/kill) 모두 정상(⚪). 발견 N10-후보(kill 후 status race)·아래.
- **Wave 4 (W1–W15+clip 30여 케이스, cellWidth/padOrTruncate, `internal/adapter/tui/audit_probe_test.go`):** CJK 셀 단위 정확 절단(w=3→"가 ", 와이드 룬 반쪽 안 잘림)·이모지 ZWJ/skin-tone/flag=2·조합문자=1·ZWSP=0·음수/0 폭 안전(⚪). **발견 N10(터미널 주입) — 하이라이트.**

## 확정 발견

### N1 🟢 enum형 플래그 미검증 (`-output`, `-permission`)
증거:
- `./magi -output xml -p "hi"` → exit 0, 정상 text 출력(xml은 조용히 무시). runHeadless는 `*output=="json"`만 보고 나머지는 전부 text로 처리.
- `./magi -permission bogus -p "hi"` → exit 0, 정상 실행.
원인: `-output`은 `{text,json}`, `-permission`은 `{ask,auto,allow,deny}` 이외 값을 검증하지 않음. 오타(`auto0`, `jsn`)가 조용히 기본 동작으로 폴백.
안전성: `-permission` 미지의 값은 `permission.go:86`에서 `Permission()=="allow"` 아니므로 헤드리스에서 **deny로 폴백**(=안전 방향). 보안 이슈 아님. 그러나 사용자 의도(auto/allow)가 조용히 deny로 바뀌면 "툴이 왜 안 도냐" 혼란.
제안: 플래그 파싱 직후 두 enum을 화이트리스트 검증하고 미지의 값은 `flag`처럼 exit2+메시지. (cmd/magi/main.go)

### N2 🟢 헤드리스 text 모드 stdout에 크롬 라인 혼입
증거: `-output text`(기본) 헤드리스에서 답변과 함께 `⟳ planner note: ...`, `⚖ council ...`, `↯ context compacted ...` 등이 stdout으로 섞여 나옴(renderText, main.go:574~). `-output json`은 이벤트를 JSONL로만 출력 → 오염 없음.
성격: text는 human-readable 의도라 설계상 수용 가능. 다만 스크립트가 text stdout을 파싱하면 답변과 크롬을 구분 못 함.
제안: 문서에 "스크립트는 `-output json`" 명시(이미 부분적으로 그러함). 또는 크롬을 stderr로 보내 stdout=순수 답변으로. (선택)

### N3 🟡 `-p ""`(빈 헤드리스 프롬프트) → TUI로 폴백 후 무-TTY 크래시
증거: `./magi -p ""` → `magi: tui: bubbletea: error opening TTY ... /dev/tty: device not configured` exit1.
원인: 빈 문자열 `-p`가 "헤드리스 프롬프트 없음"으로 취급되어 TUI 분기로 감. 그런데 파이프/비TTY 환경이면 TUI가 즉시 실패.
제안: `-p`가 **명시적으로 주어졌으면**(빈 값이라도) 헤드리스로 확정하고, 빈 프롬프트는 `error: empty prompt` 같은 명확한 메시지로 처리. (headless 판정 로직, main.go:~121/307)

### N4 🟡 잘못된 glob 패턴이 조용히 무매치 (grep과 비대칭)
증거: `glob {"pattern":"["}` → `isErr=false, null`. grep의 `{"pattern":"("}` 는 `isErr=true "invalid regex: ... missing closing )"`.
원인: `glob.go:87` `ok, _ := filepath.Match(...)` — `ErrBadPattern`을 버림. 잘못된 패턴과 "매치 없음"이 구분되지 않아, 에이전트가 패턴 오타를 알 수 없음.
제안: `matchSegs`에서 Match의 err를 상위로 전파해, 첫 세그먼트 컴파일 단계에서 `ErrBadPattern`이면 grep처럼 `errResult("invalid glob pattern: ...")`. (또는 실행 전 `filepath.Match(pattern,"")`로 사전 검증)

### N5 🟢 glob이 dotfile/dot-dir 전부 스킵 → 숨은 경로 패턴 도달 불가
증거: `glob.go:41-48`이 `.`으로 시작하는 dir/파일을 무조건 제외. `glob {"pattern":".github/workflows/*.yml"}` 류는 절대 매치 안 됨.
성격: 노이즈(.git 등) 제거 의도지만, 명시적으로 숨은 경로를 겨냥한 패턴도 못 잡음. 제안: 패턴이 `.`으로 시작하는 세그먼트를 포함하면 그 경로에 한해 스킵 해제(opt-in).

### N6 🟡 범위 밖 offset 읽기 → 조용히 빈 출력(빈 파일과 구분 불가)
증거: 4줄 파일에 `read {"path":"a.txt","offset":100}` 및 `offset:9999` → `isErr=false, ""`(빈 문자열). 에이전트에겐 "빈 파일"로 보임.
제안: offset이 총 줄 수를 넘으면 `(note: offset N is past end; file has M lines)` 주석을 붙여 무결과 사유를 명시. (read.go)

### N7 🟢 read 빈 path → 혼란스러운 "is a directory: " 메시지
증거: `read {"path":""}` → `isErr=true "is a directory: "`(빈 이름). 빈 path는 workdir로 resolve되어 디렉터리 에러가 됨.
제안: bash처럼 `"path is required"`로 선검증. (read.go, list/write도 동일 패턴 점검)

### N8 🟢 write 실패 에러가 상대경로 대신 절대 임시경로 노출
증거: `write {"path":"sub",...}`(디렉터리 덮어쓰기) → `"open /var/folders/6n/.../001/sub: is a directory"`. 다른 에러(jail 등)는 상대경로("outside workdir: sub")인데 여기만 절대경로 노출 → 비일관 + 경로 누출.
제안: os 에러를 그대로 반환하지 말고 `fmt.Errorf("%s: is a directory", a.Path)` 등 jail-상대 메시지로 정규화. (write.go)

### N9 🟢 무매치/빈결과가 JSON `null` (빈 배열 `[]` 아님)
증거: `grep`/`glob` 무매치 → 리터럴 `null`. `okJSON`이 nil 슬라이스를 마샬. 소비자가 `[]`를 기대하면 오작동 여지.
제안: 반환 직전 `if out == nil { out = []string{} }` 로 정규화. (grep.go/glob.go) — 사소하지만 계약 명확화.

### N10 🟡→🔴 신뢰불가 콘텐츠의 ANSI/제어 이스케이프가 터미널로 무검열 방출 (터미널 주입)
증거(코드경로 + 프로브):
- `clipLine`(toolbody.go:232)은 `\t`→스페이스만 치환, **C0/ESC/OSC 제어시퀀스는 스트립 안 함**. 짧은 라인(폭 이내)은 `ansi.Truncate`도 안 타서 원문 그대로 통과.
- Wave 4 프로브 `control-chars "a\x01\x02\x1b[31mb"` → `padOrTruncate` 출력에 `\x1b[31m` 그대로 잔존 확인.
- `bashBody`는 raw bash 출력을, `textBody`(webfetch/websearch)는 raw 페이지 텍스트를 `clipLine`에 직접 통과 → `styleToolResult.Render()`가 감싸도 **중간 임베드 이스케이프는 생존**.
경로: `read`로 읽은 악성 리포 파일 내용, `bash`/`webfetch` 출력에 든 `\x1b]0;...\a`(타이틀 스푸핑)·`\x1b[2J`(화면 클리어)·`\x1b[...H`(커서 이동)·색상 조작 등이 사용자 터미널에 그대로 렌더됨. 취약 터미널에선 응답-주입 계열로 확대 여지.
위협모델: "공격자"=신뢰불가 파일/명령 출력. 에이전트 벤치·미지 리포 탐색 시 현실적 표면.
제안: 표시 직전 콘텐츠에서 **허용 목록 외 C0/C1 제어문자·OSC/DCS/APC 시퀀스를 스트립**(SGR 색상만 유지하려면 화이트리스트 파서, 아니면 전부 제거 후 magi 자체 스타일만 적용). `read`(chroma 경유라 부분 완화 가능성 있음—별도 확인 필요)보다 `bashBody`/`textBody` 경로가 직접 노출. (toolbody.go clipLine 또는 각 *Body 진입부에서 새니타이즈)

### N11 🟢 bash_kill 직후 bash_output이 여전히 `[id running]` 표기 (상태 보고 레이스)
증거: B17 `killed bg_1` 직후 B18 `bash_output` → `[bg_1 running 1s]`(exited 아님). `status()`는 `p.done` 기반인데 이 플래그는 Wait 고루틴이 세팅 → kill 후 reaper가 아직 못 돌아 잠시 "running"으로 보임(결국 수렴).
성격: 짧은 창의 오보. 제안: `BashKill`에서 `p.done=true` 또는 별도 `p.killed` 플래그를 동기 세팅해 즉시 `[id killed]` 반영. (bgproc.go BashKill/status)

### N12 🟢 bash_output 빈/누락 id → `no such background process: `(빈 id) 혼란 메시지
증거: B13 `bash_output {}` → `isErr=true "no such background process: "`. N7과 동일 계열(빈 인자 선검증 부재).
제안: `if a.ID=="" { return errResult("id is required") }`. (bgproc.go BashOutput/BashKill/BashInput 공통)

### N13 🟡 헤드리스에서 `-permission auto`는 bash/webfetch를 조용히 전부 거부 (벤치 footgun)
증거(라이브 gpt-oss:20b, `-permission auto -p ...`):
- L-run1 `write` 태스크 → 파일 생성 성공(fileModifier 자동승인) → council done → 정상 종료.
- L-run3 `frobnicate --status 실행` 태스크 → `which frobnicate`·`frobnicate --status` 2회 bash 시도가 **permission deny** → 에이전트 최종: *"I'm not able to run arbitrary shell commands directly here. Could you please allow the execution..."* (없는 사용자에게 승인 요청하며 종료).
원인(설계): `permission.go:61-68` `auto`=accept-edits — `fileModifiers`만 자동승인, bash/webfetch는 "ask"로 폴백 → 헤드리스(`!Interactive`, :84-86)에선 `allow`만 통과하므로 **deny**. 즉 문서화된 의도된 동작.
성격: 버그 아님이나 **벤치/스크립트 footgun**. `auto`는 자연스러운 선택인데 헤드리스에서 쓰면 에이전트가 명령을 하나도 못 돌리고 조용히 무력화(에러 없이 정중히 거절만). 헤드리스에서 "다 하게"의 정답은 `allow`.
제안: 헤드리스 + `auto`(또는 `ask`)로 부팅 시 stderr에 1회 경고 — `note: --permission auto denies bash/webfetch in headless; use --permission allow to enable them`. N1(플래그 검증)과 함께 처리.
정직성 확인(⚪): 거부 상황에서도 에이전트가 exit 코드를 **날조하지 않고**("Do not invent" 준수) 한계를 사실대로 보고 → 거짓완료 방어 정상.

---

## 요약 (2026-07-07 야간, ~100 프로브 / 6 웨이브)

| 계층 | 프로브 | 결과 |
|---|---|---|
| CLI/플래그/에러경로 (W1) | P1–P10 | 대체로 견고. N1(enum 미검증)·N2(text stdout 크롬 혼입)·N3(빈 `-p`→TUI 크래시) |
| 파일/검색 툴 적대적 입력 (W2) | R/W/E/M/G/GL/L/S 40+ | **보안 핵심 전부 견고**(경로감옥·심링크·NUL·multiedit 원자성). N4~N9(대부분 🟢 UX/계약) |
| bash/bgproc 런타임 (W3) | B1–B18 | timeout·detachTTY·거대출력·`&`자식생존(F1)·bg lifecycle 정상. N11·N12(🟢 상태보고/빈인자) |
| 폭/유니코드 렌더 (W4) | cellWidth/clip 30+ | 셀 단위 정확, 이모지/조합문자/음수폭 안전. **N10(터미널 주입, 하이라이트)** |
| 설정/에러경로 (W5) | C1–C4 | `-config` 플래그 부재, 빈/가짜 모델 깔끔한 provider 에러 |
| 라이브 gpt-oss:20b (W6) | L-run1~3 | end-to-end 정상(도구루프·council·종료). 거짓완료 0. N13(auto footgun) |

### 우선순위 제안 (수정 착수 순)
1. **N10** 🔴/🟡 — 터미널 주입: `bashBody`/`textBody` 표시 전 제어시퀀스 새니타이즈. (보안 표면, 신뢰불가 콘텐츠)
2. **N4** 🟡 — glob 잘못된 패턴 조용한 무매치 → grep처럼 에러 전파. (에이전트 디버깅성)
3. **N3** 🟡 — 빈 `-p` 비TTY 크래시 → 명시적 에러. (헤드리스 견고성)
4. **N1 + N13** 🟢/🟡 — enum 검증 + 헤드리스 auto 경고. (벤치 footgun, 한 묶음)
5. N6/N11/N12/N7/N8 🟢 — 빈-인자 선검증·offset-past-EOF 주석·kill 후 상태 등 UX 일괄.
6. N5/N9/N2 🟢 — dotfile glob·null→[]·text 크롬 분리(선택).

### 정상 확인(⚪, 회귀 감시 가치)
경로 감옥(lexical `..`+EvalSymlinks)·심링크 탈출 차단·바이너리 NUL 감지·multiedit 원자성·bash timeout/detachTTY·`&` 백그라운드 자식 생존(F1)·CJK 셀 단위 절단·council 구조적 수렴(self-check unverified→쓰기 증거 기반 done)·거짓완료/날조 방어.

> 프로브 스캐폴딩(임시, 관찰 전용, `-run`으로만 실행): `internal/adapter/tool/builtin/audit_probe_test.go`, `audit_probe2_test.go`, `internal/adapter/tui/audit_probe_test.go`. 감사 종료 시 삭제 또는 회귀 테스트로 승격.
