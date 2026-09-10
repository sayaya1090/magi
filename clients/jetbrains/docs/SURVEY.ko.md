# 이웃 조사 — IDE 코딩 어시스턴트 기능 비교 (2026-08)

[↑ 화면 설계](./UI.ko.md)

> **조사 배경.** 컨텍스트 첨부 기능(파일, 프로젝트 뷰 선택, 에디터 선택영역) 구현 요구에 따라 주요 IDE 어시스턴트 도구의 기능 구조를 분석하였습니다. 분석 대상은 Continue(오픈소스, VS Code + JetBrains), JetBrains AI Assistant(+Junie), GitHub Copilot for JetBrains입니다. 각 항목별로 magi 플러그인의 구현 현황과 채택 여부를 정리하였습니다.

## 1. 기능 지형 — 공통 및 고유 기능 분석

| 기능 | Continue | JetBrains AI | Copilot | magi 플러그인 현황 |
|---|---|---|---|---|
| 채팅 판 | ✓ | ✓ | ✓ | ✓ 하단 독 (데몬 스트림 연동 — 타 IDE 및 웹 콘솔, 터미널과 동일 세션 실시간 공유) |
| 컨텍스트 첨부 — 파일/폴더 | `@file` `@folder` | Add attachment 단추 + 검색 | `#`-컨텍스트 | **✓ 구현 완료** — refs 프로토콜 기반 칩 및 `@` 멘션 연동 |
| 선택영역 전송 | ⌘J (선택→채팅) | 자동(열린 파일+선택 자동 동봉, 토글) | 플로팅 툴바→인라인 챗 | **✓ 구현 완료** — 에디터 우클릭 첨부 및 Alt+Enter 인텐션 |
| 열린 파일 자동 컨텍스트 | — | ✓ (Junie: 현재 파일+선택) | ✓ | **✓ 구현 완료** — `OpenBufferListener`가 실시간 미저장 버퍼 동기화(ambient) 수행 |
| 파일 훑어보기(현재 파일 검토) | ✓ | ✓ | ✓ | **✓ 구현 완료** — `LookOverAction`(에디터 우클릭, 미저장 버퍼 즉시 검토) |
| 인라인(탭) 자동완성 | ✓ (모델 분리 권장) | ✓ | ✓ | **✓ 구현 완료** — `MagiInlineCompletion` 및 `[autocomplete]` 프로필 라우팅 |
| 인라인 편집(선택→자연어 지시→수정) | Edit 모드 | 인라인 프롬프트(거터 표시) | 인라인 챗/에이전트 (⇧⌘I) | **✓ 구현 완료** — Alt+Enter 인텐션 기반 컴포저 연동 및 IDE 나란히-보기 diff 연동 |
| 다중 파일 에이전트 편집 + diff 리뷰 | Agent 모드 | Multi-file Edit(2026.1) | Agent 모드 | **부분 지원** — 컴패니언의 직접 편집 도구 및 이벤트 전사 diff 연동 완료 |
| 코드베이스 시맨틱 검색 | `@codebase`(인덱싱) | Codebase 모드 | ✓ | **접근 방식 상이** — 사전 정적 인덱싱 대신 컴패니언의 실시간 파일 탐색 도구 활용 |
| Next Edit Suggestions | — | ✓ | ✓ (NES) | 미지원 — 향후 과제 |
| 터미널/diff 컨텍스트 주입 | `@Terminal` `@Git Diff` | ✓ | ✓ | **접근 방식 상이** — 컴패니언이 셸 및 Git 도구를 통해 필요한 정보를 직접 자율 수집 |
| 이미지 첨부 | — | ✓ | — | 미지원 — 향후 과제 |
| 커스텀 규칙/스킬 | rules | .aiignore 등 | skills(프리뷰) | **✓ 구현 완료** — 워크스페이스 `/init` 지침 및 플러그인 확장 체계 활용 |

주요 어시스턴트들이 IDE 내부 보조자에 집중하여 수동 컨텍스트 주입 UI를 발전시킨 반면, magi는 독립 상주형 데몬으로서 자율 탐색 체계를 중심으로 동작합니다. 특히 에디터 텍스트 선택영역 첨부는 개발자의 현재 집중 맥락을 전달하는 핵심 진입점으로 판단하여 우선 구현하였습니다.

## 2. 채택 기능 — 컨텍스트 첨부 3종

1. **에디터 선택영역 첨부:** 우클릭 「magi: 채팅에 추가」 액션을 통해 파일 본문 전체 복사 대신 **`경로:시작줄-끝줄` 참조**를 프롬프트에 첨부합니다. 데몬이 디스크 최신 원본을 직접 조회하여 전달하므로 데이터 신선도를 보장합니다.
2. **프로젝트 뷰 선택 첨부:** 프로젝트 탐색기에서 선택한 파일 및 디렉토리 경로를 참조 칩으로 일괄 첨부합니다.
3. **입력창 `@` 멘션:** 입력 도중 `@` 입력 시 워크스페이스 파일 목록 자동완성 팝업을 제공합니다.

첨부된 항목은 별도 프로토콜 증설 없이 `refs` 배열 규약으로 전송되어, 컴패니언의 표준 파일 조회 샌드박스를 거쳐 안전하게 처리됩니다.

## 3. 채택 기능 — 인라인 편집

선택영역에 대한 자연어 지시 결과를 에디터 내부에서 검토할 수 있도록 연동합니다. 승인 프롬프트의 「변경 보기」 단추 및 전사 내역의 적용된 `edit` 행을 통해 IDE 표준 나란히-보기 diff 뷰어를 직접 호출합니다.

## 4. 보류 및 제외 항목

- **코드베이스 사전 인덱싱:** 실시간 정합성 유지 한계 및 중복 캐싱 부담으로 인해 채택하지 않으며, 컴패니언의 실시간 탐색 도구를 유지합니다.
- **Next Edit Suggestions (NES):** 자동완성 프로필 안정화 이후 도입 타당성을 재검토합니다.
- **사용자 이미지 첨부:** 현재 데몬 프로토콜이 도구 실행 결과(`ToolResult.Images`)로서의 이미지 수신만을 지원하므로, 코어 API 확장 이후 클라이언트 연동을 진행합니다.
- **전역 자동 승인:** 보안 가드레일 무력화 위험을 방지하기 위해 일괄 자동 승인 기능은 의도적으로 제공하지 않습니다.

## 출처

- Continue: [Chat quick start](https://docs.continue.dev/ide-extensions/chat/quick-start) · [Context selection](https://docs.continue.dev/ide-extensions/chat/context-selection)
- JetBrains AI Assistant: [AI Chat(첨부)](https://www.jetbrains.com/help/ai-assistant/ai-chat.html) · [Junie(자동 컨텍스트)](https://www.jetbrains.com/help/ai-assistant/junie-agent.html)
- Copilot for JetBrains: [인라인 에이전트 프리뷰](https://github.blog/changelog/2026-04-24-inline-agent-mode-in-preview-and-more-in-github-copilot-for-jetbrains-ides/)
