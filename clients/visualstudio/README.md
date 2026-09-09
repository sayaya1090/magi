# clients/visualstudio/ — Visual Studio 확장

[↑ 저장소](../../README.md) · [설계](docs/DESIGN.ko.md) · [플랫폼 규약](docs/PLATFORM.ko.md) · [편집기 셋 타당성](../../docs/proposals/EDITORS.ko.md) · [형제: VS Code](../vscode/README.md) · [형제: 젯브레인](../jetbrains/README.md)

Visual Studio에서 연 솔루션의 magi 데몬과 통신하여 대화창 및 IDE 제어 기능을 제공하는 공식 확장입니다.
새 프로토콜을 만들지 않고 본체 데몬의 소켓 계약([`docs/CLIENTS`](../../docs/CLIENTS.ko.md))을 준수하며, C# 구현 중복을 최소화하기 위해 공통 로직을 처리하는 `magi ide-bridge` 프로세스를 중계자로 사용합니다.

> **현재 상태**: 띄웠다. 패널이 그려지고, 컴패니언에게 말을 걸고, 무엇을 하는 중인지 그립니다.
>
> `src/Magi.Core`는 브리지와 말하고 IDE를 모르며, 그 위의 `src/Magi.Extension`이 대화 툴 윈도를 냅니다 (`dotnet test`로 36개 단위 테스트 통과, `dotnet build`로 VSIX 패키징 완료).
>
> ✅ **2026-09-10, 실험 인스턴스에 얹었다**(VS Community 2022 **17.14.40**). 오른쪽에 붙은 판이 컴패니언의 상태를 한 단어로 그리고, 없으면 띄우겠다고 묻습니다. 짐작으로 남겨 두었던 넷 중 셋(리소스 VSIXSubPath 누락, XAML 기본 xmlns, Remote UI `[DataMember]` 누락)이 빌드 경고 없이 조용히 틀려 있던 것을 실측하고 해결했습니다 ([설계 §10](docs/DESIGN.ko.md)).
>
> **다음은 전사다.** 판은 아직 마지막 답 한 통만 보여 줍니다 — 전사는 스트림이고 브리지의 전달 문은 한 요청에 한 답이라, `watch`가 생기기 전까지 건널 것이 없습니다 ([IDE_BRIDGE §5](../../docs/IDE_BRIDGE.ko.md)).
> ※ Visual Studio 확장은 Windows 환경에서만 빌드됩니다 (Mac용 Visual Studio는 2024년 단종).

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

## 3. 설계 핵심: `magi ide-bridge`의 도입

VS Code, JetBrains에 이어 세 번째 에디터 클라이언트를 구현하면서 발생하는 공통 로직 중복을 방지하기 위해 `magi ide-bridge`를 설계했습니다:

- **브리지가 전담하는 공통 8대 로직**:
  1. 소켓 경로 계산 (`WorkspaceKey` 해싱)
  2. 줄 단위 JSON 파싱
  3. 대화 전사(Transcript) 조립
  4. 상태 추적 (현재 진행 중인 작업 표시)
  5. 권한/승인(Approval) 어휘 및 상태 머신
  6. 인레이/어노테이션 텍스트 가공
  7. 인라인 완성(Completion) 처리
  8. 백엔드/모델 선택 처리
- 따라서 `Magi.Core`는 복잡한 FNV 해시나 소켓 경로 계산 없이, 브리지 프로세스와 표준 입출력/파이프로 소통하며 화면 렌더링에만 집중합니다.

---

## 4. 단위 테스트가 보증하는 10대 불변식

어겼을 때 컴파일 에러 없이 런타임에서 조용히 실패하는 항목들을 테스트로 엄격히 통제합니다 (★ 표시는 2026-09-10 실물 인스턴스 검증에서 확인된 항목):

| 규칙 | 어기면 발생하는 장애 |
|---|---|
| `Magi.Core`가 `Microsoft.VisualStudio*`를 참조하지 않는다 | 재는 것이 줄어든다. 이 편집기에는 실물 확장을 자동으로 재는 1급 길이 없다 ([설계 §6](docs/DESIGN.ko.md)) |
| 소켓 경로를 이쪽에서 유도하지 않는다 (FNV 상수가 소스에 없다) | **에러 없이** 아무 데도 안 닿는다. 컴패니언이 있는 트리를 「안 돌고 있다」고 말하고 두 번째를 띄운다 |
| 와이어 이름이 브리지의 Go 소스에 실제로 있다 | 예외가 아니라 기본값이 된다. 화면은 「없다」고 말하고 아무것도 실패하지 않는다 |
| ★ 활동 어휘가 **양쪽으로** 브리지의 것과 같다 (주석이 아니라 선언만 읽어서) | 안 맞는 낱말은 영영 안 걸리는 갈래가 되고, 화면은 전선의 낱말을 날것으로 그린다. 브리지가 낱말을 바꿨는데 여기가 안 따라간 것이 실제로 있었다 |
| `Encoding.UTF8`을 쓰지 않는다 (BOM을 쓴다) | **첫 요청만** malformed가 되고 그다음부터 멀쩡하다 — 인코딩이 아니라 「가끔 그런다」로 보고된다 |
| 확장이 대는 `%키%`가 실제로 정의돼 있다 | 메뉴에 퍼센트 기호가 그대로 뜬다. **빌드는 경고 0개** — 없는 키를 넣어 재 봤다 |
| `string-resources.json`이 csproj에 선언돼 있다 | 꾸러미에 안 실린다. 모든 키가 미해결이 되고, 역시 빌드는 조용하다 |
| ★ 그 선언이 `.vsextension`에 싣는다 (`VSIXSubPath`) | 꾸러미에는 실리는데 **셸이 안 보는 자리**에 실린다. 위의 두 규칙이 **둘 다 초록인 채로** 메뉴가 퍼센트 기호를 그렸다 |
| ★ XAML의 기본 xmlns가 WPF 것이다 | 빈칸 하나가 아니라 **판 전체**다. 루트 `DataTemplate`부터 못 만들고, 대화 자리에 `XamlParseException`이 벽으로 그려진다 |
| ★ XAML이 대는 바인딩 이름이 뷰모델에 있고, **`[DataMember]`가 붙어 있다** | 이름만 맞으면 패널은 서고 값만 전부 빈칸이 된다 — 요소는 다 있고 에러는 없다. 안 온 값은 기본값을 데려와서, 묻지도 않은 솔루션에 컴패니언을 띄우겠다는 단추가 떠 있었다 |

---

## 5. 주요 기술 문서

- [설계 문서 (`docs/DESIGN.ko.md`)](docs/DESIGN.ko.md): 프로세스 외(Out-of-Process) 확장 모델 선정 배경 및 브리지 계약, §10 띄워 보고 나서 남은 것.
- [플랫폼 규약 (`docs/PLATFORM.ko.md`)](docs/PLATFORM.ko.md): Remote UI 요구사항, 선언형 설정 구성 및 VS Code/JetBrains 대비 플랫폼 특성 비교.
- [편집기 제안서 (`docs/proposals/EDITORS.ko.md`)](file:///Users/sayaya/IdeaProjects/magi/docs/proposals/EDITORS.ko.md): 3대 IDE 클라이언트 개발 타당성 및 브리지 추출 전략.
- [IDE 브리지 사양 (`docs/IDE_BRIDGE.ko.md`)](../../docs/IDE_BRIDGE.ko.md): 공통 브리지 프로세스 프로토콜 규격.
