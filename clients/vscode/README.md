# clients/vscode/ — VS Code 확장

[↑ 저장소](../../README.md) · [사용자 매뉴얼](docs/MANUAL.ko.md) · [설계](docs/DESIGN.ko.md) · [화면 설계](docs/UI.ko.md) · [플랫폼 규약](docs/PLATFORM.ko.md) · [이웃 조사](docs/SURVEY.ko.md) · [무엇을 어디서 재나](docs/TESTING.ko.md) · [형제: 젯브레인](../jetbrains/README.md)

VS Code 가 연 폴더의 magi 컴패니언에게 말을 걸고, 그가 이 편집기를 부릴 수 있게 하는 확장.
**새 프로토콜을 만들지 않는다** — [`docs/CLIENTS`](../../docs/CLIENTS.ko.md) 가 정본이고 이 확장은
그 문을 두드리는 여섯 번째 클라이언트다.

## 지금 무엇이 되나

대화(패널) · 계획과 계기판(사이드바) · 상태 표시줄 · 인라인 완성 · 타이핑 중 훑어보기와 인레이 ·
편집 표식 · 승인 답하기 · 코드 액션 · 첨부 · 커밋 메시지 초안 · 「이 줄 누가 썼나」 · 대화 바꾸기 ·
모델·승인 고르기.

아직 없는 것은 [매뉴얼 §8](docs/MANUAL.ko.md) 에 있다.

## 만들고 깔기

```
npm install && npx tsc -p .
npx --yes @vscode/vsce package --no-dependencies --allow-missing-repository
code --install-extension magi-0.2.0.vsix --force
```

## 재기

```
npx tsc -p . && node --test 'out/test/*.test.js'   # 편집기 없이
npx tsc -p . && node out/live/run.js               # 실물 VS Code 를 띄워서
```

**둘 다 돌려야 한다.** 매니페스트의 오타는 첫째를 전부 통과하고 편집기에서 아무 일도 안 한다 —
아무도 등록 안 한 뷰는 그냥 안 나타나서 에러도 없다. 자세한 것은
[TESTING](docs/TESTING.ko.md).

## 구조

```
src/core/   vscode 를 import 하지 않는다. node 만으로 돈다 → 여기서 시험된다
src/ide/    vscode API 가 사는 유일한 자리
src/live/   실물 편집기 안에서만 알 수 있는 것
src/test/   core 를 잰다
```

그 갈림은 젯브레인과 같고, 이유도 같다: 프로토콜·전사·승인 어휘가 편집기 없이 재져야 한다.
`layering.test.ts` 가 그 규칙을 붙든다.

## 먼저 읽을 것

- [`docs/DESIGN.ko.md` §5](docs/DESIGN.ko.md) — **소켓 계약.** 워크스페이스 키가 한 글자라도
  다르면 **에러가 안 난다.** 골든은 코어의 `WorkspaceKey` 가 직접 답한 값이고, 이 포팅을 두 번
  잡았다(해시 상수가 표준이 아니다, `basename("/")` 이 Go 와 Node 가 다르다).
- [`docs/PLATFORM.ko.md` §9](docs/PLATFORM.ko.md) — **젯브레인과 정반대인 것 넷.** 설정 화면을
  손으로 짜는 습관이 여기서는 금지다.
- [`docs/UI.ko.md` §0](docs/UI.ko.md) — **불변식 일곱.**
