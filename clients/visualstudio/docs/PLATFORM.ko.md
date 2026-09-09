# Visual Studio 플랫폼 규약 — 대조표

[↑ 클라이언트 개요](../README.md) · [설계](./DESIGN.ko.md) · [형제: VS Code 대조표](../../vscode/docs/PLATFORM.ko.md) · [형제: 젯브레인 대조표](../../jetbrains/docs/PLATFORM.ko.md)

> **문서 목적**: 자체 내부 규칙과 **Visual Studio 플랫폼 규약**은 구분되어야 합니다. 플랫폼 고유의 규칙을 사전에 검토하지 않고 구현할 경우, 동작은 하더라도 해당 IDE의 사용자 경험에 부합하지 않는 결과물이 생성됩니다. JetBrains 클라이언트 개발 당시 이 기준 문서의 부재로 세 차례의 재작업이 발생하였고, VS Code 클라이언트에서는 코드 작성에 앞서 규약을 정리함으로써 동일한 시행착오를 방지했습니다.
>
> **Visual Studio 문서의 성격**: VS Code는 8페이지에 달하는 공식 UX 가이드라인과 명시적인 권장/금지(✔️/❌) 목록을 제공하여 원문을 직접 반영할 수 있었습니다. 반면 Visual Studio의 확장 문서는 **모델 선택 및 API 명세 중심이며 UX 규약 성격이 아닙니다.** 이에 따라 본 문서의 대조표는 단순 API 존재 여부와 권장 규약을 명확히 구분하여 기술했습니다.
>
> **2026-09-09 실측을 통한 검증**: 문서의 불확실성을 해소하기 위해 설치된 SDK 어셈블리의 공개 타입을 전수 조사하여 실측했습니다. 이를 통해 §3의 3개 불확실 항목을 확정하고, §4의 설정 관련 가설을 바로잡았습니다.
>
> **2026-09-10 실물 런타임 기동 검증**: 단순 타입 검사로는 확인할 수 없었던 런타임 동작 특성 3가지를 실험 인스턴스 검증을 통해 확인했습니다: 기본 XAML 네임스페이스, 프로세스 외(Out-of-Process) 프록시 직렬화 속성(`[DataMember]`), 그리고 다국어 리소스(`%키%`)의 VSIX 패키지 내 해석 경로입니다. 세 항목 모두 컴파일 시점에는 오류가 보고되지 않으나 런타임에 결함을 유발하는 요소들로, 실측을 통해 원인을 규명하고 해결했습니다([설계 §10](./DESIGN.ko.md)).

---

## 1. 모델이 규약이다

VS Code에서는 액티비티 바·패널·상태 표시줄 등의 위치 배치가 주요 규약이었습니다. Visual Studio에서는 **어느 확장 모델을 채택하는가**가 이러한 위치와 권한을 직접 결정합니다. 확장 모델 자체가 지원 가능한 기능 범위를 규정하기 때문입니다.

| 규약 | 본 확장 계획 |
|---|---|
| 신규 확장은 VisualStudio.Extensibility로 시작할 것 (공식 권고) | ✓ 권고에 따라 VisualStudio.Extensibility를 채택합니다. |
| 필요한 확장점이 없는 경우 In-process로 전환하여 VSSDK 사용 | ⏸ **원칙적으로 In-process 전환을 배제합니다.** 전환 시 .NET Framework 4.8로 회귀하고 프로세스 격리를 상실하게 됩니다. 4대 불확실 확장점(설계 §2)의 실측 결과를 바탕으로 Out-of-Process 모델을 유지합니다. |
| VS 2019 이하 버전을 지원하려면 별도 VSIX 프로젝트 구성 | ✗ **지원하지 않습니다.** 6개 클라이언트를 관리하는 상황에서 하위 호환 전용 중복 프로젝트를 유지하지 않습니다. |
| 명령은 코드로 선언 (`.vsct` 파일 불필요) | ✓ `CommandConfiguration` 코드로 선언합니다. |
| 모든 명령이 백그라운드 스레드에서 실행 | ✓ 본 확장의 모든 호출은 프로세스 간 소켓 왕복 통신이므로 백그라운드 실행 방식이 적합합니다. |

## 2. 화면 — Remote UI

프로세스 외(Out-of-Process) 모델이므로 WPF 요소를 IDE 프로세스에 직접 그릴 수 없습니다. 확장이 XAML 마크업과 데이터를 제공하고, Visual Studio 셸이 이를 렌더링하는 Remote UI 방식을 사용합니다.

| 영역 | 본 확장 계획 |
|---|---|
| 테마 | **확장에서 색상을 수동으로 정의하지 않습니다.** IDE가 XAML 컨트롤에 테마 스타일을 자동 적용합니다. 이는 색상 토큰을 수동 매핑해야 했던 VS Code 웹뷰 방식보다 유리한 점입니다. |
| 마크다운 | ⚠ **내장 렌더러가 부재하며, 커스텀 컨트롤을 제작하여 삽입할 수도 없습니다.** 공식 문서에 "Remote UI는 사용자 지정 컨트롤 참조를 허용하지 않는다"고 명시되어 있어, XAML은 VS 메인 프로세스의 타입만 참조할 수 있습니다. 따라서 마크다운 파싱은 `Magi.Core`에서 수행하고, XAML은 표준 WPF 요소를 결합한 `DataTemplate`을 통해 형태(Shape) 단위로 조립하여 렌더링합니다([설계 §2](./DESIGN.ko.md)). |
| 코드 비하인드 | **지원되지 않습니다.** 이벤트 핸들러도 작성할 수 없으며, MVVM 패턴, 데이터 바인딩, 비동기 커맨드(`IAsyncCommand`), XAML 트리거만으로 UI를 구성합니다. |
| 네임스페이스 | ⚠ **기본 xmlns는 WPF 네임스페이스여야 합니다.** 렌더링 대상이 모두 WPF 기본 타입이며, `.../extensibility/2022/xaml`은 Remote UI 전용 추가 네임스페이스이므로 접두사(`vs:`)에 지정해야 합니다. 이를 반대로 선언하면 루트 `DataTemplate`부터 인스턴스화되지 않아 패널 전체가 `XamlParseException` 오류 화면으로 대체됩니다([설계 §10](./DESIGN.ko.md)). |
| 바인딩 대상 속성 | ⚠ **`[DataContract]` 및 `[DataMember]`가 지정된 멤버만 바인딩됩니다.** 패널은 VS 프로세스의 **프록시 객체**를 대상으로 바인딩되며, 문서상 "직렬화 가능한 타입의 `DataMember` 속성만 데이터 바인딩이 가능하다"고 규정되어 있습니다. 이를 누락할 경우 에러나 경고 없이 화면의 모든 바인딩 값이 빈칸으로 렌더링됩니다([설계 §10](./DESIGN.ko.md)). |
| 리소스 문자열 | ⚠ `%키%` 형식의 다국어 문자열은 **`.vsextension/string-resources.json`** 경로에서 해석됩니다. 패키지 루트에 배치하면 셸이 이를 인식하지 못하므로, csproj에서 `<VSIXSubPath>.vsextension</VSIXSubPath>`를 반드시 지정해야 합니다([설계 §10](./DESIGN.ko.md)). |
| 툴 윈도 | 대화 패널 1개, 계획 패널 1개로 제한합니다. 이는 플랫폼 제약이 아닌 본 프로젝트의 설계 원칙입니다. |

## 3. 확장점 — 문서와 실측 대조표

아래 표는 공식 개요 문서 및 샘플 코드에서 확인한 내용과, 실물 SDK 어셈블리 공개 타입 조사를 대조한 결과입니다.

| 확장점 | 공식 문서 기술 여부 | 본 확장 적용 방안 |
|---|---|---|
| Commands | ✓ 지원 (개요 명시) | 모든 메뉴 명령 구현에 사용합니다. |
| Tool windows | ✓ 지원 (개요: "dockable windows within the Visual Studio IDE") | 대화창 및 계획 패널 구현에 사용합니다. |
| Editor / Documents | ✓ 지원 (개요 명시) | 코드 편집 적용 및 텍스트 버퍼 읽기에 사용합니다. |
| Output window | ✓ 지원 (개요 명시) | 사용하지 않습니다. 대화 패널 전사가 해당 역할을 담당합니다. |
| User prompts · Dialogs | ✓ 지원 (개요 명시) | 도구 실행 승인은 맥락 보존을 위해 **툴 윈도 내부**에 배치합니다. |
| Taggers / classification | ✓ 샘플 제공 → **SDK 표면 확인** (`ITextViewTaggerProvider<T>`, `ClassificationTag`, `TextMarkerTag`) | 인라인 파일 분석 어노테이션 표시에 사용합니다. |
| Text view margin | ✓ 샘플(단어 수 카운터) 제공 → **SDK 표면 확인** (`ITextViewMarginProvider`, `MarginPlacement`) | 파일 전체 상태 요약 띠(Margin) 표시에 사용합니다. |
| Project Query | ✓ 지원 (개요 명시) | 컴패니언 데몬이 자체 도구로 프로젝트 트리를 탐색하므로 확장에서는 최소한의 워크스페이스 경로 획득에만 활용합니다. |
| Debugger visualizers | ✓ 지원 (개요 명시) | 해당 사항 없습니다. |
| **CodeLens** | 개요에는 미기재되었으나 **SDK 표면 확인** (`ICodeLensProvider`, `InvokableCodeLens`) | 코드 요소별 액션 트리거로 활용 가능하나 1차 마일스톤에서는 제외합니다([설계 §2](./DESIGN.ko.md)). |
| **Settings** | 개요에는 미기재되었으나 **SDK 표면 확인** (`Setting`, `SettingCategory`, `ArraySetting<T>`) | **선언형 설정 모델**입니다. 확장이 자체 옵션 페이지를 구현하지 않습니다. |
| 상태 표시줄 항목 | ✗ **미지원 (실측 확인)**: `StartProgressReportingAsync` 및 `ProgressReporterOptions`는 상태 표시줄 텍스트가 아닌 **Task Status Center** 동작을 제어하는 옵션입니다. | 대화 툴 윈도 헤더에 상태 요약 텍스트를 직접 렌더링합니다. |
| 인라인 코드 완성 | ✗ **미지원 (실측 확인)** | 프로세스 외 모델에서 지원되지 않으므로 1차 구현에서 제외합니다. |
| SCM (커밋 창) 확장 | ✗ **미지원 (실측 확인)** | 프로세스 외 모델에서 지원되지 않으므로 제외합니다. |
| 진단 위치의 코드 액션 (전구) | ✗ **본 모델 미지원 (실측 확인)**: 진단 보고(`DiagnosticsReporter`) API는 존재하나, 제안 액션(전구) 표시는 VSSDK In-process 전용입니다. | 프로세스 외 모델 유지를 위해 구현 대상에서 제외합니다. |

### 실측 방법 및 어셈블리 전수 조사

문서 요약 목록의 누락 가능성을 배제하기 위해, 실제 설치된 SDK 어셈블리의 공개 타입을 전수 조사했습니다. 확장 설치 경로 `Common7\IDE\CommonExtensions\Microsoft\Extensibility`의 6개 어셈블리(공개 타입 248개)와 `Editor` 어셈블리, 그리고 `CommonExtensions\Microsoft` 전체 1,336개 어셈블리(공개 타입 59,158개)의 브로커 계약(`RpcContracts.*`)을 분석했습니다.

그 결과 `StatusBar`, `Completion`, `SourceControl`, `CodeAction` 관련 공개 타입은 **0건**으로 확인되었습니다.

이어 공식 API 참조와 재대조를 수행했습니다. `ShellExtensibility` API 참조의 패키지 버전은 **17.14.2088**로 실측 대상(17.14.2099)과 일치하였으며, 진행 상황 보고 옵션(`ProgressReporterOptions`)은 상태 표시줄이 아닌 작업 상태 센터(Task Status Center)용임이 확인되었습니다. 코드 액션 전구 안내서 역시 VSSDK In-process MEF 기반 문서로 확인되었습니다. 실측 세부 사항은 [설계 §2](./DESIGN.ko.md)에 기술되어 있습니다.

---

## 4. VS Code와 정반대인 규약 셋

플랫폼 이식 과정에서 가장 유의해야 할 요소는 부재한 API가 아닌 **서로 상충하는 플랫폼 규약**입니다.

| 항목 | VS Code | Visual Studio |
|---|---|---|
| 화면 | 웹뷰 사용은 최소화 권고 ("only if you absolutely need them") — 이에 따라 2개로 제한 | 웹뷰가 부재하며 **XAML 렌더링이 기본**입니다. IDE 테마가 자동 적용됩니다. |
| 설정 | ❌ 자체 설정 화면 구현 금지. `contributes.configuration` 선언만 허용 | `Setting`, `SettingCategory`로 **선언**하면 Visual Studio 설정 UI가 이를 렌더링합니다. |
| 매니페스트 | `package.json`의 `contributes` 섹션 (JSON 선언형) | **C# 코드 선언형** (`CommandConfiguration` 등) — `.vsct` 파일이 불필요합니다. |

### 설정 모델에 대한 사전 오해와 정정 (2026-09-09)

초기 분석에서는 Visual Studio 확장이 전통적인 Tools > Options 커스텀 페이지를 구성해야 한다고 가정하였으나, 이는 **기존 VSSDK(In-process) 모델에 해당하는 사항**이었습니다.

`VisualStudio.Extensibility` 표면에는 `OptionPage`, `DialogPage`, `ToolsOptions` 등의 In-process API가 존재하지 않으며(0건), 대신 `Settings` 네임스페이스에 35개의 선언형 타입(`Setting<T>`, `SettingCategory`, `EnumSettingEntry`, `ArraySetting<T>`, `SettingRule`, `SettingMessage`, `SettingsExtensibility`)이 제공됩니다. 확장은 설정 항목의 구조만 선언하고, 실제 UI 렌더링은 Visual Studio 셸이 전담합니다.

따라서 VS Code의 `contributes.configuration`과 동일한 선언형 원칙이 적용되며, "자체 설정 UI를 직접 구현하지 않는다"는 프로젝트 규칙을 그대로 유지할 수 있습니다.

---

## 5. 정하지 않은 것

- **알림 표출 세부 기준**: VS Code는 알림 사용에 대한 엄격한 가이드라인을 제공하나, Visual Studio Extensibility SDK에는 `Notification` 명칭의 API가 없으며(0건) `Shell.PromptOptions`, `ChoiceDescription`, `ProgressReporter`가 제공됩니다. API 도구는 확정되었으나 상황별 표출 기준은 향후 사용 패턴에 따라 수립할 예정입니다.
- **아이콘, 대소문자, 메뉴 배치 스타일**: VS Code 및 JetBrains와 달리 Visual Studio 확장 문서에서는 명시적인 텍스트 케이스나 아이콘 배치 UX 가이드라인을 규정하고 있지 않습니다. Visual Studio 네이티브 관례를 준수하여 구현합니다.

---

## 6. 참고 문헌 및 실측 출처

측정 및 대조 일자: 2026-09-09 및 2026-09-10.

**실측 환경:**
- Windows 11
- Visual Studio Community 2022 **17.14.37628.2** (실측) 및 **17.14.40** (기동 검증)
- 워크로드: `ManagedDesktop` (`Microsoft.VisualStudio.Workload.ManagedDesktop`), `VisualStudioExtension` (`Microsoft.VisualStudio.Workload.VisualStudioExtension`)
- 분석 대상: `Common7\IDE\CommonExtensions\Microsoft\Extensibility` 내 6개 어셈블리(공개 타입 248개), `CommonExtensions\Microsoft` 전체(어셈블리 1,336개, 공개 타입 59,158개) 및 `RpcContracts.*`
- 참조 어셈블리: `Microsoft.VisualStudio.Extensibility.dll` 17.14.2099, NuGet SDK 패키지 17.14.40608

**공식 문서 출처:**
- [VisualStudio.Extensibility overview](https://learn.microsoft.com/en-us/visualstudio/extensibility/visualstudio.extensibility/visualstudio-extensibility?view=visualstudio) — 14개 기능 영역 개요 및 모델 현황
- [`ShellExtensibility` Class Reference](https://learn.microsoft.com/en-us/dotnet/api/microsoft.visualstudio.extensibility.shell.shellextensibility?view=visualstudiosdk-2022) — 셸 확장 API 명세
- [`ProgressReporterOptions` Class Reference](https://learn.microsoft.com/en-us/dotnet/api/microsoft.visualstudio.rpccontracts.progressreporting.progressreporteroptions?view=visualstudiosdk-2022) — 작업 상태 센터 제어 옵션 명세
- [Displaying Light Bulb Suggestions Walkthrough](https://github.com/MicrosoftDocs/visualstudio-docs/blob/main/docs/extensibility/walkthrough-displaying-light-bulb-suggestions.md) — VSSDK In-process 전용 전구 구현 가이드
- [Choose the right Visual Studio extensibility model](https://learn.microsoft.com/en-us/visualstudio/extensibility/visualstudio.extensibility/extensibility-models?view=visualstudio) — 확장 모델 비교 및 권고 기준
- [microsoft/VSExtensibility GitHub Repository](https://github.com/microsoft/VSExtensibility) — 확장점 사양 및 개발 워크로드 요구조건
- [Using VisualStudio.Extensibility SDK and VSSDK together](https://learn.microsoft.com/en-us/visualstudio/extensibility/visualstudio.extensibility/get-started/in-proc-extensions?view=visualstudio) — In-process 호환성 가이드

