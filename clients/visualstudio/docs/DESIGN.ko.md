# Visual Studio 확장 — 설계

[↑ 클라이언트 개요](../README.md) · [플랫폼 규약](./PLATFORM.ko.md) · [클라이언트 계약 정본](../../../docs/CLIENTS.ko.md) · [형제: VS Code](../../vscode/docs/DESIGN.ko.md) · [형제: 젯브레인](../../jetbrains/README.md)

> **상태: 설계. 코드는 없다. 그리고 이 기계에서는 만들 수도 없다.**
>
> Visual Studio 확장은 **Windows 에서만** 지어진다. 여기는 macOS 이고, `msbuild` 도 Visual Studio
> 도 없다(잰 값: `uname -s` = Darwin, `which msbuild` = 없음, `dotnet` 9.0.301 만 있음).
> **Visual Studio for Mac 은 2024 에 단종됐다.** 그러니 이 문서는 설계까지이고, 짓고 깔고 재는
> 것은 Windows 기계에서 해야 한다 — §9 에 무엇을 어떻게 해야 하는지 적었다.
>
> ✅ **그 Windows 기계에서 쟀다(2026-09-09).** 코드는 여전히 없지만, 문서로 읽었을 뿐이라고
> 적어 둔 것들은 이제 실물로 판정됐다 — §2 의 물음표 넷, §5 의 소켓 함정, §6 의 시험 하네스,
> §7 의 CI 러너, §8 의 요구. 무엇을 어떻게 쟀는지는 그 자리마다 적었고, 아직 못 잰 것만 §10 에
> 남겼다. 잰 기계: Windows 11, VS Community 2022 **17.14.37628.2**, 워크로드
> `ManagedDesktop` · `VisualStudioExtension`.
>
> ✅✅ **그리고 띄웠다(2026-09-10).** 위 두 문단은 이 문서가 설계였을 때의 것이라 그대로 둔다 —
> 이제 코드가 있고, 실험 인스턴스(VS Community 2022 **17.14.40**)에서 **판이 그려진다.** 오른쪽에
> 붙고, 메뉴 이름이 제 이름으로 뜨고, 브리지 왕복이 IDE 안에서도 된다. 넷 다 §10 에서 「짐작」으로
> 적어 두었던 것이고, **셋은 짐작이 틀렸다** — 그 셋을 §10 에 그대로 적는다. 재는 것과 띄우는
> 것은 여기서도 다른 말이었다.
>
> [EDITORS](../../../docs/proposals/EDITORS.ko.md) 가 셋을 견주고 「VS Code 부터, 두 벌을 본 뒤에
> 공통을 뽑는다」고 정했다. VS Code 는 섰다. 이 문서는 **두 번째 벌**이고, 그래서 §3 이 이 설계의
> 진짜 값이다: 무엇이 공통이고 무엇이 편집기마다 다른지가 이제야 두 벌로 보인다.

---

## 1. 어느 모델로 만드는가 — 이 설계의 첫 갈림

Visual Studio 에는 확장 모델이 셋이고, 고르는 것이 나머지를 거의 다 정한다. 공식 비교표를 그대로
옮긴다(출처 §10).

| | **VSSDK** | **Community Toolkit** | **VisualStudio.Extensibility** |
|---|---|---|---|
| 런타임 | .NET Framework | .NET Framework | **.NET** |
| VS 로부터 격리 | ❌ | ❌ | **✅** |
| 단순한 API | ❌ | ✅ | ✅ |
| 비동기 실행과 API | ❌ | ❌ | **✅** |
| VS 시나리오 폭 | ✅ | ✅ | **⏳** |
| 재시작 없이 설치 | ❌ | ❌ | **✅** |
| VS 2019 이하 지원 | ✅ | ✅ | ❌ |

**`VisualStudio.Extensibility` 를 고른다. 프로세스 밖(out-of-proc)으로.** 이유가 셋이고, 셋 다
우리 사정에서 나온다.

1. **.NET 을 쓴다.** VSSDK 는 .NET Framework 4.8 에 묶여 있다. 우리 코어 층은 소켓·JSON·전사
   조립이고 그것을 2026 년에 .NET Framework 로 쓸 이유가 없다.
2. **격리.** 우리 확장은 유닉스 소켓을 잡고 스트림을 열어 둔 채 산다. 그 프로세스가 IDE 와 같으면
   우리 실수가 남의 IDE 를 멈춘다. 젯브레인에서 실제로 그 값을 치렀다 — dispose 하나가 터져 그
   아래 정리가 통째로 걸러졌다.
3. **모든 명령이 백그라운드 스레드에서 돈다.** VSSDK 는 UI 스레드 위에서 돌고, 스레드 전환을
   손으로 맞춰야 하며, 공식 문서가 그것을 「VS 엔지니어에게도 끊임없는 버그의 원천」이라고 적는다.
   우리는 매 호출이 소켓 왕복이다. 그 둘이 만나면 안 된다.

⚠ **값도 분명하다: 폭이 아직 ⏳ 다.** 필요한 확장점이 없으면 in-proc 으로 내려가야 하고, 그러면
.NET Framework 로 돌아간다. 무엇이 되는지는 §2 에서 잰다.

---

## 2. 기능 대조 — 무엇이 되고 무엇이 아직인가

VS Code 이식표(마흔여덟)를 그대로 가져와 이 편집기에서 다시 판정한다. **읽은 것은 공식 문서와
샘플 목록이지, 실물로 띄워 본 것이 아니다** — 그 차이를 §10 에 적었다.

| 우리 기능 | VisualStudio.Extensibility 의 자리 | 판정 |
|---|---|---|
| 대화 판 | **Tool Window** — "dockable windows within the Visual Studio IDE" | ✓ |
| 계획·계기판 | Tool Window 둘째 | ✓ |
| 명령·메뉴 | `CommandConfiguration` — **`.vsct` 가 없다.** 배치도 코드로 | ✓ |
| 컴패니언이 고친 파일 | Editor / Documents API | ✓ |
| 편집 적용(손) | `Extensibility.Editor().EditAsync(...)` | ✓ |
| 훑어본 말을 줄에 걸기 | **Tagger / Classification tagger** — 샘플에 있다 | ✓ |
| 파일 전체에 대한 말(띠) | **Text view margin** — word-count margin 샘플이 그 모양이다 | ✓ |
| 상태 표시줄 | **항목을 다는 문은 없다.** 있는 것은 `StartProgressReportingAsync` 인데, 그 옵션 클래스가 문서에 **「Task Status Center 의 동작을 조정하는 옵션」**이라고 적혀 있다 — 상태 표시줄의 그 아이콘에서 열리는 작업 목록이지, 우리 글자를 놓는 자리가 아니다 | ✗ |
| 인라인 완성 | **없다.** 완성이라는 낱말이 표면에 하나도 없다. VS 의 회색 이어쓰기는 IntelliCode 의 것이고 서드파티에 열린 문을 못 찾았다 | ✗ |
| 승인 답하기 | 툴 윈도 안(우리 판) 또는 `Shell.PromptOptions` · `ChoiceDescription` · `ShowDialogAsync` | ✓ |
| 커밋 메시지 초안 | **없다.** 실제로 그 일을 하는 서드파티 확장이 **메뉴 명령 + 클립보드**로 우회한다 — 상자 안에 단추를 다는 문이 있으면 그렇게 만들 리 없다 | ✗ |
| 진단 자리의 코드 액션 | **이 모델에는 없다.** 진단을 *내는* 문(`DiagnosticsReporter` · `DocumentDiagnostic`)은 있다. 전구는 **VSSDK(in-proc)** 쪽에만 있다 | ✗ |
| 코드 요소 위의 눌리는 딱지 | `ICodeLensProvider` · `VisualCodeLens` · `InvokableCodeLens` | ✓ **표에 없던 것** |
| 설정 | `Settings` 에 **선언형** 한 벌(`Setting` · `SettingCategory` · `ArraySetting<T>`) | ✓ |
| 코어 받기·데몬 기동 | 그냥 .NET 이다 | ✓ |

### 어떻게 쟀나 — 문서가 아니라 표면을 셌다

물음표 넷은 「목록에 안 보인다」였고, 그때 남긴 걱정은 **그 목록이 전수인지 요약인지 모른다**는
것이었다. 그래서 목록을 다시 읽지 않고 **설치된 어셈블리의 공개 타입을 셌다.** 설치 경로
`Common7\IDE\CommonExtensions\Microsoft\Extensibility` 의 6개 어셈블리에 공개 타입 248개,
`…\Editor` 까지 더해 이 모델이 확장 작성자에게 내주는 이름 전부다.

| 찾은 말 | Extensibility 표면 | 브로커 계약(`RpcContracts.*`) |
|---|---|---|
| `StatusBar` | 0 | 0 |
| `Completion` | 0 | 0 |
| `SourceControl` · `Scm` | 0 | 0 |
| `CodeAction` · `QuickFix` · `SuggestedAction` | 0 | 0 |
| `Markdown` | 0 | — |

브로커 계약까지 센 이유는, 프로세스 밖 확장이 `IServiceFactory`·`ServiceHubServiceMoniker` 로
브로커 서비스를 잡을 수 있어서다. **그쪽에도 없다** — 확장 SDK 가 안 내주는 것을 옆문으로
집을 수 있는지까지 본 값이다. `RpcContracts` 쪽 `Commit` 3건은 전부 `UnifiedSettings` 의 설정
커밋이라 소스 제어와 무관하다.

⚠ **이것이 답하는 것과 안 하는 것.** 「이 모델이 그 이름을 안 내준다」까지가 잰 값이다. VS 자체에
상태 표시줄이 없다는 뜻이 아니고(있다), **프로세스 밖 확장이 그것을 건드릴 문이 이 판에 없다**는
뜻이다. 판이 오르면 달라질 수 있으니 잰 판을 적는다: `Microsoft.VisualStudio.Extensibility.dll`
**17.14.2099**(파일 판 17.14.2099.59265), NuGet `Microsoft.VisualStudio.Extensibility.Sdk`
최신 **17.14.40608**.

### 센 것을 문서와 대조했다 — 한 줄이 날카로워졌다

「내 기계의 사본이 뒤처진 것 아닌가」는 표면을 세는 방법의 정당한 약점이다. 그래서 네 개의 ✗ 를
공식 문서·API 참조와 다시 맞췄고, **넷 다 유지되지만 하나는 표현이 틀렸다.**

| 대조한 곳 | 나온 것 |
|---|---|
| 공식 문서의 기능 영역 목록 | **열넷**이다(command · debugger-visualizer · diagnostics · dialog · document · editor · language-server-provider · output-window · project · settings · tool-window · user-prompt 등). **상태 표시줄·완성·소스 제어·코드 액션은 그 안에 없다** |
| `ShellExtensibility` API 참조 | 패키지 판이 **17.14.2088** — 내가 센 17.14.2099 와 같은 줄이다. **뒤처진 사본을 센 것이 아니다** |
| `ProgressReporterOptions` | 「**Task Status Center** 의 동작을 조정하는 옵션」. ⚠ **내 첫 문장이 부정확했다** — 「`ProgressReporter` 뿐」이라고만 적으면 그것이 상태 표시줄 항목의 사촌처럼 읽힌다. 아니다. 작업 상태 센터로 가고, 그 센터의 아이콘이 상태 표시줄에 있을 뿐이다 |
| 전구(코드 액션) | 문서가 있다 — **`walkthrough-displaying-light-bulb-suggestions`**. 그런데 그것은 **VSSDK(in-proc) MEF** 문서다. 「어디에도 없다」가 아니라 **「우리가 고른 모델에 없다」**가 맞는 문장이다 |
| 커밋 상자 | 확장으로 그 일을 하는 실물이 있는데 **메뉴 명령으로 만들고 클립보드로 건넨다.** 상자 안에 단추를 다는 문이 있었다면 그렇게 만들 리 없다 |
| 인라인 완성 | 서드파티에 열린 문을 못 찾았다. 검색이 내놓는 것은 전부 **VS Code** 의 `InlineCompletionItemProvider` 이거나 VS 의 **IntelliCode** 자체 기능이다 |

⚠ **여전히 못 지운 가능성 하나.** 「문서에 없고 표면에도 없다」는 「비공개 API 로도 불가능하다」와
다르다. 브로커 계약까지 센 것이 그 틈을 좁히지만 없애지는 못한다. 그리고 개요 문서는 이 모델을
아직 **preview** 라고 부른다(17.9 부터 대부분의 API 가 stable 이라고 하면서도) — 즉 **이 표는
날짜가 붙은 표**다.

### 그래서 §1 의 결정은 유지된다

물음표 넷이 전부 ✗ 로 닫혔으니 「필요한 확장점이 없으면 in-proc 으로 내려간다」를 저울에 올릴
차례인데, **안 내려간다.** 넷 중 우리가 실제로 쓰려던 것이 없다:

- 상태 표시줄은 **우리 판 안에 있으면 된다.** 「무엇을 하는 중인가」는 대화 판 머리에 그리는 것이
  자연스럽고, 젯브레인·VS Code 도 결국 제 판에 그린다.
- 인라인 완성·커밋 초안·코드 액션은 **이 클라이언트의 첫 벌에 없다.** 척추(§9 걸음 2)와 편집기
  안(걸음 3)이 먼저고, 그 둘은 태거·마진·편집 API 로 전부 선다.

넷을 위해 .NET Framework 로 돌아가고 격리를 잃는 것은 값이 맞지 않는다. **대신 얻은 것이 있다** —
`ICodeLensProvider` 는 VS Code 이식표에 아예 없던 자리다. 코드 요소마다 눌리는 딱지를 걸 수
있으니 「이 함수에 대해 물어보기」가 붙을 자리가 되는데, **첫 벌에는 안 넣는다.** 걸음 3 을
지나고 나서 값을 따진다.

### 화면 만들기 — Remote UI

프로세스 밖이라 **WPF 를 직접 그릴 수 없다.** VisualStudio.Extensibility 는 Remote UI 라는
모델을 쓴다: XAML 을 확장이 주고 VS 가 그린다.

이것이 우리에게 **VS Code 보다 나은 자리**다. VS Code 에서는 대화를 웹뷰로 그리고, 그래서 색
토큰을 손으로 맞추고 CSP 를 다루고 마크다운 렌더러를 어떻게 할지 아직 못 정했다([VS Code 설계
§7](../../vscode/docs/DESIGN.ko.md)). Remote UI 는 XAML 이라 **IDE 가 테마를 알아서 입힌다.**

⚠ 대신 **마크다운을 그릴 것이 없다.** VS Code 웹뷰는 최소한 브라우저였다. XAML 에는 그마저
없으므로, 코드 펜스·굵게·목록을 XAML 요소로 직접 지어야 한다. 이것이 이 이식에서 가장 큰 미지의
작업이고, §10 에 그렇게 적었다.

**세어 봤다.** `Extensibility.UI` 가 내주는 공개 타입은 **일곱뿐**이다 — `RemoteUserControl`,
`XamlFragment`, `ObservableList<T>`, `ResourceDictionaryCollection`, `AsyncCommand`,
`IAsyncCommand`, `NotifyPropertyChangedObject`. 그릇과 묶기와 명령이 전부고, **그리는 것은 하나도
없다.** 걱정이 맞았다는 뜻이지 놀랄 일은 아니다: Remote UI 는 화면을 그려 주는 층이 아니라 XAML
을 건네는 통로다. 마크다운 렌더러는 우리가 쓴다.

#### 그런데 「우리가 쓴다」가 생각보다 좁다 — 문서에서 확인한 제약 셋

XAML 이 VS 프로세스에서 인스턴스화되기 때문에 따라오는 것들이고, 문서가 명시한다:

1. **「Remote UI 는 당신의 커스텀 컨트롤을 참조하도록 허용하지 않는다.」** XAML 은 **확장의 타입과
   어셈블리를 참조할 수 없고**, VS 프로세스의 것만 참조할 수 있다. 즉 `MarkdownTextBlock` 같은
   컨트롤을 우리가 만들어 끼우는 길이 **막혀 있다.**
2. **코드 비하인드도 이벤트 핸들러도 없다.** MVVM 과 데이터 바인딩, 명령, 트리거로만 짠다.
3. 테마는 `Microsoft.VisualStudio.Shell` · `PlatformUI` 의 스타일을 XAML 안에서 참조해 따른다 —
   그 어셈블리를 확장 프로젝트가 직접 참조하는 것은 아니다.

**그래서 마크다운은 컨트롤이 아니라 모양(shape)으로 푼다.** 전사 한 줄을 「무엇을 그릴지」로 미리
갈라 뷰 모델에 담고, XAML 쪽은 표준 WPF 원시 요소에 `DataTemplate` 을 걸어 그린다. 파싱은 C#
(`Magi.Core`)에서 일어나고 — **IDE 없이 시험되는 자리다** — XAML 은 그 결과를 늘어놓기만 한다.
이 갈림은 §4 의 모듈 갈림과 우연히 같은 선이 아니라, 이 플랫폼이 강제하는 선이다.

⚠ 분량은 여전히 짐작하지 않는다. 다만 **어디서 막힐지는 이제 안다**: 인라인 서식이 섞인 문단
하나를 원시 요소로 조립하는 일이고, 코드 펜스의 강조까지 가면 더 는다.

---

## 3. 두 벌을 보고 나서 — 무엇이 진짜 공통인가

EDITORS §7 이 「두 벌을 본 뒤에 공통을 뽑는다」고 한 그 자리다. VS Code 를 지어 보고 나니 갈린다.

### 공통인 것 (언어를 바꿔도 그대로 온다)

| | 왜 |
|---|---|
| **소켓 경로 유도** | 코어가 정한 규칙이다. 틀리면 **에러 없이** 아무 데도 안 닿는다. VS Code 포팅에서 이것이 두 번 나를 잡았다 |
| **줄 단위 JSON 왕복과 스트림** | 프로토콜이다 |
| **전사 → 행** | 「행을 짓는 규칙은 한 벌」이 불변식인데, 클라이언트마다 한 벌씩 생기고 있다 |
| **「무엇을 하는 중인가」 한 단어** | 모름·없음·도는 중·기다림의 갈림 |
| **승인 어휘** | allow · deny · always |
| **훑어본 말 가르기** | 줄에 걸리는 것과 아닌 것. 구분자가 탭만이 아니라는 실측까지 |
| **완성의 겹침 제거** | |
| **판 고르기와 받기** | 어느 판을 어디서 |

**여덟이다.** VS Code 에서 이것을 TypeScript 로 한 벌 썼고, Visual Studio 에서 C# 으로 또 쓰면
**세 벌**이 된다(젯브레인의 코틀린까지). 같은 사실이 세 군데 적히면 그중 하나는 갈라진다 — 이
저장소가 오늘만 두 번 겪은 모양이다.

### 편집기마다 다른 것

명령을 어디에 다는가 · 무엇으로 그리는가(웹뷰 / XAML / Swing) · 인레이·완성의 API · 설정을 누가
그리는가 · 알림의 규약. 이쪽은 **옮길 것이 없다.** 각 편집기의 규약이 다르고, 그 규약을 따르는
것이 이 클라이언트들의 일이다.

### 그래서 — `magi ide-bridge` 를 여기서 결정한다

EDITORS 가 미뤄 둔 그 결정이다. 두 벌을 봤으니 이제 답할 수 있다.

**권고: 만든다. 다만 「공통 여덟」만 담는다.**

- 코어 바이너리에 `magi ide-bridge` 서브커맨드를 둔다. stdio 로 JSON 을 주고받고, 위 여덟을
  전부 그쪽이 한다. 편집기 층은 **문 두드리기와 그리기만** 한다.
- 클라이언트는 이미 코어 바이너리를 받아 두고 있다. 프로세스를 하나 더 띄우는 것이 새 요구가
  아니다.
- 값: 새 편집기 하나가 느는 비용이 「여덟 + 화면」에서 「화면」으로 준다. Visual Studio 가 그
  첫 수혜자다.
- 비용: 층이 하나 늘고 그 계약이 또 하나의 정본이 된다. `docs/CLIENTS` 의 「새 문 원칙 5」에
  얹어야 한다.

✅ **정해졌다(2026-09-09): 심을 먼저 짓는다.** 계약은
[`docs/IDE_BRIDGE`](../../../docs/IDE_BRIDGE.ko.md) 에 있고, 이 확장이 그 첫 사용자가 된다.
그 문서 §1 이 이 결정의 근거를 실측으로 싣는다 — 완성 하나가 두 벌에서 이미 다르게 군다
(창 크기: 파일 전체 vs 4,000자 / 겹침: 안 지움 vs 지움).

---

## 4. 모듈 갈림

젯브레인·VS Code 와 같다. 그 갈림 덕에 프로토콜과 전사가 IDE 없이 시험된다.

```
src/Magi.Core/        VisualStudio.Extensibility 를 참조하지 않는다. 순수 .NET → 여기서 시험된다
src/Magi.Extension/   VisualStudio.Extensibility 가 사는 유일한 자리
test/Magi.Core.Tests/ 코어를 잰다
```

**규칙 하나를 시험으로 세운다: `Magi.Core` 가 `Microsoft.VisualStudio.Extensibility` 를 참조하면
실패한다.** 젯브레인의 `ArchitectureTest`, VS Code 의 `layering.test.ts` 와 같은 자리다. 없으면
편한 쪽으로 한 줄씩 새어 결국 코어를 못 시험하게 된다.

---

## 5. 소켓 계약 — Windows 에서 달라지는 것

규칙은 같다([VS Code 설계 §5](../../vscode/docs/DESIGN.ko.md)에 골든까지 있다). Windows 에서만
다른 것 셋:

1. **설정 디렉토리가 `%AppData%\magi` 다.** `AppData` 환경변수가 있으면 그것, 없으면
   `<home>\AppData\Roaming`.
2. **유닉스 소켓은 Windows 10 1803+ 에서 된다.** .NET 은 `UnixDomainSocketEndPoint` 를 준다.
   오피스 클라이언트가 이미 그 길을 쓰고 있다.
3. ⚠ **`%AppData%` 아래에서는 AF_UNIX 가 안 된다.** 「이상하게 군다」고만 적어 뒀던 것을
   이 판에서 쟀다(아래).

### `%AppData%` 아래 AF_UNIX — 실측 (2026-09-09, Windows 11, .NET 9)

프로브가 한 것: 디렉토리를 만들고 → bind·listen → connect → 5바이트 왕복 → 닫고 → 지운다.
bind 와 connect 사이에서는 **아무것도 안 만진다** — 재분석 지점을 들여다보는 것 자체가 syscall
이라, 앞선 판이 거기서 파일 속성을 읽는 바람에 「재는 것이 재려는 것을 망가뜨렸나」를 못 갈랐다.

| 디렉토리 | bind | connect | 지우기 |
|---|---|---|---|
| `%AppData%\…`(Roaming) | 됨 | **`WSAEINVAL`(10022)** | 못 지움 |
| `%LocalAppData%\…` | 됨 | **`WSAEINVAL`(10022)** | 못 지움 |
| `%LocalAppData%\Microsoft\…` | 됨 | **`WSAEINVAL`(10022)** | 못 지움 |
| `C:\Users\<나>\AppData\…`(바로 아래) | 됨 | **`WSAEINVAL`(10022)** | 못 지움 |
| `%LocalAppData%\Temp\…` (더 깊어도) | 됨 | 0ms | 지움 |
| `C:\Users\<나>\…` | 됨 | 0ms | 지움 |
| `…\Documents\…` · `C:\Users\Public\…` | 됨 | 0ms | 지움 |

**경계가 `AppData` 트리 전체다. 예외는 `Temp` 하위뿐이다.** bind 는 늘 성공하고 파일도 생기므로,
띄우는 쪽은 잘 떴다고 믿는다. 무너지는 것은 붙는 쪽이고, 그래서 증상이 「붙기 타임아웃」으로
보였던 것이다.

**두 번째 실측이 더 아프다: 그 소켓 파일은 지울 수 없다.** `File.Delete` 는 물론이고, 코어가
[`listen_windows.go`](../../../internal/adapter/daemon/listen_windows.go) 에서 쓰는 그 방법 —
`FILE_FLAG_OPEN_REPARSE_POINT | FILE_FLAG_DELETE_ON_CLOSE` 로 여는 것 — 조차 **ERROR 1920**
(`ERROR_CANT_ACCESS_FILE`)로 진다. 코어의 복구 경로가 하필 코어가 Windows 에서 기본으로 삼는
그 디렉토리에서만 무력하다. 한 번 남으면 그 주소는 영영 못 쓴다.

**원인은 아직 모른다. 다만 아닌 것을 넷 지웠다.**

- **주소 길이가 아니다.** 실패한 경로가 46–61바이트였다. 상한은 약 100이고, 성공한 `Temp` 경로가
  오히려 64바이트로 더 길었다.
- **ACL 이 아니다.** `AppData\{Roaming,Local}` 에만 앱 기능 SID(`S-1-15-3-…`) ACE 가 상속돼 있어
  유력해 보였는데, 상속을 끊고 그 ACE 를 지워 `Temp` 와 똑같은 3개짜리로 만든 디렉토리도 그대로
  10022 였다.
- **디렉토리 속성·볼륨이 아니다.** 넷 다 평범한 `Directory` 이고 같은 `C:` 다.
- **재는 도구의 샌드박스가 아니다.** 샌드박스를 끄고 돌려도 값이 같다.

남은 유력한 자리는 **`AppData` 경로에 붙은 필터 드라이버**인데, `fltmc filters` 가 승격을
요구해서 못 봤다. 등록된 보안 제품은 Windows Defender 하나뿐이다.

⚠ **코어에 적힌 이유는 이 현상을 설명하지 못한다.** `SocketDir` 의 주석과 `socketdir_test.go` 는
`MAGI_SOCKET_DIR` 이 갈라진 이유를 **「주소가 100바이트 상한을 넘어서」**로만 적는다. 그 이유도
참일 수 있지만(긴 사용자 이름에서), 이 기계에서 실제로 무는 것은 그것이 아니다. 사용자 이름이
다섯 글자인데도 `AppData` 아래면 전부 죽는다. **적힌 원인이 좁다.**

**이 확장이 할 일:** 소켓을 `%AppData%` 아래에 두지 않는다. `MAGI_SOCKET_DIR` 을 따르는 것이
선택이 아니라 조건이고, 그 규칙을 §6 의 골든이 지킨다.

**골든은 VS Code 것을 그대로 쓴다.** 코어의 `WorkspaceKey` 가 답한 값이고, 언어가 달라도 답은
같아야 한다. 특히 두 가지를 C# 에서 다시 밟기 쉽다:

- **해시 상수가 표준 FNV-1a 가 아니다.** 코어는 `1469598103934665603` 을 쓰는데 표준은
  `14695981039346656037` 이다(자릿수 하나가 빠졌다). **고치면 안 된다** — 소켓 이름이 거기서
  나온다. TypeScript 포팅이 여기서 한 번 틀렸다.
- **`filepath.Base("/")` 는 `"/"` 다.** .NET 의 `Path.GetFileName` 은 루트에서 빈 문자열을 준다.

---

## 6. 무엇을 어디서 재나

| 층 | 어떻게 |
|---|---|
| `Magi.Core` | `dotnet test`. IDE 없이 돈다 |
| **와이어 대조** | Go 소스를 읽어 필드 이름을 맞춘다. 짝을 너무 적게 찾으면 **그것부터 실패한다** |
| 소켓 키 | VS Code 와 같은 골든 |
| 계층 | `Magi.Core` 가 Extensibility 를 참조하지 않는다 |
| 매니페스트·확장점 | ⚠ **`@vscode/test-electron` 에 해당하는 1급 하네스가 없다**(아래) |

**와이어 대조는 조건이다.** C# 의 `System.Text.Json` 도 모르는 필드를 조용히 버리고 없는 필드를
기본값으로 준다 — 코틀린의 `ignoreUnknownKeys`, TypeScript 의 `undefined` 와 같은 함정이다.
이름이 어긋나면 예외가 아니라 기본값이고, 화면은 「없다」고 말한 뒤 아무것도 실패하지 않는다.

### 실물 확장을 자동으로 재는 길 — 있긴 한데 우리 것이 아니다 (2026-09-09)

⚠ **처음에 「없다」고 적었다가 고쳤다.** `api.nuget.org` 에 이름을 직접 물어 404 를 받고 없다고
결론지었는데, **패키지는 있다.** nuget.org 에 없을 뿐이다 — Microsoft 의 `vs-extension-testing`
이 그것을 **Azure DevOps 피드**(`dev.azure.com/azure-public/vside/_artifacts/feed/vssdk`)로
낸다. 「내가 아는 피드에 없다」를 「없다」로 세면 안 된다는 것을 이 줄이 남긴다.

| 후보 | 있나 | 우리에게 |
|---|---|---|
| `Microsoft.VisualStudio.Extensibility.Testing.Xunit` | **있다** — 단 nuget.org 가 아니라 Azure DevOps 피드 | ⚠ **VSIX/VSSDK 확장을 위한 것**이다. 문서 어디에도 `VisualStudio.Extensibility`(프로세스 밖) 이야기가 없고, **저장소는 보관 처리되어** `dotnet/roslyn` 으로 옮겨 갔다 |
| `Microsoft.VisualStudio.Sdk.TestFramework(.Xunit)` v17.11.66 | 있다 | ⚠ **VSSDK(in-proc)용**이다. VS 서비스를 흉내 내는 물건이라 프로세스 밖 모델과 판이 다르다 |
| `VsixTesting.Xunit` v0.1.78 · `xunit.vsix` v0.9.3 | 있다 | 서드파티. VS 실험 인스턴스를 띄운다 |

**그래서 결론은 안 바뀐다.** 셋 다 실험 인스턴스에 VSIX 를 심고 IDE 안에서 도는 모양이고, 어느
것도 **프로세스 밖 모델을 지원한다고 말하지 않는다.** `@vscode/test-electron` 처럼 「이 모델의
확장을 이 모델대로 띄워 재는」 1급 길은 못 찾았다. 다만 **「없다」가 아니라 「우리 모델을 겨냥한
것이 없다」**이고, 그 둘은 다른 문장이다.

**그래서 §4 의 갈림이 여기서 값을 낸다.** 재는 길이 얇을수록 `Magi.Core` 에 든 것이 많아야
한다. 프로토콜·소켓 키·전사 조립이 IDE 없이 `dotnet test` 로 서고, IDE 층에는 시험이 얇아도
견딜 만큼만 남긴다 — 이건 취향이 아니라 이 편집기의 사정에 맞춘 것이다.

---

## 7. 배포

`vscode-v*` · `jetbrains-v*` 와 같은 모양으로 `vsstudio-v*` 레인을 둔다. 자산은 `.vsix`,
판 번호는 태그에서만.

⚠ **CI 가 Windows 러너를 써야 한다.** 다른 셋은 `ubuntu-latest` 인데 이것만 `windows-latest` 다.

✅ **러너에 워크로드가 있다 — 확인했다(2026-09-09).** `actions/runner-images` 의 이미지 설명서를
직접 읽었다. `windows-2022` 와 `windows-2025` **둘 다** Visual Studio Enterprise 2022
17.14.37614.0 을 싣고, 목록에 `Microsoft.VisualStudio.Workload.VisualStudioExtension` 과
`Microsoft.VisualStudio.Workload.ManagedDesktop` 이 둘 다 있다. **러너에서 워크로드를 따로 깔
필요가 없다.** 판도 §8 의 17.9 요구를 넘는다.

---

## 8. 이 편집기가 요구하는 것

- **Visual Studio 2022 17.9 이상**, `Visual Studio extension development` 워크로드
  (`Microsoft.VisualStudio.Workload.VisualStudioExtension`).
- **Windows 전용.** VS for Mac 은 2024 에 단종됐다.

✅ **깔아 보고 쟀다(2026-09-09).** 요구가 맞다. 세 가지를 기록해 둔다 — 다음 사람이 같은 데서
멈추지 않도록.

- `CoreEditor` 만 있는 VS 로는 **`.csproj` 조차 안 열린다**: `error MSB4236: 지정된
  'Microsoft.NET.Sdk' SDK 를 찾을 수 없습니다`. `ManagedDesktop` 을 같이 넣어야 풀린다. 넣은 뒤
  VS 의 MSBuild 로 SDK 형식 프로젝트가 483ms 에 복원됐다.
- 워크로드를 넣으면 `Microsoft.VisualStudio.Component.VSSDK` 가 함께 들어온다 — 패키지 126 →
  **582**.
- 자동 설치는 **`--passive` 를 쓸 거면 반드시 승격된 프로세스에서** 시작해야 한다. 아니면
  `Exit Code: 5007` 로 즉시 닫히고, 로그가 그 이유를 적는다: "Commands with --quiet or --passive
  should be run elevated from the beginning". VS 가 떠 있으면 사전 검사 `VSProcessesRunning` 에
  걸려 **8006** 이다. 그리고 `--installPath` 는 공백이 있으니 **따옴표째** 넘어가야 한다 —
  안 그러면 「일치하는 설치된 제품을 찾을 수 없습니다」로 튕기면서도 **종료 코드는 0** 이다.
  그 0 은 설치 관리자가 제 판을 갱신한 값이지 수정이 된 값이 아니다. **종료 코드로 세지 말고
  `vswhere` 로 세라.**
- VS 2019 이하는 이 모델이 지원하지 않는다. 지원해야 하면 VSSDK 로 별도 프로젝트를 두는 것이
  공식 권고인데, **우리는 안 한다** — 클라이언트가 여섯인데 일곱째를 두 벌로 만들 이유가 없다.

---

## 9. 순서

| 걸음 | 무엇 | 어디서 | 상태 |
|---|---|---|---|
| 0 | **`magi ide-bridge`** — 공통 여덟(§3), [계약](../../../docs/IDE_BRIDGE.ko.md) | 코어 | ✅ 척추(`about`·`daemon`). 유도 여섯은 남았다 |
| 1 | 물음표 넷을 실물로 확인(§2) | **Windows** | ✅ 2026-09-09 · 넷 다 ✗ |
| 2 | 척추 — 발견·악수·대화 툴 윈도·데몬 기동 | Windows | ✅ **띄웠고 돈다**(2026-09-10) — `Magi.Core` + `Magi.Extension` + 시험 36. 판이 상태를 그리고 컴패니언을 띄운다. 전사는 `watch` 를 기다린다(§10) |
| 3 | 편집기 안 — 태거·마진 | Windows | |
| 4 | 진입점·계획판 | Windows | |
| 5 | `vsstudio-v*` 레인 | CI(windows-latest) | |

### Windows 기계에서 첫날 할 것

**걸음 1 이 나머지를 정하므로 먼저 한다** — 물음표 넷(§2)이 실제로 되는지 보기 전에 척추를
지으면, 안 되는 것을 전제로 지은 설계 위에 코드가 얹힌다.

✅ **그 첫날이 2026-09-09 였다.** 아래는 그날 실제로 밟은 길이고, 이제는 **걸음 2 를 시작하는
사람의 준비 절차**다. 걸음 1 의 답은 §2 에 있으니 다시 재지 않아도 된다.

준비물 — Visual Studio 2022 **17.9 이상** + `Visual Studio extension development` 워크로드.
설치 확인:

```powershell
& "${env:ProgramFiles(x86)}\Microsoft Visual Studio\Installer\vswhere.exe" `
    -latest -products * -requires Microsoft.VisualStudio.Component.VSSDK `
    -property catalog_productDisplayVersion
```

빈 확장 하나를 띄워 확장점 목록을 실물로 확인한다:

```powershell
dotnet new install Microsoft.VisualStudio.Extensibility.Templates
dotnet new vsextension -n MagiProbe
cd MagiProbe; dotnet build
```

**브리지는 그 기계에서 이렇게 확인한다** — 클라이언트가 무엇을 상대하는지부터 눈으로 본다:

```powershell
go build -o magi.exe ./cmd/magi
'{"id":1,"method":"about"}' | .\magi.exe ide-bridge -workspace C:\path\to\project
```

`daemon` 이 `null` 이고 `why` 가 있으면 그 워크스페이스에 컴패니언이 없는 것이다(에러가 아니다).
⚠ 윈도우에서는 `%AppData%` 아래 AF_UNIX 함정(§5)이 여기서 처음 걸릴 자리다 — **걸린다.** 잰
값이 §5 에 있으니 처음부터 `MAGI_SOCKET_DIR` 로 소켓만 `AppData` 밖에 뗀다.

---

## 10. 재지 않은 것

정직하게 적는다. 아래는 **아직 실물로 확인 안 한 것**이다. 2026-09-09 에 다섯이 이 목록을 떠나
각자의 자리로 갔다 — §2(물음표 넷) · §5(AF_UNIX) · §6(시험 하네스) · §7(러너) · §8(요구).
**2026-09-10 에 여섯째가 떠났다** — 아래 첫 항목이고, 그것이 이 목록에서 제일 컸다.

### ✅ 떠난 것: 「확장을 한 번도 띄워 보지 않았다」 (2026-09-10)

띄웠다. 실험 인스턴스(`devenv /rootSuffix Exp`), VS Community 2022 **17.14.40**. 짐작으로 적어
두었던 넷의 답은 이렇다.

| 짐작이던 것 | 답 |
|---|---|
| 패널이 실제로 그려지는지 | ❌→✅ **안 그려졌다.** 대화 자리에 `XamlParseException` 이 벽으로 떴다 |
| 오른쪽에 붙는지 | ✅ 붙는다. 문서 웰 + `Dock.Right` 가 맞았다 |
| 메뉴 이름이 퍼센트 기호가 아닌지 | ❌→✅ **퍼센트 기호였다.** `%Magi.OpenConversation.DisplayName%` 가 그대로 |
| 브리지 왕복이 IDE 안에서도 되는지 | ✅ 된다. `MAGI_SOCKET_DIR` 이 자식에게 상속되어 §5 함정도 피한다 |

**셋이 틀렸고, 셋 다 빌드 경고 0개였다.** 무엇이었는지 적어 둔다 — 다음 사람이 같은 데서 멈추지
않도록, 그리고 이 항목이 왜 「시험이 대신할 수 없는 것」이었는지 남도록.

1. **`string-resources.json` 이 vsix 의 루트에 실렸다.** 셸이 보는 곳은 `.vsextension/` 이다
   (로케일 폴더가 그 옆, 루트의 것이 기본값). `<VSIXSubPath>.vsextension</VSIXSubPath>` 가 답이다.
   ⚠ **이 자리에 시험이 둘 있었고 둘 다 초록이었다** — 하나는 `%키%` 가 정의됐는지 보고, 하나는
   파일이 csproj 에 선언됐는지 본다. 「키가 있다」와 「셸이 닿을 수 있다」는 다른 문장이었다.
2. **XAML 의 기본 xmlns 가 extensibility 네임스페이스였다.** 거기에는 `DataTemplate` 이 없다.
   그리는 것은 전부 WPF 타입이고 `…/extensibility/2022/xaml` 은 Remote UI 의 추가분이라 접두사에
   붙는다. 틀리면 **루트 요소부터** 안 서므로 빈칸 하나가 아니라 판 전체가 예외로 바뀐다.
3. **뷰모델에 `[DataContract]`·`[DataMember]` 가 없었다.** 판은 VS 프로세스의 **프록시**에 대고
   바인딩하고, 「`DataMember` 속성만 데이터 바인딩될 수 있다」가 문서의 문장이다. 없으면 아무것도
   복제되지 않는데 — **판은 서고, 요소도 다 있고, 에러도 없고, 값만 전부 빈칸이다.** 그리고 안 온
   값은 기본값을 데려온다: `StartVisibility` 가 안 와서 WPF 가 `Visible` 을 썼고, 판은 묻지도 않은
   솔루션에 컴패니언을 띄우겠다고 단추를 내밀고 있었다.

셋 다 이제 시험이 진다(README 의 ★ 셋). 다만 그 시험들은 **이 실패를 보고 나서** 세운 것이지
시험이 이 실패를 찾은 것이 아니다 — §6 이 「재는 길이 얇다」고 적은 것의 값이 여기서 치러졌다.

### 아직 재지 않은 것

- **툴 윈도 배치는 두 속성이다** — 이건 쟀다. `Placement` 는 *무엇에* 붙일지고 이름댈 수 있는 것은
  셋뿐(문서 웰 · 떠 있는 창 · 다른 툴 윈도의 GUID), 방향은 `DockDirection`. 「오른쪽 도크」라는
  `Placement` 값은 **없다** — 문서 웰 + Right 로 적는다. **실물에서 그렇게 붙는 것도 봤다**(위).
- **Remote UI 로 전사를 어떻게 그리나.** 제약은 이제 안다(§2): 타입 일곱, 그리는 것 없음, **커스텀
  컨트롤 금지**, 코드 비하인드 없음, 그리고 **`DataMember` 만 건너간다**(위). 그래서 **길도 하나로
  좁혀졌다** — 파싱은 코어에서, XAML 은 `DataTemplate` 으로. **안 잰 것은 그 길의 분량과, 인라인
  서식이 섞인 문단이 원시 요소로 얼마나 깔끔하게 조립되는가**이다. 판은 섰지만 **전사는 아직
  안 그린다** — 전사는 스트림이고 브리지의 전달 문은 한 요청에 한 답이라, `watch`
  ([IDE_BRIDGE §5](../../../docs/IDE_BRIDGE.ko.md))가 생기기 전까지 건널 것이 없다. 지금 판은
  마지막 답 한 통을 보여 주고, 그것이 전사가 아니라고 적어 둔다.
- **`%AppData%` 아래 AF_UNIX 가 왜 그러는지.** 무엇이 일어나는지는 이제 정확히 안다(§5).
  **원인은 모른다.** 길이·ACL·속성·볼륨·재는 도구는 아니라고 지웠고, 남은 유력한 자리인 필터
  드라이버는 `fltmc` 가 승격을 요구해서 못 봤다. 다른 기계에서도 같은지도 안 봤다 — 표본이
  하나다.
- **`ICodeLensProvider` 가 실제로 어떻게 그려지는지.** 표면에 있다는 것만 쟀고 띄워 보지 않았다.
  첫 벌에 안 넣기로 했으니 급하지 않다.
- **분량.** 젯브레인 9,778줄, VS Code 는 코어+IDE 를 새로 썼다. C# 으로 몇 줄인지는 짐작이라 안
  적는다.
