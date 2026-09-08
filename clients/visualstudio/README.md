# clients/visualstudio/ — Visual Studio 확장 (설계 단계)

[↑ 저장소](../../README.md) · [설계](docs/DESIGN.ko.md) · [플랫폼 규약](docs/PLATFORM.ko.md) · [편집기 셋 타당성](../../docs/proposals/EDITORS.ko.md) · [형제: VS Code](../vscode/README.md) · [형제: 젯브레인](../jetbrains/README.md)

> **상태: 설계만 있다. 코드는 없고, 이 기계에서는 만들 수도 없다.**
>
> Visual Studio 확장은 **Windows 에서만** 지어진다. 여기는 macOS 이고 `msbuild` 도 Visual Studio
> 도 없다. **VS for Mac 은 2024 에 단종됐다.** 짓고 깔고 재는 것은 Windows 기계에서 해야 한다 —
> [설계 §9](docs/DESIGN.ko.md) 에 무엇을 어떤 순서로 해야 하는지 적었다.

## 무엇을 만드는가

Visual Studio 가 연 솔루션의 magi 컴패니언에게 말을 걸고, 그가 이 IDE 를 부릴 수 있게 하는 확장.
**새 프로토콜을 만들지 않는다** — [`docs/CLIENTS`](../../docs/CLIENTS.ko.md) 가 정본이다.

## 이 설계의 값 — 두 번째 벌

[EDITORS](../../docs/proposals/EDITORS.ko.md) 가 「VS Code 부터, **두 벌을 본 뒤에** 공통을
뽑는다」고 정했다. VS Code 는 섰다. 이것이 두 번째 벌이고, 그래서 [설계 §3](docs/DESIGN.ko.md) 이
비로소 답할 수 있다: **공통은 여덟이다**(소켓 경로 · 줄 단위 JSON · 전사 조립 · 「무엇을 하는
중인가」 · 승인 어휘 · 훑어본 말 가르기 · 완성 겹침 · 판 고르기). 그 여덟을 C# 으로 또 쓰면 세
벌이 되고, 같은 사실이 세 군데 적히면 하나는 갈라진다.

그래서 §3 이 `magi ide-bridge` 를 **권고한다** — 공통 여덟을 코어 바이너리가 맡고, 편집기 층은
문 두드리기와 그리기만 한다. 그 결정은 사용자 몫으로 남겼다.

## 먼저 읽을 것

- [`docs/DESIGN.ko.md` §1](docs/DESIGN.ko.md) — **어느 모델인가.** 확장 모델 셋 중 하나를 고르는
  것이 나머지를 거의 다 정한다. `VisualStudio.Extensibility` 를 프로세스 밖으로 쓴다
- [`docs/DESIGN.ko.md` §2](docs/DESIGN.ko.md) — **기능 대조와 물음표 넷.** 상태 표시줄·인라인
  완성·커밋 초안·코드 액션이 이 모델에서 되는지 **확인 못 했다.** 지어내지 않고 Windows 에서
  실물로 확인할 일로 남겼다
- [`docs/DESIGN.ko.md` §5](docs/DESIGN.ko.md) — **소켓 계약.** Windows 에서 달라지는 셋. 특히
  `%AppData%` 아래 AF_UNIX 의 함정을 오피스 클라이언트가 이미 겪었다
- [`docs/PLATFORM.ko.md` §4](docs/PLATFORM.ko.md) — **VS Code 와 정반대인 것 셋.** 특히 설정
  화면: VS Code 는 금지고 여기서는 보통이다. 그 금지를 그대로 옮기면 이 IDE 사람들이 기대하는
  자리를 안 만드는 것이 된다
