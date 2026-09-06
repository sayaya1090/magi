# 실물을 재는 자리 — 사람이 돌리는 스크립트 둘

층 1~4 는 「우리가 정한 것을 지키는가」를 잰다. 여기 둘은 **호스트가 실제로 어떻게 답하는가**를
잰다 — [무엇을 어디서 재나 §5](../docs/TESTING.ko.md) 의 다섯째 층이고, 이 저장소에서 자동
시험이 초록인 채로 결함이 나온 자리는 **전부 이 층**이었다.

둘 다 PowerShell 5.1 에서 돈다. ⚠ **파일에 BOM 이 있어야 한다** — PowerShell 5.1 은 BOM 없는
UTF-8 을 ANSI 로 읽어서, 한글이 든 스크립트가 **파서 오류**로 죽는다.

## `sweep.ps1` — 도구 전수 점검

광고된 도구를 하나씩 실물에 대고 부르고, 마지막에 통과·실패 수를 찍는다.

```powershell
& clients\powerpoint\tools\sweep.ps1
```

먼저 데몬·헬퍼를 띄우고 작업창을 열어 컴패니언에 붙여 둬야 한다(그래야 손이 산다). 토큰은
`%TEMP%\ppt-token.txt` 에서 읽으므로, 헬퍼를 새로 띄웠으면 페이지에서 다시 뽑아 둔다:

```powershell
$page = (Invoke-WebRequest https://127.0.0.1:3000/ppt/taskpane.html -UseBasicParsing).Content
if ($page -match '"token":\s*"([^"]+)"') { $matches[1] | Set-Content "$env:TEMP\ppt-token.txt" -NoNewline }
```

**끝의 `restore_slide` 가 앞에서 그린 것을 되돌린다.** 스냅샷이 실제로 도는지를 재는 자리이면서
점검이 덱에 쓰레기를 안 남기는 자리다 — 그래서 **눈으로 볼 것은 따로 그려야 한다.**

## `drive.ps1` — 작업창을 사람 대신 누른다

리본에서 애드인을 열고, 입력줄에 글을 넣어 보내고, 판을 캡처한다. 작업창은 WebView2 라 안쪽이
접근성 트리에 안 보이므로 **좌표로 누르되 좌표를 작업창 사각형에서 뽑는다** — 창을 옮기거나
크기를 바꿔도 안 깨진다.

```powershell
& clients\powerpoint\tools\drive.ps1 -Do open  -Shot pane      # 리본 → magi, 열고 캡처
& clients\powerpoint\tools\drive.ps1 -Do say   -Text "..." -Shot sent
& clients\powerpoint\tools\drive.ps1 -Do shot  -Shot now       # 작업창만
& clients\powerpoint\tools\drive.ps1 -Do full  -Shot window    # 창 전체
& clients\powerpoint\tools\drive.ps1 -Do box                   # 작업창 사각형
```

캡처는 `C:\Users\velve\Workspace\ppt-test\` 로 떨어진다(이 저장소 밖 — 스크린샷은 골라서
`docs/img/` 로 옮긴다).

## sweep.py — Mac/Linux 전수 스윕

`python3 clients/powerpoint/tools/sweep.py` — 붙은 첫 덱에 도구 48개를 순서대로 다 부르고(장을 만들고 끝에 지운다) ok/ERR 표를 낸다. `tools-sweep.ps1` 의 Mac 판. 실측 2026-09-05: 48/48 · 57호출 · 오류 0.
