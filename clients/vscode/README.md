# clients/vscode/ — VS Code 확장

[↑ 저장소](../../README.md) · [사용자 매뉴얼](docs/MANUAL.ko.md) · [설계](docs/DESIGN.ko.md) · [화면 설계](docs/UI.ko.md) · [플랫폼 규약](docs/PLATFORM.ko.md) · [이웃 조사](docs/SURVEY.ko.md) · [무엇을 어디서 재나](docs/TESTING.ko.md) · [형제: 젯브레인](../jetbrains/README.md)

VS Code에서 연 워크스페이스의 magi 데몬과 유닉스 도메인 소켓(UDS)으로 통신하며, 에이전트 대화 및 에디터 제어를 제공하는 공식 확장입니다.
새 통신 프로토콜을 정의하지 않고 본체 데몬의 소켓 계약([`docs/CLIENTS`](../../docs/CLIENTS.ko.md))을 그대로 준수합니다.

---

## 1. 빌드 및 설치

```sh
# 1. 의존성 설치 및 컴파일
npm install && npx tsc -p .

# 2. VSIX 패키징
npx --yes @vscode/vsce package --no-dependencies --allow-missing-repository

# 3. 로컬 VS Code에 설치
code --install-extension magi-0.2.0.vsix --force
```

---

## 2. 제공 기능

- **대화 패널 (하단 패널)**: 실시간 대화 전사 스트리밍, 승인(Approval) 요청 처리, 모델/모드 전환.
- **사이드바 (계획/계기판)**: 에이전트 작업 목표 목록(TODOs), 세션 컨텍스트 윈도 사용량 표시.
- **에디터 통합**: 인라인 코드 자동 완성, 실시간 편집 힌트(인레이 힌트), 파일 변경 마커 데코레이션, 코드 액션.
- **작업 지원**: 빠른 파일 첨부, 커밋 메시지 초안 생성, Git Blame(코드 라인별 작성자 추적).

> 아직 구현되지 않은 기능 목록은 [사용자 매뉴얼 §8](docs/MANUAL.ko.md)을 참조하십시오.

---

## 3. 디렉토리 구조 (계층 분리)

`vscode` 모듈 의존성을 분리하여 IDE 없이도 프로토콜과 상태 로직을 독립적으로 검증합니다 (`layering.test.ts` 불변식).

| 디렉토리 | 역할 | 의존성 원칙 |
|---|---|---|
| `src/core/` | 데몬 소켓 프로토콜, 이벤트 파싱, 승인 상태 머신 | **Node.js 순수 환경** (`vscode` 모듈 import 금지, 단위 테스트 대상) |
| `src/ide/` | VS Code UI 등록 (Webview 패널, 에디터 커맨드, 설정 연동) | `vscode` API 사용하는 유일한 레이어 |
| `src/live/` | 실제 구동 중인 VS Code 인스턴스 환경 전용 테스트 하네스 | VS Code Extension Host 런타임 |
| `src/test/` | `src/core/`의 순수 비즈니스 로직 및 와이어 프로토콜 단위 테스트 | 빠른 실행 (에디터 실행 불필요) |

---

## 4. 테스트 실행

```sh
# 1. 단위 테스트 (에디터 없이 순수 Node 환경에서 고속 실행)
npx tsc -p . && node --test 'out/test/*.test.js'

# 2. 실물 VS Code E2E 테스트 (실제 에디터를 띄워 매니페스트 및 뷰 등록 검증)
npx tsc -p . && node out/live/run.js
```

> **주의**: 매니페스트(`package.json`)의 오타나 뷰 등록 누락은 단위 테스트를 통과하더라도 실물 에디터에서 에러 없이 뷰만 미표시될 수 있으므로, 두 테스트를 모두 실행해야 합니다. 자세한 기준은 [TESTING.ko.md](docs/TESTING.ko.md)를 참고하십시오.

---

## 5. 핵심 연동 규칙 및 주의사항

- **소켓 경로 및 워크스페이스 키 일치**: [`docs/DESIGN.ko.md` §5](docs/DESIGN.ko.md)
  - 데몬 소켓 식별자인 `WorkspaceKey` 해시가 코어 바이너리의 계산 결과와 정확히 일치해야 합니다. 불일치 시 에러 없이 통신이 단절됩니다.
- **설정 선언 원칙**: [`docs/PLATFORM.ko.md` §9](docs/PLATFORM.ko.md)
  - 설정 화면을 코드로 직접 조립하지 않고 `package.json`의 `contributes.configuration` 선언형 스키마를 통해 제공합니다.
- **UI 불변식**: [`docs/UI.ko.md` §0](docs/UI.ko.md)
  - 웹뷰 및 UI 렌더링 시 지켜야 하는 7대 불변식 규약.



