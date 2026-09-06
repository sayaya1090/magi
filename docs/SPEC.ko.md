# magi — 기능 명세 (역사)

[English](SPEC.md) · [한국어](SPEC.ko.md) · [↑ Docs](README.ko.md)

> ⚠️ **역사 문서다.** 여기 기술된 것 중 상당수 — 절차 플래너, 서브에이전트 위임과 큐레이티드 워커,
> 저술된 억셉턴스 체크와 스텝 게이트, 종료를 투표로 결정하던 카운슬, 서브에이전트 리스 — 는
> 걷어냈다. **현재 as-built 기준은 [`ARCHITECTURE.ko.md`](ARCHITECTURE.ko.md)**, 사용자 기준은
> [`MANUAL.ko.md`](MANUAL.ko.md)이며, 충돌 시 그 문서들이 우선한다. 이 문서는 그 결정들이 어떤 근거로
> 내려졌는지의 기록으로 보존한다.

> 데몬, 플릿 뷰, 웹 콘솔(`clients/web/server`)은 여기에 아예 없다 — 이 문서의 유지가 멈춘 뒤에 만들어졌다.
> [`ARCHITECTURE.ko.md`](ARCHITECTURE.ko.md) §11, [`MANUAL.ko.md`](MANUAL.ko.md) §12 참조.

> 각 기능 = **규칙(R)** + **예시 케이스**. 예시는 `given → when ⇒ then` 형식(코드블록)으로,
> Go 테이블 테스트의 한 행에 1:1 대응. 케이스 ID(`read-1` 등)는 그 규칙을 붙드는 테스트를 찾는
> 방법입니다 — **Go 소스에서 ID를 검색하십시오.**
>
> 이 검색이 보장되는 것은 **Part A뿐**이고, 보장하는 주체는 테스트입니다. `internal/spec/probes_test.go`는
> Part A의 ID가 어느 `.go` 파일에도 없으면 실패하며, 아직 못 찾은 몇 개는 사유와 함께 그 안에 적혀
> 있습니다. 대신 두 가지는 약속하지 않습니다: ID는 테스트 이름이 아니라 **대개 테스트 위 주석에**
> 있고, **Part B의 ID는 대부분 코드에 아예 없습니다**(2026-08-29 실측: 88개 중 44개가 어디에도 없음).
> Part A 밖에서는 검색 결과가 없다는 사실이 아무것도 뜻하지 않습니다.
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
아래 두 집합은 표본이 아니라 **어휘 전부**입니다. 클라이언트는 로그와 버스를 이 이름들로 읽으므로,
여기 빠진 이름은 그 클라이언트가 만나고도 모를 줄이고, 여기 있는데 코드에 없는 이름은 영영 기다릴
줄입니다. `vocab-1`이 이 둘을 `event.go`에 붙들어 둡니다.

`vocab-1`이 붙드는 것은 R1·R2 — **어느 타입이 로그에 앉을 수 있는가**입니다. 어느 프레임이 seq를
갖고 오는가에 대해서는 아무 말도 하지 않으며, 두 집합을 커서 규칙으로 읽는 사람은 R4가 이름을 대는
그 타입에서 틀립니다. 두 물음은 하나처럼 보이지만 하나가 아닙니다.

규칙:
- R1 영속 타입(`session.created` / `prompt.submitted` / `part.appended` / `permission.decided` /
  `compaction` / `result.elided` / `turn.finished` / `todos.changed` / `labels.changed` / `error` /
  `council.convened` / `council.verdict` / `council.decided` / `interjection.deferred` /
  `interjection.answered` / `prompt.abandoned` / `session.moved` / `model.changed`)은 Store 기록.
- R2 전이 타입(`part.delta` / `tool.progress` / `permission.requested` / `question.requested` /
  `context.usage` / `workflow.phase` / `council.deliberating` / `question.answered` /
  `user.label.changed`)은 버스만, 기록 안 함.
- R3 모든 이벤트 봉투(seq/sessionId/type/actor/ts/data) JSON 왕복 무손실.
- R4 두 집합은 **어느 타입이 Store에 앉을 수 있는가**를 말하며, **어느 프레임이 seq를 갖고
  오는가**를 말하지 않습니다. 클라이언트는 `seq == 0`을 손에 든 봉투에서 읽어야 하고 타입에서
  유도하면 안 됩니다 — 한 타입이 양쪽으로 다 옵니다. `model.changed`는 App에 Store가 있으면
  append 되어 seq가 찍히고, 없으면 **같은 호출이** 그냥 알리기만 해서 seq가 0입니다.

```
fact-1:      bus.Publish(part.delta)        ⇒ Store unchanged (not persisted)
fact-2:      app completes a part           ⇒ exactly 1 part.appended line in Store
roundtrip-1: Event → JSON → Event           ⇒ deep-equal to original
vocab-1:     위 R1·R2                       ⇒ event.go의 사실 상수 / transientTypes와 정확히 일치
seq-1:       Store 없는 SetModel            ⇒ 버스에 model.changed, seq 0 (R1 타입인데 seq 없음)
seq-2:       Store 있는 SetModel            ⇒ 같은 호출·같은 타입이 버스에 seq를 갖고 옴
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
- R7 **stall 워치독**(`consumeStream`; 한도 둘, 의도적 분리): 백엔드가 요청 수락→200→**아무 이벤트도 안 보내는** hang을 유휴시간(마지막 이벤트 이후 경과)으로 감지해 중단 — 메인 generate의 read가 턴 벽시계(45분)까지 매달리던 것 봉합(실측: cobol-modernization 침묵 hang). **이벤트마다 리셋**이라 토큰/reasoning을 흘리는 느린-생성엔 오발 없음. 두 침묵은 뜻이 달라 한도도 다르다: **첫 토큰 전**은 PREFILL이 지배하는 대기 — 강한 로컬 모델이 magi의 ~20k 토큰 프롬프트를 프리필하면 수 분 — 라서 `firstTokenTimeout`(기본 300s, `MAGI_FIRST_TOKEN`; 0=별도 한도 없음, 토큰-간 한도가 처음부터 적용)이 관장한다. 걸리면 `streamStep.stalled`→같은 요청 재발행(`maxStreamStallRetries`=2, 커밋된 출력 없어 안전), 소진 시 에러. **생성 도중 freeze**(출력 시작 후)는 `streamStallTimeout`(기본 120s, `MAGI_STREAM_STALL`, 0=off)이 관장 — 중단하되 부분출력 보존·재시도 안 함. 카운슬 멤버 데드라인도 같은 prefill 사유로 3분에 첫토큰 값을 더한다.
- R7b **모델 I/O 단일 가드**(`guardedProvider`, `provider_guard.go`): **모델에 대한 모든 요청**(메인 generate·플래너·카운슬·모든 side call)은 생성 시점에 `GuardProvider`로 감싼 provider의 **단일 `StreamChat` chokepoint**를 통해 송수신된다(`providerFor`가 반환하는 것 전부 가드됨 — 각 소비자 워치독의 whack-a-mole 대체). R7의 `consumeStream`(행동 가드: stall-retry·reasoningSpin)이 **메인 generate에서 먼저** 발화하고, guardedProvider는 그 **위 안전망**(임계값 2×)으로 자기 처리가 없는 경로를 backstop한다. 세 실패 모드 취소: **침묵 백엔드**(idle ≥ 2×max(`streamStall`, 첫토큰 한도) — 기본 600s, 정상 prefill이 핸들러 밑의 안전망에 죽는 일이 없게), **byte-spin**(완료 없이 ≥ 2×`spinCap`), **degenerate 반복**(꼬리에 짧은 단위가 back-to-back ≥128B·≥3회 — 같은 문장/단어 무한 loop; `MAGI_REPEAT_CAP` 기본 on, 꼬리 4KB·256B마다 검사, ~800KB byte-cap을 기다리지 않고 수백 B 만에 중단). 순수-공백 단위(빈 줄)는 반복으로 안 봄, 비반복 꼬리는 첫 비교에서 mismatch라 스캔 저렴.
### F-LLM-FALLBACK — 네이티브 지원이 없는 모델의 툴 호출
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
fallback-4: 툴 이름 없는 객체만 출력                    ⇒ 답을 인용해 「이름이 없다」고 되묻는 repair 요청,
            (예: {"address":"A1","text":"…"})            이름 없음/툴이 아닌 이름을 갈라 말한다. 턴당 최대 3회, 그 뒤엔 text.
            인자 키로 툴을 추측하지 않는다.
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
- R3 **페이스를 정하는 상한은 없고, 폭주 백스톱만 있다.** magi 자신의 산수로 걸던 graceful 종료는 측정으로 걷어냈다(ARCHITECTURE §4). 남은 것은 `MaxSteps`, 기본 **240** — 생산적인 턴이라면 절대 닿지 않을 높이다. 이걸 다 쓴 턴은 백스톱을 사유로 실은 UNVERIFIED로 착지하고, 작업물은 놓인 그대로 선다. 자기 예산을 선언하는 것은 워크플로 페이즈뿐이다.
- R4 R1의 조용한 종료는 그대로 끝이 아니다 — **종료 경로**(`loop_gates.go`, `finishTurn`)가 게이트 여섯 개를 이 순서로 돈다: Stop 훅 → 빈 결과 넛지 → **선언 요구** → 선언 뒤 버려진 콜 통지 → 미회수 인계 → 돌아온 답의 평가. 하나라도 걸리면 턴은 작업으로 되돌아간다. 그리고 정말 끝날 때: 선택적 증류 패스(기본 꺼짐), 늦은 인터젝션 수거, `finalizeTodos`(열려 있던 스텝은 진짜 완료면 완료로, 아니면 취소로), 그리고 UNVERIFIED 사유가 있으면 그것을 실은 `turn.finished`. 카운슬은 그 목록에 없다: 에이전트가 부르는 툴이고(Part B의 F-COUNCIL), 선언 게이트는 불렀는지만 확인한다.

- R5 **성공한 단계를 그대로 되풀이하면 턴이 끝난다**(2026-09-07). 글과 호출(이름·인자)이 앞 단계와 같고, 앞 단계의 호출이
  전부 성공했고, 그 사이에 프롬프트 이벤트가 없었으면 새로 시킨 것이 없는 것이다: 호출을 다시 돌리지 않고 루프 메모를 적고
  턴을 끝낸다(R1). 실측: `land` 가 답한 뒤 같은 `land` 단계 일곱 번. 실패 뒤 반복은 재시도라 돈다. `council`·`wait_for`·
  `bash_output`·`ask_user`·`hand_off` 는 면제 — 반복 자체가 뜻이거나 제 캡이 있다.
- R6 **툴이 제가 도는 턴을 끝낼 수 있다**(`magi.finish`, `turnControl.finishNow`). 그 단계의 호출이 돈 직후 읽는다: 답이 이미
  적혀 있으면 거기서 종료 경로를 타고, 아직 없으면 표시는 지워지고 다음 단계가 R1 대로 답을 싣고 끝낸다.

```
loop-stop-1: fake replies ["안녕"]                       ⇒ 1 step, turn.finished, 1 text part
loop-stop-2: fake replies [tool-call read]→["완료"]       ⇒ 2 steps, tool-result part + text part
loop-stop-3: fake replies [tool-call]×N, no declaration    ⇒ finish path asks for the declaration (bounded)
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
- R4 툴 Execute 콜백은 loop-local(`turnTask`/`guard`)을 못 만지므로 세션별 `turnControl` 신호만 기록하고 루프가 매 스텝 최상단에서 드레인.
- R5 **큐 유실 방지**: 큐잉된 개입은 턴이 정상 종료면 자기 턴으로 재부상하고, 백엔드 에러/취소로 run 고루틴이 종료돼도 in-memory 맵에 고립되지 않는다 — 남은 개입을 미답변 user 프롬프트로 로그에 영속화(다음 run에서 픽업)하되 실패 중 백엔드로 즉시 재실행하진 않는다(no-retry-storm 유지). run 고루틴 post-loop 블록은 `a.mu`를 잡은 채 실행되므로 큐를 **인라인**으로 검사·삭제(자체 잠금 헬퍼 호출 금지 — 재잠금 시 고루틴 데드락).

---

## F-COMPACT — 컨텍스트 압축
규칙:
- R1 컨텍스트 토큰이 임계치(모델 window의 X%) 초과 시 자동 압축. 비율은 `[limits] compact_ratio`(기본 0.8).
- R2 오래된 메시지 요약 → `compaction` 이벤트 append(원본 보존).
- R3 이후 컨텍스트 = 최신 compaction 요약 + 그 이후 이벤트. 압축이 떨궈낸 상세는 `recall_context`로 되찾는다.
- R4 수동 `Compact` 커맨드도 동일.
- R4b 샤드는 호출이 이름 댄 것으로 나눈다: 파일 경로(모든 도구)와, 도구가 **주제라고 선언한** 인자 — 스키마 속성의
  `"x-magi-topic": true` — 값마다 샤드 하나(「sheet 매출」「slides 7」). Office 헬퍼가 sheet·slide/slides·paragraph 를
  선언한다(2026-09-07); 그 전엔 Office 대화가 통째로 「discussion」 한 조각이었다.
- R5 접기는 싼 것부터 층으로 간다(2026-09-07). 요약 전에: 어시스턴트가 이미 설명한 큰 도구 결과를 최신 것부터
  스텁으로, 그래도 모자라면 **읽기 전용 도구**(내장 읽기 도구와 `annotations.readOnlyHint` 를 단 MCP 도구)의
  결과를 오래된 것부터 — 최신 셋은 남기고 — 스텁으로. 다시 얻을 수 있는 것이라 스텁이 그 길을 적는다.
  접기(요약)는 그래도 초과일 때만 돈다.
- R6 접을 때 남기는 꼬리는 토큰으로 잰다 — 예산의 `[limits] compact_keep`(기본 0.25), 최소 사건 6개 — 그리고
  브리프는 **누적**된다: 앞 브리프는 그대로 두고 새 턴만 접어 뒤에 붙이며, 예산의 1/10 을 넘으면 그때 한 번
  통째로 줄인다. 브리프는 틀이 있고(요청·결정·한 것·남은 것·이름), 사람의 언어로 쓰며, 「지금 상태가 아니라
  한 일의 기록」이라는 머리말을 단다. `[limits] compact_model` 로 쓰는 모델을 따로 둘 수 있다.

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
- R6 **카운슬 이의는 표가 아니라 본문이 증거다**: 헤드리스 로그는 `council round N: continue — a/b`처럼 **tally만** 찍고, 그 continue를 만든 요구는 다음 턴에 주입되는 프롬프트를 통해서만 흘러 `PromptSubmitted` 노트의 **200자 truncate**에 걸렸다 — 게다가 keep-list 권고가 피드백 **위에** 붙어 그 200자를 먼저 소진하므로, 턴을 계속 열어둔 요구가 **로그 어디에도 남지 않았다**(실측: 3라운드 continue의 주제를 모델의 패러프레이즈로만 역추적 가능). 그래서 `CouncilDecided`가 **피드백 본문 자체를 자기 case로** 렌더한다 — 줄 단위(≤12줄 × ≤200자, 초과분은 "feedback continues"), 이미 `PlanRevised` diff가 같은 이유로 쓰는 방식. 로그는 사후 진단의 유일한 기록이므로 tally는 요약이지 증거가 아니다.

```
headless-1: magi -p "hi" --output json  ⇒ JSONL events to stdout, exit 0
headless-2: echo "hi" | magi -p -        ⇒ reads prompt from stdin
headless-3: run via pipe (non-TTY)         ⇒ no ANSI color codes in output
headless-4: LLM error                      ⇒ message to stderr, exit != 0
headless-5: council continue + 피드백 본문 ⇒ tally 아래 본문 줄단위 렌더(200자 노트 truncate 우회)
headless-6: 장문 피드백                    ⇒ 12줄에서 절단 + "feedback continues" 꼬리(로그 폭주 방지)
```

---

# Part B — 이후 마일스톤 (윤곽, 해당 시점 Part A 수준으로 확장)

> 지금 과도 명세 금지(설계 변동 위험). 진입 시 규칙+예시 추가.

## F-COUNCIL — 에이전트가 부르는 카운슬(D14)
시그니처 기능. 3인 카운슬이 같은 기록을 서로 다른 렌즈로 읽고 답한다. **기본 on** — `[council] enabled=false`로 끈다.

> ⛔ **한 번 뒤집혔다.** 예전엔 루프의 자연종료 지점을 카운슬이 **스스로 가로채는 게이트**였다. 그 배치가 카운슬이 옳게 정할 수 없는 두 가지를 정해버렸다 — **언제** 묻는가(에이전트가 이미 마음을 정한 그 순간)와, 그 답이 읽히기는 하는가(헤드리스에선 자문 주입과 `turn.finished`가 같은 틱이라 안 읽혔다). 지금은 **에이전트가 `council` 툴로 부른다**: `{question}`은 자문, `{complete:true}`는 **종료 선언**이며 위원이 받아들이면 루프에 신호가 간다. 아래 R5·R6·R8은 그에 맞춰 다시 썼다.

규칙:
- R1 `core/council.Tally(verdicts, rule)`는 **순수 함수** — 동일 입력 동일 출력, I/O 없음.
- R2 합의규칙: `unanimous`(전원 done) · `majority`(done>50%) · `quorum:k`(done≥k) · `weighted:θ`(done 가중합/총가중≥θ) · `veto`(지정 위원 거부 시 done 무시).
- R3 **동률·정족수 미달 → continue**(조기종료 방지). `abstain`은 분모에서 제외.
- R4 위원 = `{Name(라벨), Lens(속성), Model, Weight}`. 기본 3인: Melchior(correctness)·Balthasar(verification)·Casper(completeness). (대체 렌즈 spec-fidelity는 설정으로 선택)
- R5 `decision==continue` → 위원 피드백(`AggregateFeedback`)이 **툴 결과로** 에이전트에게 돌아간다(선언이 받아들여지지 않았다는 문장과 함께). 자문 호출(`{question}`)이면 판정 없이 읽을거리만 돌아가고, 표는 렌더하지 않는다 — 표를 세는 건 게이트가 하던 일이고 숫자는 다수를 명령으로 읽게 만든다.
- R6 안전: 심의는 에이전트가 부를 때만 일어나므로 **라운드 폭주 자체가 구조적으로 없다**. 대신 종료 경로가 선언을 요구하고(`requireFinishDeclaration`), 그 요구는 **무진전 구간당 3회**로 경계된다 — 마지막 요구 이후 실제 파일 뮤테이션이 있으면 예산이 재시작한다.
- R7 이벤트: `council.convened`·`council.verdict`(위원별)·`council.decided` 영속, `council.deliberating` 전이. 개별 deliverable-check 실행결과는 **`step.check`**(step·deliverable·source·assert·exit·pass 구조화 필드; 모델이 커맨드 모양으로 저술했던 흔적은 command/expect로 함께 실리되 **평가되지 않는 비활성 데이터**)로 영속 — council 투표가 아니므로 라운드/tally 없는 자기 이벤트로 기록·렌더(우측 패널 Completion checks는 이 결과로 ✓/스피너/• 글리프 표시; 스트림엔 `✓/✗ check [step] deliverable` 한 줄). 과거엔 `council.decided`(Forced, note="check […]")로 실려 "round 0: finished (no consensus) — 0 done/0 continue" 오표기됐던 것 봉합.
- R8 **작업 턴만 선언을 요구받는다**: 툴을 하나도 쓰지 않은 대화 턴(인사·질문)은 선언 요구를 건너뛴다 — 작은 대화가 선언 루프에 갇히지 않는다.
- R9 **위원 투표 정책**: 위원은 자기 렌즈로 **구체적·실재하는 결함**(실패 시그널·report가 드러내는 미충족 계약·명백한 오류)을 짚을 때만 `continue`(피드백에 다음 스텝 명시), 과제를 합당히 만족하면 `done`, 렌즈로 판단 불가면 `abstain`. **증거(diff/signal)의 부재 자체는 결코 `continue` 사유가 아니다** — 조사·읽기·분석·응답 턴은 원래 diff가 없으며, 없는 산출물을 요구하는 게 만성 churn의 주원인. 증거는 *있으면* 활용하고, 없으면 report/과제로 판단하거나 abstain. council 증거의 diff는 **untracked 신규 파일 내용까지 포함**(임시 `GIT_INDEX_FILE` 인덱스, 실제 인덱스 불변) → 갓 만든 파일도 증거로 보임.
  - **R9a 목적만 전달, 방법은 위임**: 위원이 스스로 검증 절차를 발명할 때(카운슬이 만든 요구, 과제가 명시한 리터럴 계약이 *아닌* 경우)는 **무엇이 참임을 보여야 하는가(목적)만** 피드백에 담고 **어떻게 확인할지는 에이전트에 맡긴다** — 특정 조사 명령(`ps`/`netstat`/`lsof`/`curl`)을 못박지 않는다(그 도구가 환경에 없으면 목적이 이미 충족돼도 영원히 미충족이 됨: kv-store-grpc run17 실증, `ps: not found`). **이미 end-to-end 기능 성공이 보이면**(예: 클라이언트 호출이 올바른 응답을 받음) 그것이 "must respond/run"을 **충족**한다 — 그 위에 프로세스/포트 목록을 추가로 요구하는 건 ritual churn이고, 기능 성공은 프로세스 목록보다 강한 증거. (단 **과제가 리터럴 명령/입출력/수치를 명시**한 경우는 그 정확한 것 그대로 요구 — brief-paraphrase false-done 방어는 유지.)
  - **R9b 메인의 반박(CONTEST) — 제거만**: continue 주입 시 메인에게 어포던스를 준다 — 카운슬 요구 중 특정 항목이 **이미 제시된 증거로 충족**됐거나 **명시된 방법이 이 환경에서 불가능**(예: 없는 도구)한데 그 목적은 이미 달성됐다면, 헛되이 순응(churn)하지 말고 리포트에 `CONTEST: <요구> — <이미 충족/불가능이라는 구체 증거>` 한 줄로 되받아친다. 카운슬은 다음 라운드에 **그 증거를 심의**: 유효하면 그 요구를 **재발행 금지**(done 투표하거나 *다른* 실재 결함 명시), 무효(구체 증거 없이 done 재주장)면 무시. **반박은 그 한 항목을 제거할 뿐, 그 자체가 task done의 증거는 아니다** — 나머지 요구는 각자 merit로 판정, done은 여전히 카운슬이 독립 결정. 카운슬 존재이유(거짓 done 차단)를 지키는 격리. [[review-gate-removed]] 계열과 상충 없음(게이트 제거가 아니라 증거-게이트 반박).
- R10 **무변경 턴 신호(NoChanges)**: diff가 (성공적으로) 비고 signal도 0이면 그 턴은 **변경 없는 read-only/조사/응답 턴**으로 판정해 `DeliberationRequest.NoChanges`로 council에 알린다 → 위원은 "검증할 산출물이 없는 작업"임을 알고 합당한 report를 승인(R9). **합의규칙은 그대로 보존**(완화/quorum:1 미사용) — 게이트가 돌 땐 언제나 진짜 합의. 단 **GitDiff 실패**(비-git 등)는 "변경 없음"으로 보지 않음(실제 쓰기 턴 오판 방지). 전원 abstain이면 무진전 가드가 종료.
- R11 **독립투표 이후 강화**(각 플래그 기본 on): ①**반박 라운드**(`MAGI_COUNCIL_DEBATE`) — would-be-done이 SPLIT일 때 위원을 1회 재폴링(각자 타 위원의 판정·근거를 보고 유지/변경) 후 재tally. ②**keep**(`MAGI_COUNCIL_KEEP`) — 위원이 고칠 것과 함께 **이미 맞는 부분**도 지목해 continue 피드백에 자문으로 싣는다(결정·집계엔 무영향). ⛔ 여기 있던 **데빌**(`MAGI_COUNCIL_DEVIL`)은 구현에 없다.
> ⛔ **여기 있던 R12(타입드 deliverable-check ①–⑦)와 R13(계약-선행 3단계)은 삭제했다.** 체크 저술·검증 패스·스텝 게이트·커버리지 보장·churn 착지·substitution, 그리고 플랜 이전에 계약을 저술하던 카운슬 라운드 — 어느 것도 코드에 없다. `verifyStepChecks`라는 이름만 종료 경로에 남아 있는데 그건 다른 일을 한다. 걷어낸 이유는 [`ARCHITECTURE.ko.md`](ARCHITECTURE.ko.md) §4 — 그 단계들은 전부 작업이 존재하기도 전에 무언가를 결정했다.
- R14 **위원 응답 읽기 — 기권은 중립 결과가 아니다**(`parseReply`, `jsonx.SalvagePrefix`, `councilRetryReminder`): 위원 응답이 안 읽히면 그 위원은 **기권**으로 기록되고, tally는 그것을 "내 렌즈로는 할 말 없음"과 **구별하지 못한다** — 즉 던져진 표가 조용히 사라지고 남은 소수가 판정을 대신한다. 그래서 읽기 실패는 세 겹으로 막는다. ①**관용 파싱**(모든 균형 객체 × `jsonx` 복구 후보 × 필드별 관용 타입) — Go는 첫 타입 불일치에서 문서 전체를 포기하므로 한 필드의 모양 하나가 표를 삼킨다. ②**접두 salvage**(`jsonx.SalvagePrefix`): 모델의 구조 실수는 문서 전체에 균일하지 않고 **한 컨테이너에 국한**된다 — 실측(11/11 동일 모양): `criteria` 배열을 `]` 없이 다음 키로 닫아 567바이트 중 563에서 깨지는데, 12바이트에 완결된 `decision`(한 번은 `critical` continue)까지 함께 버려졌다. 구문 오류 지점 **앞까지**를 남기고(복구 후보를 먼저 적용해 다중행 문자열의 raw newline을 절단점으로 오인하지 않음, 마지막 **완성된** 원소까지 되감아 반쪽 객체는 버림) 열린 컨테이너를 닫는다. `decision`이 결함 뒤에 있으면 **살릴 표가 없으므로 기권**(없는 표를 지어내지 않음). 이 복구는 **lossy**라 `jsonx.Unmarshal`/`RepairCandidates`(공유 경로)에 배선하지 **않는다** — 세 번째 스텝에서 잘린 플랜이 "2스텝 플랜"으로 조용히 성공하기 때문(`CloseTruncated`가 span 추출기에만 배선된 것과 같은 선). 손실은 stderr에 **결함 진단과 함께** 명시(성공이지만 criteria/checks가 비었을 수 있음). ③**1회 재폴 리마인더는 모양별**(`councilRetryReminder`): 단일 리마인더가 모든 실패를 "산문으로 감쌌다"로 가정하던 것이 결함이었다 — 맨 객체를 보냈지만 배열이 어긋난 모델에게 *쓰지도 않은 산문을 걷어내라*고 요구했고, 실측에서 재시도는 동일 malformation을 내고 표를 잃었다. 이제 magi가 **이미 로그용으로 계산하던** `jsonx.Diagnose`(오프셋 + `⟪HERE⟫` 창)를 그 결함에 대해 뭘 할 수 있는 유일한 당사자인 모델에게 되먹인다: 구문 오류=위치와 "다음 키 전에 `[`를 닫아라", 스키마(파싱은 되는데 `decision` 없음)=필수 필드 명시, 그 외=기존 JSON-only.

- R15 **요구사항 훑기가 판정보다 앞선다**(`memberPrompt`, `verdictSchema`, `panelSchema`): 위원은 `checks[]`를 쓴다 — 과제가 명시한 요구사항 하나에 한 줄씩 `<요구사항> - SATISFIED|UNSATISFIED - <툴 결과에서 그대로 떼어 온 조각 또는 NO-EVIDENCE>` — 그리고 이 필드는 위원이 채우는 스키마에서 `decision`보다 **앞**에 놓인다. 이미 내려놓은 결론에서 읽기를 거꾸로 조립할 수 없게 하는 배치다. 한 줄을 결론지을 수 있는 것은 **툴이 돌려준 것**이고, 에이전트 자신의 진술은 아무것도 결론짓지 못하며, `NO-EVIDENCE`는 위원이 조용히 넘어가도 되는 빈칸이 아니라 기록되는 답이다. 이 훑기는 **무조건**이다 — keep 플래그나 diff 유무에 걸지 않는다. 산출물이 없는 턴(R10)이야말로 결론부터 내리고 싶은 유혹이 가장 큰 턴이기 때문이다.
- R16 **렌즈마다 경로가 있다: 관할이 아니라 탐색 순서**(`core/council.Routes`, `RouteFor`). `correctness`는 과제의 리터럴 문구 → 작업이 딛고 선 전제 → **값 그 자체**(보고된 수가 과제의 *대상*이 인정하는 수인가) 순으로 훑고, 의심스러운 수에 대해 제시될 두 가지 해명이 전부 함정임을 함께 듣는다. **자기일관성**(같은 입력에서 나온 값들은 그 입력을 잘못 읽었어도 서로 맞으며, 같은 배수로 어긋난 일치는 상류의 한 원인이 낳은 *증상*이지 증거가 아니다)과 **에이전트 자신의 해명**(수가 이상해 보이는 이유에 대한 설명은 심사 대상인 주장의 일부이지 그 해소가 아니며, 툴이 그것이 참임을 보여주는 무언가를 돌려준 경우에만 받아들인다). `verification`은 동작들을 먼저 훑는다 — 돌아야 하는 것 하나하나에 대해 실제로 돌아간 순간과 돌아온 출력. `completeness`는 부분들을 먼저 훑는다 — 지나가듯 한 번 불린 것까지. **셋 다 여전히 과제 전체를 판정한다**: 관할을 나누는 편이 경로가 아예 없는 것보다 나쁘다. 한 위원의 몫 안에 든 결함은 아무것도 모르는 done 둘에 continue 하나로 맞서게 되고 규칙이 그대로 통과시킨다. 인식되지 않는 렌즈에는 중립 경로가 간다. 경로를 넣은 근거: 한 arm 실측에서 렌즈 한 줄만 다르고 나머지 지시가 전부 같은 세 위원은 **21회 중 21회**를 이견 없이 done으로 투표했다 — 세 의견이 아니라 한 의견의 표본 셋이었다.
- R17 **패널 1회 호출, 그리고 한 방향으로만 조이는 닫는 호출**(`samePanelBackend`, `pollPanel`, `panelCloseAsk`, `closeSaid`).
  - **한 번의 호출**이 위원 전원의 훑기와 판정을 싣는다 — 단 provider와 model이 모두 같을 때만이다. 서로 다른 백엔드에 핀된 위원들은 위원별 호출 모양을 유지한다. 일부러 섞어 놓은 카운슬을 한 요청으로 접으면 첫 위원이 지목한 백엔드가 전부를 대신 답하게 되기 때문이다. 한 번의 호출은 **셋에 하나의 데드라인**을 뜻한다: 잘린 패널은 *답하지 않았음*으로 기록되며(*읽을 수 없었음*과 구별된다), 부분 라운드로 위장되지 않는다.
  - **닫는 호출**은 다른 재료에 대한 다른 질문이다. 세 훑기를 한자리에서 보는 유일한 독자이고, 그 자리에서만 보이는 것을 묻는다: 같은 출력에 대한 두 읽기의 **모순**, 어느 훑기도 덮지 않은 **요구사항**, 그리고 **그 자체로 틀린 값**(R16 경로의 백스톱이지 그 소유자가 아니다). 앞선 두 모양이 실패로 이것을 증명했다 — 같은 프롬프트로 같은 증거를 다시 읽힌 판은 11회 소집 중 이견 0, 리포트를 치우고 기계적 일을 준 판은 4회 중 이견 0이었다.
  - **클램프**: `close==continue`가 done 집계 위에 오면 라운드는 continue가 된다. 반대는 결코 적용되지 않는다. 이 카운슬의 실측된 실패 방향은 과다승인이므로, *막고 있는* 집계를 뒤집을 수 있는 결론은 첫 길에 대한 점검이 아니라 done으로 가는 두 번째 길이 된다. 그래서 질문은 중립적으로 던진다 — 두 답을 다 이름 부르고, 어느 쪽도 묻는 목적으로 제시하지 않는다.
  - **어느 쪽이든 기록된다**(`Deliberation.Close`, `renderCouncilAdvice`에서 리드 위에 렌더, 그리고 라운드당 stderr 한 줄로 *agreed with* / *DISAGREED with*): 그 줄을 한 번도 못 본 arm은 동의한 결론과 아예 돌지 않은 결론을 구별할 수 없다.

```
council-tally-unanimous-1: rule=unanimous, [done,done,continue]      ⇒ continue
council-tally-majority-1:  rule=majority,  [done,done,continue]      ⇒ done
council-tally-tie-1:       rule=majority,  [done,continue]           ⇒ continue (동률→continue)
council-tally-veto-1:      rule=veto(Balthasar), [done,done, Balthasar=continue] ⇒ continue
council-tally-abstain-1:   rule=majority,  [done, abstain, continue] ⇒ continue (abstain 분모 제외 → 1/2)
council-gate-continue-1:   decision=continue ⇒ prompt.submitted(actor=council) 1건 + 루프 속행
council-gate-skip-1:       툴 미사용 대화 턴 ⇒ 게이트 스킵(council.convened 0)         (R8)
council-abstain-noevid-1:  verification 렌즈 + 시그널·diff 없음 ⇒ abstain(반사적 continue 금지)(R9)
council-evidence-newfile-1: 신규 untracked 파일 생성 ⇒ diff에 파일 내용 포함 ⇒ done 수렴(R9)
council-noevid-noContinue-1: 증거 부재만으로는 continue 금지 ⇒ report/과제로 판단 or abstain (R9)
council-objective-not-method-1: terminate 프롬프트 ⇒ 목적(무엇을 증명)만 요구·특정 조사명령 미지정·end-to-end 성공 수용 (R9a)
council-contest-affordance-1: continue 주입 ⇒ CONTEST 어포던스 안내 + terminate 프롬프트 ⇒ 반박 심의절(유효 증거면 재발행 금지, done은 독립 판정) (R9b)
council-nochanges-1:       diff 성공·공백 + signal 0 ⇒ NoChanges=true, 합의규칙 보존(완화 X)   (R10)
council-nochanges-noterror-1: GitDiff 실패(비-git) ⇒ NoChanges=false(쓰기 턴 오판 방지)         (R10)
council-debate-split-1:    would-be-done + SPLIT ⇒ 반박 1라운드 재폴링 후 재tally               (R11)
council-salvage-prefix-1:  구문오류 뒤만 손상 + decision 온전 ⇒ 접두 salvage로 표 보존(로그에 손실 명시) (R14)
council-salvage-nodecision-1: decision이 결함 뒤 ⇒ salvage 거부, 기권(없는 표 지어내지 않음)         (R14)
council-salvage-notshared-1: SalvagePrefix ∉ jsonx.Unmarshal/RepairCandidates (lossy, 플랜 조용한 절단 방지) (R14)
council-retry-shape-1:     재폴 리마인더 = 구문/스키마/산문 3분기(Diagnose 되먹임), 단일 산문가정 금지  (R14)council-walk-unconditional-1: keep on/off·diff 유무와 무관하게 훑기를 요구                        (R15)
council-walk-before-verdict-1: 위원/패널 스키마 모두에서 checks[]가 decision보다 앞               (R15)
council-routes-differ-1:   세 경로가 서로 다르고, 어느 것도 관할을 조각으로 좁히지 않음            (R16)
council-panel-once-1:      같은 백엔드의 위원들 ⇒ 단일 호출이 모든 렌즈를 돌려줌                  (R17)
council-panel-split-backend-1: 다른 곳에 핀된 위원 ⇒ 위원별 호출, 조용한 접기 없음                (R17)
council-close-material-1:  닫는 호출이 받는 것은 훑기와 결과이지 에이전트의 리포트가 아님          (R17)
council-close-tightens-1:  close=continue + done 집계 ⇒ continue / close=done + continue 집계 ⇒ continue (R17)
council-close-recorded-1:  닫는 호출이 한 말은 결정을 바꿨든 아니든 라운드와 함께 실림            (R17)
```

## F-LOOP-STAGES (루프 트랙) — macro 단계(D15, stage 태그는 철회)
- 단계: `Plan(계약)→Execute→Verify(증거)→Report(주장)→Council(감사)→Finalize`.
- Plan/Report는 **soft 유도**(planner/todos/artifact·report 툴 재사용), Council만 **하드 게이트**.
- Loop map은 그대로 남아 로그에서 턴을 되읽습니다 — `internal/app/loopmap.go`의 `scanTurns`, `/loop`가 씁니다. 태그가 아니라 이벤트가 스스로 말하는 것으로 묶습니다.
- **이벤트 봉투의 `stage` 태그는 철회했습니다**(`d77a064f`, 2026-08-05). 모든 이벤트에 찍히고 모든 로그 줄에 영속됐지만, 리더는 딱 둘 있었고 둘 다 `scanTurns` 안에서 `e.Stage == stagePlan`을 물었습니다. 그런데 `8eacf04`에서 단계가 빠진 뒤로 그 stage를 세팅하는 곳이 없었습니다 — `setStage`는 execute 아니면 finalize로만 불렸습니다. 그래서 리더가 비교하는 그 하나의 값은 한 번도 나타나지 않았고, `loopTurn.planned`는 참이 될 수 없었으며, 그것이 여닫던 `◈ plan` 줄은 찍힐 수 없었습니다. 필드와 함께 `setStage`/`currentStage`와 그 네 호출부, `sessionState` 필드와 그것을 비우던 rewind, 상수 셋, 렌더가 같이 나갔습니다. Go 바깥에서도 읽는 곳이 없었습니다. 되살린다면 읽는다고 주장하는 모든 자리에 태그를 찍어야 합니다. 아니면 로그 줄마다 값을 치르고 아무것도 답하지 않는 필드입니다.

## F-SIGNAL (루프 트랙) — 피드백 시그널 1급화(D16, 철회)
- 설계 목표는 훅·진단·report 등 생애주기 산출물을 `{source, kind, verdict, payload, atSeq}` 한 모델로 통일해 council이 소비하는 것이었다.
- **철회**: 출하됐던 절반은 설정에 미리 적는 명령(`[council] verify`, `[[council.signal]]`)을 심의마다 돌리는 것이었다. 설정 파일에 적는 명령은 앞으로 어떤 태스크가 올지 알 수 없고, 무엇이 그 태스크를 검증하는지는 태스크마다 정해진다 — 그건 요청에서 유도되는 억셉턴스 크라이테리아·산출물 체크가 이미 하는 일이다. 생산자는 종료 게이트와 함께 제거됐고(`e4acdd2`) 나머지도 삭제했다. 되살린다면 고정 문자열이 아니라 태스크에서 유도해야 한다.

## F-PLAN / F-PLAN-REC (루프 트랙) — 절차 planner · 계획 감사 · 재귀 분해 — **철거됨**

> ⛔ **여기 있던 두 절(D17·D18, 합쳐 70여 줄의 R 항목과 테스트 시나리오)은 삭제했다.** 서술하던
> 것 중 코드에 남은 게 없다 — 절차 planner와 step별 전략, 실행 전 계획 감사 카운슬(`Phase="plan"`,
> `runPlanAuditGate`), 완료기준 도출, `delegate`/`refine` 재귀와 공유 자식 세션,
> `guardExpansion`·`planEnvelope`·`MaxPlanDepth`, `redecomposeStuck`. 서브에이전트 자체가 없다.
>
> **왜 걷어냈는지**: 그 단계들은 전부 작업이 존재하기도 전에 무언가를 결정했고, 그 시기 결함은
> 예외 없이 한 종류였다 — magi가 실제로 일어난 일의 기록보다 자기 사전 판단을 믿은 것
> ([`ARCHITECTURE.ko.md`](ARCHITECTURE.ko.md) §4). 명세를 남겨두면 없는 손잡이를 광고하게 되므로 지운다.
> 지금 계획은 에이전트 자신의 `todowrite`이고, 카운슬은 미리 감사하지 않는다.

## F-PLUGIN (M3) — Lua 플러그인
- 매니페스트(TOML) 파싱: name/version/capabilities/permissions, 그리고 `exec_timeout` —
  플러그인 하나의 `magi.exec` 상한, [1s, 10m] 클램프 (기본 60s는 프로브 기준이었고, 백엔드
  플러그인의 모델 턴은 60초짜리 명령이 아닙니다). 호출 단위 `magi.exec(cmd, args, {timeout=...})`는
  줄이기만 합니다.
- capability 등록(tool/command/skill/hook/mcp-server/agent/context-provider/ui-panel).
- 샌드박스: `os.execute` 등 차단, `magi.*` 브리지만 노출.
- 권한 집행: 미선언 권한 호출 → 거부.
- **핫리로드**: 파일 변경 → 해당 플러그인만 언로드/재로드, 세션 상태 무손실.
- 예시(추후): 플러그인 로드 시 tool 레지스트리 등장 / 미선언 fs 접근 거부 / 파일 수정 후 N초 내 재로드.
- **멀티 인스턴스 격리**: 한 머신에서 여러 magi를 동시에 띄우면 기본적으로 **하나의** config 트리(`ConfigDir()/config.toml`)와 data 트리(`DataDir()/plugin-data/<name>.json`, 예: SSO 토큰 캐시)를 공유한다. 플러그인이 런타임 선택을 영속하면(`set_model`→`config.SetKey`, `store_set`) 한 인스턴스의 쓰기가 다른 인스턴스 파일에 착지하는 충돌점이다. 두 방어: ①`config.SetKey`/`AppendListItem`은 in-process 뮤텍스에 더해 **크로스-프로세스 O_EXCL 락**(`withFileLock`, Windows 이식성 위해 flock 대신)으로 read-modify-write를 프로세스 간에도 원자화 — 두 인스턴스의 동시 쓰기가 torn-write/lost-update로 config.toml을 깨뜨리지 않음(깨진 config는 TOML 파싱 실패→기동 거부, 즉 조용한 기본값 폴백 없음). ②`MAGI_CONFIG_DIR`/`MAGI_DATA_DIR` 환경변수가 config/data 디렉토리를 인스턴스별로 **완전 분리** → 각자 자기 config.toml·플러그인 토큰 슬롯을 가짐(공유 자체를 없앰).

## F-MCP (M4)
- 서버 spawn(stdio) → tools/list 발견 → 레지스트리 등록 → 호출 브리지. 서버 죽으면 툴 제거.

## F-AGENT-MULTI (M5) — 멀티에이전트 — **철거됨**
> ⛔ 지어졌다가 걷어냈다. `task` 툴·spawn·병렬 자식·번들 오케스트레이션 플러그인 어느 것도 코드에 없고
> `ToolEnv`의 `Spawn`/`Dispatch`/`Ask`/`Report`도 함께 나갔다. **에이전트는 하나다.** 사유는
> [`ARCHITECTURE.ko.md`](ARCHITECTURE.ko.md) "에이전트는 하나다".

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
