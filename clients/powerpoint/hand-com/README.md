# magi-ppt-hand — Office 2021 용 COM 손

Office 2021(LTSC 포함)은 Office.js 의 PowerPoint API 천장이 `PowerPointApi 1.2` 라 도형에 닿을 수 없습니다
(`../LTSC.md` §3). COM 객체 모델은 전부 열려 있으므로, 이 프로세스가 PowerPoint 를 COM 으로 움직이는
**손** 하나로 헬퍼에 붙습니다. 헬퍼(`magi-ppt`)·MCP 도구 이름과 스키마·magi 는 그대로입니다(`../LTSC.md` §1).

```
magi ── MCP ── magi-ppt(헬퍼) ── /hand/stream ── magi-ppt-hand(이 프로세스) ── COM ── PowerPoint 2021
                                └── 창(taskpane, Office.js 손)      ← 365 판
```

## 규약

- 붙기: `GET /hand/stream?token=&presentation=<키>&label=<파일명>` (SSE). 첫 프레임 `hello{document,label,epoch}`,
  이어 `call{id,op,args}`. 토큰은 창이 받는 것과 같은 것으로, 헬퍼가 `taskpane.html` 에 박아 주는 값을 읽습니다.
- 답: `POST /hand/reply?token=` 에 `{id, document, result, changed[], error, epoch, count}`. `document` 는 **hello 가 준
  키**(`pid-…`)여야 합니다 — 손의 제 키로 답하면 헬퍼가 길을 못 찾습니다(2026-09-05 실측, 404).
- `changed` 는 사람이 읽는 한국어 한 줄씩, `error` 는 한 문장. 모르는 op 는 「이 손이 아직 모릅니다 — 아는 것: …」로
  답합니다. 조용히 삼키지 않습니다.

## 실측(2026-09-05, mac · `--fake`)

PowerPoint 없이 메모리 덱으로 이 맥의 헬퍼에 붙여 MCP 로 흘렸습니다. 첫 판(15개)에서 여섯 호출, 48개 판에서 열넷
(`add_table`·`set_table_cells`·`add_chart`·`set_theme_colors`·`set_background`·`suggest`·`read_suggestions`·`animate_slide`·
`align_shapes`·`snapshot_slide`·`format_text`(글이 없어 정직한 거절)·`export_slide_ooxml`·`describe_style`·`list_slides`).
전부 헬퍼를 거쳐 손에 닿고 답이 돌아왔고, `revision.count` 가 바꾼 호출마다 올랐습니다.

## 다루는 것 — 도구 48개 전부

헬퍼 catalogue 의 48개 이름을 다 압니다(`helper/hand_com_parity_test.go` 가 양쪽 차집합이 비었는지 잽니다).
「스킬이 안 부르는 도구도 사람이 쓰다 보면 필요하다」(사용자, 2026-09-05)가 기준이라 빼 둔 것이 없습니다. 다만:

| | |
|---|---|
| 창이 있어야 뜻이 있는 것 | `advise`/`clear_advice` 는 「받았다」만 답하고 `shown:false` 를 적습니다. `suggest` 는 덱 파일의 태그(`MAGI.FIX.*`, 365 판과 같은 규약)로 남으니 그 덱을 365 에서 열면 카드가 보입니다 |
| 이 손이 거절하는 인자 | `format_shape{decorative}` — 2021 객체 모델에 그 속성이 없습니다. `table_style` 은 `Styles.cs` 의 이름 31개만(GUID 표) |
| 365 판보다 나은 것 | `add_chart` 는 품은 통합 문서에 값을 적어 「데이터 편집」이 열립니다(`data_sheet:true`). `ea_font` 는 `Font.NameFarEast` 한 줄 |
| 스냅숏 | 덱 사본(.pptx)을 임시 파일로 저장해 두고 `InsertFromFile` 로 되살립니다 — 손이 떠 있는 동안만 압니다 |

## 실측(2026-09-05, Windows · 실물 PowerPoint)

**COM 쪽(`InteropOps*.cs`)을 실물 PowerPoint 에 대고 처음 쟀습니다. 48개 전부 도는 것을 확인했습니다.**

작업창(Office.js 손)은 안 열고 이 손만 붙였습니다 — 손이 둘이면 무엇이 답했는지 못 가른다.

| | |
|---|---|
| 불러 본 것 | **48/48** — 헬퍼 catalogue 의 이름 전부 |
| 성공 | **48** (첫 판에서 다섯이 거절됐는데 전부 시험 각본의 인자가 틀린 것이었고, 고쳐 부르니 다 됐습니다) |
| 덱에 정말 들어갔나 | COM 으로 되읽어 확인 — 7장, 차트 2·표 1·그림 2, 노트·태그·애니메이션 다 남았고, 저장 뒤 85,744 바이트 |

의심 순위가 높다고 적어 뒀던 셋도 실물에서 돌았습니다: 표 스타일(`format_table_cells`·`replace_table`),
애니메이션(`animate_slide` → `read_animation` 이 걸음 1개를 되읽음), 배경(`set_background`).

**한 가지 값을 쟀습니다.** `add_chart` 가 **2.4~3.5초** 걸립니다 — 다른 도구는 10~400ms 입니다. COM 으로 품은
통합 문서를 만들어 값을 적기 때문이고, 그 대가로 「데이터 편집」이 열립니다(365 판은 OOXML 이라 빠른 대신 안 열립니다).
느린 것이 결함이 아니라 **그 기능의 값**이라는 것을 이제 압니다.

## 실측(2026-09-05, **Office LTSC Professional Plus 2021** 16.0.14334.20848)

M365 를 지우고 이 머신에 2021 을 깔아 같은 훑기를 한 번 더 돌렸습니다. **48/48 돕니다.**

「의심 순위가 높다」고 적어 뒀던 셋이 전부 2021 에서 돌았습니다:

| 의심했던 것 | 2021 실측 |
|---|---|
| 표 스타일 GUID 33개 | `ThemedStyle1Accent1`·`MediumStyle2Accent1` 둘 다 먹습니다 |
| 애니메이션 `BuildByLevelEffect` | 문단별로 걸리고 COM 이 효과 3개로 셉니다 |
| 배경 무늬 | `set_background` 가 칠합니다 |

`add_chart` 가 품는 통합 문서도 2021 에서 열립니다(`ChartData.Workbook` → 1시트) — **「데이터
편집」이 됩니다.** 걸리는 시간은 2.4초로 365 에서 잰 것과 같습니다.

### 2021 에서만 드러난 결함 둘 — 고쳤습니다

**하나. `read_notes` 가 365 와 다른 칸 이름을 썼습니다.** 365 는 `notes`, 이 손은 `text` 였습니다.
같은 도구가 손마다 다른 이름을 쓰면 모델이 한쪽에서 배운 것이 다른 쪽에서 안 맞습니다 — 실제로
노트는 잘 적혔는데 읽는 쪽이 「없다」로 읽었습니다. 이제 `notes` 를 싣고, `text` 도 그대로 둡니다
(이 손의 답을 `text` 로 읽는 것이 이미 있을 수 있고, 칸 하나 더 싣는 값이 그것을 깨뜨리는 값보다
쌉니다).

**둘. 줄바꿈이 문단이 안 됐습니다.** `\n` 을 그대로 넘겨서 세 줄짜리 본문이 문단 하나였고, 그래서
`paragraphs:"each"` 가 **걸음을 하나만** 걸었습니다. 애니메이션 코드가 아니라 문단이 애초에
하나였던 것입니다. 365 판이 같은 자리를 먼저 고쳤는데(`asParagraphs`) 이 손에는 없었습니다.
이제 글을 쓰는 다섯 자리(자리표시자·도형·표 칸 둘)에서 `\n` 을 `\r` 로 바꿉니다.

고친 뒤 다시 재니 문단 3개, 걸음 3개입니다.

### 화면은 365 작업창이고, 그 창이 스스로 물러선다

**화면은 365 작업창을 그대로 씁니다 — 2021 에서 뜹니다(2026-09-05 실측).** 처음에는 「안 뜬다」고
적었는데, 개발자 레지스트리 키로 넣어서 그랬습니다. MS 가 Windows 데스크톱에 적어 둔 길은 **신뢰
카탈로그(공유 폴더)** 이고, 그 길로 넣으니 홈 탭에 단추가 서고 창이 열리고 헬퍼에 붙습니다. 창이 말한
값은 `PowerPointApi 1.2 ✓ · 1.5~1.10 ✗ · SharedRuntime 1.1 ✓`. 절차는 `docs/INSTALL.ko.md`.

**그 창은 1.8 아래 호스트에서 화면(viewer)으로 붙습니다**(2026-09-06). 그 전에는 손으로 붙어 도구 48개를
광고했고, 헬퍼가 호출을 창에 주면 1.2 에서 못 하는 것이 Office.js 의 날 오류로 돌아왔습니다(실측:
`'index' 속성을 사용할 수 없습니다`). 이제 창은 `isSetSupported` 를 읽고 `role=viewer` 로 붙고 손도 안
세웁니다(`addin/src/usecase/HandRole.js`). 헬퍼는 창의 덱 이름이 이 손의 이름과 달라도 **있는 손**을
보여 줍니다(`helper/hand.go Peek`) — 2021 에는 태그 칸이 없어 창이 빈 이름을 들고 오기 때문입니다. 고친
뒤 헬퍼가 보는 덱은 이 손 하나이고 `list_slides` 가 그리로 갑니다.

### 참고: 365 에서 먼저 쟀던 판

### 그래도 이건 **LTSC 실측이 아닙니다**

이 머신은 Microsoft 365(빌드 16.0.20326)입니다. COM 자동화는 LTSC 전용이 아니라 데스크톱 PowerPoint 면 붙으므로
여기서 잰 것은 **COM 코드가 진짜 PowerPoint 를 움직인다**까지입니다.

| 이제 아는 것 | 아직 모르는 것 |
|---|---|
| COM 호출이 실물에서 통한다 | **2021 의 옛 객체 모델 차이** |
| 헬퍼↔손 규약이 실물 덱에서 돈다 | 365 에만 있는 속성을 쓰고 있어도 여기서는 통과한다 |
| 48개가 다 닿는다 | 그 호출이 2021 에서도 될지 |

**LTSC 2021 이 깔린 머신에서 한 번 더 돌려야 이 표의 오른쪽이 비워집니다.**

## 화면(창)은 365 작업창입니다

2021 에서도 365 판 작업창이 뜹니다(위 절). 창은 **손으로 붙으면 안 됩니다** — 헬퍼의 허브는 같은
`presentation` 키로 붙는 연결을 하나로 보고 호출을 아무 쪽에나 줍니다(`helper/hand.go Join`). 그래서 창은
1.8 아래에서 `role=viewer`(호출은 안 받고 전사만 받는 연결)로 붙습니다. 이 프로세스를 먼저 띄우든 창을 먼저
열든 상관없습니다 — 손이 없으면 창이 「아직 안 붙었다」고 적고 기다렸다가 따라 붙습니다.

## 빌드·실행

- 빌드는 어디서나 됩니다(PIA 는 메타데이터). 실행은 **Windows + PowerPoint 2021 이 떠 있고 덱이 열려 있을 때**만.
- .NET 9 런타임이 필요합니다(`dotnet --list-runtimes`). 없으면 `dotnet publish -r win-x64 --self-contained` 로 묶습니다.
- `Microsoft.Office.Core` 는 NuGet 에 공식 PIA 가 없어 재포장본(`MicrosoftOfficeCore` 15.0.0)을 씁니다. 설치된 Office
  의 PIA(GAC 의 `office.dll`)로 바꿔도 됩니다.

```
cd clients/powerpoint/hand-com/src
dotnet run -- --helper https://127.0.0.1:3000          # Windows: 떠 있는 PowerPoint 의 활성 덱에 붙는다
dotnet run -- --fake                                   # 어디서나: 메모리 덱으로 규약만 돈다
cd ../tests && dotnet test                             # 규약 시험 19
```

떠 있는 PowerPoint 는 ROT 에서 꺼냅니다(`GetActiveObject`, .NET 9 엔 `Marshal.GetActiveObject` 가 없어 P/Invoke).
몰래 PowerPoint 를 띄우지 않습니다.
