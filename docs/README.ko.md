# magi 문서 체계

[English](README.md) · [한국어](README.ko.md) · [↑ 프로젝트 README](../README.ko.md)

`docs/` 디렉터리에 포함된 전체 문서 목록과 위상을 안내하는 문서 지도입니다. 각 문서는 제목, 다국어 전환 링크, 그리고 문서의 성격(현행 참조 기준 vs 의사결정 이력 보존)을 명시합니다.

## 핵심 가이드

| 문서 | 설명 |
|---|---|
| [MANUAL](MANUAL.ko.md) · [English](MANUAL.md) | **사용자 매뉴얼**: 설치, 실행, 설정, TUI 및 웹 콘솔 사용법 전체 안내. |
| [CONTEXT](CONTEXT.md) | **길잡이**: 프로젝트 목표, 핵심 아키텍처, 턴 실행 루프 요약. |
| [SECURITY](../SECURITY.ko.md) · [English](../SECURITY.md) | **보안 및 신뢰 경계**: 도구 게이트, 워크스페이스 격리, 인증 모델. |

## 기술 참조 (현행 구현 기준)

| 문서 | 설명 |
|---|---|
| [ARCHITECTURE](ARCHITECTURE.ko.md) · [English](ARCHITECTURE.md) | **현행 시스템 아키텍처**: 헥사고날 구조, 루프 흐름, 검증 게이트, 가드레일, 확장점. 초기 설계 문서와 상충 시 본 문서가 우선합니다. |
| [DIAGRAMS](DIAGRAMS.ko.md) · [English](DIAGRAMS.md) | **아키텍처 다이어그램**: 프로세스 경계(L0)부터 상세 클래스 다이어그램(L5–L9)까지의 Mermaid 시각화. |
| [UI](UI.ko.md) · [English](UI.md) | **UI 설계 규약**: 웹 콘솔(`clients/web/server`) 및 터미널 UI(`internal/adapter/tui`) 화면 구조 및 디자인 원칙. |
| [CLIENTS](CLIENTS.ko.md) · [English](CLIENTS.md) | **클라이언트 플랫폼 연동**: 터미널, 웹 콘솔, JetBrains 플러그인, Visual Studio 확장, Office 애드인의 역할과 소켓 계약. |
| [EXTENDING](EXTENDING.ko.md) · [English](EXTENDING.md) | **확장 개발 가이드**: 외부 도구(MCP) 및 팀 공유 지식/스킬 저장소 연동 절차. |

## 설계 이력 & 보존 문서

초기 아키텍처 결정 배경을 추적하기 위한 보존 문서입니다. 현행 참조 기준이 아니며, 최신 코드나 ARCHITECTURE/MANUAL과 내용이 상충할 경우 최신 문서가 우선합니다.

| 문서 | 설명 |
|---|---|
| [SPEC](SPEC.ko.md) · [English](SPEC.md) | **초기 명세 (보존)**: 카운슬/루프 개편 이전의 원형 기능 명세 및 테스트 케이스. |
| [DESIGN](DESIGN.ko.md) · [English](DESIGN.md) | **초기 설계 (보존)**: 마일스톤 1 단계의 상세 설계 의도 및 의사결정 배경. |

## 제안서 (Proposals)

`docs/proposals/` 디렉터리에는 특정 시점에 검토된 설계 제안서들이 보존되어 있습니다. 현행 사양 규격이 아니며, 기능 도입 배경이나 설계 변경 이유를 확인할 때 참고합니다.

