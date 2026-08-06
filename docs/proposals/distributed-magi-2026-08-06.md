# 제안: 프로세스로 나뉜 magi — 워커, 팀, 그리고 UI 분리

상태: 제안(uncommitted). 브랜치 `engine-ui-split`. 근거는 아래에 출처를 붙였고,
**측정으로 확인한 것과 추정을 구분해 표시**한다.

> 이 문서는 2026-08-06에 나눈 논의의 기록이다. 결론만이 아니라 **틀린 판단과 그것이 뒤집힌
> 근거**도 남긴다 — 뒤집힌 이유가 다음 결정의 재료이기 때문이다.

---

## 0. 한 줄

태스크마다 자기 체크아웃을 가진 인스턴스를 하나 띄우고, 작업을 **커밋 범위**로 돌려받는다.
인스턴스가 로컬 프로세스든 원격 머신이든 클라우드 VM이든 **모양은 같다**.

```
태스크 → 인스턴스 하나 (로컬 프로세스 / 원격 머신 / 클라우드 VM)
        → 자기 클론에서 작업
        → base..head 커밋 범위로 반환
        → 대장이 review로 판정, merge_child로 병합
        → UI는 필요할 때만 붙는다
```

**이 그림에 필요한 코어 기능은 이미 다 있다.** 남은 것은 조립(플러그인)과, UI를 붙였다 떼는
것(§5)뿐이다.

---

## 1. 무엇이 이미 있나 (2026-08-06 기준, 전부 이 날 커밋됨)

| 조각 | 어디 |
|---|---|
| 자식 실행 | `magi.spawn{system, prompt, tools, max_steps, timeout}` |
| 발자국 읽기 | `magi.child_steps(sid)` — 이름·인자·성패, **실패 출력은 원문** |
| 되돌리기 | `magi.restore_child(sid)` — 저널→git→사가, **못 되돌린 것 보고** |
| 자기 체크아웃 | `magi.spawn{workspace="clone"}` — 부모의 **미커밋 작업까지** 실어감 |
| 커밋 기반 병합 | `magi.merge_child(sid)` — `base..head`를 `git apply --3way` |
| 종료 판정 | `magi.spawn{review=function(round,text,steps)}` — 같은 세션으로 되돌려보냄 |
| 미완 턴 감지 | `App.UnfinishedTurnOf` — 크래시 후 무엇이 끊겼는지 |
| 원격 실행 | `ssh host magi -p "<지시>"` — **벤치 어댑터가 이미 이렇게 쓴다** |

원격 실행에 새 기전이 필요 없다는 점이 중요하다. `bench/harbor/magi_agent.py`가
`session.copy_to_container(binaries, …)` 로 정적 바이너리를 밀어넣고
`magi -p … --permission allow --model X` 를 돌린다. **네트워크 없는 환경에서 도는 워커
드라이버가 이미 동작 중**이고, 클라우드 VM 드라이버는 컨테이너가 VM으로 바뀔 뿐이다.

---

## 2. 뒤집힌 판단: "머신이 갈라지면 트리 문제가 나빠진다"

**내가 처음 주장한 것**: 코딩 에이전트의 컨텍스트는 저장소 자체이므로, 머신이 갈라지면 그게
사라진다. 선택지는 공유 파일시스템 / 클론+병합 / 패치 전송인데 전부 비싸고, 특히 클론+병합은
"사실상 분산 VCS를 짜는 일"이다.

**틀렸다.** 클론+병합은 짜는 게 아니라 **git을 쓰는 것**이고, 그건 개발자가 매일 하는 일이다.

정정하고 보면 방향이 반대다:

| | 한 머신 | 여러 머신 |
|---|---|---|
| 격리 | **만들어야 함** — worktree, 파일 락, 외부 디렉토리 권한 | **공짜** — 각자 자기 클론 |
| 병합 | 도구가 자체 구현 | **git** |
| 충돌 | "격리했으니 안 난다"는 주장 | 개발자 둘이 같은 파일 고친 것과 동일 |

한 머신 도구들이 worktree 기계를 만드는 이유는, 여러 머신에 **처음부터 있는 것**을 흉내
내려는 것이다.

**"아무도 여러 머신을 안 한다"도 틀렸다.** Tier 3(Claude Code Web, GitHub Copilot Coding
Agent, Jules, Codex Web)이 정확히 그것이다 — 태스크마다 VM 하나, 결과는 PR. 이름이
"클라우드 에이전트"라 "팀 모드"로 검색해서 놓쳤다.

**교훈**: 한 도구의 해법을 보고 그 해법이 푸는 문제를 본질적 난제로 옮겨 붙이지 말 것.
worktree는 *한 머신이라서* 생긴 문제의 답이었다.

---

## 3. 그래서 남는 진짜 문제 셋 (트리가 아니다)

1. **미커밋 작업.** git은 커밋된 것만 옮긴다. 워커가 대장의 최신 상태를 보려면 먼저
   커밋하거나 WIP 브랜치를 푸시해야 한다. 마찰이지만 정상 관행이다.
   → 한 머신 안에서는 이미 해결했다: 클론이 부모의 미커밋 변경(추적분은 패치, untracked는
   복사, ignored는 제외)을 실어간다. **여러 머신에서는 사람이 커밋해야 한다.**
2. **환경 동등성.** 머신마다 툴체인·의존성·**모델 엔드포인트**가 있어야 한다. 없으면 워커가
   빌드도 테스트도 못 해 **검증 안 된 코드**가 돌아온다. ★이것이 한 머신 도구들이 여기 오지
   않는 진짜 이유로 보인다(추정) — 노트북 사용자는 설치 한 번으로 쓰길 원한다.
3. **전송.** magi는 데몬이 없고 `magi.serve`는 **127.0.0.1 전용**(`bridge_net.go:197`).
   그래서 ssh다.

**클라우드는 2번을 없앤다.** 인스턴스가 클라우드 모델 API를 때리면 GPU가 필요 없다. 로컬
맥미니가 동시 실행을 못 해 벤치를 직렬로 돌리는 제약이 거기서는 사라진다.

대신 클라우드는 **비용 축을 추가**한다. 놀고 있는 인스턴스도 과금되므로:

- **상주 팀원은 손해다.** Claude Code 문서가 "팀원 수에 선형으로 토큰 증가"를 명시하고,
  클라우드에선 인스턴스 시간이 더 붙는다.
- **태스크마다 띄우고 버리는 게 맞다.** 그래서 결과가 브랜치/PR이어야 한다 — 인스턴스가
  사라져도 작업이 남아야 하니까. **§1의 커밋 범위 반환이 정확히 이 요구를 만족한다.**

---

## 4. 업계 대조 (1차 출처로 확인)

| 도구 | 팀 모드 | 트리 해법 | 머신 |
|---|---|---|---|
| Claude Code Agent Teams | 내장(실험적, 기본 꺼짐) | **파일 락** + "팀원마다 다른 파일" 조언 | 1대 |
| OpenCode 내장 | dev 브랜치 3 PR | — | 1대(단일 프로세스) |
| opencode-ensemble | 있음 | **팀원마다 git worktree + 브랜치** | 1대 |
| Tier 3 클라우드 | — | **git / PR** | VM 1대씩 |

확인된 사실 몇 가지:

- Claude Code 한계 목록에 **"/resume이 in-process 팀원을 복원하지 않는다"**가 있다.
  magi는 이 날 미완 턴 감지를 넣었으므로 **이 축에서는 앞선다**.
- ensemble은 워크트리를 **프로젝트 밖**(`~/.local/share/opencode/worktree/**`)에 두고,
  그래서 `external_directory` 권한 허용목록이 설치 요구사항이다. 병합은 unstaged로 떨궈
  사람이 `git diff`로 검토하고, 충돌은 **탐지만 하고 해결하지 않는다**.
- ensemble의 `worktree: false`가 **읽기 전용 에이전트 권장 설정**이다. magi도 같은 결론에
  독립적으로 도달했고(읽기 전용 자식은 부모 트리 공유), **방향만 뒤집어** 말 안 하면 안전한
  쪽이 되게 했다.

**내 반대 중 하나는 틀렸다**: worktree를 반대하며 "부모의 미커밋 변경을 자식에게 보이려면
dirty 파일 복사 계층을 따로 만들어야 하고 그게 기능의 대부분이 된다"고 했는데, 실제로
만들어보니 그 정도는 아니었다(`carryUncommitted`, 약 40줄).

---

## 5. UI 분리 — 유일하게 코어가 필요한 부분

**요구**: 프로세스를 여러 개 띄우고, 필요한 것에만 UI를 붙여 모니터링·간섭한다. 원격도.

**현재**: UI와 엔진이 한 프로세스다. TUI가 `*app.App`을 **Go 포인터로 직접** 들고 있고
(`model.go:107`), TUI가 종료하면 프로세스가 끝난다.

**실측한 경계 크기: 39개 메서드.**

```
BackgroundJobs BackgroundTail Compact ContextView CouncilMemberNames CreateSession Fork
GitDiff Interrupt KillBackgroundJob ListSessions LoopMap Permission Profiles Replay
RespondPermission RespondQuestion Rewind SessionDiff SessionModel SessionState
SetContextWindow SetGroupEnabled SetModel SetPermission SetProfile SetSubagentEnabled
SetSubagentModel SetTodos Steer SubagentJobs Subagents Submit Subscribe Todos ToolNames
UnfinishedTurnOf UsageFor UsageTotal
```

이 중 `Subscribe(ctx, sid, fromSeq)`가 **과거 이벤트 + 라이브 스트림**을 함께 주므로
재접속·따라잡기 모양은 이미 맞다.

### 선택지

| | 필요한 것 | 얻는 것 | 못 얻는 것 |
|---|---|---|---|
| **A. ssh + tmux** | **코어 변경 0** | 원격, 붙였다 떼기, 여러 프로세스 | 프로세스가 tmux에 묶임. 웹 UI 불가 |
| **B. 엔진/UI 분리** | 경계 + 이벤트 전송 + 데몬 모드 | 웹 UI, 여러 UI 동시 접속, 프로세스 독립 | 상당한 작업 |

**나는 A를 먼저 권했다**(오늘 되고, 실제 불편을 알려주므로). **사용자는 B를 택했고, 이
문서를 쓰는 브랜치가 그 작업 브랜치다.**

### B의 단계

1. **경계를 인터페이스로 추출.** `*app.App`이 그대로 만족하는 39개짜리 인터페이스. TUI가
   그것에 의존한다. **동작 변경 0, 되돌리기 쉬움, 전송을 아직 안 골라도 됨.**
2. **데몬 모드.** 유닉스 소켓에 listen. 원격은 **ssh 포워딩**으로 얻는다 — 인증·암호화를
   직접 만들지 않아도 된다는 것이 이 선택의 핵심 이점이다.
3. **UI가 붙었다 떼기.** 엔진이 UI 없이 계속 돈다. 이것이 "필요한 프로세스에만 UI"의 전제다.

★주의: 지금 `magi.serve`가 loopback 전용인 것은 **의도**로 보인다(주석이 "Binds 127.0.0.1
only"를 두 번 적는다). 데몬 소켓을 열 때 그 결정을 뒤집는 것인지, 유닉스 소켓이라 별개인지
분명히 하고 갈 것.

---

## 6. 하지 않기로 한 것

- **자동 git 설치.** 최상급 부수효과이고 실패 지점이 많다. 그리고 git이 **있어도** 자식이
  부모 트리를 공유하면 넓은 롤백은 사용자의 미커밋 작업을 날린다 — 실제 안전장치는 git 유무가
  아니라 **범위를 자식이 건드린 경로로 한정**하는 것이다.
- **magi가 자동으로 병합/되돌리기.** 실패한 시도의 잔재가 다음 회차의 증거일 수 있다. 판단은
  루프 작성자 몫이고, magi는 **못 한 것을 이름으로 보고**한다.
- **한 머신 상주 팀원.** 로컬 모델 한 대에 물린 환경에서 병렬성이 하드웨어로 뒷받침되지
  않으면 토큰만 늘고 벽시계는 그대로다.
- **시크릿 전달 기전.** 워커에 API 키를 옮기는 기전을 magi가 만들면 그게 유출 경로가 된다.

---

## 7. 열린 질문

1. **§5의 39개 경계 중 정말 원격이어야 하는 것은 몇 개인가.** 상당수가 UI 편의 질의라
   이벤트 스트림 + 소수 질의로 대체될 수 있다(추정, 미검증).
2. **병렬 자식.** 지금 스폰은 동기·단일이라 격리를 쓸 자리가 아직 없다. 열려면
   `luatool.go:41`이 Execute 전체 동안 플러그인 뮤텍스를 잡는 것부터 봐야 한다.
3. **인스턴스 20개일 때 tmux로 충분한가.** B의 근거를 실제로 키우는지는 띄워봐야 안다.
4. **워커가 검증을 못 하면?** 환경 동등성이 깨진 워커의 산출물을 어떻게 표시할 것인가.
   `child_steps`가 발자국을 주므로 "빌드를 돌린 적이 없다"는 판별 가능하다.

---

## 출처

- [Orchestrate teams of Claude Code sessions](https://code.claude.com/docs/en/agent-teams)
- [Introducing Muse Code and Muse Spark 1.2](https://research.meta.ai/blog/introducing-muse-code-and-muse-spark-1-2)
- [opencode-ensemble](https://github.com/hueyexe/opencode-ensemble)
- [opencode #12661 — Add Agent Teams Equivalent or Better](https://github.com/anomalyco/opencode/issues/12661)
- 이 저장소: `bench/harbor/magi_agent.py`, `internal/app/workspace.go`,
  `internal/app/spawn.go`, `internal/adapter/plugin/lua/bridge_net.go:197`
