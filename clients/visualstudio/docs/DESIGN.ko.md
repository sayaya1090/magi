# Visual Studio 확장 — 설계

[↑ 클라이언트 개요](../README.md) · [플랫폼 규약](./PLATFORM.ko.md) · [클라이언트 계약 정본](../../../docs/CLIENTS.ko.md) · [형제: VS Code](../../vscode/docs/DESIGN.ko.md) · [형제: 젯브레인](../../jetbrains/README.md)

> **개발 경과 및 현재 상태**
>
> 1. **설계 단계**: Visual Studio 확장은 Windows 환경 전용으로 빌드됩니다. 초기 작성 환경(macOS, Darwin, `which msbuild` 없음, .NET SDK 9.0.301만 지원, Visual Studio for Mac은 2024년 공식 단종)의 제약으로 인해 구조 설계를 선행하였으며, 실물 빌드 및 검증 절차를 §9에 정의했습니다.
>
> 2. **2026-09-09 실측 검증**: Windows 11 실물 머신(VS Community 2022 **17.14.37628.2**, 워크로드 `ManagedDesktop` · `VisualStudioExtension`)에서 SDK 표면 및 런타임을 실측했습니다. §2의 4대 불확실 확장점, §5의 AppData AF_UNIX 소켓 제약, §6의 테스트 하네스 실태, §7의 CI 러너 사양, §8의 설치 요구조건을 모두 검증하여 확인했습니다.
>
> 3. **2026-09-10 실물 인스턴스 기동 검증**: 실험 인스턴스(VS Community 2022 **17.14.40**)에서 대화 패널(Remote UI) 기동에 성공했습니다. 우측 도킹, 메뉴 정상 표출, IDE 내 브리지 양방향 RPC 연동을 확인했습니다. 사전 설계 당시 짐작으로 분류했던 4개 항목 중 3건(리소스 VSIXSubPath 누락, XAML 기본 xmlns 네임스페이스 오류, Remote UI `[DataMember]` 누락)이 컴파일 경고 없이 런타임 결함으로 이어지던 것을 실측으로 규명하고 해결했습니다 (§10).
>
> [EDITORS 제안서](../../../docs/proposals/EDITORS.ko.md)에서 수립한 "VS Code 선행 구현 후 2개 구현체를 바탕으로 공통 추출" 원칙에 따라, 본 확장은 **두 번째 구현체**로서 공통 로직과 에디터별 전용 로직을 엄격히 분리하는 기준이 됩니다 (§3).

---

## 1. 어느 모델로 만드는가 — 이 설계의 첫 갈림

Visual Studio 확장은 세 가지 아키텍처 모델 중 하나를 선택할 수 있으며, 이 선택이 구현 전반을 결정합니다. 공식 비교표는 다음과 같습니다 (출처: §10).

| 구분 | **VSSDK** | **Community Toolkit** | **VisualStudio.Extensibility** |
|---|---|---|---|
| 런타임 | .NET Framework | .NET Framework | **.NET** |
| VS 프로세스로부터 격리 | ❌ | ❌ | **✅** |
| 단순한 API 표면 | ❌ | ✅ | ✅ |
| 비동기 실행 및 API | ❌ | ❌ | **✅** |
| 지원 시나리오 범위 | ✅ | ✅ | **⏳** |
| 재시작 없는 설치 | ❌ | ❌ | **✅** |
| VS 2019 이하 하위 지원 | ✅ | ✅ | ❌ |

본 프로젝트에서는 **`VisualStudio.Extensibility` Out-of-Process(프로세스 외) 모델**을 채택합니다. 선정 사유는 세 가지입니다.

1. **최신 .NET 런타임 활용**: VSSDK는 .NET Framework 4.8에 고정되어 있습니다. 본 확장의 코어 계층은 소켓 통신, JSON 스트림 파싱, 전사 데이터 가공을 담당하므로 레거시 런타임에 종속될 이유가 없습니다.
2. **프로세스 격리**: 본 확장은 Unix 도메인 소켓 스트림을 상시 유지합니다. 확장이 IDE 메인 프로세스 내부에서 실행될 경우, 확장의 오류가 IDE 전체 크래시로 이어질 위험이 있습니다. 실제로 JetBrains 플러그인 개발 과정에서 dispose 처리 중 발생한 단일 예외로 인해 하위 정리 루틴 전체가 중단되는 문제를 겪은 바 있습니다.
3. **기본 백그라운드 스레드 실행**: VSSDK는 UI 스레드 기반으로 동작하여 수동 스레드 전환이 필수적이며, 공식 문서에서도 이를 주된 버그의 원인으로 지적하고 있습니다. 본 확장은 모든 호출이 프로세스 간 소켓 RPC이므로 백그라운드 비동기 모델이 구조적으로 부합합니다.

다만 본 모델의 기능 지원 범위는 아직 확장 중(⏳)입니다. 필요한 확장점이 지원되지 않을 경우 In-process 모델로의 전환을 검토해야 합니다. 지원 가능 여부는 §2에서 실측합니다.

---

## 2. 기능 대조 — 무엇이 되고 무엇이 아직인가

VS Code의 48개 기능 이식표를 바탕으로 `VisualStudio.Extensibility`의 지원 여부를 판정했습니다. 공식 문서 및 샘플 목록 분석과 함께, 설치된 SDK 어셈블리 공개 타입 전수 조사를 통해 실물 검증을 수행했습니다.

| 기능 항목 | VisualStudio.Extensibility 대응 요소 | 지원 여부 |
|---|---|---|
| 대화 판 | **Tool Window** ("dockable windows within the Visual Studio IDE") | ✓ |
| 계획·계기판 | Tool Window 보조 패널 | ✓ |
| 명령·메뉴 | `CommandConfiguration` (`.vsct` 파일 불필요, C# 코드로 배치 선언) | ✓ |
| 컴패니언 파일 수정 추적 | Editor / Documents API | ✓ |
| 편집 적용 (손) | `Extensibility.Editor().EditAsync(...)` | ✓ |
| 인라인 코드 어노테이션 | **Tagger / Classification tagger** | ✓ |
| 파일 전체 상태 띠 | **Text view margin** | ✓ |
| 상태 표시줄 항목 | **직접 추가 불가**: `StartProgressReportingAsync` 및 `ProgressReporterOptions`는 상태 표시줄 텍스트가 아닌 **Task Status Center** 제어용 옵션입니다. | ✗ |
| 인라인 완성 | **미지원**: 인라인 코드 완성 공개 API가 존재하지 않으며, VS의 회색 추천은 내부 IntelliCode 전용입니다. | ✗ |
| 승인 응답 | 툴 윈도 내부 UI 또는 `Shell.PromptOptions` · `ChoiceDescription` · `ShowDialogAsync` | ✓ |
| 커밋 메시지 초안 작성 | **미지원**: SCM 커밋 창 직접 연동 API가 부재하여, 서드파티 확장들도 메뉴 명령과 클립보드로 우회합니다. | ✗ |
| 진단 위치의 코드 액션 | **본 모델 미지원**: 진단 보고(`DiagnosticsReporter` · `DocumentDiagnostic`)는 가능하나, 제안 액션 전구는 **VSSDK(In-process)** 전용입니다. | ✗ |
| 코드 요소 위 액션 태그 | `ICodeLensProvider` · `VisualCodeLens` · `InvokableCodeLens` | ✓ (신규 발굴) |
| 설정 | `Settings` 네임스페이스의 **선언형 모델** (`Setting` · `SettingCategory` · `ArraySetting<T>`) | ✓ |
| 코어 바이너리 다운로드 및 데몬 기동 | 순수 .NET 프로세스 실행 | ✓ |

### 어셈블리 공개 타입 전수 조사를 통한 실측 검증

공식 문서 요약 목록의 누락 가능성을 배제하기 위해, 실제 설치된 SDK 어셈블리의 공개 타입을 전수 조사했습니다. 설치 경로 `Common7\IDE\CommonExtensions\Microsoft\Extensibility`의 6개 어셈블리(공개 타입 248개)와 `Editor` 어셈블리, 그리고 `CommonExtensions\Microsoft` 전체 1,336개 어셈블리(공개 타입 59,158개)의 브로커 계약(`RpcContracts.*`)을 전수 분석했습니다.

| 검색 키워드 | Extensibility SDK 표면 | 브로커 계약 (`RpcContracts.*`) |
|---|---|---|
| `StatusBar` | 0 | 0 |
| `Completion` | 0 | 0 |
| `SourceControl` · `Scm` | 0 | 0 |
| `CodeAction` · `QuickFix` · `SuggestedAction` | 0 | 0 |
| `Markdown` | 0 | — |

브로커 계약까지 조사한 이유는 Out-of-Process 확장이 `IServiceFactory`나 `ServiceHubServiceMoniker`를 통해 브로커 서비스를 간접 호출할 수 있는지 확인하기 위함이었습니다. 브로커 계약에도 해당 API는 존재하지 않았습니다 (`RpcContracts`의 `Commit` 3건은 모두 `UnifiedSettings`의 설정 커밋입니다).

이는 Visual Studio 자체에 기능이 없다는 의미가 아니라, **Out-of-Process 확장에 공개된 인터페이스가 존재하지 않는다**는 사실을 나타냅니다. 실측 버전은 `Microsoft.VisualStudio.Extensibility.dll` **17.14.2099**(파일 버전 17.14.2099.59265), NuGet `Microsoft.VisualStudio.Extensibility.Sdk` 최신 **17.14.40608**입니다.

### 공식 문서 및 API 참조 대조

타입 조사 결과를 공식 문서 및 API 참조와 대조하여 검증했습니다.

| 대조 대상 | 확인 내용 |
|---|---|
| 공식 문서 기능 영역 목록 | 14개 영역(command, debugger-visualizer, diagnostics, dialog, document, editor, language-server-provider, output-window, project, settings, tool-window, user-prompt 등) 중 상태 표시줄, 완성, 소스 제어, 코드 액션은 포함되어 있지 않습니다. |
| `ShellExtensibility` API 참조 | 패키지 버전이 **17.14.2088**로 실측 대상(17.14.2099)과 동일 계열입니다. |
| `ProgressReporterOptions` | 문서상 "Task Status Center의 동작을 조정하는 옵션"으로 명시되어 있으며, 상태 표시줄의 해당 아이콘을 통해 진입하는 작업 센터 전용입니다. |
| 전구 코드 액션 안내서 | `walkthrough-displaying-light-bulb-suggestions` 문서는 **VSSDK(In-process) MEF** 전용 안내서입니다. |
| 커밋 입력창 연동 | 서드파티 확장들도 입력창 직접 제어 API가 없어 메뉴 명령 및 클립보드로 우회하고 있습니다. |
| 인라인 코드 완성 | 서드파티 공개 API가 없으며, 검색 결과는 모두 VS Code API이거나 VS 자체 IntelliCode 기능입니다. |

### Out-of-Process 모델 유지 결론

4대 확장점의 미지원 확인에도 불구하고 In-process 모델로 회귀하지 않습니다. 1차 마일스톤에 해당 기능들이 필수적이지 않기 때문입니다:

- 상태 표시는 **대화 툴 윈도 상단 헤더**에 표시할 수 있으며, JetBrains 및 VS Code 클라이언트 역시 자체 패널에 상태를 표시합니다.
- 인라인 완성, 커밋 메시지 생성, 코드 액션은 1차 구현 대상이 아니며, 발견·악수·대화창 척추 구현과 에디터 내 태거/마진 연동이 우선입니다.
- 반면 `ICodeLensProvider`는 VS Code 이식표에 없던 새로운 확장점으로 발굴되었으며, 향후 함수 단위 질문 기능 등의 연동에 활용할 수 있습니다.

### Remote UI를 통한 화면 구성 및 제약 사항

Out-of-Process 모델이므로 확장이 WPF 컨트롤을 직접 렌더링할 수 없으며, 확장이 제공한 XAML을 Visual Studio 프로세스가 렌더링하는 Remote UI 방식을 따릅니다.

이는 웹뷰 기반인 VS Code와 비교할 때 **Visual Studio의 네이티브 테마가 자동 적용된다**는 장점이 있습니다. 반면 **마크다운 렌더러가 기본 제공되지 않는다**는 제약이 있습니다.

`Extensibility.UI`의 공개 타입은 7개(`RemoteUserControl`, `XamlFragment`, `ObservableList<T>`, `ResourceDictionaryCollection`, `AsyncCommand`, `IAsyncCommand`, `NotifyPropertyChangedObject`)에 불과합니다. Remote UI는 렌더링 엔진이 아닌 XAML 전달 파이프라인 역할을 수행합니다.

#### Remote UI의 3대 핵심 제약

1. **커스텀 컨트롤 참조 금지**: 공식 문서에 따르면 "Remote UI는 확장의 사용자 지정 컨트롤 참조를 허용하지 않습니다." XAML은 Visual Studio 프로세스의 타입만 참조할 수 있으므로, `MarkdownTextBlock`과 같은 컨트롤을 임베드할 수 없습니다.
2. **코드 비하인드 및 이벤트 핸들러 부재**: 순수 MVVM, 데이터 바인딩, 비동기 커맨드, XAML 트리거만으로 작성해야 합니다.
3. **테마 스타일 참조**: `Microsoft.VisualStudio.Shell` 및 `PlatformUI` 스타일을 XAML 내부 리소스로 참조하여 테마를 적용합니다.

**해결 방안: 형상(Shape) 단위 조립**: 대화 전사의 문단을 파싱하여 뷰모델에 텍스트 블록, 코드 블록 등의 구조화된 형상으로 분해하고, XAML에서는 표준 WPF 요소를 활용한 `DataTemplate`으로 렌더링합니다. 파싱 로직은 `Magi.Core`에서 처리되어 IDE 없이 독립적으로 검증되며, XAML은 이를 나열하는 역할만 담당합니다.

---

## 3. 두 벌을 보고 나서 — 무엇이 진짜 공통인가

EDITORS 제안서 §7에서 "2개 구현체를 먼저 완성한 뒤 공통을 추출한다"고 명시한 원칙에 따라, VS Code 구현 완료 후 공통 기능과 편집기 고유 기능을 명확히 분리했습니다.

### 공통인 것 (언어를 변경해도 동일하게 유지되는 로직)

| | 기능 항목 | 공통화 사유 |
|---|---|---|
| 1 | **소켓 경로 유도** | 코어 데몬이 정한 필수 규칙입니다. 계산 불일치 시 **에러 없이** 연결이 실패하며, VS Code 포팅 당시 두 차례의 시행착오를 유발했습니다. |
| 2 | **줄 단위 JSON 왕복과 스트림** | 프로토콜 계층 사양입니다. |
| 3 | **대화 전사(Transcript) → 행 변환** | "행을 구성하는 규칙은 한 벌"이라는 불변식에도 불구하고 클라이언트마다 중복 구현되고 있던 영역입니다. |
| 4 | **「무엇을 하는 중인가」 상태 요약 단어** | 모름(`unknown`) · 미실행(`not-running`) · 도는 중(`working`) · 기다림(`waiting`) · 연결됨(`attached`) 분류 규칙입니다. |
| 5 | **도구 권한 승인 어휘** | `allow` · `deny` · `always` 승인 상태 머신입니다. |
| 6 | **훑어본 말(인라인 어노테이션) 가르기** | 줄 번호 매핑 및 구분자가 탭에 국한되지 않는다는 실측을 포함합니다. |
| 7 | **코드 완성의 겹침 제거** | 완성 윈도우 계산 및 접두/접미 중복 처리입니다. |
| 8 | **바이너리 판 고르기와 받기** | 플랫폼별 바이너리 릴리스 조회 및 버전 확인입니다. |

이 8개 공통 기능을 VS Code에서는 TypeScript로 작성하였고, Visual Studio에서 C#으로 재작성하면 JetBrains의 Kotlin 구현까지 더해져 **총 세 벌**이 됩니다. 동일한 사양이 세 군데로 분산되면 로직 불일치가 필연적으로 발생합니다.

### 편집기마다 다른 것

명령 등록 위치, 화면 렌더링 엔진(웹뷰 / XAML / Swing), 에디터별 인레이 및 자동완성 API, 설정 화면 구성 방식, 알림 시스템 등은 각 편집기의 고유 규약에 종속되며, 이는 각 클라이언트 확장이 전담해야 합니다.

### `magi ide-bridge` 서브커맨드 도입 결정

두 클라이언트의 구현 경험을 바탕으로 공통 8대 기능만을 전담하는 `magi ide-bridge` 서브커맨드 도입을 결정했습니다 (2026-09-09 확정, 사양서: [`docs/IDE_BRIDGE.ko.md`](../../../docs/IDE_BRIDGE.ko.md)).

- 코어 바이너리에 `magi ide-bridge` 명령을 두고 stdio 기반 JSON 통신을 수행하며, 공통 8대 기능을 브리지가 전담합니다. 편집기 확장은 **호출 및 화면 렌더링**에만 집중합니다.
- 확장은 이미 코어 바이너리를 관리하므로 추가 프로세스 실행은 새로운 부담이 아닙니다.
- 신규 편집기 확장 개발 비용이 "8대 공통 로직 + 화면"에서 "화면"으로 대폭 절감되며, Visual Studio 확장이 이 구조의 첫 번째 수혜자입니다.
- 계약의 정본은 [`docs/IDE_BRIDGE`](../../../docs/IDE_BRIDGE.ko.md)에 수록되었으며, 해당 문서 §1은 에디터 간 완성 처리 불일치 실측(창 크기: 전체 파일 vs 4,000자, 중복 제거 여부)을 도입 근거로 제시합니다.

---

## 4. 모듈 갈림

JetBrains 및 VS Code와 동일하게 계층을 분리하여, 프로토콜 및 데이터 가공 로직을 IDE 없이 검증할 수 있도록 설계했습니다.

```
src/Magi.Core/        VisualStudio.Extensibility를 참조하지 않는 순수 .NET 라이브러리 → IDE 없이 독립 테스트
src/Magi.Extension/   VisualStudio.Extensibility SDK를 참조하는 유일한 계층
test/Magi.Core.Tests/ Magi.Core의 와이어 및 상태 불변식 단위 테스트
```

**아키텍처 불변식 테스트 (`LayeringTests`)**: `Magi.Core`가 `Microsoft.VisualStudio.Extensibility`를 참조할 경우 테스트가 실패하도록 통제합니다. JetBrains의 `ArchitectureTest`, VS Code의 `layering.test.ts`와 동일한 역할을 수행하며, 계층 경계가 무너지는 것을 원천 차단합니다.

---

## 5. 소켓 계약 — Windows 에서 달라지는 것

기본 계약 규칙은 동일하며([VS Code 설계 §5](../../vscode/docs/DESIGN.ko.md) 골든 벡터 포함), Windows 환경 고유의 차이점 세 가지를 반영합니다.

1. **설정 디렉토리 경로**: `%AppData%\magi`를 기본으로 사용합니다 (`MAGI_CONFIG_DIR` > `AppData` 환경변수 > `<home>\AppData\Roaming\magi`).
2. **유닉스 도메인 소켓 지원**: Windows 10 1803 이상에서 지원되며, .NET에서는 `UnixDomainSocketEndPoint`를 통해 접근합니다.
3. ⚠ **`%AppData%` 하위 AF_UNIX 소켓 통신 결함 (실측 검증)**:

### `%AppData%` 하위 AF_UNIX 실측 (2026-09-09, Windows 11, .NET 9)

디렉토리 생성 → bind · listen → connect → 5바이트 송수신 → 소켓 닫기 → 파일 삭제 절차를 실측했습니다. bind와 connect 사이에는 어떠한 파일 속성 접근도 배제하여 측정 간섭을 방지했습니다.

| 디렉토리 경로 | bind | connect | 파일 삭제 |
|---|---|---|---|
| `%AppData%\…` (Roaming) | 성공 | **`WSAEINVAL` (10022)** | 삭제 불가 |
| `%LocalAppData%\…` | 성공 | **`WSAEINVAL` (10022)** | 삭제 불가 |
| `%LocalAppData%\Microsoft\…` | 성공 | **`WSAEINVAL` (10022)** | 삭제 불가 |
| `C:\Users\<계정>\AppData\…` (직하위) | 성공 | **`WSAEINVAL` (10022)** | 삭제 불가 |
| `%LocalAppData%\Temp\…` (깊은 하위 경로 포함) | 성공 | **0ms (정상)** | 정상 삭제 |
| `C:\Users\<계정>\…` (AppData 외부) | 성공 | **0ms (정상)** | 정상 삭제 |
| `…\Documents\…` · `C:\Users\Public\…` | 성공 | **0ms (정상)** | 정상 삭제 |

**결함 경계는 `AppData` 트리 전체이며, 유일한 예외는 `Temp` 하위 경로뿐입니다.** bind는 항상 성공하고 파일도 생성되므로 기동 측에서는 정상 실행된 것으로 인식하지만, 연결하는 클라이언트 측에서 `WSAEINVAL`(10022) 오류가 발생하여 최종적으로 "연결 타임아웃" 증상으로 표출됩니다.

더 심각한 결함은 **해당 소켓 파일이 삭제되지 않는다는 점**입니다. `File.Delete`는 물론, 코어 데몬이 [`listen_windows.go`](../../../internal/adapter/daemon/listen_windows.go)에서 사용하는 복구 로직(`FILE_FLAG_OPEN_REPARSE_POINT | FILE_FLAG_DELETE_ON_CLOSE`)조차 **ERROR 1920 (`ERROR_CANT_ACCESS_FILE`)**로 실패합니다. 코어의 복구 경로가 Windows 기본 설정 디렉토리에서 무력화되므로, 고스트 소켓이 한 번 남으면 해당 주소를 재사용할 수 없게 됩니다.

**원인 규명 분석 결과 (4가지 요인 배제)**:
- **주소 경로 길이**: 실패 경로(46–61바이트)는 상한(약 100바이트) 미만이며, 성공한 `Temp` 경로가 64바이트로 더 길었습니다.
- **디렉토리 ACL**: `AppData`의 AppContainer SID(`S-1-15-3-…`) 상속을 끊고 `Temp`와 동일한 3개 ACE로 재구성해도 동일하게 10022 오류가 발생했습니다.
- **디렉토리 속성 및 볼륨**: 동일한 NTFS `C:` 드라이브의 표준 디렉토리입니다.
- **도구 샌드박스**: 샌드박스를 비활성화한 상태에서도 결과가 동일했습니다.

가장 유력한 원인은 **`AppData` 경로에 등록된 파일 시스템 필터 드라이버**로 추정됩니다 (`fltmc filters`는 관리자 권한 필요, 기본 보안 제품은 Windows Defender).

⚠ **코어 문서 주석과의 괴리**: `SocketDir` 주석 및 `socketdir_test.go`는 `MAGI_SOCKET_DIR` 분리 이유를 **"소켓 주소가 100바이트 상한을 초과하기 때문"**으로만 설명하고 있습니다. 그러나 본 실측에서는 사용자 이름이 5글자여도 `AppData` 하위에서는 통신이 전면 차단되었습니다.

**확장 적용 지침**: 소켓 파일을 `%AppData%` 하위에 생성하지 않으며, `MAGI_SOCKET_DIR`을 지정하여 AppData 외부에 소켓을 배치하도록 통제합니다 (§6 골든 테스트로 검증).

**골든 테스트 검증 항목 (VS Code와 동일)**:
- **비표준 FNV-1a 해시 상수 유지**: 코어 데몬은 `1469598103934665603`을 사용합니다 (표준 FNV-1a의 `14695981039346656037`에서 1자리가 빠져 있음). 이를 임의로 수정하면 데몬과의 소켓 경로 일치성이 깨집니다.
- **루트 디렉토리 Base 연산**: Go의 `filepath.Base("/")`는 `"/"`를 반환하는 반면, .NET의 `Path.GetFileName`은 루트 경로에서 빈 문자열을 반환하므로 분기 처리가 필요합니다.

---

## 6. 무엇을 어디서 재나

| 계층 | 검증 방법 |
|---|---|
| `Magi.Core` | `dotnet test` (IDE 없이 독립 고속 실행) |
| **와이어 대조** | Go 소스코드를 직접 파싱하여 필드 이름을 맞춥니다. 필드 짝을 충분히 찾지 못하면 **테스트 자체가 실패합니다.** |
| 소켓 키 | VS Code와 동일한 골든 테스트 벡터를 검증합니다. |
| 계층 경계 | `Magi.Core`가 Extensibility SDK를 참조하지 않음을 검증합니다. |
| 매니페스트 및 확장점 | ⚠ **`@vscode/test-electron`에 상응하는 1급 테스트 하네스가 부재합니다** (아래 상술). |

**와이어 대조는 필수 조건입니다.** C#의 `System.Text.Json` 역시 알 수 없는 필드를 조용히 무시하고 누락된 필드는 기본값으로 역직렬화합니다. 이는 Kotlin의 `ignoreUnknownKeys`, TypeScript의 `undefined`와 동일한 런타임 함정입니다. 이름이 불일치할 경우 예외가 발생하지 않고 기본값이 대입되므로, 화면에는 에러 없이 "실행 중이 아님"이 표시됩니다.

### 실물 확장의 자동화 테스트 도구 현황 조사 (2026-09-09)

초기 조사에서 패키지 부재로 오판했던 부분을 정정합니다. nuget.org에는 없으나 Microsoft의 `vs-extension-testing`이 **Azure DevOps 피드**(`dev.azure.com/azure-public/vside/_artifacts/feed/vssdk`)를 통해 배포되고 있었습니다.

| 후보 도구 | 존재 여부 | 적용 가능성 검토 |
|---|---|---|
| `Microsoft.VisualStudio.Extensibility.Testing.Xunit` | **존재** (Azure DevOps 피드) | ⚠ **VSIX/VSSDK 확장 전용**입니다. 문서상 `VisualStudio.Extensibility`(프로세스 외) 모델에 대한 지원이 없으며, 저장소가 보관 처리되어 `dotnet/roslyn`으로 이관되었습니다. |
| `Microsoft.VisualStudio.Sdk.TestFramework(.Xunit)` v17.11.66 | 존재 | ⚠ **VSSDK(In-process) 전용**입니다. VS 내부 서비스를 모의하는 도구이므로 프로세스 외 모델과는 아키텍처가 상이합니다. |
| `VsixTesting.Xunit` v0.1.78 · `xunit.vsix` v0.9.3 | 존재 | 서드파티 도구로서 VS 실험 인스턴스를 기동하여 검증합니다. |

세 도구 모두 실험 인스턴스에 VSIX를 설치하고 IDE 내부에서 실행되는 방식이며, 어느 것도 **프로세스 외 모델을 지원하지 않습니다.** `@vscode/test-electron`처럼 프로세스 외 확장을 격리된 상태 그대로 기동하여 검증하는 1급 도구는 존재하지 않았습니다.

따라서 §4의 계층 분리가 결정적인 역할을 합니다. IDE 결합 테스트 경로가 취약할수록 `Magi.Core`에 핵심 로직(프로토콜, 소켓 키, 전사 가공)을 집중하여 `dotnet test`로 완전 검증하고, IDE 확장 계층에는 가벼운 연동부만 남겨 위험을 통제합니다.

---

## 7. 배포

`vscode-v*`, `jetbrains-v*`와 동일한 패턴으로 `vsstudio-v*` 릴리스 레인을 구성합니다. 배포 아티팩트는 `.vsix`이며, 버전 번호는 Git 태그를 정본으로 사용합니다.

⚠ **CI 환경은 Windows 러너(`windows-latest`)가 필수입니다.** 다른 에디터 클라이언트는 `ubuntu-latest`에서 빌드되나, Visual Studio 확장은 Windows 환경에서만 컴파일됩니다.

✅ **GitHub Actions 러너 워크로드 확인 (2026-09-09)**: `actions/runner-images` 사양서를 직접 분석한 결과, `windows-2022` 및 `windows-2025` 러너 **모두** Visual Studio Enterprise 2022 17.14.37614.0과 `Microsoft.VisualStudio.Workload.VisualStudioExtension`, `Microsoft.VisualStudio.Workload.ManagedDesktop` 워크로드가 사전 탑재되어 있습니다. 러너에서 별도의 워크로드 설치 작업이 불필요하며, 버전 역시 §8의 요구조건(17.9 이상)을 충족합니다.

---

## 8. 이 편집기가 요구하는 것

- **Visual Studio 2022 17.9 이상**, `Visual Studio extension development` 워크로드 (`Microsoft.VisualStudio.Workload.VisualStudioExtension`).
- **Windows 전용**: Visual Studio for Mac은 2024년 공식 단종되었습니다.

✅ **실제 설치 및 실측 검증 (2026-09-09)**: 요구조건이 정확함을 확인하였으며, 후속 작업을 위해 세 가지 주의사항을 기록합니다.

- `CoreEditor`만 설치된 Visual Studio 환경에서는 **`.csproj` 파일조차 열리지 않습니다**: `error MSB4236: 지정된 'Microsoft.NET.Sdk' SDK를 찾을 수 없습니다`. `ManagedDesktop` 워크로드를 함께 설치해야 문제가 해결되며, 설치 후 VS MSBuild를 통해 SDK 형식 프로젝트 복원이 483ms 만에 완료되었습니다.
- 워크로드 설치 시 `Microsoft.VisualStudio.Component.VSSDK` 컴포넌트가 함께 포함되어 패키지 수가 126개에서 **582개**로 증가합니다.
- 설치 관리자의 무인 설치(`--passive`) 옵션은 **반드시 관리자 권한으로 승격된 프로세스에서 실행**해야 합니다. 비승격 실행 시 `Exit Code: 5007`로 즉시 종료되며 로그에 "Commands with --quiet or --passive should be run elevated from the beginning"이 기록됩니다. Visual Studio가 실행 중인 상태에서는 사전 검사 `VSProcessesRunning`에 걸려 오류 코드 **8006**이 반환됩니다. 또한 `--installPath`는 경로 내 공백이 포함될 수 있으므로 **따옴표로 감싸서 전달**해야 합니다. 따옴표를 누락하면 "일치하는 설치된 제품을 찾을 수 없습니다" 메시지와 함께 실패하면서도 **종료 코드는 0**을 반환합니다(해당 0은 설치 관리자가 자체 버전을 갱신한 코드일 뿐입니다). 따라서 성공 여부는 종료 코드가 아닌 **`vswhere` 도구의 출력으로 판정**해야 합니다.
- VS 2019 이하 버전은 본 Out-of-Process 모델을 지원하지 않습니다. 구버전 지원을 위해서는 VSSDK 기반의 별도 프로젝트를 유지해야 하므로, 본 확장의 지원 대상에서 제외합니다.

---

## 9. 순서

| 단계 | 작업 내용 | 실행 환경 | 상태 |
|---|---|---|---|
| 0 | **`magi ide-bridge`** — 공통 8대 기능(§3), [사양서](../../../docs/IDE_BRIDGE.ko.md) | 코어 | ✅ 기본 RPC 척추(`about`, `daemon`) 구현 완료. 유도 6종 연동 진행 중 |
| 1 | 4대 불확실 확장점 실물 확인 (§2) | **Windows** | ✅ 2026-09-09 실측 완료 (4개 항목 모두 미지원 확인) |
| 2 | 척추 구현 — 발견, 악수, 대화 툴 윈도, 데몬 기동 | Windows | ✅ **실물 기동 완료 (2026-09-10)**: `Magi.Core` + `Magi.Extension` + 단위 테스트 36건 통과. 패널 상태 렌더링 및 컴패니언 기동 지원. 전사는 `watch` 도입 대기 중(§10) |
| 3 | 편집기 연동 — 태거, 마진 | Windows | ⏳ 계획 수립 |
| 4 | 진입점 및 계획 패널 확장 | Windows | ⏳ 계획 수립 |
| 5 | `vsstudio-v*` CI 배포 레인 구성 | CI (`windows-latest`) | ⏳ 계획 수립 |

### Windows 개발 환경 준비 절차

단계 1의 검증이 후속 구현의 전제가 되므로 이를 선행했습니다. 2026-09-09에 실제 검증을 완료하였으며, 아래 절차는 단계 2 이후 작업을 위한 환경 준비 절차입니다.

준비물: Visual Studio 2022 **17.9 이상** + `Visual Studio extension development` 워크로드. 설치 확인 명령어:

```powershell
& "${env:ProgramFiles(x86)}\Microsoft Visual Studio\Installer\vswhere.exe" `
    -latest -products * -requires Microsoft.VisualStudio.Component.VSSDK `
    -property catalog_productDisplayVersion
```

확장 프로젝트 템플릿 설치 및 템플릿 생성 확인:

```powershell
dotnet new install Microsoft.VisualStudio.Extensibility.Templates
dotnet new vsextension -n MagiProbe
cd MagiProbe; dotnet build
```

**브리지 프로세스 수동 통신 검증**:

```powershell
go build -o magi.exe ./cmd/magi
'{"id":1,"method":"about"}' | .\magi.exe ide-bridge -workspace C:\path\to\project
```

`daemon`이 `null`이고 `why` 필드가 존재하면 해당 워크스페이스에 활성 컴패니언이 없는 상태를 나타냅니다(정상 응답). Windows 환경에서는 `%AppData%` 하위 AF_UNIX 결함(§5)이 발생하므로, 처음부터 `MAGI_SOCKET_DIR`을 통해 소켓을 `AppData` 외부에 배치해야 합니다.

---

## 10. 재지 않은 것

본 문서에서는 검증된 사실과 미검증 가설을 명확히 구분합니다. 2026-09-09에 5개 항목이 실측 검증되어 각 절(§2 4대 확장점, §5 AF_UNIX 소켓 결함, §6 테스트 하네스 현황, §7 CI 러너 사양, §8 설치 요구조건)로 이동했습니다. **2026-09-10에는 실물 인스턴스 기동 검증이 완료되었습니다.**

### ✅ 실물 기동 검증 완료 내역 (2026-09-10)

실험 인스턴스(`devenv /rootSuffix Exp`, VS Community 2022 **17.14.40**)에서 확장을 기동하여 사전 가설 4개 항목을 검증했습니다.

| 사전 가설 항목 | 실측 결과 |
|---|---|
| 패널의 정상 렌더링 여부 | ❌→✅ **초기 렌더링 실패 후 해결**: 대화 영역에 `XamlParseException`이 발생하던 결함을 해결하고 정상 렌더링을 확인했습니다. |
| 우측 도킹 동작 여부 | ✅ **정상 도킹 확인**: DocumentWell + `Dock.Right` 조합으로 정상 도킹됨을 확인했습니다. |
| 메뉴 이름의 정상 표시 여부 | ❌→✅ **초기 %키% 노출 후 해결**: `%Magi.OpenConversation.DisplayName%`가 그대로 노출되던 결함을 해결했습니다. |
| IDE 내 브리지 양방향 RPC 연동 | ✅ **정상 동작 확인**: `MAGI_SOCKET_DIR`이 자식 프로세스에 상속되어 §5의 소켓 함정을 정상적으로 회피함을 확인했습니다. |

**컴파일 경고 없이 발생했던 3대 런타임 결함 분석**:

1. **`string-resources.json`의 VSIX 패키지 루트 배치 오류**:
   - Visual Studio 셸은 다국어 리소스를 `.vsextension/` 경로에서 탐색합니다(로케일 폴더가 그 하위에 위치하며 루트는 기본값).
   - csproj에서 `<VSIXSubPath>.vsextension</VSIXSubPath>`를 명시해야 정상 로드됩니다.
   - 사전 단위 테스트는 키 정의 여부와 csproj 파일 포함 여부만 검사하여 초록불을 유지했으나, 실제 셸이 접근할 수 있는 경로와는 차이가 있었습니다.
2. **XAML 기본 xmlns 네임스페이스 지정 오류**:
   - XAML의 기본 xmlns를 extensibility 네임스페이스로 지정하면 `DataTemplate` 타입을 해석하지 못합니다.
   - 렌더링되는 요소는 모두 WPF 타입이므로 기본 네임스페이스는 WPF여야 하며, `.../extensibility/2022/xaml`은 Remote UI 전용 추가 요소이므로 접두사(`vs:`)에 지정해야 합니다. 이를 거꾸로 선언하면 루트 요소부터 해석되지 않아 패널 전체가 `XamlParseException` 예외 화면으로 대체됩니다.
3. **뷰모델 속성의 `[DataMember]` 누락으로 인한 프록시 복제 실패**:
   - Remote UI 패널은 Visual Studio 프로세스 내부의 **프록시 객체**를 대상으로 바인딩됩니다. 공식 문서에 따르면 "직렬화 가능한 타입의 `DataMember` 속성만 데이터 바인딩이 가능"합니다.
   - `[DataMember]`가 누락되면 예외 없이 빈 문자열이나 기본값으로 처리됩니다. 이로 인해 `StartVisibility` 속성이 전달되지 않아 WPF 기본값인 `Visible`이 적용되었고, 데몬 실행 여부를 묻지도 않은 솔루션에 컴패니언 기동 버튼이 노출되는 결함이 발생했습니다.

현재는 세 결함 모두 단위 테스트(README의 ★ 3개 항목)로 엄격히 방어되고 있습니다.

### 향후 과제

- **툴 윈도 배치 속성 사양**: `Placement`는 도킹 대상을 지정하며(문서 웰, 플로팅 창, 타 툴 윈도 GUID 중 택일), 방향은 `DockDirection`으로 결정됩니다. "우측 도크"라는 단일 `Placement` 값은 존재하지 않으며, 문서 웰 + `Dock.Right` 조합으로 선언해야 합니다.
- **Remote UI 대화 전사 렌더링 연동**: Remote UI 제약(공개 타입 7개, 렌더러 부재, 커스텀 컨트롤 금지, 코드 비하인드 부재, `[DataMember]` 직렬화 필수)에 따라, 파싱은 `Magi.Core`에서 수행하고 XAML은 `DataTemplate`으로 형태를 조립하는 구조로 확정되었습니다. 현재 패널은 마지막 단일 응답만 표시하며, 전사 스트림을 위한 `watch` 메서드([IDE_BRIDGE §5](../../../docs/IDE_BRIDGE.ko.md))가 브리지에 추가되는 대로 전사 렌더링을 연동할 계획입니다.
- **`%AppData%` 하위 AF_UNIX 결함의 근본 원인 규명**: 결함의 현상(§5)은 정확히 규명되었으나, 경로 길이, ACL, 볼륨 속성, 샌드박스가 아닌 파일 시스템 필터 드라이버 추정 원인에 대해 향후 추가 환경에서의 교차 검증이 필요합니다.
- **`ICodeLensProvider` 실물 렌더링 검증**: 공개 SDK 표면에 존재함을 확인하였으나 실물 렌더링은 1차 마일스톤 이후 구현할 예정입니다.
- **코드 규모 통계**: JetBrains 플러그인은 9,778줄이며 VS Code 확장은 코어 및 IDE 어댑터를 신규 구현했습니다. Visual Studio 확장의 세부 규모는 기능 구현 진척도에 따라 집계할 예정입니다.

