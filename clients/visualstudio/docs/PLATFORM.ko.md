# Visual Studio 플랫폼 규약 — 대조표

[↑ 클라이언트 개요](../README.md) · [설계](./DESIGN.ko.md) · [형제: VS Code 대조표](../../vscode/docs/PLATFORM.ko.md) · [형제: 젯브레인 대조표](../../jetbrains/docs/PLATFORM.ko.md)

> **왜 있나.** 집 규칙과 **Visual Studio 가 정해 둔 것**은 다른 글이다. 후자를 안 읽고 만들면
> 되긴 되는데 그 IDE 답지 않은 물건이 된다. 젯브레인에서 이 문서가 없어 세 번 났고, VS Code
> 에서는 코드보다 먼저 써서 안 났다.
>
> **다만 이 표는 앞의 둘보다 약하다.** VS Code 는 UX 가이드라인 여덟 페이지에 ✔️/❌ 목록이 있어
> 문장을 그대로 옮겼는데, Visual Studio 의 확장 문서는 **모델 선택과 API 설명이지 UX 규약이
> 아니다.** 그래서 아래 칸은 대개 「문서가 이렇게 한다고 말한다」이지 「하라고 말한다」가 아니고,
> 그 차이를 칸마다 표시했다.

---

## 1. 모델이 규약이다

VS Code 는 자리(액티비티 바·패널·상태 표시줄)를 고르는 것이 규약이었다. Visual Studio 는
**어느 확장 모델을 쓰느냐**가 그 자리를 대신한다 — 모델이 무엇을 할 수 있는지까지 정하기 때문이다.

| 규약 | 우리 계획 |
|---|---|
| 새 확장은 VisualStudio.Extensibility 로 시작하라(공식 권고) | ✓ 그렇게 한다 |
| 필요한 확장점이 없으면 in-proc 으로 내려가 VSSDK 를 쓴다 | ⏸ **안 내려가는 것을 기본으로 둔다.** 내려가면 .NET Framework 로 돌아가고 격리를 잃는다. 물음표 넷(설계 §2)을 실물로 재고 나서 정한다 |
| VS 2019 이하를 지원하려면 별도 VSIX 프로젝트를 둔다 | ✗ **안 한다.** 클라이언트가 여섯인데 일곱째를 두 벌로 만들 이유가 없다 |
| 명령은 코드로 설정한다(`.vsct` 불필요) | ✓ `CommandConfiguration` |
| 모든 명령이 백그라운드 스레드에서 돈다 | ✓ 우리 호출은 전부 소켓 왕복이라 이쪽이 맞다 |

## 2. 화면 — Remote UI

프로세스 밖이라 WPF 를 직접 못 그린다. XAML 을 확장이 주고 VS 가 그린다.

| | 우리 계획 |
|---|---|
| 테마 | **우리가 색을 안 정한다.** IDE 가 XAML 에 테마를 입힌다 — VS Code 에서 색 토큰을 손으로 맞춘 것보다 낫다 |
| 마크다운 | ⚠ **그릴 것이 없다.** 웹뷰가 아니라 XAML 이다. 코드 펜스·굵게·목록을 요소로 지어야 하고, 이 이식에서 가장 큰 미지수다(설계 §10) |
| 툴 윈도 | 대화 하나, 계획 하나. 둘뿐인 것은 우리 규칙이다 |

## 3. 확장점 — 문서가 있다고 말하는 것

**아래는 공식 개요와 샘플 목록에서 읽은 것이다. 실물로 띄워 본 것이 아니다.**

| 확장점 | 문서가 말하나 | 우리가 쓸 자리 |
|---|---|---|
| Commands | ✓ 개요 | 명령 전부 |
| Tool windows | ✓ 개요 — "dockable windows within the Visual Studio IDE" | 대화 · 계획 |
| Editor / Documents | ✓ 개요 | 손(편집 적용) · 버퍼 읽기 |
| Output window | ✓ 개요 | 안 쓴다 — 전사가 그 일을 한다 |
| User prompts · Dialogs | ✓ 개요 | 승인은 **툴 윈도 안**에 둔다(맥락이 실린다) |
| Taggers / classification | ✓ 샘플 | 훑어본 말을 줄에 걸기 |
| Text view margin | ✓ 샘플(word count) | 파일 전체에 대한 말(띠) |
| Project Query | ✓ 개요 | 안 쓴다 — 컴패니언이 제 도구로 읽는다 |
| Debugger visualizers | ✓ 개요 | 해당 없음 |
| 상태 표시줄 항목 | ❓ **목록에 없다** | 설계 §2 의 물음표 |
| 인라인 완성 | ❓ **확인 못 했다** | 〃 |
| SCM(커밋 칸) 확장 | ❓ **확인 못 했다** | 〃 |

## 4. VS Code 와 정반대인 것 셋

포팅에서 비싼 것은 없는 API 가 아니라 **반대인 규약**이다.

| | VS Code | Visual Studio |
|---|---|---|
| 화면 | 웹뷰는 "only if you absolutely need them" — 그래서 **둘로 제한**했다 | 웹뷰가 없다. **XAML 이 기본**이고 IDE 가 테마를 준다 |
| 설정 | ❌ 자체 설정 화면 금지. `contributes.configuration` 만 | VS 의 Options 페이지는 확장이 만드는 것이 보통이다 — 우리 「설정 화면을 안 짠다」 규칙이 **여기서는 근거가 없다** |
| 매니페스트 | `package.json` 의 `contributes` — 선언 | **코드**(`CommandConfiguration`) — `.vsct` 가 없어졌다 |

⚠ 가운데 줄이 특히 조심할 자리다. VS Code 의 그 금지는 **그 편집기의 규약**이지 우리 원칙이
아니었다. 여기서 그대로 가져오면 이 IDE 사람들이 기대하는 자리를 안 만드는 것이 된다. 다만
값(데몬이 원천인 설정을 두 자리에 두는 문제)은 그대로이므로, **정하지 않고 §5 에 남긴다.**

## 5. 정하지 않은 것

- **설정을 Options 페이지로 낼 것인가.** VS 규약은 그것을 허락하고 VS Code 규약은 금했다. 값과
  비용이 편집기마다 다르다.
- **알림의 규약.** VS Code 는 ✔️/❌ 목록이 있었다. VS 쪽에서 같은 급의 문장을 못 찾았다.
- **아이콘·대문자·메뉴 배치**의 규약. VS Code 와 젯브레인 모두 이 자리에 문장이 있었는데, VS
  확장 문서에서는 못 찾았다. 안 찾은 것인지 없는 것인지도 모른다.

## 출처

읽은 날: 2026-09-09.

- [Choose the right Visual Studio extensibility model](https://learn.microsoft.com/en-us/visualstudio/extensibility/visualstudio.extensibility/extensibility-models?view=visualstudio) — §1 의 비교표와 권고는 이 페이지의 것이다
- [microsoft/VSExtensibility](https://github.com/microsoft/VSExtensibility) — §3 의 확장점 목록과 요구 사항(VS 2022 17.9+, `Visual Studio extension development` 워크로드)
- [Using VisualStudio.Extensibility SDK and VSSDK together](https://learn.microsoft.com/en-us/visualstudio/extensibility/visualstudio.extensibility/get-started/in-proc-extensions?view=visualstudio) — in-proc 탈출구
