# 생태계 조사 — VS Code 코딩 어시스턴트 확장 구조 분석 (2026-09-08)

[↑ 클라이언트 개요](../README.md) · [화면 설계](./UI.ko.md) · [플랫폼 규약 대조표](./PLATFORM.ko.md) · [형제: 젯브레인 이웃 조사](../../jetbrains/docs/SURVEY.ko.md)

> **조사 목적.** [화면 설계](./UI.ko.md)에서는 대화 뷰를 하단 패널에, 계획 뷰를 사이드바에 배치하기로 결정하였습니다. 이 결정이 기존 VS Code 사용자들의 멘탈 모델과 상충하는지 검증하기 위해 주요 코딩 어시스턴트 확장의 실제 배치 구성을 분석하였습니다. 기존 확장과 상이한 배치는 사용자에게 추가적인 학습 비용을 유발할 수 있기 때문입니다.
>
> **분석 기준.** 2차 가공된 아티클을 배제하고 각 확장의 공식 리포지토리 `package.json` 매니페스트 내 `contributes` 선언을 직접 추출하여 대조하였습니다. GitHub Copilot Chat의 경우 소스 코드가 비공개이므로 공식 사용자 문서를 참조하였으며, 한계점은 §5에 명시하였습니다.

---

## 1. 뷰 배치 위치 — 액티비티 바 집중 현황

| 확장 | 기여 컨테이너 | 선언 뷰 ID | 뷰 타입 |
|---|---|---|---|
| Cline | **activitybar** `claude-dev-ActivityBar` | `claude-dev.SidebarProvider` | webview |
| Roo Code | **activitybar** `roo-cline-ActivityBar` | `roo-cline.SidebarProvider` | webview |
| Continue | **activitybar** `continue` | `continue.continueGUIView` | webview |
| Continue (보조) | **panel** `continueConsole` | `continue.continueConsoleView` | webview (`config.continue.enableConsole` 활성화 시) |
| Copilot Chat | 공식 문서상 "the editor sidebar" 표기 | — | — |
| **magi (대화)** | **panel** | 대화 세션 | webview |
| **magi (계획)** | **secondarySidebar** | 계획 및 계기판 | webview |

**대다수 확장이 주 대화 창을 액티비티 바(프라이머리 사이드바)에 배치하고 있으며, magi는 패널을 기본 위치로 채택하였습니다.** 이 차이점에 대한 설계 근거는 §4에서 상술합니다.

- ⚠ **조사 시점의 플랫폼 제약 사항:** 본 조사는 `activitybar`와 `panel`만 지원되던 시점의 매니페스트를 기준으로 수행되었습니다. 이후 VS Code 1.106에서 `secondarySidebar` 슬롯이 개방되었으므로 타 확장의 최신 배치 전략은 변경되었을 수 있습니다.
- Continue 확장의 경우 패널 컨테이너를 보조 활용하고 있으나, 이는 **대화 창이 아닌 콘솔 로그 뷰**이며 기본값으로 비활성화되어 있습니다. 이는 「패널은 지속적인 관찰을 요하는 보조 기능에 적합하다」는 공식 가이드라인의 취지와 부합합니다.

---

## 2. 커맨드 등록 체계 — 접두사 및 등록 수

| 확장 | 등록 명령 수 | 등록 카테고리 | 비고 |
|---|---|---|---|
| Continue | 39개 (고유 38개) | `Continue` | 타이틀 내에 「Continue: …」 중복 접두 혼재 |
| Cline | 19개 | `Cline` (14개에만 부여, 나머지 5개는 아이콘 전용) | 아이콘 전용 액션 카테고리 생략 |
| Roo Code | 18개 | 설정 항목 13개 · `Terminal` 3개 · 아이콘 전용 4개 | 다국어 현지화 적용 |

**아이콘 전용 명령에는 카테고리를 부여하지 않는 것이 공통 규칙입니다.** 툴바 아이콘으로만 소비되는 내부 커맨드가 커맨드 팔레트에 불필요하게 노출되는 것을 방지하기 위함입니다. magi의 초기 화면 설계 §5에 명시되었던 「모든 명령에 `magi:` 접두사 부여」 규칙은 이 분석을 바탕으로 수정되었습니다(§4-나 참조).

---

## 3. 키보드 단축키 할당 현황

| 확장 | 기본 단축키 수 | 주요 할당 단축키 |
|---|---|---|
| Continue | **21개** | 채팅 호출 `Cmd+L`, 인라인 편집 `Cmd+I`, diff 수락/거절, 코드 조각 수락/거절, 터미널 디버그 등 |
| Roo Code | 2개 | 컨텍스트 추가(`Cmd+K Cmd+A`), 자동 승인 토글(`Cmd+Alt+A`) |
| Cline | 3개 | 채팅 추가 / 프롬프트 포커스(선택 영역 여부에 따라 동일 키 `Cmd+'` 공유), 커밋 메시지 생성 |

**Cline의 단일 키 다중 분기 패턴:** 동일한 `Cmd+'` 단축키를 에디터 내 텍스트 선택이 있을 때는 「채팅에 추가」로, 선택이 없을 때는 「프롬프트 입력창 포커스」로 `when` 절을 분기하여 단축키 충돌을 최소화하였습니다.

**Continue의 광범위한 단축키 할당:** 21개의 단축키를 기본 선점하는 방식은 플랫폼 가이드라인의 ❌ "Overwrite existing keyboard shortcuts" 금지 권고와 상충될 위험이 높습니다.

---

## 4. magi 설계 결정에 대한 분석 및 결론

### (가) 대화 뷰의 하단 패널 배치 — **유지 (설계 근거 보강)**

타 확장이 모두 액티비티 바를 채택한 상황에서 패널을 유지하는 이유는 magi의 아키텍처적 고유성 때문입니다.

1. **세션 전사의 범위가 단일 창에 국한되지 않습니다.** 타 확장의 세션은 해당 VS Code 창 내부에서 생성 및 소멸하지만, magi의 전사는 **백그라운드 데몬이 원천**입니다. 터미널 CLI, 웹 콘솔, JetBrains 플러그인이 동일한 워크스페이스 전사를 실시간 공유합니다. 이는 개별 에디터의 일시적 작업이 아닌 워크스페이스 전역의 런타임 이벤트이므로, 터미널·출력·문제가 상주하는 패널 컨테이너의 성격과 정확히 일치합니다.
2. **독립된 2개의 웹뷰 영역이 필요합니다.** magi는 계획, 컨텍스트 윈도우 점유율, 백그라운드 작업, 플릿 현황을 별도 사이드바(`secondarySidebar`)에 지속 노출합니다(화면 설계 §3). 타 확장은 이를 대화 웹뷰 내부에 인라인으로 합쳐서 렌더링하므로 1개의 컨테이너로 족하지만, magi는 2개의 전용 공간을 필요로 합니다.

- ⚠ **사용성 보완 대책:** 사용자가 패널을 최소화하더라도 작업 상태를 놓치지 않도록 상태 표시줄 상시 인디케이터([화면 설계 §4](./UI.ko.md))를 제공하며, `magi: Open the conversation` 명령을 통해 언제든 즉시 호출할 수 있도록 지원합니다. 또한 사용자가 원하는 경우 패널을 사이드바로 드래그하여 재배치할 수 있으므로 반응형 레이아웃을 엄격히 보장합니다.

### (나) 커맨드 접두사 정책 수정 — **반영 완료**

뷰 툴바 및 컨텍스트 메뉴 전용 액션이 커맨드 팔레트를 오염시키지 않도록 규칙을 세분화하였습니다.

> 팔레트에 노출되는 명령에만 `magi:` 카테고리를 명시하고, **툴바 및 컨텍스트 메뉴 전용 액션은 카테고리를 부여하지 않거나 `commandPalette` 컨텍스트에서 `when: false`로 은닉**합니다.

### (다) 기본 단축키 최소화 정책 — **유지**

조사 대상 중 2개 확장이 2~3개의 최소 단축키만 할당하고 있었습니다. 플랫폼 가이드라인이 기존 사용자 단축키와의 충돌 방지를 강력히 권고하므로, 확장은 기본 단축키를 강제 선점하지 않고 사용자가 필요에 따라 직접 바인딩하도록 위임합니다.

---

## 5. 분석 한계 및 추가 검토 사항

- **GitHub Copilot Chat 세부 구현:** 소스 비공개로 인해 매니페스트 대신 공식 문서에 의존하였으므로 내부 컨테이너 선언의 정확한 기술적 규격은 확인하지 못했습니다.
- **동적 공급자(Provider) 등록 방식:** 인라인 자동완성, 인레이 힌트, 코드 액션 등은 매니페스트가 아닌 런타임 코드에서 동적으로 등록되므로 정적 매니페스트 분석 범위에 포함되지 않았습니다.
- **실제 사용자 커스텀 배치 비율:** VS Code의 UI 커스터마이징 기능으로 인해 사용자가 기본 배치를 변경하여 사용하는 실측 통계는 수집 대상에서 제외되었습니다.

---

## 6. 참고 문헌 및 소스 출처

수집 기준일: 2026-09-08

- [Cline `apps/vscode/package.json`](https://raw.githubusercontent.com/cline/cline/main/apps/vscode/package.json) — 매니페스트 `contributes` 실측
- [Roo Code `src/package.json`](https://raw.githubusercontent.com/RooCodeInc/Roo-Code/main/src/package.json) — 매니페스트 `contributes` 실측
- [Continue `extensions/vscode/package.json`](https://raw.githubusercontent.com/continuedev/continue/main/extensions/vscode/package.json) — 매니페스트 `contributes` 실측
- [GitHub Copilot Chat 공식 문서](https://code.visualstudio.com/docs/copilot/chat/copilot-chat) — 문서 참조
- VS Code 플랫폼 UX 가이드라인 인용문은 [플랫폼 대조표](./PLATFORM.ko.md) 출처 준용

