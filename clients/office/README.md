# magi office — 파워포인트·엑셀·워드 애드인의 헬퍼, 한 프로세스

[파워포인트 매뉴얼](../powerpoint/docs/MANUAL.ko.md) · [엑셀 매뉴얼](../excel/docs/MANUAL.ko.md) · [워드 매뉴얼](../word/docs/MANUAL.ko.md) · [클라이언트 문 계약](../../docs/CLIENTS.ko.md)

`magi office` 는 `magi` 바이너리의 하위 명령이다. 2026-09-06 까지 셋이던 헬퍼(`magi-ppt`·`magi-xl`·`magi-word`)를
한 벌로 모았다 — 사람이 신뢰 저장소에 넣는 인증서가 **하나**, 자동 시작이 하나, 볼륨 판 Excel 의 신뢰 카탈로그 키가 하나,
받을 파일이 하나(`magi`)가 되게.

```
go build -o magi ./cmd/magi
./magi office -cert-hint         # <config>/office-helper-cert.pem 을 신뢰 저장소에 — 한 번
./magi office                    # 127.0.0.1:3000 — /ppt·/xl·/word 세 판
./magi office -allow-rules=xl    # 그 프로그램의 읽기 도구 허용 규칙(config.toml 에 붙여 넣는다)
```

볼륨 판(LTSC 2021) PowerPoint 는 작업창으로 편집이 안 돼 COM 손(`magi-ppt-hand`)이 편집한다. `clients/office/install.ps1`
은 볼륨 판이면 그 어댑터도 짓는다(.NET SDK 필요). 어댑터를 띄우는 것은 헬퍼다(`helper/adapter.go`) — 로그인 때 뜨는 등록은 헬퍼 하나뿐이다(2026-09-07 — 그 전엔
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

.NET SDK 가 없어 추가 기능을 못 지으면 설치기가 **로그인 등록으로 물러선다**(`Run\magi-office`) — 안 그러면 리본의 Magi 가
빈 창을 띄운다. `-NoAutostart` 는 둘 다 안 한다.

## 문서 사이·프로그램 사이

**같은 프로그램의 문서 둘**(옆 덱의 서식을 이 덱에): 모든 도구가 `document` 인자를 받고 인자가 주소를 이긴다. 파워포인트의
`list_documents` 가 열린 덱의 키와 이름을 내므로, 옆 덱을 `describe_style` 로 읽고 이 덱에 `format_shape` 로 옮기면 된다.

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
