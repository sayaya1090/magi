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

PowerPoint 없이 메모리 덱으로 이 맥의 헬퍼에 붙여 MCP 로 여섯 호출을 흘렸습니다: `list_slides` → `add_slides`(2장, 긴
제목에 ⚠) → `set_text` → `render_slide` → `add_chart`(모름, 정직한 거절) → `list_slides`(3장). 여섯 다 헬퍼를 거쳐
손에 닿고 답이 돌아왔고, `revision.count` 가 바꾼 호출마다 올랐습니다.

## 다루는 것 / 아직 아닌 것

| 됨(15) | 아직(33) |
|---|---|
| list_slides · read_slide · list_layouts · add_slide · add_slides · delete_slide · reorder_slide · duplicate_slide · set_text · read_notes · set_notes · render_slide · add_shape · delete_shape · apply_style(서체·크기·색, `ea_font` 제외) | 표 6종 · 차트 · 그림 · 애니메이션 · 테마 색 읽기/쓰기 · 배경 · 하이퍼링크 · 태그 · 제안·안내 · 그룹 · 정렬 · 스냅숏/복원 · OOXML 내보내기 · find_shapes · describe_style · format_shape · format_text · render_shape · move_shape · apply_layout |

`ea_font` 는 365 판이 OOXML 재작성으로 하는 것이라 COM 에서는 `Font.NameFarEast` 로 따로 걸어야 합니다(미착수).

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
cd ../tests && dotnet test                             # 규약 시험 9
```

떠 있는 PowerPoint 는 ROT 에서 꺼냅니다(`GetActiveObject`, .NET 9 엔 `Marshal.GetActiveObject` 가 없어 P/Invoke).
몰래 PowerPoint 를 띄우지 않습니다.
