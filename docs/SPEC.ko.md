# magi — 기능 명세 (역사)

[English](SPEC.md) · [한국어](SPEC.ko.md) · [↑ Docs](README.ko.md)

> ⚠️ **역사적 보존 문서입니다.** 본 명세에 기술된 내용 중 상당수(절차 플래너, 서브에이전트 위임 및 큐레이티드 워커, 사전 억셉턴스 체크와 스텝 게이트, 종료를 투표로 결정하던 카운슬 등)는 실측 평가를 거쳐 간소화되거나 제거되었습니다. **현재 기준 정본은 [`ARCHITECTURE.ko.md`](ARCHITECTURE.ko.md)**, 사용자 기준 정본은 [`MANUAL.ko.md`](MANUAL.ko.md)이며, 충돌 시 해당 문서들이 우선합니다. 본 문서는 초기 설계 결정들이 내려진 배경과 근거를 보존하기 위해 유지합니다.
>
> 데몬, 플릿 뷰, 웹 콘솔(`clients/web/server`)은 본 문서 작성 이후 추가된 기능으로 본 명세에는 포함되어 있지 않습니다. [`ARCHITECTURE.ko.md`](ARCHITECTURE.ko.md) §11 및 [`MANUAL.ko.md`](MANUAL.ko.md) §12를 참고하십시오.
>
> 각 기능은 **규칙(R)**과 **예시 케이스**로 구성됩니다. 예시는 `given → when ⇒ then` 형식(코드블록)으로 Go 테이블 주도 테스트의 한 행에 1:1 대응합니다. 케이스 ID(`read-1` 등)는 해당 규칙을 검증하는 테스트를 찾는 방법입니다 — **Go 소스코드에서 해당 ID를 검색하십시오.**
>
> 이 검색이 보장되는 범위는 **Part A에 한정**되며, 보장하는 주체는 자동화 테스트 코드입니다. `internal/spec/probes_test.go`는 Part A의 ID가 어느 `.go` 파일에도 존재하지 않으면 실패하며, 아직 미발견된 일부 항목은 사유와 함께 테스트 코드 내에 명시되어 있습니다. 단, 두 가지 사항에 유의해야 합니다: ID는 테스트 함수명이 아니라 **대개 테스트 상단 주석에** 위치하며, **Part B의 ID는 대부분 코드베이스에 존재하지 않습니다**(2026-08-29 실측: 88개 중 44개 미존재). Part A 이외의 영역에서 검색 결과가 없는 것은 결함이 아니라 초기 개요 설계 단계의 미구현 상태를 뜻합니다.
>
> 표 대신 코드블록을 사용하는 이유: 셀 내부의 백틱/중괄호/개행으로 인해 마크다운 테이블 렌더링이 깨지는 현상을 방지하기 위함입니다.
> 표기: `\n`=개행, `ok`=IsError:false, `ERR("...")`=IsError:true + 메시지 포함.
>
> **Part A = M1(상세 구현)** / **Part B = 이후 마일스톤(개요 설계)**.

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
아래 두 집합은 단순 표본이 아니라 **전체 어휘 목록**입니다. 클라이언트는 로그와 버스를 이 식별자들로 파싱하므로,
누락된 이름은 클라이언트의 파싱 오류를 유발하고 코드에 없는 이름은 미처리 대기 상태를 발생시킵니다.
`vocab-1` 테스트가 이 어휘 집합을 `event.go`의 선언과 대조하여 정합성을 검증합니다.

`vocab-1`이 검증하는 규칙은 R1·R2 — **어느 타입이 Store 로그에 영속화될 수 있는가**입니다. 특정 이벤트 프레임이
실제로 seq 번호를 동반하여 전달되는지 여부와는 독립적인 규칙이며, 두 개념을 혼동할 경우 R4에서 규정하는
조건부 영속화 케이스에서 파싱 오류가 발생합니다. 두 명세는 상호 연관되어 있으나 엄격히 분리된 규칙입니다.

규칙:
- R1 영속 타입(`session.created` / `prompt.submitted` / `part.appended` / `permission.decided` /
  `compaction` / `result.elided` / `turn.finished` / `todos.changed` / `labels.changed` / `error` /
  `council.convened` / `council.verdict` / `council.decided` / `interjection.deferred` /
  `interjection.answered` / `prompt.abandoned` / `session.moved` / `model.changed`)은 Store에 기록합니다.
- R2 전이 타입(`part.delta` / `tool.progress` / `permission.requested` / `question.requested` /
  `context.usage` / `workflow.phase` / `council.deliberating` / `question.answered` /
  `user.label.changed`)은 버스로만 전송하고 영속 기록하지 않습니다.
- R3 모든 이벤트 봉투(seq/sessionId/type/actor/ts/data) JSON 왕복 무손실을 보장합니다.
- R4 두 집합은 **어느 타입이 Store에 기록될 수 있는가**를 규정하며, **특정 프레임이 seq 번호를 보유하고
  도착하는가**를 직접 결정하지 않습니다. 클라이언트는 `seq == 0` 여부를 수신된 이벤트 봉투의 필드값에서 직접 읽어야 하며
  이벤트 타입으로부터 임의 추론해서는 안 됩니다 — 동일 타입이 상황에 따라 양쪽 형태로 모두 전달되기 때문입니다.
  예컨대 `model.changed`는 App에 Store가 연결되어 있으면 append되어 단조증가 seq가 부여되고,
  Store가 없으면 **동일한 호출이라도** 단순 버스 브로드캐스트로만 전송되어 seq가 0으로 설정됩니다.

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
- R6 **스트림 종결 기준=finish_reason**(`[DONE]` 단독 의존 배제): 일부 백엔드(Ollama 클라우드 게이트웨이 등)가 `[DONE]` 전송을 지연하거나 누락한 채 TCP 연결을 유지하여 스트림 리더가 전체 턴 벽시계 한도까지 블로킹되는 현상을 방지한다 — `finish_reason` 및 후속 `usage` 수신 시 스트림을 정상 종료하며, 잔여 처리는 에필로그 유예 기간(`streamEpilogueGrace`)으로 백스톱한다.
- R7 **Stall 워치독**(`consumeStream`; 2단계 한도의 의도적 분리): 백엔드가 요청을 수락(HTTP 200)한 뒤 **어떠한 이벤트도 전송하지 않고 정지(hang)하는 현상**을 마지막 이벤트 이후 경과한 유휴 시간으로 감지하여 강제 중단한다 — 메인 `generate`의 읽기 대기가 전체 턴 벽시계 한도(45분)까지 무한정 점유되던 결함을 해결했다(실측치: `cobol-modernization` 태스크의 무응답 정지). **이벤트가 도착할 때마다 타이머가 리셋**되므로, 긴 토큰/추론(reasoning) 생성을 스트리밍하는 느린 모델 환경에서도 오작동이 발생하지 않는다. 대기의 성격에 따라 2개의 서로 다른 한도를 적용한다:
  - **첫 토큰 수신 전**: 모델의 프롬프트 프리필(prefill) 연산이 지배하는 대기 구간입니다 — 성능이 높은 로컬 모델이 magi의 ~20k 토큰 규모 프롬프트를 프리필할 경우 수 분이 소요됩니다. 따라서 `firstTokenTimeout`(기본 300s, 환경변수 `MAGI_FIRST_TOKEN`; 0일 경우 토큰 간 한도가 시작부터 일괄 적용)이 관리합니다. 타임아웃 발생 시 `streamStep.stalled` 이벤트를 발행하고 동일 요청을 재시도합니다(`maxStreamStallRetries`=2, 커밋된 출력이 없으므로 안전하게 재발행 가능). 재시도 소진 시 에러로 처리합니다.
  - **생성 진행 중 정지(freeze)**: 첫 토큰 출력 이후 스트리밍 도중 정지되는 구간은 `streamStallTimeout`(기본 120s, 환경변수 `MAGI_STREAM_STALL`, 0=비활성화)이 관리합니다 — 스트림을 즉각 중단하되 이미 수신된 부분 출력을 보존하고 재시도는 수행하지 않습니다. 카운슬 위원의 타임아웃 데드라인 역시 동일한 프리필 특성을 고려하여 기본 3분에 첫 토큰 허용 시간을 합산하여 산출합니다.
- R7b **모델 I/O 단일 보호 계층**(`guardedProvider`, `provider_guard.go`): **모델에 전달되는 모든 요청**(메인 generate, 플래너, 카운슬, 보조 호출 전량)은 인스턴스 생성 시 `GuardProvider`로 래핑된 프로바이더의 **단일 `StreamChat` 초크포인트(chokepoint)**를 통해서만 송수신됩니다(`providerFor`가 반환하는 모든 프로바이더에 가드가 강제됨 — 개별 호출부마다 별도 워치독을 중복 구현하는 비효율 배제). R7의 `consumeStream`(행동 가드: stall-retry, reasoningSpin)이 **메인 generate 단계에서 1차로 동작**하고, `guardedProvider`는 그 상위의 **최종 안전망**(임계값 2배 적용)으로서 자체 방어 로직이 없는 보조 경로들을 일괄 백스톱합니다. 세 가지 실패 모드를 강제 취소합니다:
  - **침묵 백엔드**: 유휴 시간 ≥ 2 × max(`streamStall`, 첫 토큰 한도) — 기본 600s를 적용하여 정상적인 대규모 프리필이 안전망에 의해 조기 차단되는 결함을 원천 방지합니다.
  - **Byte-spin**: 응답 완료 신호 없이 데이터 수신량 ≥ 2 × `spinCap`에 도달할 경우 강제 종료.
  - **Degenerate 반복**: 스트림 꼬리 부분에서 단일 텍스트 청크(≥128B)가 연속으로 3회 이상 동일 반복되는 결함(단어/문장 무한 루프; `MAGI_REPEAT_CAP` 기본 활성화, 꼬리 4KB 버퍼를 256B 간격으로 검사하여 ~800KB 바이트 캡을 기다리지 않고 수백 바이트 내에서 즉시 차단). 순수 공백 줄은 반복 카운트에서 제외하며, 비반복 텍스트는 첫 1바이트 비교에서 불일치 처리되므로 스캔 오버헤드가 극히 낮습니다.
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
            (예: {"address":"A1","text":"…"})            이름 없음/툴이 아닌 이름을 갈라 말합니다. 턴당 최대 3회, 그 뒤엔 text.
            인자 키로 툴을 추측하지 않습니다.
```

> ⚠️ 이 영역은 **mock SSE 픽스처 단위테스트 + 실제 Ollama 모델 라이브 통합테스트** 둘 다 필수적입니다.
> 픽스처만으로는 실제 모델의 tool-calling 버그를 탐지하기 어렵습니다.

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
- R3 **진행 페이스를 임의 제한하는 상한은 존재하지 않으며, 폭주 백스톱만 유지합니다.** 자체 휴리스틱으로 조기 종료를 강제하던 graceful 종료는 벤치마크 실측을 거쳐 전면 제거되었습니다(ARCHITECTURE §4). 현재 유지되는 제한은 `MaxSteps`(기본 **240**)뿐입니다 — 정상적인 작업 턴에서는 도달할 수 없는 충분한 상한선입니다. 이 상한을 모두 소진한 턴은 백스톱 사유를 명시한 `UNVERIFIED` 상태로 종료되며 작업 결과물은 최종 상태 그대로 보존됩니다. 단계별 예산을 명시적으로 선언하는 것은 워크플로 페이즈에 한정됩니다.
- R4 R1의 도구 미호출 종료는 즉각 완료되지 않습니다 — **종료 경로**(`loop_gates.go`, `finishTurn`)가 6개 게이트를 다음 순서로 순차 평가합니다: Stop 훅 → 빈 결과 넛지 → **종료 선언 요구** → 선언 이후 미실행 도구 호출 통지 → 미회수 인계(hand-off) 검증 → 반환된 응답 평가. 하나라도 조건을 충족하지 못하면 턴은 작업 실행 단계로 복귀합니다. 모든 게이트를 통과하여 실제로 종료될 때: 선택적 증류 패스(기본 비활성), 지연된 사용자 개입(interjection) 수거, `finalizeTodos`(진행 중이던 스텝을 실완료 또는 취소로 확정), 그리고 `UNVERIFIED` 사유가 존재할 경우 이를 포함한 `turn.finished` 이벤트를 발행합니다. 카운슬은 게이트 목록에 포함되지 않습니다: 에이전트가 명시적으로 호출하는 도구이며(Part B의 F-COUNCIL), 선언 게이트는 해당 도구가 호출되었는지만 검증합니다.
- R5 **성공한 스텝의 동일 반복 시 턴 자동 종료**(2026-09-07). 텍스트 및 도구 호출(도구명 및 인자)이 직전 스텝과 동일하고, 직전 스텝의 실행이 모두 성공했으며, 그 사이에 신규 프롬프트 이벤트가 유입되지 않았다면 신규 작업 지시가 없는 것으로 판정합니다: 도구를 중복 실행하지 않고 루프 진단 메모를 기록한 뒤 턴을 정상 종료합니다(R1). 실측치: `land` 도구가 성공 회신된 후 동일한 `land` 스텝이 7회 반복 실행되던 루프 차단. 단, 실패 이후의 반복은 정상 재시도로 간주하여 허용합니다. 또한 `council`, `wait_for`, `bash_output`, `ask_user`, `hand_off` 도구는 면제됩니다 — 반복 자체가 고유한 의미를 갖거나 자체 캡을 보유하기 때문입니다.
- R6 **도구 실행 결과에 따른 턴 즉각 종료 지원**(`magi.finish`, `turnControl.finishNow`). 해당 스텝의 도구 호출이 완료된 직후 플래그를 검사합니다: 최종 답변이 이미 작성되어 있다면 그 즉시 종료 경로를 진입하고, 아직 답변이 없으면 플래그를 리셋한 뒤 다음 스텝이 R1 규격에 따라 답변을 생성하며 종료합니다.

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
규칙: `turnTask`(넛지·council 앵커)는 step 0에 1회 동결됩니다. 이에 따라 실행 *중* 도착한 2번째 사용자 요청이 앵커에 반영되지 않아 에이전트가 이미 완료한 1번을 재실행하는 병목 현상을 방지하도록 개선되었습니다.
- R1 **기본=큐잉**: step>0에서 새 `ActorUser` 프롬프트 감지 시(≠현재 turnTask) `pendingInterject` FIFO에 적재 + "요청은 현재 과업 종료 후 처리되도록 큐잉됐으니 현재 과업에 집중" 결정론적 지시 1회 주입. 턴 종료 시 `startRun`이 큐를 드레인해 자기 턴으로 재부상. depth 0·비워크플로만 적용됩니다.
- R2 **`route_interjection`**(orchestrator 전용): `redirect`=개입으로 `turnTask` 재앵커 + reground, `append`=현재 과업에 합류(A∪개입) + reground, `queue`=명시적 유지. 흡수(redirect/append)된 개입은 큐에서 제거(`consumeInterject`)되어 재부상하지 않습니다.
- R4 툴 Execute 콜백은 loop-local(`turnTask`/`guard`)을 직접 변경할 수 없으므로 세션별 `turnControl` 신호만 기록하고 루프가 매 스텝 최상단에서 드레인합니다.
- R5 **큐 유실 방지**: 큐잉된 개입은 턴이 정상 종료되면 자기 턴으로 재부상하고, 백엔드 에러/취소로 run 고루틴이 종료되어도 in-memory 맵에 고립되지 않습니다 — 남은 개입을 미답변 user 프롬프트로 로그에 영속화(다음 run에서 픽업)하되 실패 중 백엔드로 즉시 재실행하지는 않습니다(no-retry-storm 원칙 준수). run 고루틴 post-loop 블록은 `a.mu`를 잡은 채 실행되므로 큐를 **인라인**으로 검사·삭제합니다(자체 잠금 헬퍼 호출 금지 — 재잠금 시 고루틴 데드락).

---

## F-COMPACT — 컨텍스트 압축
규칙:
- R1 컨텍스트 토큰이 임계치(모델 window의 X%) 초과 시 자동 압축. 비율은 `[limits] compact_ratio`(기본 0.8).
- R2 오래된 메시지 요약 → `compaction` 이벤트 append(원본 보존).
- R3 이후 컨텍스트 = 최신 compaction 요약 + 그 이후 이벤트입니다. 압축으로 축약된 상세 내용은 `recall_context`를 통해 복원할 수 있습니다.
- R4 수동 `Compact` 커맨드도 동일하게 동작합니다.
- R4b 샤드는 호출이 지정한 식별자를 기준으로 분할합니다: 파일 경로(모든 도구)와 도구가 **주제라고 선언한** 인자 — 스키마 속성의 `"x-magi-topic": true` — 값마다 샤드 하나(「sheet 매출」「slides 7」)가 생성됩니다. Office 헬퍼는 sheet·slide/slides·paragraph를 선언하도록 구성되었습니다(2026-09-07).
- R5 접기는 비용이 저렴한 계층부터 단계적으로 진행됩니다(2026-09-07). 요약 이전에: 어시스턴트가 이미 설명한 큰 도구 결과를 최신 것부터 스텁으로 축약하고, 여전히 용량이 부족하면 **읽기 전용 도구**(내장 읽기 도구 및 `annotations.readOnlyHint`를 보유한 MCP 도구)의 결과를 오래된 순서대로(최신 3개는 보존) 스텁화합니다. 다시 조회할 수 있는 정보이므로 스텁에 재조회 경로를 명시합니다. 텍스트 요약은 이러한 단계적 축약 후에도 여전히 임계치를 초과할 때만 수행됩니다.
- R6 접을 때 보존하는 후미 컨텍스트는 토큰 단위로 측정합니다 — 예산의 `[limits] compact_keep`(기본 0.25), 최소 사건 6개 기준 — 그리고 브리프는 **누적**됩니다: 기존 브리프는 유지한 채 신규 턴만 요약하여 덧붙이며, 누적 크기가 예산의 1/10을 초과할 때 전체를 한 번에 재압축합니다. 브리프는 정형화된 틀(요청·결정·수행내역·잔여과업·식별자)을 갖추어 작성하며, 「현재 상태가 아닌 수행된 작업의 기록」임을 서두에 명시합니다. `[limits] compact_model`을 통해 별도의 요약 전용 모델을 지정할 수 있습니다.

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
- R6 **카운슬 이의 제기의 정본 증거는 집계 수치가 아닌 피드백 본문입니다**: 헤드리스 로그가 `council round N: continue — a/b`와 같이 **투표 집계(tally)만** 출력할 경우, continue 결정을 유발한 구체적 요구사항은 다음 턴 프롬프트 주입 경로를 통해서만 전달되어 `PromptSubmitted` 노트의 **200자 절단(truncate) 제한**에 걸리는 문제가 있었습니다 — 특히 유지 권고(keep-list)가 피드백 상단에 결합될 경우 200자 제한을 먼저 소진하여, 턴을 지속시킨 핵심 요구사항이 **로그에 기록되지 않는 결함**이 발생했습니다(실측치: 3라운드 연속 continue 사유를 모델의 사후 패러프레이즈 발화로만 역추적할 수 있었던 문제). 이에 따라 `CouncilDecided` 이벤트는 **피드백 본문 자체를 구조화된 case 데이터로 직접 렌더링**합니다 — 줄 단위 제한(최대 12줄 × 줄당 200자, 초과분은 "feedback continues" 표기; `PlanRevised` diff와 동일한 로그 보호 규격). 파일 로그는 사후 진단을 위한 유일한 영속 기록이므로, 단순 집계 수치가 아닌 검증 본문 자체가 보존되어야 합니다.

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
핵심 차별화 기능입니다. 3인의 카운슬 위원이 동일한 실행 기록을 서로 다른 렌즈로 교차 검토하여 판단합니다. **기본값 활성** 상태이며, `[council] enabled=false` 설정을 통해 비활성화할 수 있습니다.

> ⛔ **설계 전환 내역:** 과거에는 루프의 자연종료 시점을 카운슬이 **스스로 가로채는 게이트 방식**이었습니다. 그러나 해당 구조는 카운슬이 객관적으로 결정할 수 없는 두 가지 문제 — **질의 시점**(에이전트가 이미 판단을 내린 순간에 강제 개입)과 **답변 수신 가능성**(헤드리스 모드에서 자문 주입과 `turn.finished`가 동일 틱에 발생하여 미수신) — 를 유발했습니다. 현재는 **에이전트가 `council` 도구를 통해 자발적으로 호출**합니다: `{question}`은 중간 자문, `{complete:true}`는 **종료 선언**이며 카운슬 위원들이 승인하면 루프에 완료 신호가 전달됩니다. 아래 R5, R6, R8은 해당 설계에 맞춰 정립되었습니다.

규칙:
- R1 `core/council.Tally(verdicts, rule)`는 **순수 함수**입니다 — 동일 입력에 대해 동일 출력을 반환하며 I/O 부작용이 없습니다.
- R2 합의 규칙: `unanimous`(전원 완료) · `majority`(완료 > 50%) · `quorum:k`(완료 ≥ k) · `weighted:θ`(완료 가중합/총가중 ≥ θ) · `veto`(지정 위원 거부 시 완료 판정 무효화).
- R3 **동률 또는 정족수 미달 시 continue 판정**(조기 종료 방지). `abstain`(기권)은 분모에서 제외합니다.
- R4 위원 = `{Name(라벨), Lens(속성), Model, Weight}`. 기본 3인: Melchior(correctness)·Balthasar(verification)·Casper(completeness). (대체 렌즈 spec-fidelity는 설정을 통해 선택 가능)
- R5 `decision==continue` 시 위원 피드백(`AggregateFeedback`)이 **도구 실행 결과로** 에이전트에게 반환됩니다(선언이 기각되었다는 안내 메시지와 함께). 단순 자문 호출(`{question}`)인 경우 판정 절차 없이 참고 피드백만 반환하며 투표 표는 렌더링하지 않습니다 — 표 집계는 과거 게이트 방식의 잔재이며, 수치 표시는 다수결을 명령으로 오인하게 만들기 때문입니다.
- R6 안정성 보장: 심의는 에이전트가 직접 호출할 때만 수행되므로 **라운드 무한 폭주가 구조적으로 발생하지 않습니다**. 대신 종료 경로에서 종료 선언을 요구하며(`requireFinishDeclaration`), 해당 요구는 **무진전 구간당 최대 3회**로 제한됩니다 — 마지막 요구 이후 실제 파일 변경(mutation)이 발생하면 허용 예산이 리셋됩니다.
- R7 이벤트 기록: `council.convened`, `council.verdict`(위원별), `council.decided`는 영속 로그에 기록되고 `council.deliberating`은 전이 이벤트로 버스에만 브로드캐스트됩니다. 개별 deliverable-check 실행 결과는 **`step.check`**(step·deliverable·source·assert·exit·pass 구조화 필드; 모델이 커맨드 형식으로 기술했던 흔적은 command/expect에 보존되되 **평가되지 않는 비활성 데이터**)로 영속화됩니다 — 카운슬 투표가 아니므로 라운드/집계 없는 독립 이벤트로 기록·렌더링됩니다(우측 패널 Completion checks는 이 결과에 기반해 ✓/스피너/• 글리프를 표시; 스트림에는 `✓/✗ check [step] deliverable` 한 줄로 출력). 과거 `council.decided`(Forced, note="check […]")에 편입되어 "round 0: finished (no consensus) — 0 done/0 continue"로 오표기되던 문제를 해결했습니다.
- R8 **작업 턴에만 선언을 요구합니다**: 도구를 일체 사용하지 않은 단순 대화 턴(인사, 질문 등)은 선언 요구를 건너뜁니다 — 일상 대화가 선언 검증 루프에 갇히는 결함을 방지합니다.
- R9 **위원 투표 정책**: 위원은 자신의 렌즈에 입각하여 **구체적이고 실재하는 결함**(실패 시그널, 최종 보고서에 드러난 미충족 계약, 명백한 런타임 오류)을 발견했을 때만 `continue`를 투표하며(피드백에 다음 조치 스텝 명시), 과제를 합당하게 완수했으면 `done`, 자신의 렌즈로 판단할 수 없는 영역이면 `abstain`을 행사합니다. **증거(diff/signal)의 부재 자체는 결코 `continue`의 사유가 될 수 없습니다** — 코드 조작이 없는 분석/읽기/조사/응답 턴은 본래 diff가 존재하지 않으며, 존재하지 않는 산출물을 요구하는 것이 만성 루프 낭비(churn)의 주요 원인이었습니다. 증거는 *존재할 때* 활용하고, 없을 때는 보고서와 과제 목표를 종합 평가하거나 기권합니다. 카운슬 증거에 제공되는 diff는 **untracked 신규 생성 파일의 내용까지 포함**하여(임시 `GIT_INDEX_FILE` 인덱스 생성, 실제 작업 트리 인덱스 불변) 갓 생성된 파일도 정상 증거로 인식됩니다.
  - **R9a 목적 중심 전달, 구체적 수단은 위임**: 위원이 자체적으로 검증 절차를 도출할 때(과제가 직접 명시한 리터럴 계약이 *아닌* 카운슬 자체 요구인 경우)는 **무엇이 참임을 입증해야 하는가(목적)만을** 피드백에 명시하고 **어떻게 확인할 것인지는 에이전트의 자율에 맡깁니다** — 특정 명령어(`ps`/`netstat`/`lsof`/`curl` 등)를 고정 요구하지 않습니다(해당 도구가 환경에 설치되어 있지 않으면 목표가 이미 충족되었더라도 영구히 미충족 상태에 갇힙니다: `kv-store-grpc` run17 실측 사례, `ps: not found`). **이미 종단 간(end-to-end) 기능 성공이 확인되었다면**(예: 클라이언트 호출이 정상 응답을 수신함) 이는 "must respond/run" 계약을 **충족**한 것으로 간주합니다 — 그 위에 프로세스/포트 목록을 추가 요구하는 것은 형식적 낭비이며, 실제 기능 성공이 프로세스 목록보다 강력한 증거입니다. (단, **과제 자체가 특정 명령/입출력/수치를 명시**한 경우에는 해당 규격을 엄격하게 요구합니다 — 요약 패러프레이즈로 인한 거짓 완료 방지 원칙 유지.)
  - **R9b 에이전트의 반박 어포던스(CONTEST) — 부당 요구 제거 목적**: continue 피드백이 주입될 때 에이전트에게 반박 수단을 부여합니다 — 카운슬의 요구 중 특정 항목이 **이미 제시된 증거로 충족**되었거나 **명시된 수단이 실행 환경상 불가능**(도구 부재 등)하지만 그 목적 자체는 달성된 경우, 맹목적으로 순응하지 않고 보고서에 `CONTEST: <요구항목> — <이미 충족/불가능에 대한 구체적 증거>` 형식의 단일 라인으로 반박할 수 있습니다. 카운슬은 다음 라운드에 **해당 반박 증거를 우선 심의**합니다: 유효성이 인정되면 해당 요구를 **재발행할 수 없으며**(done으로 투표하거나 *다른* 실재 결함을 명시), 무효(구체적 증거 없는 단순 완료 주장)인 경우 무시합니다. **반박은 부당한 단일 요구사항을 제거할 뿐이며, 반박 성공 자체가 태스크 완료의 증거가 되지는 않습니다** — 나머지 요구사항은 각자의 적합성에 따라 개별 판정되며, 최종 완료 여부는 카운슬이 독립적으로 결정합니다. 거짓 완료를 차단하는 카운슬 본연의 역할을 훼손하지 않기 위함입니다.
- R10 **무변경 턴 신호(NoChanges)**: 파일 diff가 정상적으로 비어 있고 시그널 카운트가 0이면 해당 턴을 **변경 없는 읽기 전용/조사/응답 턴**으로 판정하여 `DeliberationRequest.NoChanges` 플래그를 카운슬에 전달합니다 → 위원은 검증할 산출물이 없는 작업임을 인지하고 정합한 보고서에 대해 승인 투표를 진행합니다(R9). **합의 규칙 자체는 그대로 유지**됩니다(완화나 quorum:1으로 우회하지 않음) — 카운슬이 실행될 때는 항상 진정한 합의를 원칙으로 합니다. 단, **GitDiff 실행 실패**(비 git 디렉터리 등)는 "변경 없음"으로 오판하지 않습니다(실제 파일 쓰기 턴의 누락 방지). 전원 기권 시 무진전 가드가 턴을 안전하게 종료합니다.
- R11 **독립 투표 이후 강화 기전**(각 환경변수 기본 활성화): ①**반박 라운드**(`MAGI_COUNCIL_DEBATE`) — 완료 후보 상태에서 의견이 분열(SPLIT)된 경우 위원들을 대상으로 1회 재질의를 수행하여(타 위원의 판정 및 근거를 열람한 후 입장 유지/변경 결정) 최종 재집계합니다. ②**유지 항목 안내**(`MAGI_COUNCIL_KEEP`) — 위원이 수정을 요구할 때 **이미 올바르게 구현된 부분**도 함께 명시하여 continue 피드백에 참고 정보로 수록합니다(최종 판정 및 집계에는 영향 없음). ⛔ 과거 제안되었던 **악마의 대변인(Devil's advocate)**(`MAGI_COUNCIL_DEVIL`)은 구현에서 제외되었습니다.
> ⛔ **기존 R12(타입드 deliverable-check ①–⑦)와 R13(계약-선행 3단계)은 철거되었습니다.** 체크 저술·검증 패스·스텝 게이트·커버리지 보장·churn 착지·substitution, 그리고 플랜 이전에 계약을 저술하던 카운슬 라운드는 코드베이스에 잔존하지 않습니다. `verifyStepChecks`라는 식별자만 종료 경로에 남아 있으나 다른 역할을 수행합니다. 상세 사유는 [`ARCHITECTURE.ko.md`](ARCHITECTURE.ko.md) §4를 참고하십시오.
- R14 **위원 응답 파싱 및 기권 처리 — 기권은 중립 결과가 아닙니다**(`parseReply`, `jsonx.SalvagePrefix`, `councilRetryReminder`): 위원의 응답을 파싱하지 못할 경우 해당 위원은 **기권**으로 처리되며, 집계 로직은 이를 "렌즈 관점에서 이견 없음"과 **구별하지 못합니다** — 즉 유효한 투표가 조용히 유실되고 소수 의견이 판정을 지배하는 왜곡이 발생합니다. 이에 따라 파싱 실패를 3중 방어로 차단합니다:
  - ①**관용적 파싱(Tolerant parsing)**: 모든 균형 잡힌 JSON 객체 탐색 × `jsonx` 복구 후보 × 필드별 관용 타입 적용 — Go의 기본 디코더는 단 하나의 타입 불일치에도 전체 문서를 파기하므로 필드 하나의 오류가 전체 투표를 무효화하는 결함을 방지합니다.
  - ②**접두 구제(Prefix salvage)**(`jsonx.SalvagePrefix`): 모델의 구문 오류는 전체 문서에 균일하게 발생하지 않고 **특정 단일 컨테이너에 국한**됩니다 — 실측치(11/11 동일 패턴): `criteria` 배열을 `]` 없이 다음 키로 바로 닫아 전체 567바이트 중 563바이트 지점에서 파싱이 중단되었으나, 이미 앞선 12바이트에서 정상 완결되었던 `decision`(심지어 중요 continue 판정)까지 일괄 폐기되는 결함이 있었습니다. 구문 오류 발생 지점 **직전까지의 완성된 데이터**를 보존하고(복구 후보를 선적용하여 다중행 문자열의 개행 문자를 절단점으로 오인하지 않도록 방지하며, 마지막으로 **완성된** 요소까지만 롤백하고 불완전 객체는 안전 폐기) 열려 있는 컨테이너를 강제 종결합니다. 만약 `decision` 필드가 오류 발생 지점 이후에 위치하여 추출할 수 없는 경우에는 **임의 조작 없이 기권으로 처리**합니다. 이 구제 기전은 **손실형(lossy)** 복구이므로 공용 역직렬화 경로(`jsonx.Unmarshal`, `RepairCandidates`)에는 배선하지 않습니다 — 플랜 생성 등에서 불완전 결과가 조용히 성공으로 위장되는 것을 방지하기 위함입니다. 복구 시 발생한 손실 내역은 stderr에 **진단 정보와 함께 명시**됩니다.
  - ③**구조 맞춤형 1회 재질의 리마인더**(`councilRetryReminder`): 단일 리마인더가 모든 파싱 실패 원인을 "불필요한 설명 산문 삽입"으로 단순 가정하던 결함을 개선했습니다 — 순수 JSON 객체를 전송했으나 배열 구문이 어긋난 모델에게 *작성하지도 않은 산문을 제거하라*고 잘못 지시하여, 재시도에서도 동일한 구문 오류를 반복하고 표를 유실하던 문제가 실측되었습니다. 이제 시스템은 로그용으로 산출하던 `jsonx.Diagnose`(오류 오프셋 및 `⟪HERE⟫` 문맥 윈도우)를 모델에게 피드백으로 되먹입니다: 구문 오류의 경우 정확한 위치와 "다음 키 이전에 `[`를 닫으라"는 구체 지침을 안내하고, 스키마 오류(파싱은 되는데 `decision` 없음)의 경우 필수 필드를 명시하며, 산문이 포함된 경우에만 기존 JSON-only 규칙을 재강조합니다.
- R15 **요구사항 교차 점검이 최종 판정에 선행합니다**(`memberPrompt`, `verdictSchema`, `panelSchema`): 위원은 응답 스키마에 `checks[]` 배열을 작성해야 합니다 — 과제가 명시한 각 요구사항에 대해 한 줄씩 `<요구사항> - SATISFIED|UNSATISFIED - <도구 실행 결과에서 직접 인용한 증거 또는 NO-EVIDENCE>` 형식을 충족해야 하며, 이 필드는 스키마상에서 `decision`보다 **물리적으로 앞선 위치**에 배치됩니다. 미리 정해둔 결론에 맞추어 검증 증거를 사후 왜곡하는 현상을 방지하는 구조적 장치입니다. 요구사항 충족을 결론지을 수 있는 유일한 근거는 **도구가 반환한 실제 데이터**뿐이며, 에이전트 자신의 자의적 진술은 효력을 갖지 못합니다. `NO-EVIDENCE` 역시 묵인되는 빈칸이 아니라 영속 기록되는 결측 상태입니다. 이 교차 점검은 **어떠한 예외 없이 무조건 강제**됩니다 — 유지 플래그나 diff 유무에 따라 생략되지 않습니다. 산출물이 없는 턴(R10)일수록 직관에 의존해 성급한 결론을 내릴 위험이 가장 높기 때문입니다.
- R16 **위원 렌즈별 독립 탐색 경로: 관할 분할이 아닌 탐색 우선순위 부여**(`core/council.Routes`, `RouteFor`). `correctness` 렌즈는 과제의 리터럴 요구 문구 → 작업의 기반 전제 → **수치 데이터 자체**(보고된 수치가 과제 *대상* 시스템이 공인하는 실제 값인가) 순으로 점검하며, 의심스러운 수치에 대해 흔히 제시되는 합리화 함정을 경계합니다: **자기일관성(Self-consistency)**(동일 입력에서 도출된 값들은 잘못 파싱되었더라도 상호 일치할 수 있으며, 일관된 오차는 상류의 단일 오류가 유발한 증상일 뿐 정확성의 증거가 아님)과 **에이전트 자체의 해명**(수치가 비정상적으로 보이는 이유에 대한 설명은 검증 대상인 가설일 뿐 그 해소가 아니며, 도구가 참임을 입증하는 데이터를 직접 반환했을 때만 인정). `verification` 렌즈는 실행 동작을 우선 점검합니다 — 실행되어야 할 각 요소의 실제 구동 시점과 반환된 출력을 대조합니다. `completeness` 렌즈는 구성 요소의 완전성을 우선 점검합니다 — 부수적으로 호출된 단발성 작업까지 확인합니다. **세 위원 모두 과제 전체에 대해 독립적으로 최종 판정을 내립니다**: 관할 영역을 분할하는 것은 검증 경로가 부재한 것보다 위험합니다 — 특정 위원의 영역에 결함이 발생하더라도 타 영역 위원 2명의 완료 투표에 의해 다수결로 결함이 은폐될 수 있기 때문입니다. 인식되지 않는 렌즈에는 중립적 표준 경로가 배정됩니다. 탐색 경로 분리의 실측 근거: 단일 지침으로 렌즈 이름만 다르게 부여했던 3인 위원 구성 실험에서 **21회 시행 전량(21/21)**이 단 하나의 이견도 없이 만장일치 완료로 수렴하는 과다승인 결함이 관측되었습니다 — 3개의 독립된 시각이 아닌 단일 편향의 3중 표본에 불과했기 때문입니다.
- R17 **패널 1회 호출 최적화 및 비대칭적 보수성 닫는 호출**(`samePanelBackend`, `pollPanel`, `panelCloseAsk`, `closeSaid`).
  - **단일 배치 호출**: 프로바이더와 모델이 완전히 동일한 경우 **단 1회의 API 호출**로 3인 위원 전원의 점검 및 판정을 통합 수신합니다. 단, 서로 다른 백엔드에 고정된 위원들은 독립된 개별 호출 구조를 유지합니다. 의도적으로 이종 백엔드를 배치한 카운슬을 단일 호출로 통합할 경우 첫 번째 위원의 백엔드가 전체 판정을 대리하는 왜곡이 발생하기 때문입니다. 단일 통합 호출은 **3인 위원에게 단일 타임아웃 데드라인이 적용**됨을 의미합니다: 타임아웃된 패널은 *미응답(unanswered)*으로 명시 기록되며(*파싱 불가*와 명확히 구분), 불완전한 부분 라운드로 은폐되지 않습니다.
  - **종결 호출(Closing call)**: 3인의 독립 점검 결과를 한자리에서 종합 검토하는 제2의 질의입니다. 개별 훑기에서는 드러나지 않는 시스템 전반의 결함을 탐색합니다: 동일 출력에 대한 두 위원 해석 간의 **모순**, 어떤 위원도 다루지 않고 누락한 **요구사항**, 그리고 **자체로 명백히 오류인 수치**(R16 탐색 경로의 최종 안전망). 과거 실험을 통해 단일 반복의 실패가 실증되었습니다: 동일 프롬프트로 동일 증거를 재검토시킨 실험은 11회 소집 중 이견 발생 0건이었고, 보고서를 배제하고 기계적 작업만 부여한 실험 역시 4회 중 이견 0건으로 무력화되었습니다.
  - **비대칭 클램프(Asymmetric clamp)**: 종결 호출이 `continue`를 반환할 경우 기존 위원 집계가 `done`이더라도 해당 라운드는 `continue`로 강제 반전됩니다. 반대의 경우(기존 continue를 done으로 반전)는 **결코 허용되지 않습니다**. 카운슬의 주된 실패 모드는 과다승인이므로, 기존의 보수적 차단 판정을 뒤집을 수 있는 권한을 부여할 경우 완료로 조기 우회하는 결함 경로가 되기 때문입니다. 질의 프롬프트는 중립적으로 구성되어 두 선택지를 동등하게 제시합니다.
  - **모든 판단의 영속 기록**(`Deliberation.Close`, `renderCouncilAdvice` 리드 상단 렌더링, 라운드당 stderr 1행 *agreed with* / *DISAGREED with* 출력): 로그에 명시되지 않는 내부 분기는 사후 진단 시 정상 합의와 미실행 상태를 구별할 수 없게 만들기 때문입니다.

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
- Loop map은 그대로 유지되어 로그로부터 턴 구조를 재구성합니다 — `internal/app/loopmap.go`의 `scanTurns` 및 `/loop` 엔드포인트가 활용합니다. 임의 태그가 아닌 이벤트 자체의 의미를 바탕으로 그룹화합니다.
- **이벤트 봉투의 `stage` 태그는 철회되었습니다**(`d77a064f`, 2026-08-05). 모든 이벤트에 기록되고 모든 로그 파일 라인에 영속화되었으나, 실제 판독부는 `scanTurns` 내에서 `e.Stage == stagePlan`을 검사하는 2곳에 불과했습니다. 그러나 `8eacf04` 커밋에서 단계 체계가 축소된 이후 해당 stage를 설정하는 코드가 존재하지 않았습니다 — `setStage`는 execute 또는 finalize로만 호출되었습니다. 이에 따라 판독부가 비교하는 대상 값은 런타임에 단 한 번도 발생하지 않았고, `loopTurn.planned`는 결코 true가 될 수 없었으며, 이를 통해 렌더링되도록 설계되었던 `◈ plan` 라인은 출력될 수 없었습니다. 따라서 해당 필드와 함께 `setStage`/`currentStage` 및 4개 호출부, `sessionState` 필드와 rewind 로직, 상수 3종 및 렌더러가 함께 제거되었습니다. 외부 클라이언트에서도 해당 필드를 참조하지 않았습니다. 만약 이를 복원하려면 태그를 판독하는 모든 위치에 일관되게 기록해야 하며, 그렇지 않으면 로그 저장 용량만 낭비하고 어떠한 기능도 수행하지 못하는 죽은 필드가 됩니다.

## F-SIGNAL (루프 트랙) — 피드백 시그널 1급화(D16, 철회)
- 설계 목표는 훅·진단·report 등 생애주기 산출물을 `{source, kind, verdict, payload, atSeq}` 한 모델로 통일해 council이 소비하도록 하는 것이었습니다.
- **철회**: 초기에 구현되었던 절반은 설정에 미리 작성한 명령(`[council] verify`, `[[council.signal]]`)을 심의마다 실행하는 구조였습니다. 그러나 설정 파일에 고정된 명령은 앞으로 어떤 태스크가 올지 알 수 없고, 무엇이 그 태스크를 검증하는지는 태스크마다 정해집니다 — 이는 요청에서 유도되는 억셉턴스 크라이테리아·산출물 체크가 이미 하는 일입니다. 생산자는 종료 게이트와 함께 제거되었으며(`e4acdd2`) 나머지도 삭제되었습니다. 복원하더라도 고정 문자열이 아니라 태스크에서 유도하는 방식으로 구현해야 합니다.

## F-PLAN / F-PLAN-REC (루프 트랙) — 절차 planner · 계획 감사 · 재귀 분해 — **철거됨**

> ⛔ **기존 두 절(D17·D18, 합쳐 70여 줄의 R 항목과 테스트 시나리오)은 철거되었습니다.** 기술되었던 내용 중 현재 코드베이스에 잔존하는 것은 없습니다 — 절차 planner와 step별 전략, 실행 전 계획 감사 카운슬(`Phase="plan"`, `runPlanAuditGate`), 완료기준 도출, `delegate`/`refine` 재귀와 공유 자식 세션, `guardExpansion`·`planEnvelope`·`MaxPlanDepth`, `redecomposeStuck` 등이 모두 해당하며 서브에이전트 구조 자체가 존재하지 않습니다.
>
> **철거 배경**: 해당 단계들은 전부 작업이 존재하기도 전에 무언가를 예단하였고, 그 시기 결함은 예외 없이 한 종류였습니다 — magi가 실제로 일어난 일의 기록보다 자기 사전 판단을 믿은 현상입니다([`ARCHITECTURE.ko.md`](ARCHITECTURE.ko.md) §4). 구현되지 않은 명세를 남겨두면 존재하지 않는 인터페이스를 광고하게 되므로 정리되었습니다. 현재 계획은 에이전트 자신의 `todowrite`로 관리되며, 카운슬은 미리 감사하지 않습니다.

## F-PLUGIN (M3) — Lua 플러그인
- 매니페스트(TOML) 파싱: name/version/capabilities/permissions, 그리고 `exec_timeout` —
  플러그인 단일 실행의 `magi.exec` 상한선이며 [1s, 10m] 범위로 클램핑됩니다(기본값 60s는 단순 프로브 기준이었으며, 백엔드 플러그인의 모델 턴은 60초 제한으로 수용할 수 없기 때문입니다). 개별 호출 단위의 `magi.exec(cmd, args, {timeout=...})`는 선언된 상한선보다 단축하는 방향으로만 설정할 수 있습니다.
- capability 등록(tool/command/skill/hook/mcp-server/agent/context-provider/ui-panel).
- 샌드박스: `os.execute` 등 차단, `magi.*` 브리지만 노출.
- 권한 집행: 미선언 권한 호출 → 거부.
- **핫리로드**: 파일 변경 → 해당 플러그인만 언로드/재로드, 세션 상태 무손실.
- 예시(추후): 플러그인 로드 시 tool 레지스트리 등장 / 미선언 fs 접근 거부 / 파일 수정 후 N초 내 재로드.
- **멀티 인스턴스 격리**: 한 머신에서 여러 magi를 동시에 띄우면 기본적으로 **하나의** config 트리(`ConfigDir()/config.toml`)와 data 트리(`DataDir()/plugin-data/<name>.json`, 예: SSO 토큰 캐시)를 공유합니다. 플러그인이 런타임 선택을 영속하면(`set_model`→`config.SetKey`, `store_set`) 한 인스턴스의 쓰기가 다른 인스턴스 파일에 착지하는 충돌점입니다. 두 방어: ①`config.SetKey`/`AppendListItem`은 in-process 뮤텍스에 더해 **크로스-프로세스 O_EXCL 락**(`withFileLock`, Windows 이식성 위해 flock 대신)으로 read-modify-write를 프로세스 간에도 원자화 — 두 인스턴스의 동시 쓰기가 torn-write/lost-update로 config.toml을 깨뜨리지 않음(깨진 config는 TOML 파싱 실패→기동 거부, 즉 조용한 기본값 폴백 없음). ②`MAGI_CONFIG_DIR`/`MAGI_DATA_DIR` 환경변수가 config/data 디렉토리를 인스턴스별로 **완전 분리** → 각자 자기 config.toml·플러그인 토큰 슬롯을 가짐(공유 자체를 없앰).

## F-MCP (M4)
- 서버 spawn(stdio) → tools/list 발견 → 레지스트리 등록 → 호출 브리지. 서버 죽으면 툴 제거.

## F-AGENT-MULTI (M5) — 멀티에이전트 — **철거됨**
> ⛔ 구현 후 실측을 거쳐 철거되었습니다. `task` 툴·spawn·병렬 자식·번들 오케스트레이션 플러그인은 코드베이스에 존재하지 않으며, `ToolEnv`의 `Spawn`/`Dispatch`/`Ask`/`Report`도 함께 정리되었습니다. **현재 아키텍처에서 에이전트는 단일 개체입니다.** 사유는 [`ARCHITECTURE.ko.md`](ARCHITECTURE.ko.md) "에이전트는 하나다" 절을 참고하십시오.

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
