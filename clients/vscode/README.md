# clients/vscode/ — VS Code 확장 (설계 단계)

[↑ 저장소](../../README.md) · [설계](docs/DESIGN.ko.md) · [편집기 셋 타당성](../../docs/proposals/EDITORS.ko.md) · [형제: 젯브레인](../jetbrains/README.md)

> **상태: 설계만 있다. 코드는 없다.**
>
> 순서는 사용자가 정했다(2026-09-08): 만들기 전에 설계문서부터. 그래서 이 디렉토리에는 지금
> [`docs/DESIGN.ko.md`](docs/DESIGN.ko.md) 하나가 있고, 그것이 승인되면 `src/` 가 선다.

## 무엇을 만드는가

VS Code 가 연 폴더의 magi 컴패니언에게 말을 걸고, 그가 이 편집기를 부릴 수 있게 하는 확장.
**새 프로토콜을 만들지 않는다** — [`docs/CLIENTS`](../../docs/CLIENTS.ko.md) 가 정본이고 이 확장은
그 문을 두드리는 여섯 번째 클라이언트다(터미널·웹 콘솔·젯브레인·PowerPoint·Excel·Word 에 이어).

## 왜 이것부터인가

[EDITORS](../../docs/proposals/EDITORS.ko.md) 가 셋을 견주고 내린 결론이다. VS Code 는 젯브레인
기능 열여섯 칸 **전부**에 대응 API 가 있어 「무엇이 진짜 공통인지」가 여기서 드러난다. 공통을
`magi ide-bridge` 로 뽑는 것은 **두 벌을 본 뒤**다 — 한 벌만 보고 뽑은 계약은 그 한 벌의 모양이
된다.

## 이 디렉토리가 설 모양

젯브레인과 같은 두 층 갈림이다. 그 갈림 덕에 프로토콜·전사 조립·승인 어휘가 **편집기 없이**
시험된다.

```
clients/vscode/
  README.md          ← 지금 이 파일
  docs/DESIGN.ko.md  ← 설계
  src/core/          vscode 를 import 하지 않는다. node 만으로 돈다
  src/ide/           vscode API 가 사는 유일한 자리
  test/              core 를 잰다
```

## 먼저 읽을 것

- [`docs/DESIGN.ko.md` §5](docs/DESIGN.ko.md) — **소켓 계약.** 워크스페이스 키가 한 글자라도
  다르면 에러가 안 난다. 아무도 없는 소켓을 찾고, 「실행되지 않음」이라 말하고, 같은 트리에 둘째
  데몬을 띄우자고 권한다. 골든은 코어의 `WorkspaceKey` 가 직접 답한 값이다.
- [`docs/DESIGN.ko.md` §4](docs/DESIGN.ko.md) — **자리가 달라지는 넷.** 젯브레인의 자리를 흉내
  내지 않고 VS Code 가 이미 그런 말을 세우는 자리로 옮긴다.
- [`docs/DESIGN.ko.md` §10](docs/DESIGN.ko.md) — **재지 않은 것.** 원격·`vscode.dev`·웹뷰 렌더러·
  배포 절차·분량. 짐작은 안 적었다.
