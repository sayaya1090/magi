# magi-office-start — Office 를 켤 때 헬퍼를 띄우는 COM 추가 기능

하는 일은 하나입니다. **헬퍼(`magi office`)가 안 떠 있으면 띄웁니다.**

## 왜 있나

작업창(웹 애드인)의 페이지는 헬퍼가 내줍니다. 그래서 헬퍼가 먼저 떠 있지 않으면 리본의 Magi 를 눌러도 빈 창이 뜹니다.
여태는 그것을 로그인 등록(`HKCU\...\Run`)으로 풀었는데, Office 를 안 켜는 날에도 magi 가 떠 있다는 뜻이었습니다.
사용자가 그것을 물렸습니다(2026-09-07): 「윈도우 로그인 때 자동 켜지는 거 하지 말라고」, 「오피스에서 플러그인 켤 때
COM 이랑 .NET 으로 프로세스 못 띄우냐」.

COM 추가 기능은 **Office 프로세스 안에서** 뜹니다. 그래서 자리가 정확히 맞습니다.

| 언제 | 무엇이 떠 있나 |
|---|---|
| Office 를 안 켰다 | 아무것도 없다 |
| Office 를 켰다 | 이 추가 기능이 헬퍼를 띄운다 |
| 작업창을 열었다 | 헬퍼가 그 프로그램 몫의 컴패니언을 마련한다 |
| PowerPoint 2021 이 떠 있다 | 헬퍼가 편집 어댑터(`magi-ppt-hand`)를 띄운다 |
| Office 를 다 껐다 | 컴패니언이 내려가고(60초), 어댑터가 끝나고, 헬퍼도 끝난다 |

## 규약

- 등록은 전부 이 계정 몫입니다(`HKCU`) — 관리자 권한이 필요 없습니다. 설치기가 씁니다:
  `Software\Classes\CLSID\{38162D7F-4C03-4B36-9F55-15D83EEA5EF3}\InprocServer32` → `magi-office-start.comhost.dll`,
  `Software\Classes\Magi.Office.Start\CLSID`, 그리고 프로그램마다
  `Software\Microsoft\Office\<PowerPoint|Excel|Word>\Addins\Magi.Office.Start` 의 `LoadBehavior = 3`.
- 헬퍼의 명령줄은 설치기가 DLL 옆 `helper-args.txt` 에 적어 둡니다 — 설정·소켓 자리는 머신마다 다르고, 그것을 아는 것은
  설치기입니다.
- 이미 떠 있으면(포트 3000 이 열려 있으면) 아무것도 안 합니다. 프로그램 셋을 한꺼번에 켜도 헬퍼는 하나입니다
  (`Local\magi-office-start` 뮤텍스).
- **무슨 일이 있어도 안 던집니다.** 추가 기능이 던지면 Office 가 `LoadBehavior` 를 2 로 내려 다음부터 아예 안 부릅니다.
  사유는 DLL 옆 `start.log` 에 적습니다.

## 비트 수

Office 프로세스 안에서 뜨므로 **Office 와 같은 비트**로 지어야 합니다. 설치기가 `ClickToRun\Configuration` 의
`Platform` 을 읽어 `-r win-x64` 또는 `-r win-x86` 으로 짓습니다.

## 빌드

쓰는 사람은 설치기로 합니다(`clients/office/install.ps1`). 개발할 때:

```
cd clients/office/addin-com/src
dotnet build -r win-x64 --self-contained false     # comhost.dll 은 Windows RID 로 지어야 나온다
```

`bin/…/magi-office-start.comhost.dll` 과 그 옆의 `.dll`·`.runtimeconfig.json`·`.deps.json` 이 한 벌입니다.

## 아직 안 잰 것

**실물 Office 에 등록해서 돌린 적이 없습니다**(2026-09-07 작성). 이 저장소에서 잰 것은 빌드가 되고 comhost 가 나오는
것까지입니다. 실물에서 볼 것: 등록 뒤 Office 를 켰을 때 헬퍼가 뜨는가, `start.log` 에 무엇이 적히는가, 32비트 Office
에서 비트가 갈리는가, Office 가 `LoadBehavior` 를 2 로 내리지 않는가.
