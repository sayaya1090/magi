# magi 문서

[English](README.md) · [한국어](README.ko.md) · [↑ 프로젝트 README](../README.ko.md)

`docs/` 아래 모든 문서의 지도다. 모든 문서는 같은 머리말을 단다 — 제목, 언어 전환 링크, 그리고 한 줄
**상태**: 현행 참조 문서인지, 근거를 남기려 보존한 역사 기록인지.

## 여기서 시작

| 문서 | 무엇인가 |
|---|---|
| [MANUAL](MANUAL.ko.md) · [English](MANUAL.md) | **사용 안내서.** 설치·실행·설정, TUI와 웹 콘솔을 처음부터 끝까지. |
| [CONTEXT](CONTEXT.md) | **길잡이 (한국어).** 목표·아키텍처·턴 루프의 짧은 요약. |

## 참조 — 지어진 그대로의 시스템

| 문서 | 무엇인가 |
|---|---|
| [ARCHITECTURE](ARCHITECTURE.ko.md) · [English](ARCHITECTURE.md) | magi 개발을 위한 **as-built 참조**: 헥사고날 계층, 에이전트 루프, 종료 게이트, 가드레일, 툴, 확장점. 설계 문서와 어긋나면 이 문서가 이긴다. |
| [DIAGRAMS](DIAGRAMS.ko.md) · [English](DIAGRAMS.md) | ARCHITECTURE의 시각적 짝 — 프로세스 경계(L0)에서 클래스 다이어그램(L5–L9)까지 한 축, 전부 mermaid. |
| [UI](UI.ko.md) · [English](UI.md) | 두 표면 — 웹 콘솔(`cmd/magi-web`)과 터미널 UI(`internal/adapter/tui`): 각 화면에 무엇이 있고, 어떤 디자인 규칙을 지키며, 왜인가. |
| [EXTENDING](EXTENDING.ko.md) · [English](EXTENDING.md) | **외부 툴(MCP)**과 **팀 공유 기억/스킬**(경험 스토어)을 magi에 붙이는 실전 절차. |

## 설계 & 역사

무엇을 어떤 근거로 정했는지의 기록으로 보존한다. 현행 참조가 **아니다** — ARCHITECTURE/MANUAL과
어긋나면 그쪽이 이긴다.

| 문서 | 무엇인가 |
|---|---|
| [SPEC](SPEC.ko.md) · [English](SPEC.md) | **역사.** 카운슬/루프 재설계 이전의 원래 기능 명세(테스트 케이스 포함). |
| [DESIGN](DESIGN.ko.md) · [English](DESIGN.md) | **역사.** M1 시작 시점의 상세 설계 의도. |

## 제안서

`docs/proposals/` 에는 날짜가 붙은 설계 제안서들이 있다 — 어떤 방향을 저울질하던 특정 시점의 기록이지
현행 문서가 아니다. 변경의 근거가 궁금할 때, 작성된 날짜와 함께 읽는다.
