# magi-ppt — PowerPoint 애드인의 헬퍼

애드인과 magi 데몬 사이에 서는 프로세스. **사용자당 하나**다(`DESIGN.md` §5.2).

얼굴이 셋이다.

| 쪽 | 무엇 | 왜 |
|---|---|---|
| magi 데몬 | **MCP 서버**(Streamable HTTP) | 데몬은 워크스페이스당 하나라 여럿인데 애드인은 하나다. stdio 면 데몬마다 헬퍼를 띄워 **같은 애드인 하나를 두고 싸운다**(§4.5) |
| 애드인 | **https 서버**(페이지 + 손) | Office 는 애드인 소스를 https 로만 받는다. 헬퍼가 그 페이지를 직접 내주면 **토큰은 페이지에 박혀 나가고 주소는 자기 오리진**이라, 「어떻게 도달하는가」가 통째로 사라진다(§5.5·§12 #7) |
| 애드인 ← 데몬 | 같은 연결의 **반대 방향** | 대화(`transcript`)를 애드인으로 흘린다. 새 포트도 새 연결도 없다(§5.7) |

## 돌려 보기

```sh
go build -o magi-ppt ./clients/powerpoint/helper
./magi-ppt                       # 기본값: 127.0.0.1:3000, 애드인은 clients/powerpoint/mockup
./magi-ppt -allow-rules          # config.toml 에 붙여 넣을 허용 규칙을 찍는다(§6)
./magi-ppt -cert-hint            # 인증서를 신뢰 저장소에 넣는 법
```

첫 기동이 인증서를 만든다(`<config>/ppt-helper-cert.pem`, 키는 `0600`). **그 인증서를 이 계정의
신뢰 저장소에 넣는 것은 사람이 한다** — 남의 신뢰 저장소를 프로그램이 고치는 것은 가볍게 볼 일이
아니라서, 헬퍼는 명령만 알려 준다(§5.5).

## 지키는 규칙 — 그리고 그것을 무는 시험

`go test ./clients/powerpoint/helper/`. 재미없는 이름이 하나도 없다: 시험 이름이 곧 규칙이다.

| 규칙 | 어디서 왔나 | 무는 시험 |
|---|---|---|
| 이름 넷이 같은 문자열(매니페스트·바인드·SAN·전송) | §5.5 ⚠ | `TestTheFourNamesAreOneString` — 처음 돌렸을 때 매니페스트의 여덟 자리를 짚었다 |
| 애드인은 오리진을 **적지 않는다** | §5.5 | `TestTheAddinDoesNotWriteTheOriginDown` |
| 스키마가 인자 검사를 켜 둔다(`properties`·`required`) | §4.3 | `TestEverySchemaKeepsTheArgumentCheckOn` |
| 모르는 키는 **서버가 거절한다** | §4.3 | `TestAnUndeclaredArgumentIsRefused` |
| 허용 규칙은 「덱을 고치는가」로 갈린다 | §6 | `TestAllowRulesCoverExactlyWhatDoesNotChangeTheDeck` |
| 알림에는 202 | §4.5 | `TestANotificationIsAcceptedWithTwoOhTwo` |
| 손이 없으면 **실패하고 사유를 댄다** | §5.4 | `TestWithNoAddinAttachedToolsFailAndSayWhy` |
| 붙기 전에 언제나 detach | §5.4 | `TestAttachingAlwaysDetachesFirst` |
| 둘째 창이 첫째의 등록을 안 뺏는다 | §5.0.1 | `TestASecondWindowDoesNotStealTheFirstsRegistration` |
| 「못 물어봤다」 ≠ 「못 받는다」 | §5.0.5 | `TestNotAskedIsNotTheSameAsCannot` |
| 잡힌 포트는 **멈춤이지 우회가 아니다** | §5.5.1 | `TestATakenPortIsAStopNotADetour` |
| 남의 리스너는 **지우지도 붙지도 않는다** | §5.3 ⚠ | `TestAStrangerOnThePortIsRefusedNotAdopted` |
| 연결이 둘이다(전사 ≠ 요청) | §5.7 ⚠ | `TestStatusAnswersWhileTheTranscriptIsStreaming` |
| 커서를 미는 것은 `seq > 0` 뿐 | §5.7 | `TestOnlyASeatedEventMovesTheCursor` |

그리고 **magi 자신의 MCP 클라이언트가 이 서버에 붙는다**(`TestMagisOwnClientAttachesToThisHelper`).
가짜 상대로 도는 시험은 내가 상대를 어떻게 이해했는지를 검사하지 상대를 검사하지 않는다 —
§4.5 가 그 표본을 하나 들고 있다(스펙이 MUST 로 적은 202 를 magi 가 유일하게 거절하던 시절,
스펙대로 만든 서버만 쫓겨났다).

## 아직 아닌 것

- **PowerPoint 에 한 번도 안 붙여 봤다.** 이 저장소를 만든 머신에 PowerPoint 가 없다.
  S1·S13·S14 는 열려 있는 채고, 붙는 날 그 자리에서 답이 난다(§9).
- **`-race` 를 못 돌렸다.** 이 머신에 C 툴체인이 없어 `CGO_ENABLED=1` 이 안 선다.
- 감독자(LaunchAgent · 예약 작업)는 안 만들었다. §5.4 의 「죽으면 다시」는 사람이 다시 띄우는
  것으로 서 있고, 그 대신 **두 번째 기동이 조용히 물러난다**(같은 인증서를 내미는 리스너를
  우리 것으로 읽는다).
- 워크스페이스가 없을 때 **덱의 디렉토리에 데몬을 띄우는 것**(§5.0)은 아직 사람이 한다. 고르는
  화면이 그렇게 말한다.
