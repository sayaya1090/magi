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
- R6 **종결=finish_reason**(not `[DONE]`): 일부 백엔드(Ollama 클라우드 게이트웨이)가 `[DONE]`을 늦추거나 생략한 채 연결을 열어둬 리더가 벽시계까지 매달리는 걸 방지 — finish_reason(+trailing usage) 도착 시 종료, epilogue grace(`streamEpilogueGrace`)로 backstop.
- R7 **stall 워치독**(`consumeStream`+`streamStallTimeout`, 기본 120s, `MAGI_STREAM_STALL`, 0=off): 백엔드가 요청 수락→200→**아무 이벤트도 안 보내는** hang을 유휴시간(마지막 이벤트 이후 경과)으로 감지해 중단 — 메인 generate의 read가 턴 벽시계(45분)까지 매달리던 것 봉합(실측: cobol-modernization 침묵 hang). **이벤트마다 리셋**이라 토큰/reasoning을 흘리는 느린-생성엔 오발 없음. **첫 토큰 전 침묵**(출력 0)=`streamStep.stalled`→메인 루프가 같은 요청 재발행(`maxStreamStallRetries`=2, 커밋된 출력 없어 안전), 소진 시 에러; **생성 도중 freeze**는 중단하되 부분출력 보존·재시도 안 함. 노트생성 side call(`specMineCall`: spec-mine·curator·check-audit)은 reasoning을 **thinking 하트비트**로 노출(약모델 hang↔사고중 구별).

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

### F-LOOP-STEER — 실행 중 사용자 개입 라우팅 + 자발적 replan
규칙: `turnTask`(넛지·council 앵커)는 step 0에 1회 동결된다. 그래서 실행 *중* 도착한 2번째 사용자 요청은 앵커에 반영되지 않아 에이전트가 진동하며 이미 끝낸 1번을 재실행하는 병목이 있었다.
- R1 **기본=큐잉**: step>0에서 새 `ActorUser` 프롬프트 감지 시(≠현재 turnTask) `pendingInterject` FIFO에 적재 + "요청은 현재 과업 종료 후 처리되도록 큐잉됐으니 현재 과업에 집중" 결정론적 지시 1회 주입. 턴 종료 시 `startRun`이 큐를 드레인해 자기 턴으로 재부상. depth 0·비워크플로만.
- R2 **`route_interjection`**(orchestrator 전용): `redirect`=개입으로 `turnTask` 재앵커 + reground, `append`=현재 과업에 합류(A∪개입) + reground, `queue`=명시적 유지. 흡수(redirect/append)된 개입은 큐에서 제거(`consumeInterject`)돼 재부상 안 함.
- R3 **`replan`**(plan-eligible 전용): 작업 결과가 플랜을 불가능하게 만들 때 새 분해 + 무진전 창 리셋 요청. `honorReplan` 예산 = 턴당 최대 `maxReplansPerTurn`(2) + 직전 replan 이후 실제 툴 작업 없으면 거부(`guard.callCount()` 불변) → 스톨 가드 무한 리셋 불가. 거부 시 가드 유지 + 지시 주입. **툴 광고 게이팅**: `replan`은 `toolSpecs`에서 `planEligible(agent, depth)`(planner on·write-capable·depth<cap)인 에이전트에만 노출 — read-only/최대깊이 서브에이전트에겐 `env.Replan` nil-게이팅과 대칭으로 죽은 툴을 감춤.
- R4 툴 Execute 콜백은 loop-local(`turnTask`/`guard`/`councilRounds`)을 못 만지므로 세션별 `turnControl` 신호만 기록하고 루프가 매 스텝 최상단에서 드레인.
- R5 **큐 유실 방지**: 큐잉된 개입은 턴이 정상 종료면 자기 턴으로 재부상하고, 백엔드 에러/취소로 run 고루틴이 종료돼도 in-memory 맵에 고립되지 않는다 — 남은 개입을 미답변 user 프롬프트로 로그에 영속화(다음 run에서 픽업)하되 실패 중 백엔드로 즉시 재실행하진 않는다(no-retry-storm 유지). run 고루틴 post-loop 블록은 `a.mu`를 잡은 채 실행되므로 큐를 **인라인**으로 검사·삭제(자체 잠금 헬퍼 호출 금지 — 재잠금 시 고루틴 데드락).
- R6 **idle-park aside 핸들러**(`handleAside`): R1의 소프트 지시는 orchestrator가 자기 스텝을 돈다는 전제에 얹힌다. 하지만 이번 턴의 유일한 작업이 백그라운드 explorer뿐이면 루프는 모델 실행 없이 *idle-park*(`awaitingExplorers`, F-ORCH 참조)하므로 소프트 지시가 굶는다(개입이 구두 인정만 받고 배선된 steer 툴이 발화 안 됨). 이 경로에선 **격리 컨텍스트(개입+task 클립만, 전체 트랜스크립트 미포함)의 상한 미니루프**를 돌린다. 노출 툴은 **신호·상호작용 3종뿐** — `route_interjection`·`cancel_dispatch`·`ask_user` — 이고 실행 툴(read/bash/write/`task`)은 **미노출**(격리 턴에서 델리게이트 작업을 벌이면 스타베이션/중복 재발). 즉 미니루프는 *신호만* 한다(잡담 즉답, 또는 route±cancel±clarify); 실제 재-플랜/재-디스패치는 전체 툴이 복원된 다음 정상 스텝이 수행. **enqueue-first**: route는 pending 개입을 요구하므로 미니루프 진입 전 개입을 큐잉한다. 처리 결과별 큐 처분 — routed(redirect/append)는 `turnControl` 드레인이 적용하도록 **큐에 잔류**, 해소된 잡담 응답/단독 cancel은 **여기서 consume**(자기 턴 중복 재실행 방지). "지금 전환" redirect는 `cancel_dispatch` 동반이 기대된다(프롬프트 지시) — 단독 redirect는 무손실이나 explorer 보고 후 synthesis 대상만 재앵커. park **진입 전** 쌓인 개입(예 계획단계)은 같은 핸들러로 park-entry flush(`pendingInterjectTexts` 스냅샷)돼 턴 종료까지 굶지 않는다. depth 0·비워크플로만.

```
steer-queue-1:    A 실행 중 B 도착, route 미호출        ⇒ A 앵커 유지 + B 큐잉, 턴 종료 후 B가 자기 턴
steer-redirect-1: route_interjection redirect            ⇒ turnTask=B로 재앵커, 넛지가 B 재접지 + reground
replan-budget-1:  replan×2 (작업 사이) 후 3번째           ⇒ 캡 도달로 거부, 스톨 가드 유지
replan-nowork-1:  직전 replan 이후 무작업 상태로 재호출    ⇒ 거부(back-to-back churn)
replan-gate-1:    read-only/최대깊이 에이전트 toolSpecs   ⇒ replan 미노출(plan-eligible만 광고)
steer-drain-err-1: 개입 큐잉 상태에서 턴이 에러로 종료      ⇒ 개입은 로그에 영속(유실 X)·즉시 재실행 X
aside-park-chat-1: idle-park 중 잡담 도착                 ⇒ 격리 미니루프 즉답 + consume, 재-park (route X)
aside-park-steer-1: idle-park 중 "docs만" 도착           ⇒ cancel_dispatch+route_interjection redirect, park 깨고 다음 스텝 재-디스패치
aside-park-flush-1: 계획단계 큐잉분이 park 진입 전 존재    ⇒ park-entry flush로 즉시 처리(턴 종료 대기 X)
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
- R4 위원 = `{Name(라벨), Lens(속성), Model, Weight}`. 기본 3인: Melchior(correctness)·Balthasar(verification)·Casper(completeness). (대체 렌즈 spec-fidelity는 설정으로 선택)
- R5 `decision==continue` → `AggregateFeedback(verdicts)`를 `prompt.submitted`(actor=council)로 주입 후 루프 속행(Stop훅과 동일 경로).
- R6 안전: `max_rounds` 초과 시 노트 + 강제 `turn.finished` / 연속 라운드 무진전 감지 시 정지 / depth>0은 기본 비활성.
- R7 이벤트: `council.convened`·`council.verdict`(위원별)·`council.decided` 영속, `council.deliberating` 전이.
- R8 **게이트 범위 = 작업 턴만**: 툴을 하나도 쓰지 않은 대화 턴(인사·질문)은 게이트 스킵 — 작은 대화가 심의 루프에 갇히지 않는다. 언어 잠금은 council/훅이 주입한 user-role 프롬프트가 아니라 **실제 사용자의 마지막 프롬프트** 기준(언어 표류 방지).
- R9 **위원 투표 정책**: 위원은 자기 렌즈로 **구체적·실재하는 결함**(실패 시그널·report가 드러내는 미충족 계약·명백한 오류)을 짚을 때만 `continue`(피드백에 다음 스텝 명시), 과제를 합당히 만족하면 `done`, 렌즈로 판단 불가면 `abstain`. **증거(diff/signal)의 부재 자체는 결코 `continue` 사유가 아니다** — 조사·읽기·분석·응답 턴은 원래 diff가 없으며, 없는 산출물을 요구하는 게 만성 churn의 주원인. 증거는 *있으면* 활용하고, 없으면 report/과제로 판단하거나 abstain. council 증거의 diff는 **untracked 신규 파일 내용까지 포함**(임시 `GIT_INDEX_FILE` 인덱스, 실제 인덱스 불변) → 갓 만든 파일도 증거로 보임.
- R10 **무변경 턴 신호(NoChanges)**: diff가 (성공적으로) 비고 signal도 0이면 그 턴은 **변경 없는 read-only/조사/응답 턴**으로 판정해 `DeliberationRequest.NoChanges`로 council에 알린다 → 위원은 "검증할 산출물이 없는 작업"임을 알고 합당한 report를 승인(R9). **합의규칙은 그대로 보존**(완화/quorum:1 미사용) — 게이트가 돌 땐 언제나 진짜 합의. 단 **GitDiff 실패**(비-git 등)는 "변경 없음"으로 보지 않음(실제 쓰기 턴 오판 방지). 전원 abstain이면 무진전 가드가 종료.
- R11 **독립투표 이후 강화**(각 플래그 기본 on): ①**반박 라운드**(`MAGI_COUNCIL_DEBATE`) — would-be-done이 SPLIT(일부 done/일부 continue)일 때 위원을 1회 재폴링(각자 타 위원 근거 보고 유지/변경) → 한 위원이 잡은 실결함이 코인플립 다수결에 묻히지 않음. ②**데빌**(`MAGI_COUNCIL_DEVIL`) — 무-split done(아무도 반대 안 함)에 지정 적수가 최강의 "미완" 논변을 제기하되, 그 우려를 위원이 **비판적으로 재판정**(데빌은 일부러 결함을 찾으니 과잉·과제-무관일 수 있음) → 실결함이면 continue, 헛것이면 기각. **데빌은 구속력 투표가 아니라 검토 입력**(veto 아님). ③**keep**(`MAGI_COUNCIL_KEEP`, 자문) — 각 위원이 이미 맞는 부분을 명시 → continue 피드백 위에 실려 재작업이 정착된 부분을 되돌리지 않게 함(plan-audit 리비전에도 동일하게 실림).
- R12 **실행 가능한 deliverable-check**(`MAGI_STEP_VERIFY`): plan-audit council이 스텝별 shell 체크(`{step, deliverable, command, expect}`)를 제안 → **실행**되며, 실패 체크는 종결 투표가 못 덮는 하드 `deliverable-check` 시그널. 위임 워커의 **acceptance 체크리스트**로도 쓰이고 TUI 메인 플랜 패널 + council/subagent 상세뷰에 표시(F-PLAN-REC). 세 단계로 강화:
  - **①체크 검증 패스**(`MAGI_CHECK_VALIDATE`, `validateChecks`): 체크가 게이트가 되기 **전에** tool-free 리뷰가 각 체크를 수리/폐기 — 명령이 자기 `expect`를 만족 못 하는 경우(`sort -u`가 동일 두 버전을 하나로 합치는데 expect는 둘을 요구 → 영원히 거짓실패), 미설치 도구(`ss`/`netstat`→python socket), 파일존재-only(→행동실행), **작업≠체크 위반**(아래). best-effort(파싱실패=원본 유지). 작성 council이 자기 체크를 검토하는 "리뷰>자기검토" 원리.
  - **작업과 체크 분리**(멱등·무변경 불변): 체크 절차는 **멱등해야 하고 어떤 상태 변경도 없어야** 함 — 몇 번을 돌려도 결과가 같고 부작용이 없는 read-only 검증. 스텝의 **작업을 수행/재수행/대체하면 안 됨**. 산출물을 *만드는/바꾸는* 명령(압축/다운로드/빌드/생성/이동/삭제: `tar -czf`, `scp`/`rsync`, `rm`, `mv`, 산출물을 쓰는 `>` 리다이렉트, `git commit`)은 **작업이지 체크가 아님** — 체크로 돌리면 매 검증마다 작업을 다시 하고(비멱등), 사소한 일시 오류에도 게이트가 거짓실패해 **재실행 루프**에 빠짐(실증: 원격 벤치 다운로드 스텝의 체크가 `ssh … 'tar -czf …'`로 원격 재압축→exit 1→다운로드 무한 재실행). 대신 **이미 만들어진 산출물을 최종 위치에서** 멱등하게 프로브(`test -s out`, `tar -tzf f.tgz`, 빌드된 바이너리 실행). 중간물이 아니라 스텝이 명시한 **자기 산출물**을 검증.
  - **②스텝-게이트**(`verifyStepChecks`): 위임 워커가 done 보고 후 **그 스텝의 체크**(strict step-label 매칭, 크로스-스텝 오검사 금지)를 실행 → 실패면 done 안 주고 **실패사유 실어 리플랜**(재시작 루프 없음; 워커가 진짜 불가면 blocked/failed+사유 보고). ⇒ **council 도달 = 다 검증됨** 불변식.
  - **③작성 프롬프트**: 체크는 파일존재 아닌 **행동 실행**(서버 실행→포트 응답, 프로그램→출력 대조), task 예제(입력→출력)를 **verbatim 재현**, **이식가능 프로브**만 사용.

```
council-tally-unanimous-1: rule=unanimous, [done,done,continue]      ⇒ continue
council-tally-majority-1:  rule=majority,  [done,done,continue]      ⇒ done
council-tally-tie-1:       rule=majority,  [done,continue]           ⇒ continue (동률→continue)
council-tally-veto-1:      rule=veto(Balthasar), [done,done, Balthasar=continue] ⇒ continue
council-tally-abstain-1:   rule=majority,  [done, abstain, continue] ⇒ continue (abstain 분모 제외 → 1/2)
council-gate-continue-1:   decision=continue ⇒ prompt.submitted(actor=council) 1건 + 루프 속행
council-gate-maxrounds-1:  rounds>max_rounds ⇒ 강제 turn.finished + 노트
council-gate-depth-1:      depth>0           ⇒ 게이트 스킵(바로 종료)
council-gate-skip-1:       툴 미사용 대화 턴 ⇒ 게이트 스킵(council.convened 0)         (R8)
council-abstain-noevid-1:  verification 렌즈 + 시그널·diff 없음 ⇒ abstain(반사적 continue 금지)(R9)
council-evidence-newfile-1: 신규 untracked 파일 생성 ⇒ diff에 파일 내용 포함 ⇒ done 수렴(R9)
council-noevid-noContinue-1: 증거 부재만으로는 continue 금지 ⇒ report/과제로 판단 or abstain (R9)
council-nochanges-1:       diff 성공·공백 + signal 0 ⇒ NoChanges=true, 합의규칙 보존(완화 X)   (R10)
council-nochanges-noterror-1: GitDiff 실패(비-git) ⇒ NoChanges=false(쓰기 턴 오판 방지)         (R10)
council-debate-split-1:    would-be-done + SPLIT ⇒ 반박 1라운드 재폴링 후 재tally               (R11)
council-devil-review-1:    무-split done + 데빌 우려 ⇒ 위원 비판검토 ⇒ 헛 우려면 done 유지        (R11)
council-check-fail-1:      deliverable-check 실행 실패 ⇒ 하드 signal ⇒ continue                 (R12)
```

## F-LOOP-STAGES (루프 트랙) — macro 단계 + stage 태그(D15)
- 단계: `Plan(계약)→Execute→Verify(증거)→Report(주장)→Council(감사)→Finalize`.
- Plan/Report는 **soft 유도**(planner/todos/artifact·report 툴 재사용), Council만 **하드 게이트**.
- 이벤트 봉투 `stage` 태그로 Loop map·rewind·diff가 단계 단위 그룹/타깃. 사소한 턴은 비례적 스킵.

## F-SIGNAL (루프 트랙) — 피드백 시그널 1급화(D16, 부분 출하)
- `port.Signal{source, kind, status(pass|fail), detail}` (현재 형태). 설계 목표는 `{source, kind, verdict, payload, atSeq}`로 확장.
- **출하**: opt-in **다중** 결정적 시그널(`[council] verify` 단축 + `[[council.signal]]` name/command, 예: test·lint·typecheck)을 게이트마다 실행 → 각 `Signal`로 council 증거에 주입, convened 이벤트에 요약 노출(`TestCouncilVerifySignal`/`TestCouncilMultipleSignals`).
- 남음: 훅·진단·report 등 *다른 생애주기*의 결정적 출력도 같은 Signal 모델로 통일.

## F-PLAN (루프 트랙) — 절차 planner + 계획 감사(D17)
시그니처 확장. pre-flight planner를 "solo/parallel 단일판단"에서 **절차 설계기**로. **기본 `[planner] enabled`** 시 동작, 실패는 언제나 solo로 degrade(턴 차단 금지). (write 자식이 자기 레벨에서 재계획하는 재귀·계층 분해는 아래 **F-PLAN-REC / D18**.)

규칙:
- P1 planner는 top-level 턴마다 **1회** tool-free 호출 → 요청을 **순서 있는 절차(steps)**로 분해, 각 step에 전략 `{solo|parallel|scout|delegate|refine}`(delegate/refine은 write-capable 재귀 전략, F-PLAN-REC). 파싱 실패/0 step → solo.
- P2 steps를 **기존 todos로 등록**(계약 통합) → TUI 단일 계획·council 계약 연결. 메인 에이전트는 이를 **이어서 갱신**(통째 replace 금지, findings 주입 메시지로 지시).
- P3 `parallel` = 미리 아는 read-only 조사 그룹 병렬. `scout` = **솔로** explorer로 work-list 확보 → 각 항목을 **병렬**(적응형: fan-out 대상이 런타임 발견). dispatch는 read-only explorer(`explore|locator`)만 — bash 없는 익스플로러라 실행(ssh·명령·원격) 필요 조사는 solo.
- P4 **계획 감사 게이트(심각도 게이팅)**: 절차가 **멀티스텝(2+)**이면 실행 전 council이 *절차*를 감사 — `Phase=plan`. 각 위원은 revise(continue) 시 결함의 **심각도**를 표기: `critical`(이대로면 실패/오답/위험) · `warn`(개선 권고) · `info`(사소). **블로킹은 critical만**(veto — **한 명이라도 critical이면 차단**; 합의규칙 Tally가 아니라 critical 유무로 판정). 누락/불명 심각도 → `warn`(비블로킹)으로 정규화.
  - **critical 있음** → 그 critical 피드백(`CriticalFeedback`)을 **planner 재계획**으로 라우팅(메인 세션 주입 아님), **종료 게이트와 공유하는 `CouncilMaxRounds`**(기본 3) 초과 → 강제 진행(note).
  - **critical 없음** → **승인·진행**(재플랜 루프 없음). warn/info 피드백(`AdvisoryFeedback`)이 있으면 **실행 에이전트가 보도록 시스템 메시지로 1회 주입**(`injectCouncilAdvice`) — 듣고 반영 기대, 비블로킹. (옵션 `[council] plan_absorb`=on이면 planner가 조언을 1회 흡수해 plan 자체를 갱신; 기본 off.)
  - **단일 step·workflow 모드는 감사 스킵**. diff/report/signal 없음(plan 전용 멤버 프롬프트; steps의 구체적 결함만 revise — 검증/수용 기준·테스트·verify 스텝 미명시는 결함 아님, 그건 `criteria` 소관). 비-critical 조언은 **criteria(아래 P6)로 종료 게이트가 검증**.
- P6 **완료기준 도출(계약)**: 계획 감사에서 각 위원이 approve/revise와 함께 자기 렌즈의 **완료기준**(기대 산출물·검증/테스트 지침)을 제안 → 순수 `council.MergeCriteria`(trim·dedup·cap)로 합성 → **승인/강제승인된 plan**의 기준을 그 턴의 `a.criteria`에 저장(재계획 시 최종 plan 것으로 덮어씀, 빈 결과는 미저장). 종료 게이트는 이 캐시를 **계약으로 항상 사용**(plan턴); plan 없는/단일 step 턴은 기존 `[council] criteria` opt-in elicit. D15 acceptance-criteria 아티팩트로 관찰. 결과는 `council.decided`(plan)에 `criteria`로 노출.
- P5 안전: per-fanout cap(`maxPlanGroups`) + **per-turn 총 explorer cap**(`maxPlanExplorers`) + step cap(`maxPlanSteps`) + **per-step degrade**(한 step 실패 → 그 step만 메인 위임).
- P7 **시그니처 채굴(specmine, `MAGI_SPEC_MINE` 기본 ON)**: 플랜 확정 직후 2-패스 사이드 도출 — ①목표-지향 자유 분석("프로즈만으로 짠 구현이 어디서 틀리나"를 이름·타입 시그니처에서) ②엄격 JSON 증류(`surface→요구→표준구조물` ≤5줄 + 무조건 `USE:` 1줄; 캡·단일-승자는 코드 재집행, 파스 1회 재시도, best-effort) → 완성 노트로 메인 세션 주입 + **종료 게이트에 소프트 계약으로 제시**(이탈은 금지가 아니라 심문; `cachedSpecMine`, 턴 리셋 시 소거). `specmine` 에이전트 정의 시 도출을 별도 가중치로 라우팅(노트의 진실성은 도출 모델 믿음에 바운드). 근거: 약한 모델은 메타지시(자기절제·상충해소)를 못 따르고 완성된 결론은 소비함 — 단일-패스는 지면 자기논쟁으로 자충수, 노트-절은 첫-샘플 프레임 불변(2026-07-19 cancel-async 캠페인, 역대 0/10→2-패스 2/2).

```
planner-solo-1:      단순 요청 ⇒ 단일 solo step ⇒ explorer 0 (감사 스킵)
planner-scout-1:     "docs 요약" ⇒ scout(목록)→각 문서 parallel (적응형 fan-out)
plan-audit-approve-1: 멀티스텝 + critical 없음 ⇒ approve → 실행                    (P4)
plan-audit-criteria-1: 승인 plan의 위원 제안 기준 합성 ⇒ a.criteria 저장 → 종료 계약(P6)
plan-audit-warn-1:    revise가 warn/info뿐 ⇒ 재플랜 없이 진행 + 조언 시스템 주입   (P4)
plan-audit-critical-1: critical revise ⇒ critical 피드백 ⇒ planner 재계획          (P4)
plan-audit-cap-1:     연속 critical ⇒ CouncilMaxRounds 도달 ⇒ 강제 진행(note)
```

## F-PLAN-REC (루프 트랙) — 재귀·계층 분해 delegate/refine + 재귀 정책(D18)
절차 planner를 `runLoop` 안에 두어 dispatch된 write 자식이 **자기 레벨에서 다시 planner를 돌린다**(재귀). F-PLAN의 read-only scout/parallel 위에 write-capable 재귀 전략 둘을 얹어 "어려운 문제를 나눠 푼다"를 실제 수행. 경계는 항상 solo로 안전 하강.

**정책 요지**: 기본은 **계획적(up-front) 계층 분해** + 지연(just-in-time) 서브계획이며, 순수 ADaPT의 정의적 메커니즘인 **반응형(as-needed) 실패 재분해는 플래그(`MAGI_ADAPT`)로 게이트**한다(default on=반응형 유지). 반응형을 끄면 HTN식 계획적 계층 분해에 가깝다 — 실패 노드는 재분해 대신 백트랙하고, stall 안전망(`redecomposeStuck`, R4)만 반응형으로 남는다.

규칙:
- R1 **`delegate`(독립 chunk 분할)**: 서로 **독립적** write sub-task를 **컨텍스트-free** producing 자식에 순차 위임(fan-out 안 함 → writes가 council change 캡처와 비경합). 자식은 depth+1 재계획. **반응형 실패 재분해**(ADaPT, `MAGI_ADAPT`로 게이트): 자식 에러/빈결과면 depth·budget 내 **1회** "더 잘게 분해" 재시도, 그래도(또는 게이트 off면 즉시) 실패 시 todo pending + `(delegate FAILED — do this yourself)`(redo-prevention 미억제).
- R2 **`refine`(비독립 sub-goal in-context 재귀)**: 의존적 조각의 대형 sub-goal은 depth+1 재계획하되, 한 계획의 순차 refine phase들이 **하나의 공유 자식 세션**을 순차 재사용(`sharedRefineEnabled` 기본): 첫 phase만 부모 대화를 **복제**(`SpawnRequest.CloneContext`→`cloneConversation`)해 세션을 만들고, 이후 phase(및 로컬 재시도)는 그 세션을 **재사용**(`SpawnRequest.ReuseSession`←`SpawnResult.SessionID`)해 직전 phase의 **실제 대화**(도구 호출·출력·코드) 위에서 이어 작업(spawn-시점 스냅샷이 아님). `MAGI_REFINE_SHARED=0`이면 phase마다 자기 spawn-시점 복제를 받는 legacy로 복귀. 세션 공유와 무관하게 회계는 phase별 불변(depth+1·budget·supervisor)이고 council 캡처는 runLoop 인보케이션 단위(`newRunGuard`)라 phase별 정상. 로컬 재계획→상위 escalate 루프: **success**=todo 완료+`delegated`; **failure**=사유를 부모에 기록(`recordRefineFailure`) 후 **informed 로컬 재시도**(`refineLocalRetries`, 프롬프트에 사유 prefix + 공유 세션이면 실패 대화 위에서 재시도 — 반응형이므로 `MAGI_ADAPT` off면 1회로 축소); **exhaustion**(또는 자식 `STATUS: FAILED` 조기 백트래킹)=todo pending+FAILED 반환→부모가 누적 실패로 재접근.
- R3 **형제 가시성**: 순차 refine phase는 의존적 → 공유 세션에서 뒤 phase가 앞 phase의 실제 작업을 **구조적으로** 이어받음(R2). 각 success는 추가로 결과를 부모(메인 세션) 맥락에 compact seed(`recordRefineSuccess`, `clipLine`)해 부모가 읽는 요약을 남기며, 이는 `MAGI_REFINE_SHARED=0`(phase별 복제) 시 형제 가시성 fallback이기도 하다. 실행자(`agent`)는 optional → 첫 phase에서 `resolveWriteExecutor`로 고정(공유 세션 일관), 없으면 solo.
- R4 **stuck 복구(분해형)**: solo가 stall(무진전 가드 소진)/repeat(루프가드 반복차단 소진, `MAGI_STUCK_DECOMPOSE` 게이트, default off — 원격 벤치 bisect에서 빌드 대기 중 재계획이 벽시계를 소모하는 회귀 확인, =1로 재활성)/council deadlock로 막히면, 복구가 남은 일을 **명시적 TODO 리스트로 재분해해 단위별로 하나씩** 진행한다(`redecomposeStuck`→`driveStuckTodos`): 각 단위는 **부모 대화 전체를 물려받은**(`CloneContext`) 자식이 그 단위만 스코프로 수행(스텝 예산 = 전체/4, floor 8 — 재고착 단위 fail-fast), 착지한 단위의 세션은 다음 단위가 **재사용**(refine 공유세션 패턴), 실패 단위는 todo pending 복귀+체인 리셋 후 계속 — 이미 착지한 단위는 살아남는다. 복구 todo는 기존 플랜에 **append**. 분해 불가(<2단위)면 구식 통째-재스폰 폴백(이젠 이것도 `CloneContext`), 분해가 실행됐는데 전 단위 실패면 폴백 스킵. 성공 시 부모 회계 리셋: stall이면 `resetStall`, repeat이면 `resetRepeat`(blocked 카운터까지 — 아니면 즉시 재정지).
- R5 **경계**: `MaxPlanDepth`(기본 2, `MaxDepth`보다 타이트) 이중 상한 · producing 에이전트(write/edit)만·bash-only 검증자는 재계획 안 함 · 인터랙티브/workflow 억제. fabrication 게이트(council)는 **depth 0만**(리프마다 재실행 X).
- R6 **계획 감사 렌즈**: `[refine]` 스텝은 **의도적 추상**(실행 시 현재 맥락으로 구체화) → 추상성만으론 critical-revise **금지**(최대 warn 권고). 단 refine 포함 계획이 genuinely **unsound**(접근 오류·필수 액션 스텝 부재·달성 불가)면 추상 여부 무관 **여전히 critical**. "부조리는 거부, 단지 추상적인 것은 승인."
- R7 **전개 가드**(`guardExpansion`, 결정적 백스톱, 항상-on — refine→solo 강등만): ①**깊이 캡** `depth+1 ≥ MaxPlanDepth`면 refine 전부 강등 — 캡의 refine는 자식이 재계획 안 하므로(`planEligible`=`depth < MaxPlanDepth`) 영영 전개 불가한 dead-end. ②**순수 재분해 금지** `depth ≥ 1`(이 계획 자체가 refine 전개)인데 구체 **work**(solo/delegate) 없이 refine만이면 강등 — 진전 없는 재이연 방지. depth 0 top-plan은 all-refine 허용(면제). planner 프롬프트가 이 규칙을 먼저 안내(R8)하고, 이 가드가 무시 시 강제.
- R8 **예산·깊이 힌트**(`planEnvelope`): planner 시스템 프롬프트에 **step 예산(`maxSteps`)**·**깊이 `depth`/`MaxPlanDepth`**·**캡 여부**를 주입 → 계획을 예산·깊이에 맞춰 사이징(캡이면 refine 금지 안내). preflight는 각 노드 step 0에서 돌고 자식마다 예산이 리셋되므로 힌트의 요체는 `maxSteps`+깊이.
- R9 **스펙 충실도**(`specFidelityEnabled`, 기본 on; `MAGI_SPEC_FIDELITY=0`=패러프레이즈-only 베이스라인): 깊은-계획 경로가 지시문을 요약하며 **채점기가 verbatim으로 검사하는 리터럴 식별자**(필드/메시지/함수명·출력 포맷·임계값·리터럴)를 잃는 실패 모드(kv-store-grpc: 요청 필드 `value`가 `val`로 정규화 → fail; 얕음/solo는 원문 직독으로 통과)를 3중 방어로 차단. ①**planner 리터럴-보존 규칙**(`literalRule`): planner 시스템 프롬프트에 "정확한 식별자는 step title/task에 verbatim 복제, 리터럴 계약 패러프레이즈 금지" 주입. ②**plan-time note**(`specFidelityNote`): 계획이 실행을 지배하는 순간(`registerPlanTodos` 직후·`executeSteps` 이전) 메인 세션에 "todos는 요약본 — 정확한 식별자는 원문 표현 verbatim" 지시문 주입 → solo·refine clone·parallel/scout findings-합성 경로가 부모 맥락으로 커버(all-solo 포함). ③**delegate SPEC 앵커**(Part C): 컨텍스트-free delegate 자식은 원문을 못 보므로 `delegateBrief`가 goal을 **authoritative SPEC**로 verbatim(넉넉히 clip) 실어 자식이 리터럴을 원문에서 복사. refine엔 미주입(clone+②로 커버).
- R10 **큐레이티드 워커**(컨텍스트 관리 — 최근 핵심 방향, 각 플래그 기본 on): 약모델의 작업 컨텍스트를 얇게 유지하려고 write 작업을 컨텍스트-free 위임 서브에이전트로 실행. ①`MAGI_WORKERS` — write-capable **worker** 로스터 추가. ②`MAGI_FORCE_DELEGATE` — 플래너가 solo로 남긴 write 스텝을 결정적으로 worker에 재라우팅. ③`MAGI_CURATE` — **컨텍스트 큐레이터**(`app/curate.go`, tool-free 엘리시테이션)가 스폰 전에 **구조화 브리프**(goal·progress·task(=키스트로크 아닌 *결과*)·verbatim `literals`·constraints·deliverable)와 **과제-스코프 툴 allowlist**(워커는 항상 base 파일/셸/report 툴 보유, 큐레이터는 전문툴만 추가 → 굶기 불가) 생성. 워커는 **구조화 accountability 리포트**(`STATUS:` + evidence·deviations·**handoff**) 반환, `STATUS: BLOCKED/FAILED` 선행줄(`delegateNotDone`)이 조기 리플랜 구동. 스텝의 **acceptance 체크리스트**(그 스텝의 plan-audit deliverable-check, F-COUNCIL R12)를 워커가 done 보고 전에 실행. 워커 프롬프트는 **`YOUR PART`(자기 슬라이스, 한 번만 진술)** 와 **`CONTEXT`(참고용 — 전체를 하지 말 것)** 를 명시 분리(스코프 혼동 방지).
  - **④공유 산출물 원장**(`ledgerEntry`/`renderLedger`, 세션-스코프): 완료된 각 스텝의 `HANDOFF`(산출물 경로/인터페이스)를 누적해 이후 모든 워커에 **큐레이션 뒤 verbatim 주입**(큐레이터 패러프레이즈가 경로를 못 지움). 자식은 부모 플랜 원장을 봄(`sharedLedger`). 다단계에서 "앞 스텝이 받은 파일 어디있지" 헤맴 방지. TUI 메인 플랜 패널 + 각 워커 상세뷰에 "Shared ledger"로 표시(모두 공유).

```
delegate-partition-1: 독립 3파일 생성 ⇒ 순차 delegate 자식 3 ⇒ depth+1 각자 재계획   (R1)
delegate-adapt-1:     자식 실패 ⇒ "더 잘게" 1회 재시도 ⇒ 실패면 FAILED finding        (R1, MAGI_ADAPT on)
refine-shared-1:      의존 phase 2 ⇒ 한 공유 자식 세션 순차 재사용 ⇒ phase2가 phase1 실제작업 위에서  (R2/R3)
refine-shared-off-1:  MAGI_REFINE_SHARED=0 ⇒ phase마다 자기 spawn-시점 복제(legacy per-phase)        (R2)
refine-sibling-1:     각 success ⇒ 부모 맥락 compact seed(요약 + flag-off fallback)               (R3)
refine-escalate-1:    로컬 재시도 소진 ⇒ FAILED 반환 ⇒ 부모가 재접근(백트래킹)           (R2)
adapt-off-1:          MAGI_ADAPT=0 ⇒ refine 실패 1회 후 백트랙(informed 재시도 없음)      (R1/R2)
stuck-redecompose-1:  solo stall ⇒ TODO 분해 단위별 full-context 자식 진행 + resetStall   (R4)
stuck-redecompose-2:  repeat 차단 소진 ⇒ 동일 분해 복구 + resetRepeat(blocked까지 리셋)   (R4)
stuck-redecompose-3:  분해 실행 후 전 단위 실패 ⇒ 통째-재스폰 폴백 스킵                   (R4)
plan-refine-abstract-1: 추상 refine 계획 ⇒ council critical 금지(warn까지만)             (R6)
guard-depthcap-1:     캡(depth+1≥MaxPlanDepth)의 refine ⇒ solo 강등                       (R7)
spec-fidelity-1:      계획 지배 턴 ⇒ 메인 세션에 스펙-충실도 note + planner에 리터럴 규칙 + delegate SPEC 앵커  (R9)
spec-fidelity-off-1:  MAGI_SPEC_FIDELITY=0 ⇒ note·규칙·앵커 없음(패러프레이즈-only 베이스라인)              (R9)
guard-redefer-1:      depth≥1 all-refine(work 없음) ⇒ solo 강등                          (R7)
plan-envelope-1:      planner 프롬프트에 예산+깊이+캡 힌트 주입                            (R8)
```

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
