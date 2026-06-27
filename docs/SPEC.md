# magi — 기능 명세 (테스트용 예시 포함)

> 각 기능 = **규칙(R)** + **예시 케이스**. 예시는 `given → when ⇒ then` 형식(코드블록)으로,
> Go 테이블 테스트의 한 행에 1:1 대응. 케이스 ID(`read-1` 등)는 테스트 이름으로 그대로 사용.
>
> 표 대신 코드블록을 쓰는 이유: 셀 안 백틱/중괄호/개행이 마크다운 테이블 렌더를 깨므로.
> 표기: `\n`=개행, `ok`=IsError:false, `ERR("...")`=IsError:true + 메시지 포함.
>
> **Part A = M1(깊게)** / **Part B = 이후 마일스톤(윤곽)**.

---

# Part A — M1 기능

## F-TOOL — 빌트인 툴 (Go 구현, POSIX 비의존)

공통 규칙:
- C1 경로는 세션 `workdir` 기준. 내부적으로 `filepath` 정규화.
- C2 **workdir 트리 밖 접근은 기본 거부**(절대경로라도). → `ERR("outside workdir")`.
- C3 에러는 결과로 반환(panic 금지): `ToolResult{IsError:true, Content:"<사유>"}`.

### F-TOOL-READ — 파일 읽기
규칙:
- R1 존재하는 파일 → 내용 반환.
- R2 `offset`/`limit`(1-based 줄 번호) → 해당 줄 범위만.
- R3 없는 파일 → `ERR("file not found")`.
- R4 디렉터리 → `ERR("is a directory")`.
- R5 바이너리(널바이트 포함) → `ERR("binary file")` (내용 안 읽음).

```
read-1: file a.txt="hello\nworld\n"      → read{path:"a.txt"}                 ⇒ "hello\nworld\n", ok
read-2: file a.txt="hello\nworld\n"      → read{path:"a.txt",offset:2,limit:1} ⇒ "world\n", ok
read-3: (no file)                        → read{path:"nope.txt"}              ⇒ ERR("file not found")
read-4: dir "sub/"                       → read{path:"sub"}                   ⇒ ERR("is a directory")
read-5: file img.png has NUL byte        → read{path:"img.png"}               ⇒ ERR("binary file")
read-6: file outside="/etc/passwd"       → read{path:"/etc/passwd"}           ⇒ ERR("outside workdir")
```

### F-TOOL-WRITE — 파일 쓰기(생성/덮어쓰기)
규칙:
- R1 새 파일 생성. 부모 디렉터리 없으면 **자동 생성**.
- R2 기존 파일 **전체 덮어쓰기**.
- R3 workdir 밖 → ERR.
- R4 성공 시 바이트수/경로 반환.

```
write-1: (empty workdir)        → write{path:"new.txt",content:"hi"}      ⇒ ok, file new.txt=="hi"
write-2: (no dir x/y)           → write{path:"x/y/z.txt",content:"a"}     ⇒ ok, dirs created, z.txt=="a"
write-3: file old.txt="old"     → write{path:"old.txt",content:"new"}     ⇒ ok, old.txt=="new"
write-4: (any)                  → write{path:"../escape.txt",content:"x"} ⇒ ERR("outside workdir")
```

### F-TOOL-EDIT — 정확 문자열 치환
규칙:
- R1 `old`가 **정확히 1회** 존재 → `new`로 치환.
- R2 0회 → `ERR("not found")`.
- R3 2회 이상 → `ERR("not unique")` (단 `replaceAll:true`면 전체 치환).
- R4 `old==new` → `ERR("no change")`.
- R5 **기존 EOL(CRLF/LF) 보존**.

```
edit-1: "foo bar baz"     → edit{old:"bar",new:"BAR"}                ⇒ "foo BAR baz", ok
edit-2: "x x x"           → edit{old:"x",new:"y"}                    ⇒ ERR("not unique")
edit-3: "x x x"           → edit{old:"x",new:"y",replaceAll:true}    ⇒ "y y y", ok
edit-4: "abc"             → edit{old:"zzz",new:"y"}                  ⇒ ERR("not found")
edit-5: "abc"             → edit{old:"abc",new:"abc"}                ⇒ ERR("no change")
edit-6: "a\r\nb" (CRLF)   → edit{old:"a",new:"A"}                    ⇒ "A\r\nb", ok (CRLF kept)
```

### F-TOOL-GREP — 정규식 검색
규칙:
- R1 정규식으로 내용 검색. 결과 = `path:line:내용` 리스트.
- R2 `glob`/`path`로 범위 한정.
- R3 매치 없음 → 빈 결과(ok, ERR 아님).
- R4 잘못된 정규식 → `ERR("invalid regex")`.
- R5 바이너리 파일 스킵.

```
grep-1: a.txt="foo\nbar\nfoobar"          → grep{pattern:"foo"}             ⇒ ["a.txt:1:foo","a.txt:3:foobar"], ok
grep-2: a.txt="foo", b.go="foo"           → grep{pattern:"foo",glob:"*.txt"}⇒ ["a.txt:1:foo"], ok
grep-3: a.txt="foo"                       → grep{pattern:"zzz"}             ⇒ [], ok
grep-4: (any)                             → grep{pattern:"[("}             ⇒ ERR("invalid regex")
```

### F-TOOL-GLOB — 파일 패턴 매칭
규칙:
- R1 글롭 패턴 → 경로 목록. **정렬됨**(결정적).
- R2 `**` 재귀 매칭.
- R3 매치 없음 → 빈 목록.
- R4 숨김 제외(기본), `.gitignore` 존중(옵션).

```
glob-1: a.go, b.go, c.txt                 → glob{pattern:"*.go"}        ⇒ ["a.go","b.go"]
glob-2: src/x.go, src/sub/y.go            → glob{pattern:"src/**/*.go"} ⇒ ["src/sub/y.go","src/x.go"]
glob-3: a.txt                             → glob{pattern:"*.md"}        ⇒ []
```

### F-TOOL-LIST — 디렉터리 목록
규칙:
- R1 항목 `{name,isDir}`. 정렬(디렉터리 우선 → 이름순).
- R2 없는 경로 → ERR.
- R3 파일을 list → `ERR("not a directory")`.

```
list-1: dir/{b.txt, a/(dir), c.txt}       → list{path:"dir"}   ⇒ [a/(dir), b.txt, c.txt]
list-2: (no path)                         → list{path:"nope"}  ⇒ ERR("not found")
```

---

## F-STORE — 이벤트소싱 영속 (jsonl 어댑터)

### F-STORE-APPEND — append + seq 부여
규칙:
- R1 세션별 **단조증가 seq**(1부터) 부여해 반환.
- R2 동시 Append도 seq 충돌/중복 없음(직렬화).
- R3 JSONL 파일에 **한 줄 = 한 이벤트**.
- R4 전이(transient) 이벤트는 Append 대상 아님.

```
append-1: empty session s1   → Append(session.created)                ⇒ seq=[1], file has 1 line
append-2: s1 (seq=1)         → Append(prompt.submitted, part.appended)⇒ seq=[2,3], file has 3 lines
append-3: s1                 → 100x Append concurrently (goroutines)  ⇒ all seq unique, no gap/dup
```

### F-STORE-READ-REPLAY — 읽기 + 재생
규칙:
- R1 `Read(s,fromSeq)` → seq 오름차순.
- R2 `fromSeq=0` → 전체 / `fromSeq=N` → seq>N (재접속/late-joiner).
- R3 재생 → Session/Message/Part 복원(F-EVENT-RECON).
- R4 프로세스 재시작 후에도 동일(영속).

```
read-replay-1: s1 has seq 1..4           → Read(s1, 0)  ⇒ 4 events, seq 1,2,3,4
read-replay-2: s1 has seq 1..4           → Read(s1, 2)  ⇒ 2 events, seq 3,4
read-replay-3: write s1, reopen Store    → Read(s1, 0)  ⇒ same 4 events (persisted)
```

### F-STORE-COMPACT — 로그 컴팩션
규칙:
- R1 `Compact(s, upToSeq, snapshot)` → upToSeq 이하를 snapshot 1개로 대체한 **새 파일**.
- R2 원본 보관(`.archive`) 또는 폐기(옵션).
- R3 컴팩션 후 Read → snapshot + 이후 이벤트.

```
compact-1: s1 has seq 1..10  → Compact(s1, 7, snap)  ⇒ Read(s1,0)==[snap, seq8, seq9, seq10]
```

### F-STORE-LIST — 세션 목록
규칙: `ListSessions(workdir)` → 해당 workdir 세션 메타(id, created, lastActivity, title) 최신순.

```
list-sessions-1: /proj has s1,s2; /other has s3  → ListSessions("/proj")  ⇒ [s2, s1] (s3 excluded, newest first)
```

---

## F-EVENT — 이벤트 모델

### F-EVENT-FACT-TRANSIENT — 사실 vs 전이
규칙:
- R1 영속 타입(`session.created/prompt.submitted/part.appended/permission.decided/artifact.emitted/compaction/turn.finished/error`)은 Store 기록.
- R2 전이 타입(`part.delta/tool.started/tool.progress/permission.requested/agent.*`)은 버스만, 기록 안 함.
- R3 모든 이벤트 봉투(seq/sessionId/type/actor/ts/data) JSON 왕복 무손실.

```
fact-1:      bus.Publish(part.delta)        ⇒ Store unchanged (not persisted)
fact-2:      app completes a part           ⇒ exactly 1 part.appended line in Store
roundtrip-1: Event → JSON → Event           ⇒ deep-equal to original
```

### F-EVENT-RECON — 로그→대화 복원
규칙: part.appended를 messageId로 그룹핑, seq 순서로 Message[]/Part[] 재구성. compaction 마커 이후만 컨텍스트.

```
recon-1: log = [session.created, prompt.submitted(user "add a test"),
                part.appended(assistant tool-call read),
                part.appended(tool result)]
         ⇒ Session{1 msg user + 1 msg assistant(tool-call) + 1 msg tool(result)}
```

---

## F-LLM — OpenAI 호환 어댑터 (Ollama/vLLM/LiteLLM)

### F-LLM-SSE — 스트림 파싱
규칙:
- R1 OpenAI SSE(`data: {...}\n\n`) → `ProviderEvent` 매핑.
- R2 `choices[].delta.content` → `text-delta`.
- R3 `data: [DONE]` → `finish`.
- R4 `usage` 청크 → `usage`.
- R5 깨진 JSON 라인 → 스킵(스트림 계속).

```
sse-1: 'data: {"choices":[{"delta":{"content":"Hel"}}]}'  ⇒ {text-delta, "Hel"}
sse-2: 'data: {"choices":[{"delta":{"content":"lo"}}]}'   ⇒ {text-delta, "lo"}
sse-3: 'data: [DONE]'                                     ⇒ {finish}
sse-4: 'data: {bad json'                                  ⇒ skipped, stream continues
```

### F-LLM-TOOLS-NATIVE — 네이티브 tool_calls
규칙: `delta.tool_calls[]`(name+arguments 조각)를 누적, 완성 시 `tool-call`.

```
native-1: tool_calls: name="read", args chunks build {"path":"x"}  ⇒ {tool-call, read, {path:"x"}}
native-2: arguments split across 3 chunks                          ⇒ 1 tool-call after accumulation
```

### F-LLM-TOOLS-FALLBACK — 프롬프트 기반 폴백 ★(gocode 교훈)
규칙:
- R1 네이티브 미지원 모델: 시스템 프롬프트로 "툴은 약속된 JSON 형식으로 출력" 지시.
- R2 어시스턴트 텍스트에서 약속 형식 파싱 → `tool-call`.
- R3 형식 위반/부분 출력 → 1회 repair 재요청, 실패 시 text 처리.
- R4 모드(native/fallback)는 모델별 config 강제 + 자동 감지.

```
fallback-1: assistant outputs fenced block:
              tool_call { "name":"read", "args":{"path":"x"} }
            ⇒ {tool-call, read, {path:"x"}}
fallback-2: assistant outputs "그냥 일반 답변입니다"   ⇒ text part, no tool-call
fallback-3: assistant outputs broken JSON             ⇒ 1 repair retry; if still bad → text part
```

> ⚠️ 이 영역은 **mock SSE 픽스처 단위테스트 + 실제 Ollama 모델 라이브 통합테스트** 둘 다 필수.
> 픽스처만으론 실모델 tool-calling 버그를 놓친다.

### F-LLM-ERROR — 에러 처리
```
llm-err-1: HTTP 500 from server        ⇒ {error} event, propagated to loop
llm-err-2: connection drops mid-stream ⇒ {error} event, partial parts preserved
llm-err-3: invalid base URL            ⇒ StreamChat returns error immediately
```

---

## F-LOOP — 에이전트 루프 (LLMProvider는 페이크 주입)

### F-LOOP-STOP — 종료 조건
규칙:
- R1 tool-call 없으면 종료 + `turn.finished`.
- R2 tool-call 있으면 실행 후 다음 스텝.
- R3 `maxSteps` 도달 시 graceful 종료.
- R4 (D14, 출하 M9) 카운슬 활성(`[council] enabled`) + depth==0 + 비워크플로면 R1의 종료 *직전*에 **council 게이트**가 가로채 done/continue 판정 → continue면 피드백 주입 후 속행. 상세 Part B의 F-COUNCIL.

```
loop-stop-1: fake replies ["안녕"]                       ⇒ 1 step, turn.finished, 1 text part
loop-stop-2: fake replies [tool-call read]→["완료"]       ⇒ 2 steps, tool-result part + text part
loop-stop-3: fake replies infinite tool-calls, maxSteps=3 ⇒ stops after 3 steps
```

### F-LOOP-INTERRUPT — 중단
규칙: ctx 취소(Interrupt) 시 진행 스텝 중단, 부분 결과 보존, interrupted 이벤트.

```
loop-int-1: Interrupt during streaming  ⇒ stop immediately, received text persisted as part.appended
```

### F-LOOP-PERMISSION — 권한 게이팅
규칙:
- R1 위험 툴(write/edit/bash…) 실행 전 `permission.requested` → `RespondPermission` 대기.
- R2 정책 `allow`→자동허용 / `deny`→자동거부 / `ask`→사용자.
- R3 `always` 응답 → 동일 (툴,세션) 이후 자동허용.
- R4 거부 시 tool-result `ERR("denied")`로 모델 피드백.

```
perm-1: policy=ask,   tool=write          ⇒ permission.requested emitted, blocks until response
perm-2: policy=allow, tool=write          ⇒ executes without request
perm-3: policy=ask, user denies           ⇒ tool-result ERR("denied"), loop continues
perm-4: policy=ask, user answers "always" ⇒ 1st write asks, 2nd write auto-allowed
```

### F-LOOP-RECURSION — bounded recursion (D7)
규칙: depth>3 거부 / 동시>8 큐잉 / 누적>50 차단 / 토큰예산 초과 graceful stop / 사이클 감지.

```
rec-1: agent at depth=3      → task spawn       ⇒ rejected ERR("max depth")
rec-2: 8 agents running      → 9th spawn        ⇒ queued (not rejected)
rec-3: cumulative=50 reached → 51st spawn       ⇒ rejected ERR("agent budget")
```

---

## F-COMPACT — 컨텍스트 압축
규칙:
- R1 컨텍스트 토큰이 임계치(모델 window의 X%) 초과 시 자동 압축.
- R2 오래된 메시지 요약 → `compaction` 이벤트 append(원본 보존).
- R3 이후 컨텍스트 = 최신 compaction 요약 + 그 이후 이벤트.
- R4 수동 `Compact` 커맨드도 동일.

```
compact-ctx-1: history over threshold → next turn       ⇒ 1 compaction event, request message count drops
compact-ctx-2: after compaction       → Read(s,0)       ⇒ full history still retrievable (preserved)
compact-ctx-3: Compact command issued                   ⇒ immediate compaction event
```

---

## F-HEADLESS — `-p` 헤드리스 모드
규칙:
- R1 `magi -p "<프롬프트>"` → 세션 생성, 1턴 실행, 결과 stdout.
- R2 `--output text|json`(기본 text). json = JSONL 이벤트 스트림.
- R3 **non-TTY 감지** → TUI/컬러/스피너 비활성(CI 안전).
- R4 종료코드: 성공 0, 에러 비0.
- R5 stdin 파이프로 프롬프트 입력.

```
headless-1: magi -p "hi" --output json  ⇒ JSONL events to stdout, exit 0
headless-2: echo "hi" | magi -p -        ⇒ reads prompt from stdin
headless-3: run via pipe (non-TTY)         ⇒ no ANSI color codes in output
headless-4: LLM error                      ⇒ message to stderr, exit != 0
```

---

# Part B — 이후 마일스톤 (윤곽, 해당 시점 Part A 수준으로 확장)

> 지금 과도 명세 금지(설계 변동 위험). 진입 시 규칙+예시 추가.

## F-COUNCIL (루프 트랙) — 합의 종료 게이트(D14, 출하 M9)
시그니처 기능. 루프 종료 판정을 단일 모델에서 **3인 council**로 옮긴다(상세 PLAN §4.2). **기본 on** — `[council] enabled=false`로 끈다.

규칙:
- R1 `core/council.Tally(verdicts, rule)`는 **순수 함수** — 동일 입력 동일 출력, I/O 없음.
- R2 합의규칙: `unanimous`(전원 done) · `majority`(done>50%) · `quorum:k`(done≥k) · `weighted:θ`(done 가중합/총가중≥θ) · `veto`(지정 위원 거부 시 done 무시).
- R3 **동률·정족수 미달 → continue**(조기종료 방지). `abstain`은 분모에서 제외.
- R4 위원 = `{Name(라벨), Lens(속성), Model, Weight}`. 기본 3인: Melchior(correctness)·Balthasar(verification)·Casper(completeness).
- R5 `decision==continue` → `AggregateFeedback(verdicts)`를 `prompt.submitted`(actor=council)로 주입 후 루프 속행(Stop훅과 동일 경로).
- R6 안전: `max_rounds` 초과 시 노트 + 강제 `turn.finished` / 연속 라운드 무진전 감지 시 정지 / depth>0은 기본 비활성.
- R7 이벤트: `council.convened`·`council.verdict`(위원별)·`council.decided` 영속, `council.deliberating` 전이.

```
council-tally-unanimous-1: rule=unanimous, [done,done,continue]      ⇒ continue
council-tally-majority-1:  rule=majority,  [done,done,continue]      ⇒ done
council-tally-tie-1:       rule=majority,  [done,continue]           ⇒ continue (동률→continue)
council-tally-veto-1:      rule=veto(Balthasar), [done,done, Balthasar=continue] ⇒ continue
council-tally-abstain-1:   rule=majority,  [done, abstain, continue] ⇒ continue (abstain 분모 제외 → 1/2)
council-gate-continue-1:   decision=continue ⇒ prompt.submitted(actor=council) 1건 + 루프 속행
council-gate-maxrounds-1:  rounds>max_rounds ⇒ 강제 turn.finished + 노트
council-gate-depth-1:      depth>0           ⇒ 게이트 스킵(바로 종료)
```

## F-LOOP-STAGES (루프 트랙) — macro 단계 + stage 태그(D15)
- 단계: `Plan(계약)→Execute→Verify(증거)→Report(주장)→Council(감사)→Finalize`.
- Plan/Report는 **soft 유도**(planner/todos/artifact·report 툴 재사용), Council만 **하드 게이트**.
- 이벤트 봉투 `stage` 태그로 Loop map·rewind·diff가 단계 단위 그룹/타깃. 사소한 턴은 비례적 스킵.

## F-SIGNAL (루프 트랙) — 피드백 시그널 1급화(D16, 부분 출하)
- `port.Signal{source, kind, status(pass|fail), detail}` (현재 형태). 설계 목표는 `{source, kind, verdict, payload, atSeq}`로 확장.
- **출하**: opt-in **다중** 결정적 시그널(`[council] verify` 단축 + `[[council.signal]]` name/command, 예: test·lint·typecheck)을 게이트마다 실행 → 각 `Signal`로 council 증거에 주입, convened 이벤트에 요약 노출(`TestCouncilVerifySignal`/`TestCouncilMultipleSignals`).
- 남음: 훅·진단·report 등 *다른 생애주기*의 결정적 출력도 같은 Signal 모델로 통일.

## F-PLUGIN (M3) — Lua 플러그인
- 매니페스트(TOML) 파싱: name/version/capabilities/permissions.
- capability 등록(tool/command/skill/hook/mcp-server/agent/context-provider/ui-panel).
- 샌드박스: `os.execute` 등 차단, `magi.*` 브리지만 노출.
- 권한 집행: 미선언 권한 호출 → 거부.
- **핫리로드**: 파일 변경 → 해당 플러그인만 언로드/재로드, 세션 상태 무손실.
- 예시(추후): 플러그인 로드 시 tool 레지스트리 등장 / 미선언 fs 접근 거부 / 파일 수정 후 N초 내 재로드.

## F-MCP (M4)
- 서버 spawn(stdio) → tools/list 발견 → 레지스트리 등록 → 호출 브리지. 서버 죽으면 툴 제거.

## F-AGENT-MULTI (M5) — 멀티에이전트
- agent capability로 명명 에이전트, task 툴 spawn, 병렬 + event bus 집계.
- 번들 오케스트레이션 플러그인(planner/executor/reviewer), artifact 보고.

## F-ARTIFACT (M5)
- artifact emit → `artifact.emitted` → ui-panel 렌더 → ReviewArtifact(approve/reject).

## F-EXPERIENCE (M5+) — 공유 두뇌(D13)
- Retrieve: 세션 시작 RAG / Propose: 학습·스킬 → 리뷰 큐 → 승인 시 git 커밋/푸시 / 시크릿 레드action.

## F-TUI (M2)
- 대화 렌더(glamour), 입력, 슬래시 커맨드, 권한 다이얼로그, 모델 피커, 세션 목록.

## F-IMAGE (M2+) — D8
- 터미널 능력 탐지 → kitty→iterm2→sixel→반블록 폴백. image part 렌더, ui-panel image.

## F-SCHEDULER (M5+) — D12
- Tier1 인프로세스 ticker(인세션), Tier2 OS 스케줄러 어댑터.

## F-UPDATE / F-DIST (M7)
- goreleaser 멀티타깃, CGO_ENABLED=0. 자동 업데이트(서명 체크섬, Windows rename-교체).
