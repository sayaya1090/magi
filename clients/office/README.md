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

## 자리

| 자리 | 파워포인트 | 엑셀 | 워드 |
|---|---|---|---|
| 작업창 | `/ppt/taskpane.html` | `/xl/taskpane.html` | `/word/taskpane.html` |
| MCP 서버 | `/ppt/mcp` (`ppt`, 도구 48) | `/xl/mcp` (`xl`, 76) | `/word/mcp` (`word`, 57) |
| 손 스트림 | `/ppt/hand/stream?presentation=` | `/xl/hand/stream?workbook=` | `/word/hand/stream?doc=` |
| 문서 키 | `pid-…` | `wb-…` | `wd-…` |
| 컴패니언 워크스페이스 | `<config>/powerpoint` | `<config>/excel` | `<config>/word` |
| 도구 표·열거형·지침·스킬 | `ppt_*.go`, `skills/powerpoint` | `xl_*.go`, `skills/excel` | `word_*.go`, `skills/word` |

셋이 나누는 것은 인증서·토큰·포트뿐이다. 손 허브·MCP 서버·API·컴패니언은 프로그램마다 따로 선다(`serve.go` 의 `mount`).
다른 점은 전부 `app.go` 의 `App` 값 하나에 있다 — 새 프로그램을 더하면 `App` 하나와 `*_tools.go`·`*_enums.go`·
`*_instructions.go`·스킬 디렉토리를 더한다.

## 시험

```
go test ./clients/office/helper/
```

66 파일. 문서 대조(매뉴얼의 허용 규칙·도구 수·매니페스트 주소)는 세 프로그램을 돌며 잰다(`Apps`). 작업창 쪽 스모크는
각 애드인의 `tools/` 에 그대로 있고, 헬퍼 소스는 `../../../office/helper/<app>_tools.go` 로 읽는다.
