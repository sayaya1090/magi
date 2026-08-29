# 이웃 조사 — IDE 코딩 어시스턴트들이 주는 것 (2026-08)

[↑ 화면 설계](./UI.ko.md)

> **왜 조사했나.** 사용자가 첨부 기능(파일·프로젝트 뷰 선택·에디터 선택영역)을 요구하면서 이웃
> 도구들의 기능 목록 확인을 지시했다(2026-08-29). 셋을 봤다 — Continue(오픈소스, VS Code+
> JetBrains), JetBrains AI Assistant(+Junie), GitHub Copilot for JetBrains. 각 항목에 **우리
> 실물의 자리**(있다/절반 있다/없다)와 채택 판정을 적는다. 출처는 절 끝에.

## 1. 기능 지형 — 셋이 공통으로 가진 것

| 기능 | Continue | JetBrains AI | Copilot | magi 플러그인의 지금 |
|---|---|---|---|---|
| 채팅 판 | ✓ | ✓ | ✓ | ✓ 하단 독 (전사는 데몬이 원천 — 셋과 달리 IDE 밖 콘솔·TUI와 **같은 대화**) |
| 컨텍스트 첨부 — 파일/폴더 | `@file` `@folder` | Add attachment 단추 + 검색 | `#`-컨텍스트 | **✓ 착지** — refs 계약 위 칩 + `@` 멘션(§2 ①②③ 완료) |
| 선택영역 보내기 | ⌘J (선택→채팅) | 자동(열린 파일+선택 자동 동봉, 토글) | 플로팅 툴바→인라인 챗 | **✓ 착지** — 우클릭 첨부 + Alt+Enter(§2 ①) |
| 열린 파일 자동 컨텍스트 | — | ✓ (Junie: 현재 파일+선택) | ✓ | **✓ 이미 있다** — `OpenBufferListener` 가 저장 안 한 버퍼를 타이핑마다 밀어 넣는다(ambient). 셋 중 누구보다 신선하다 |
| 파일 훑어보기(현재 파일에 대해 묻기) | ✓ | ✓ | ✓ | **✓** — `LookOverAction`(에디터 우클릭, 미저장 버퍼 그대로) |
| 인라인(탭) 자동완성 | ✓ (모델 분리 권장) | ✓ | ✓ | **✓ 문은 있다** — `MagiInlineCompletion`; `[autocomplete]` 프로필 라우팅이 스위치 |
| 인라인 편집(선택→자연어 지시→그 자리 수정) | Edit 모드 | 인라인 프롬프트(거터 보라 표시) | 인라인 챗/에이전트 (⇧⌘I) | **1단 착지** — Alt+Enter 인텐션이 선택을 refs 로 첨부+컴포저 미리채움(§3); 그 자리 diff 는 승인 프롬프트의 「± 변경 보기」가 절반 |
| 다중 파일 에이전트 편집 + 파일별 diff 리뷰 | Agent 모드 | Multi-file Edit(2026.1) | Agent 모드 | **절반** — 편집은 컴패니언이 손(Hand)으로 이미 하고 diff 는 전사 행이 됨; "제안→사람이 파일별 승인" 흐름은 없다(우리는 퍼미션 게이트가 그 자리) |
| 코드베이스 시맨틱 검색 | `@codebase`(인덱싱) | Codebase 모드 | ✓ | **다르다** — 인덱스 대신 컴패니언의 검색 툴이 실시간으로 훑는다. 채택 안 함(§4) |
| Next Edit Suggestions | — | ✓ | ✓ (NES, 멀리면 거터 화살표) | 없다 — §4 보류 |
| 터미널/diff 를 컨텍스트로 | `@Terminal` `@Git Diff` | ✓ | ✓ | **다르다** — 컴패니언이 자기 셸·git 툴로 직접 읽는다; 사람이 손으로 먹일 필요가 없는 구조 |
| 이미지 첨부 | — | ✓ (스크린샷의 에러 읽기) | — | 없다 — §4 보류 |
| 커스텀 규칙/스킬 | rules | .aiignore 등 | skills(프리뷰) | **✓ 다른 몸** — 워크스페이스의 에이전트 지침 파일(`/init` 이 굽는다)·플러그인이 그 자리 |

읽고 남는 감상 하나: 셋은 전부 **"IDE 안의 조수"**라 컨텍스트를 사람이 손으로 먹이는 UI가
발달했고, magi 는 **"워크스페이스의 컴패니언"**이라 스스로 읽는 쪽이 발달했다. 그래서 첨부가
없던 것이 구멍이 아니라 방향 차였는데 — **선택영역만은 예외다.** "지금 내가 보는 이 줄들"은
컴패니언이 스스로 알 수 없는, 사람 머릿속에만 있는 컨텍스트다. JetBrains 쪽 유저들이 정확히
그 지점(선택 자동 추적이 클릭 한 번에 풀리는 것)을 불평하고 우클릭 "Add to chat"을 요구하는
이슈가 열려 있다 — 첨부 UX 의 핵심이 파일 목록이 아니라 **선택영역**이라는 방증이다.

## 2. 채택 — 첨부 셋 (사용자 요구 그대로)

구현 순서대로:

1. **에디터 선택영역 첨부.** 우클릭 「magi: 대화에 첨부」— 선택 텍스트가 아니라
   **`경로:시작줄-끝줄` 참조**를 입력창에 끼워 넣는다. 본문을 복사해 보내면 모델이 낡은 사본을
   읽는다 — ambient 가 이미 신선한 전문을 밀고 있으므로 참조면 족하고, 이것이 셋 중 누구도
   못 하는 우리만의 이점이다(그쪽은 붙여넣은 순간의 스냅샷).
2. **프로젝트 뷰 선택 첨부.** 같은 액션을 ProjectViewPopupMenu 에도 — 파일/디렉토리 경로 참조.
3. **입력창 `@` 멘션.** 치다가 `@` 면 워크스페이스 파일 완성 — Continue 의 `@file` 꼴. 파일
   목록은 데몬의 파일 문(`files.go` 쪽)이 이미 답한다. 셋째인 이유: 위 둘은 액션 등록이면 되고
   이것은 입력창에 완성 UI 가 필요하다.

전부 **참조를 싣는 것**이지 새 문이 아니다 — 컴패니언은 받은 경로를 자기 read 툴로 읽는다
(신선도·퍼미션·워크스페이스 경계 전부 기존 규칙 그대로).

## 3. 채택 — 인라인 편집 (다음 급)

선택 → 자연어 지시 → 그 자리 diff. 우리 몸에 맞는 모양: 지시는 `steer`/`submit` 으로 가고
편집은 컴패니언 손이 하되, **결과 diff 를 에디터 안에서** 보여 주는 것(전사 행의 diff 를
IDE diff 뷰어로 여는 §8 항목과 한 몸). 셋 다 이걸 주력으로 민다 — 사람들이 채팅 판보다
에디터를 안 떠나는 쪽을 고른다는 뜻이다.

## 4. 안 하거나 보류

- **코드베이스 인덱싱** — 안 한다. 컴패니언의 실시간 검색과 이중 진실이 되고, 인덱스는 낡는다.
- **NES(다음 편집 제안)** — 보류. 자동완성 프로필 라우팅 위에서 가능하지만 값 대비 호출량이
  크다. 자동완성이 실사용에서 자리 잡은 뒤 재론.
- **이미지 첨부** — 보류. 와이어(parts 에 image kind)는 이미 있으니 입력 UI 만의 문제다.
- **자동 승인 전부 켜기**(Copilot 의 global auto approve) — **안 한다.** 우리 퍼미션 게이트를
  통째로 끄는 스위치를 화면에 두지 않는다 — 그쪽 문서조차 보안 경고를 달아 두고 있다.

## 출처

- Continue: [Chat quick start](https://docs.continue.dev/ide-extensions/chat/quick-start) · [Context selection](https://docs.continue.dev/ide-extensions/chat/context-selection) · [기능 개관](https://www.local-llm.net/tools/continue-dev/)
- JetBrains AI Assistant: [AI Chat(첨부)](https://www.jetbrains.com/help/ai-assistant/ai-chat.html) · [Junie(자동 컨텍스트)](https://www.jetbrains.com/help/ai-assistant/junie-agent.html) · [선택-추적 불평 이슈](https://youtrack.jetbrains.com/projects/LLM/issues/LLM-25965/Optimize-the-code-file-folder-attaching-in-the-AI-assistant-ACP)
- Copilot for JetBrains: [인라인 에이전트 프리뷰(2026-04)](https://github.blog/changelog/2026-04-24-inline-agent-mode-in-preview-and-more-in-github-copilot-for-jetbrains-ides/) · [NES 등(2026-02)](https://github.blog/changelog/2026-02-13-new-features-and-improvements-in-github-copilot-in-jetbrains-ides-2/) · [에이전트 강화(2026-07)](https://github.blog/changelog/2026-07-07-codex-as-agent-provider-and-agentic-enhancements-in-jetbrains-ides/) · [GitHub Docs](https://docs.github.com/en/copilot/concepts/agents/copilot-in-jetbrains)
