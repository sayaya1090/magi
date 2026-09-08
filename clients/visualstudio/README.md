# clients/visualstudio/ — Visual Studio 확장

[↑ 저장소](../../README.md) · [설계](docs/DESIGN.ko.md) · [플랫폼 규약](docs/PLATFORM.ko.md) · [편집기 셋 타당성](../../docs/proposals/EDITORS.ko.md) · [형제: VS Code](../vscode/README.md) · [형제: 젯브레인](../jetbrains/README.md)

> **상태: 코어 층이 섰다. IDE 층은 아직 없다.**
>
> `src/Magi.Core` 는 브리지와 말하고 IDE 를 모른다 — `dotnet test` 로 25개가 돈다. 그 위에 얹을
> `src/Magi.Extension` 이 다음이다([설계 §9](docs/DESIGN.ko.md) 걸음 2).
>
> Visual Studio 확장은 **Windows 에서만** 지어진다. **VS for Mac 은 2024 에 단종됐다.** 이 설계는
> macOS 에서 쓰였고, 그래서 실물로 못 잰 것을 물음표로 남겼다 — 그 물음표들이 **2026-09-09 에
> Windows 기계에서 닫혔다.** 무엇을 어떤 순서로 하는지는 [설계 §9](docs/DESIGN.ko.md), 아직 안
> 잰 것은 [§10](docs/DESIGN.ko.md).

## 무엇을 만드는가

Visual Studio 가 연 솔루션의 magi 컴패니언에게 말을 걸고, 그가 이 IDE 를 부릴 수 있게 하는 확장.
**새 프로토콜을 만들지 않는다** — [`docs/CLIENTS`](../../docs/CLIENTS.ko.md) 가 정본이다.

## 이 설계의 값 — 두 번째 벌

[EDITORS](../../docs/proposals/EDITORS.ko.md) 가 「VS Code 부터, **두 벌을 본 뒤에** 공통을
뽑는다」고 정했다. VS Code 는 섰다. 이것이 두 번째 벌이고, 그래서 [설계 §3](docs/DESIGN.ko.md) 이
비로소 답할 수 있다: **공통은 여덟이다**(소켓 경로 · 줄 단위 JSON · 전사 조립 · 「무엇을 하는
중인가」 · 승인 어휘 · 훑어본 말 가르기 · 완성 겹침 · 판 고르기). 그 여덟을 C# 으로 또 쓰면 세
벌이 되고, 같은 사실이 세 군데 적히면 하나는 갈라진다.

그래서 §3 이 `magi ide-bridge` 를 **권고했고, 정해졌고, 이 클라이언트가 그 첫 사용자다.** 공통
여덟을 코어 바이너리가 맡고 편집기 층은 문 두드리기와 그리기만 한다. `Magi.Core` 에는 소켓 경로
유도도 FNV 상수도 **없고, 없다는 것을 시험이 지킨다.**

## 지금 있는 것

```
src/Magi.Core/         브리지와 말한다. VisualStudio.Extensibility 를 참조하지 않는다
test/Magi.Core.Tests/  25개. IDE 없이, 바이너리 없이, 컴패니언 없이 돈다
tools/smoke.ps1        실물 브리지를 한 번 두드린다 — 유닛 시험이 일부러 안 하는 일
```

```powershell
dotnet test clients/visualstudio/test/Magi.Core.Tests
pwsh clients/visualstudio/tools/smoke.ps1        # magi 가 PATH 에 있어야 한다
```

**시험이 지키는 규칙 넷** — 넷 다 어겼을 때 조용히 틀리는 것들이라 시험으로 세웠다.

| 규칙 | 어기면 |
|---|---|
| `Magi.Core` 가 `Microsoft.VisualStudio*` 를 참조하지 않는다 | 재는 것이 줄어든다. 이 편집기에는 실물 확장을 자동으로 재는 1급 길이 없다(설계 §6) |
| 소켓 경로를 이쪽에서 유도하지 않는다(FNV 상수가 소스에 없다) | **에러 없이** 아무 데도 안 닿는다. 컴패니언이 있는 트리를 「안 돌고 있다」고 말하고 두 번째를 띄운다 |
| 와이어 이름이 브리지의 Go 소스에 실제로 있다 | 예외가 아니라 기본값이 된다. 화면은 「없다」고 말하고 아무것도 실패하지 않는다 |
| `Encoding.UTF8` 을 쓰지 않는다(BOM 을 쓴다) | **첫 요청만** malformed 가 되고 그다음부터 멀쩡하다 — 인코딩이 아니라 「가끔 그런다」로 보고된다 |

## 먼저 읽을 것

- [`docs/DESIGN.ko.md` §1](docs/DESIGN.ko.md) — **어느 모델인가.** 확장 모델 셋 중 하나를 고르는
  것이 나머지를 거의 다 정한다. `VisualStudio.Extensibility` 를 프로세스 밖으로 쓴다
- [`docs/DESIGN.ko.md` §2](docs/DESIGN.ko.md) — **기능 대조.** 물음표 넷(상태 표시줄·인라인
  완성·커밋 초안·코드 액션)은 **넷 다 없는 것으로 닫혔다.** 문서 목록을 다시 읽어서가 아니라
  설치된 SDK 의 공개 타입을 세서 — 그리고 표에 없던 `ICodeLensProvider` 가 그때 나왔다
- [`docs/DESIGN.ko.md` §5](docs/DESIGN.ko.md) — **소켓 계약.** `%AppData%` 아래에서는 AF_UNIX
  **connect 가 안 된다**(bind 는 된다). 남은 소켓 파일은 코어의 삭제 기법으로도 못 지운다.
  이 확장은 소켓을 거기 두면 안 된다
- [`docs/PLATFORM.ko.md` §4](docs/PLATFORM.ko.md) — **VS Code 와 정반대인 것 셋… 이었던 것.**
  설정 줄은 **틀렸다.** 「VS 에서는 확장이 Options 페이지를 만든다」는 VSSDK 이야기고, 우리가
  고른 모델에는 그 문이 없다 — 선언형이라 VS Code 와 같은 모양이다
