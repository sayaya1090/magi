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

⚠ **COM 쪽(`InteropOps*.cs`)은 실물 PowerPoint 2021 에서 아직 안 재 봤습니다.** 객체 모델 문서로 썼습니다. 가짜 덱에서
초록인 것은 Hand 의 판단(인자 검사·정렬 계산·문구)이 맞다는 뜻이지 COM 호출이 맞다는 뜻이 아닙니다 — 첫 Windows 실측에서
고칠 것이 나온다고 보고 읽어야 합니다. 특히 표 스타일 GUID, 애니메이션 `BuildByLevelEffect`, 배경 무늬 이름은 의심 순위가 높습니다.

## 화면(창)은 아직 없습니다

365 판은 작업창(Office.js)이 손이자 화면입니다. 2021 에서는 손이 이 프로세스이고, 화면은 따로입니다. 후보는
VSTO 애드인 안의 WebView2 로 같은 `taskpane.html` 을 띄우는 것인데, 그 창은 **손으로 붙으면 안 됩니다** — 헬퍼의
허브는 같은 `presentation` 키로 붙는 연결을 하나로 보고 호출을 아무 쪽에나 줍니다(`helper/hand.go Join`). 헬퍼에
`role=viewer`(호출은 안 받고 전사만 받는 연결)를 더하고 창이 그 역할로 붙는 것이 다음 일입니다. 그 전까지 2021 에서는
브라우저로 헬퍼의 `taskpane.html?…` 를 여는 것도 안 됩니다 — 그 페이지가 손 역할까지 하려 듭니다.

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
