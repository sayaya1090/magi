# clients/visualstudio/ — Visual Studio 확장

[↑ 저장소](../../README.md) · [설계](docs/DESIGN.ko.md) · [플랫폼 규약](docs/PLATFORM.ko.md) · [편집기 셋 타당성](../../docs/proposals/EDITORS.ko.md) · [형제: VS Code](../vscode/README.md) · [형제: 젯브레인](../jetbrains/README.md)

Visual Studio에서 연 솔루션의 magi 데몬과 통신하여 대화창 및 IDE 제어 기능을 제공하는 공식 확장입니다.
새 프로토콜을 만들지 않고 본체 데몬의 소켓 계약([`docs/CLIENTS`](../../docs/CLIENTS.ko.md))을 준수하며, C# 구현 중복을 최소화하기 위해 공통 로직을 처리하는 `magi ide-bridge` 프로세스를 중계자로 사용합니다.

> **현재 상태**: 구현 및 실물 기동 완료. 대화 패널 렌더링, 컴패니언 연동, 상태 표시가 정상 동작합니다.
>
> `src/Magi.Core`는 브리지와 통신하며 IDE 종속성이 전혀 없는 순수 .NET 라이브러리이고, 그 상위의 `src/Magi.Extension`이 대화 툴 윈도를 제공합니다 (`dotnet test` 기준 36개 단위 테스트 전원 통과, `dotnet build` 기준 VSIX 패키징 완료).
>
> ✅ **2026-09-10, 실험 인스턴스 기동 검증 완료**(VS Community 2022 **17.14.40**). 우측 도킹 패널에서 컴패니언의 상태를 단일 단어로 표시하며, 미실행 상태일 때는 기동 여부를 묻는 버튼을 표시합니다. 사전 짐작으로 남겨 두었던 4개 항목 중 3건(리소스 VSIXSubPath 누락, XAML 기본 xmlns 네임스페이스 오류, Remote UI `[DataMember]` 누락)이 빌드 경고 없이 런타임 결함을 유발하던 것을 실측을 통해 규명하고 해결했습니다 ([설계 §10](docs/DESIGN.ko.md)).
>
> **향후 과제: 대화 전사(Transcript) 스트리밍 연동.** 현재 패널은 마지막 단일 응답만 표시합니다. 대화 전사는 지속적인 이벤트 스트림이며 현재 브리지의 전달 문(`daemon`)은 1회성 단일 요청/단일 응답 방식이므로, 향후 `watch` 스트림 메서드가 도입된 후 전사 표시를 연동할 예정입니다 ([IDE_BRIDGE §5](../../docs/IDE_BRIDGE.ko.md)).
> ※ Visual Studio 확장은 Windows 환경에서만 빌드됩니다 (Mac용 Visual Studio는 2024년 공식 단종되었습니다).

---

## 1. 빌드 및 테스트

```powershell
# 1. 코어 라이브러리 단위 테스트 (IDE 없이 고속 실행, 36개 통과)
dotnet test clients/visualstudio/test/Magi.Core.Tests

# 2. VSIX 확장 빌드 (Windows 환경 필요)
dotnet build clients/visualstudio/Magi.sln

# 3. 실물 ide-bridge 스모크 테스트 (magi 바이너리가 PATH에 있어야 함)
pwsh clients/visualstudio/tools/smoke.ps1
```

---

## 2. 프로젝트 구조 (계층 분리)

`VisualStudio.Extensibility` Out-of-Process 확장 모델을 따르며, IDE 종속성을 완전히 격리했습니다.

| 프로젝트 | 역할 | 의존성 원칙 |
|---|---|---|
| `src/Magi.Core/` | `magi ide-bridge` 프로세스 통신, JSON Lines 스트림 처리, 상태 관리 | **순수 .NET 클래스 라이브러리** (`VisualStudio.Extensibility` 참조 금지) |
| `src/Magi.Extension/` | 대화 툴 윈도 및 IDE 커맨드 등록 | Visual Studio Extensibility SDK 참조 전용 계층 |
| `test/Magi.Core.Tests/` | `Magi.Core`의 와이어 프로토콜 및 브리지 계약 단위 테스트 (36개) | IDE 및 데몬 프로세스 없이 독립 실행 |
| `tools/smoke.ps1` | 실제 빌드된 `magi ide-bridge` 바이너리와의 통합 연동 검증 스크립트 | Windows PowerShell 스크립트 |

---

## 3. 설계 핵심: `magi ide-bridge` 도입

VS Code, JetBrains에 이어 세 번째 에디터 클라이언트를 구현하면서 발생하는 공통 로직 중복을 방지하기 위해 `magi ide-bridge`를 도입했습니다:

**브리지 이관 계획과 현재 구현 상태** ([설계 §3](../../docs/DESIGN.ko.md) · [프로토콜 사양](../../docs/IDE_BRIDGE.ko.md)):
공통 8대 기능 중 현재 빌드에서는 `about`, `activity`, `daemon` 3개 메서드가 브리지에 구현되어 있습니다. 나머지 기능은 추후 단계적으로 이관되며 현재는 확장 플러그인이 직접 처리합니다.

| | 공통 8대 기능 | 브리지 지원 여부 | 비고 |
|---|---|---|---|
| 1 | 워크스페이스 키 및 소켓 경로 유도 (`WorkspaceKey` · FNV) | ✅ 지원 | `about` 메서드를 통해 계산 결과 반환 |
| 2 | 줄 단위 JSON Lines 스트림 처리 | ✅ 지원 | stdio 기반 양방향 RPC |
| 3 | 대화 전사 이벤트 파싱 및 행 변환 | ⏳ 이관 예정 | 향후 `watch` 스트림과 함께 브리지로 통합 |
| 4 | 에이전트 현재 상태 요약 단어 | ✅ 지원 | `activity` 메서드 |
| 5 | 도구 권한 승인 어휘 (`allow` · `deny` · `always`) | ⏳ 확장 직접 처리 | 확장 계층에서 승인 상태 머신 관리 |
| 6 | 인라인 어노테이션 파싱 (줄 번호 매핑) | ⏳ 이관 예정 | 향후 `look` 메서드로 이관 |
| 7 | 코드 인라인 자동완성 윈도우 계산 및 중복 제거 | ⏳ 이관 예정 | 향후 `complete` 메서드로 이관 |
| 8 | 바이너리 버전 확인 및 다운로드 관리 | ⏳ 분담 처리 | 브리지 실행을 위한 최소 1회 다운로드는 확장이 직접 수행 |

- `Magi.Core`는 복잡한 소켓 경로 계산이나 해시 알고리즘 없이, `magi ide-bridge`와 표준 입출력(stdio) 기반 JSON RPC로 통신하며 UI 렌더링에 집중합니다.
- 브리지 프로세스의 실제 지원 메서드는 `about` 호출 결과를 통해 런타임에 동적으로 확인할 수 있습니다.

---

## 4. 단위 테스트가 보증하는 10대 불변식

어겼을 때 컴파일 에러 없이 런타임에서 조용히 실패하는 항목들을 테스트로 엄격히 통제합니다 (★ 표시는 2026-09-10 실물 인스턴스 검증에서 확인된 항목):

| 규칙 | 어기면 발생하는 장애 |
|---|---|
| `Magi.Core`가 `Microsoft.VisualStudio*`를 참조하지 않는다 | Visual Studio Extensibility SDK 참조 시 단위 테스트 환경에서 실행 불가. 순수 .NET 클래스 라이브러리로 유지해야 독립 테스트 가능 ([설계 §6](../../docs/DESIGN.ko.md)) |
| 소켓 경로를 확장에서 유도하지 않는다 (FNV 상수가 소스에 없음) | 소켓 경로 계산 불일치 시 에러 없이 데몬 연결 실패. 이미 실행 중인 데몬을 감지하지 못하고 중복 실행 시도 |
| 와이어 필드명이 브리지의 Go 소스 선언과 일치한다 | 역직렬화 예외 없이 기본값으로 처리되어 화면에 데이터가 누락됨 |
| ★ 활동 어휘가 양쪽으로 브리지 사양과 정확히 일치한다 | 브리지의 상태 어휘와 매핑되지 않는 값 수신 시 UI 상태 매핑이 누락되어 원시 문자열 노출 |
| `Encoding.UTF8` 대신 BOM 없는 UTF-8 인코딩 사용 | C# 기본 UTF-8은 BOM을 포함하므로 브리지 프로세스의 첫 번째 JSON 파싱이 실패함 (`new UTF8Encoding(false)` 필수) |
| 확장이 참조하는 `%키%`가 리소스에 정의되어 있다 | 미정의 리소스 사용 시 컴파일 경고 없이 메뉴에 `%키%` 문자열이 그대로 노출됨 |
| `string-resources.json`이 csproj에 선언되어 있다 | VSIX 패키징 시 리소스가 누락되어 모든 다국어 텍스트가 미해결 상태로 표시됨 (빌드는 성공하나 런타임 결함) |
| ★ 리소스 선언이 `.vsextension`의 `VSIXSubPath`에 올바르게 배치된다 | VSIXSubPath 누락 시 런타임 셸이 리소스를 찾지 못해 메뉴에 키 문자열이 그대로 표시됨 |
| ★ XAML의 기본 xmlns가 WPF 네임스페이스를 참조한다 | 잘못된 네임스페이스 참조 시 툴윈도 전체가 렌더링 실패하며 `XamlParseException` 예외 화면 출력 |
| ★ XAML 바인딩 속성이 뷰모델에 정의되고 `[DataMember]`가 지정되어 있다 | Out-of-Process IPC 직렬화 특성상 `[DataMember]`가 없으면 에러 없이 빈 문자열로 전달되어 비정상 기본 UI가 노출됨 |


---

## 5. 주요 기술 문서

- [설계 문서 (`docs/DESIGN.ko.md`)](docs/DESIGN.ko.md): 프로세스 외(Out-of-Process) 확장 모델 선정 배경 및 브리지 계약, §10 띄워 보고 나서 남은 것.
- [플랫폼 규약 (`docs/PLATFORM.ko.md`)](docs/PLATFORM.ko.md): Remote UI 요구사항, 선언형 설정 구성 및 VS Code/JetBrains 대비 플랫폼 특성 비교.
- [편집기 제안서 (`docs/proposals/EDITORS.ko.md`)](../../docs/proposals/EDITORS.ko.md): 3대 IDE 클라이언트 개발 타당성 및 브리지 추출 전략.
- [IDE 브리지 사양 (`docs/IDE_BRIDGE.ko.md`)](../../docs/IDE_BRIDGE.ko.md): 공통 브리지 프로세스 프로토콜 규격.

