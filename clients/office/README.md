# magi office — 파워포인트·엑셀·워드 애드인의 헬퍼, 한 프로세스

[파워포인트 매뉴얼](../powerpoint/docs/MANUAL.ko.md) · [엑셀 매뉴얼](../excel/docs/MANUAL.ko.md) · [워드 매뉴얼](../word/docs/MANUAL.ko.md) · [클라이언트 문 계약](../../docs/CLIENTS.ko.md)

`magi office` 는 `magi` 바이너리의 하위 명령이다. 2026-09-06 까지 셋이던 헬퍼(`magi-ppt`·`magi-xl`·`magi-word`)를
한 벌로 모았다 — 사람이 신뢰 저장소에 넣는 인증서가 **하나**, 자동 시작이 하나, 볼륨 판 Excel 의 신뢰 카탈로그 키가 하나,
받을 파일이 하나(`magi`)가 되게.

## 까는 법

```powershell
irm https://raw.githubusercontent.com/sayaya1090/magi/main/clients/office/install.ps1 | iex
```

저장소도 툴체인도 필요 없다 — 이 한 줄이면 된다. 파일로 받아 두고 돌려도 되고, 그때는 아래
플래그를 쓸 수 있다(`irm … | iex` 로는 인자를 넘길 수 없어 기본 설치만 된다).

```powershell
.\install.ps1                 # 같은 것
.\install.ps1 -Uninstall      # 지우기
.\install.ps1 -Clean          # 등록과 Office 캐시를 비우고 다시
```

⚠ **파일로 받아 두면 그 줄 그대로는 안 돈다 — 두 가지가 같이 막는다.** 클라이언트 Windows 의 기본
실행 정책(`Restricted`)과, 받은 파일에 붙는 「인터넷에서 왔다」는 표시다. 실측(2026-09-08, 깨끗한
LTSC 2021 머신에 릴리스의 스크립트를 받아서): `.\install.ps1` 이 한 글자도 못 돌고
`이 시스템에서 스크립트를 실행할 수 없으므로 … 로드할 수 없습니다`(`PSSecurityException`)로 멈췄다.
표시를 떼고 **이 실행에만** 정책을 푼다 — 시스템 정책은 안 건드린다:

```powershell
Unblock-File .\install.ps1
powershell -NoProfile -ExecutionPolicy Bypass -File .\install.ps1
```

**`irm … | iex` 가 기본인 이유가 이것이다.** 그쪽은 파일이 아니라 둘 다에 안 걸린다. 파일로 받는 것은
플래그가 필요할 때이고, 그때는 위 두 줄이 함께 간다.

**툴체인이 필요 없다.** 설치기가 릴리스에서 받는다 — 코어는 `v*` 레인에서, Office 자산 둘은
`office-v*` 레인에서. 어느 판인지는 `badges` 브랜치의 한 줄짜리 파일(`core-latest.txt`·
`office-latest.txt`)이 말한다. `/releases/latest` 를 안 쓰는 이유는 그것이 이 저장소의 네 레인을
안 가려서다 — 콘솔이 더 최근에 나갔으면 Office 자산은 404 이고 `checksums.txt` 는 남의 것을 200 으로
답한다.

추가 기능만 .NET 데스크톱 런타임이 필요하고(자체 포함이 COM 호스팅에서 미지원이라 그렇다),
없으면 설치기가 받아서 깐다. 어댑터는 자체 포함이라 런타임과 무관하다.

⚠ **32비트 Office 는 릴리스 자산이 없다.** COM 추가 기능은 Office 프로세스 안에서 뜨므로 비트 수가
같아야 하고, 레인은 x64 만 낸다. 그 경우 설치기가 사유를 말하고 멈춘다 — 아래 `-FromSource` 로는 된다.

### 저장소에서 짓기

```powershell
.\install.ps1 -FromSource     # Go + .NET 9 SDK 가 필요하다
```

고치는 사람의 자리다. 방금 고친 것이 배포판보다 중요할 때 쓴다.

### 헬퍼를 직접 굴리기

```
go build -o magi ./cmd/magi
./magi office -cert-hint         # <config>/office-helper-cert.pem 을 신뢰 저장소에 — 한 번
./magi office                    # 127.0.0.1:3000 — /ppt·/xl·/word 세 판
./magi office -allow-rules=xl    # 그 프로그램의 읽기 도구 허용 규칙(config.toml 에 붙여 넣는다)
```

볼륨 판(LTSC 2021) PowerPoint 는 작업창으로 편집이 안 돼 COM 손(`magi-ppt-hand`)이 편집한다. 설치기가
볼륨 판이면 그 어댑터도 받는다. 어댑터를 띄우는 것은 헬퍼다(`helper/adapter.go`) — 로그인 때 뜨는 등록은 헬퍼 하나뿐이다(2026-09-07 — 그 전엔
파워포인트 판 설치기에 미뤄, 통합 설치기만 돌린 2021 은 「magi-ppt-hand 를 띄워야 편집이 됩니다」에서 멈췄다).

Windows 에서 컴패니언 셋은 평소의 magi 와 **같은 설정 나무**(`%APPDATA%\magi` — config.toml·plugins)를 보고, 소켓과 명단
파일만 `~/.magi`(`MAGI_SOCKET_DIR`, 설치기가 사용자 환경 변수로 건다)에 둔다. 유닉스 주소는 100바이트라 `%APPDATA%\magi`
아래는 긴 사용자 이름에서 넘치고, 카탈로그는 공유로 내주는 폴더여야 해서다. 2026-09-07 까지는 설정 디렉토리 자체를
`~/.magi` 로 못 박아 평소 데몬은 되는데 창 셋만 「API 키가 없다」였다 — 백엔드를 플러그인이 실행 뒤에 넣는 판에서는
config 를 복사해도 소용이 없어, 소켓 자리를 떼는 것으로 갈랐다(`daemon.SocketDir`).

## 자리

| 자리 | 파워포인트 | 엑셀 | 워드 |
|---|---|---|---|
| 작업창 | `/ppt/taskpane.html` | `/xl/taskpane.html` | `/word/taskpane.html` |
| MCP 서버 | `/ppt/mcp` (`ppt`, 도구 49 — 손 도구 48 + `list_documents`) | `/xl/mcp` (`xl`, 76) | `/word/mcp` (`word`, 66) |
| 손 스트림 | `/ppt/hand/stream?presentation=` | `/xl/hand/stream?workbook=` | `/word/hand/stream?doc=` |
| 문서 키 | `pid-…` | `wb-…` | `wd-…` |
| 컴패니언 워크스페이스 | `<config>/powerpoint` | `<config>/excel` | `<config>/word` |
| 도구 표·열거형·지침·스킬 | `ppt_*.go`, `skills/powerpoint` | `xl_*.go`, `skills/excel` | `word_*.go`, `skills/word` |

셋이 나누는 것은 인증서·토큰·포트뿐이다. 손 허브·MCP 서버·API·컴패니언은 프로그램마다 따로 선다(`serve.go` 의 `mount`).
다른 점은 전부 `app.go` 의 `App` 값 하나에 있다 — 새 프로그램을 더하면 `App` 하나와 `*_tools.go`·`*_enums.go`·
`*_instructions.go`·스킬 디렉토리를 더한다.

## Office 를 켤 때 뜨고, 다 끄면 없다

사용자 결정(2026-09-07): 「윈도우 로그인 때 자동 켜지는 거 하지 말라고」, 「헬퍼랑 데몬은 각 오피스가 켜진 게 있을 때만
켜져 있고, 인스턴스가 없으면 종료되고」. 그래서 이 계정에 magi 가 떠 있는 구간은 **Office 를 켠 동안**뿐이다.

| 언제 | 무엇이 뜨고 지나 |
|---|---|
| Office 를 켠다 | COM 추가 기능(`addin-com`)이 Office 프로세스 안에서 뜨며 헬퍼를 띄운다 — 로그인 등록이 없는 이유다 |
| 작업창을 연다 | 헬퍼가 그 프로그램 몫의 컴패니언(`magi --daemon`)을 마련한다 |
| PowerPoint 2021 이 떠 있다 | 헬퍼가 편집 어댑터(`magi-ppt-hand`)를 띄운다(`helper/adapter.go`) |
| 그 프로그램을 다 끈다 | 그 컴패니언이 60초 뒤 내려간다(`helper/idle.go`) |
| Office 를 다 끈다 | 어댑터가 스스로 끝나고, 헬퍼도 60초 뒤 끝난다(`officeWatch`) |

재는 것은 프로세스다(Windows `tasklist`, mac `pgrep`). **못 재는 OS 에서는 아무것도 안 내린다** — 모르는 것을 「없다」로
읽으면 사람이 쓰는 헬퍼를 끈다. 개발할 때 Office 없이 헬퍼를 띄워 두려면 `-keep-running`.

**실측(2026-09-07, LTSC 2021 16.0.14334 · x64).** 이 표를 끝까지 돌렸다 — PowerPoint 를 켜면 COM 추가 기능이 헬퍼를
띄우고(`start.log`), 셋을 다 켜도 헬퍼는 하나이고, 다 끄면 **60초에 헬퍼·컴패니언·어댑터가 전부 스스로 끝나** 이
계정에 magi 가 하나도 안 남고, 다시 켜면 다시 뜬다. 로그인 등록은 하나도 없다. 자세한 표는
[`addin-com/README.md`](addin-com/README.md) §실측.

**그리고 배포본으로 다시(2026-09-08, 같은 머신을 비운 뒤 `office-v0.0.4`).** 저장소도 툴체인도 없이 릴리스의
설치기만 받아 끝까지 돌렸다 — 받는 것 넷, 컴패니언 설정 셋, 인증서, 카탈로그 등록, COM 추가 기능 등록까지 오류 0.
그다음 **magi 를 전부 죽이고 PowerPoint 만 켰다**:

```
죽인뒤 magi=0  →  ppt=1 · magi=4
start.log: [POWERPNT] 헬퍼를 띄웠습니다: …\magi.exe office -config-dir … -socket-dir …
```

로그를 POWERPNT 프로세스가 직접 적었다. 대화도 돌았고 턴 끝에 ⚖ 판정 줄이 없었다 — 카운슬이 꺼진 채로 깔린다.

그 전에 **그 추가 기능이 PowerPoint 를 죽이고 있었다** — `IDTExtensibility2` 를 `InterfaceIsIDispatch` 로 선언해
vtable 이 넷 밀렸고, Office 가 `OnConnection` 을 부르는 순간 `AccessViolationException` 으로 프로세스가 사라졌다.
사유와 고친 모양은 같은 문서에 있다. 재는 자리는 `helper/addin_com_vtable_test.go`.

**헬퍼는 다시 띄워 줄 것이 있을 때만 스스로 끝난다**(`wakesUpAgain`). Windows 는 COM 추가 기능이, 맥은 launchd 가 그 일을
한다. 둘 다 없으면(손으로 띄운 헬퍼, `-NoAutostart` 로 깐 판) 안 끝낸다 — 끝내면 다음에 Office 를 켤 때 빈 창이 뜬다.

### 맥·리눅스 — 이 요구를 못 맞춘다

요구가 둘이다(사용자, 2026-09-07): **상주 프로세스도 시작 프로그램 등록도 없을 것**, 그리고 **Office 를 켜는 것 말고
사람이 따로 켜거나 관리할 것이 없을 것**. Windows 는 COM 추가 기능이 그 둘을 다 만족한다 — Office 프로세스 안에서만 살고,
사람은 Office 만 켠다.

**맥에는 그 자리가 없다.** Office for Mac 은 COM 추가 기능을 안 받고(웹 애드인과 VBA 뿐), 웹 애드인의 페이지는 헬퍼가
내주므로 헬퍼보다 먼저 뜰 수 없다. 남는 길은 로그인 항목이나 launchd 주기 작업인데 둘 다 첫째 요구를 어기고, 사람이
`magi office` 를 켜는 것은 둘째를 어긴다. 그래서 **맥은 개발용이다** — 작업창을 붙여 보고 도구를 눌러 보는 자리이지,
쓰는 사람에게 내주는 자리가 아니다. 거기서는 헬퍼가 스스로 끝나지도 않는다(`wakesUpAgain`).

**리눅스에는 데스크톱 Office 가 없다.** 이 클라이언트 셋은 Office 애드인이라 깔 자리 자체가 없다. magi 자신(데몬·TUI·웹
콘솔)과 다른 클라이언트는 그대로 돈다.

## 문서 사이·프로그램 사이

**같은 프로그램의 문서 둘**(옆 덱의 서식을 이 덱에): 모든 도구가 `document` 인자를 받고 인자가 주소를 이긴다. 파워포인트의
`list_documents` 가 열린 덱의 키와 이름을 낸다. 그리고 **옮기는 것은 도구가 한다**(`matchstyle.go`) — 둘 다 `match_document` 에
옆 덱 키를 받는다: `apply_style` 이 그 덱의 `describe_style` 을 읽어 **서체·크기·색**을, `set_theme_colors{scope:"master"}` 가
`read_theme_colors` 를 읽어 **테마 색 열둘**을 가져온다. 헬퍼가 읽어 이 호출의 **빈 칸만** 채우므로 직접 준 값은 안 덮인다.
손은 덱마다 따로 붙어서 이 일을 못 한다 — 허브를 쥔 헬퍼만 할 수 있다.

**실측(2026-09-08, LTSC 2021 · COM 손 · 덱 둘).** 「옆 덱 서식 그대로, 내용만 다르게 3장」이 끝까지 돌았다. 그 전에는 모델이
옆 덱을 찾아 놓고도 서식은 안 읽고 **글꼴을 지어냈고**(원본은 맑은 고딕인데 `ea_font:"본고딕"`), 카운슬이 두 번 되돌려서야
맞췄다 — 카운슬을 끈 판이면 틀린 채로 착지한다. 인자를 만든 뒤 같은 부탁이 툴콜 16 → **8**, 카운슬 **첫 심의 통과**.
사유와 표는 [파워포인트 매뉴얼 §6.22](../powerpoint/docs/MANUAL.ko.md).

**프로그램 둘**(엑셀 표로 덱을): 컴패니언이 다르다 — 엑셀 대화에는 파워포인트 도구가 없다. 길은 `hand_off{to:"powerpoint"}`
다(컴패니언 이름은 워크스페이스 디렉토리 이름 `powerpoint`·`excel`·`word`). 넘어간 부탁은 파워포인트 컴패니언이 **옆 대화**
에서 돌리는데, 덱 몫의 도구 등록은 그 덱의 대화에만 보여서 옆 대화에는 도구가 없었다 — 그래서 헬퍼가 덱 등록 옆에
**컴패니언 전체 등록** 하나를 더 둔다(`serve.go` `settle`, 2026-09-07). 그 등록의 호출은 `document` 를 안 대면 허브의
「하나뿐이면 그것, 둘이면 이름을 대라」 규칙으로 가고, 이름은 `list_documents` 가 준다. 브리프에는 데이터를 글로 싣는다 —
받는 쪽은 보내는 쪽의 시트를 못 본다. **실물로는 아직 안 돌렸다.**

## 시험

```
go test ./clients/office/helper/
```

66 파일. 문서 대조(매뉴얼의 허용 규칙·도구 수·매니페스트 주소)는 세 프로그램을 돌며 잰다(`Apps`). 작업창 쪽 스모크는
각 애드인의 `tools/` 에 그대로 있고, 헬퍼 소스는 `../../../office/helper/<app>_tools.go` 로 읽는다.
