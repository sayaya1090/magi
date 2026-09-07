# magi-office-start — Office 를 켤 때 헬퍼를 띄우는 COM 추가 기능

하는 일은 하나입니다. **헬퍼(`magi office`)가 안 떠 있으면 띄웁니다.**

## 왜 있나

작업창(웹 애드인)의 페이지는 헬퍼가 내줍니다. 그래서 헬퍼가 먼저 떠 있지 않으면 리본의 Magi 를 눌러도 빈 창이 뜹니다.
여태는 그것을 로그인 등록(`HKCU\...\Run`)으로 풀었는데, Office 를 안 켜는 날에도 magi 가 떠 있다는 뜻이었습니다.
사용자가 그것을 물렸습니다(2026-09-07): 「윈도우 로그인 때 자동 켜지는 거 하지 말라고」, 「오피스에서 플러그인 켤 때
COM 이랑 .NET 으로 프로세스 못 띄우냐」.

COM 추가 기능은 **Office 프로세스 안에서** 뜹니다. 그래서 자리가 정확히 맞습니다. 로그인 등록으로 물러서는 길은
두지 않았습니다 — 사용자: 「쓰지도 않는데 켜져 있는 건 악성코드 아니냐」. 볼륨 판에 .NET 이 없으면 설치기가 멈추고,
M365 에 없으면 헬퍼를 사람이 띄웁니다.

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

## vtable — 이 파일이 한 번 Office 를 죽였다

`IDTExtensibility2` 는 **dual** 인터페이스입니다. vtable 이 **IUnknown(3) + IDispatch(4) + 메서드(5)** 예요.

첫 판은 이것을 `[InterfaceType(ComInterfaceType.InterfaceIsIDispatch)]` 로 선언했습니다. 그러면 CLR 은 **IDispatch
일곱 슬롯짜리 vtable 만** 만듭니다. Office 는 `OnConnection` 을 여덟째 슬롯에서 부르고 — **빈 자리로 뜁니다.**

실물(2026-09-07, LTSC 2021 16.0.14334): PowerPoint 를 켜면 20초쯤 뒤 창이 그냥 사라졌습니다.

```
Microsoft Office 16 : PowerPoint에서 'magi.office.start' 추가 기능을 사용할 경우 문제가 발생합니다…
.NET Runtime        : POWERPNT.EXE | CoreCLR 9.0.19 | terminated due to an unhandled exception
                      System.AccessViolationException: Attempted to read or write protected memory
Application Error   : POWERPNT.EXE  예외 코드: 0xc0000005
```

**「무슨 일이 있어도 안 던진다」는 관리 코드 안에서만 참입니다.** 여기는 관리 코드에 **닿기 전**이라 `try/catch` 로
못 막습니다 — 막는 자리는 선언 하나뿐입니다. 그래서 지금은 `InterfaceIsIUnknown` 으로 선언하고 **IDispatch 넷을
손으로 앞에 적습니다.** 인자는 전부 `IntPtr` 이고 `[PreserveSig]` 로 HRESULT 를 직접 답합니다 — 원래 시그니처의
`object`·`ref Array` 는 VARIANT/SAFEARRAY 마샬링을 타는데, 우리는 그 인자를 하나도 안 씁니다.

`IID_IDispatch` 로 물으면 `ICustomQueryInterface` 로 **같은 vtable** 을 내줍니다. dual 이라 앞머리가 IDispatch 이므로
그게 맞는 답이고, 이렇게 해야 늦은 바인딩 호출이 CLR 의 IDispatch 로 안 갑니다 — 그쪽은 타입 라이브러리를 요구하는데
(`Typelib export: Type library is not registered`) `EnableComHosting` 은 TLB 를 만들지도 등록하지도 않습니다.

무는 자리: `clients/office/helper/addin_com_vtable_test.go`. 소스를 글자로 읽습니다 — 이 결함은 Go 로도 C# 로도
못 잡습니다. 틀리면 **프로세스가 죽어서** 시험이 결과를 못 받기 때문입니다. 돌연변이 둘(선언 되돌리기·IDispatch
슬롯 빼기) 다 울립니다.

## 실측 (2026-09-07 · Office LTSC 2021 16.0.14334 · x64)

| 잰 것 | 결과 |
|---|---|
| 등록 뒤 PowerPoint 를 켠다 | ✓ 안 죽고, `start.log` 에 `[POWERPNT] 헬퍼를 띄웠습니다 …` 가 적힌다 |
| Office 가 `LoadBehavior` 를 2 로 내리는가 | ✗ 안 내린다 — 3 그대로 |
| 셋을 다 켠다(PowerPoint·Excel·Word) | ✓ 헬퍼는 **하나**. 뒤의 둘은 포트가 열린 것을 보고 아무것도 안 한다(로그에 줄이 안 는다) |
| Office 를 다 끈다 | ✓ **60초에 헬퍼·컴패니언·어댑터가 전부 스스로 끝난다** — 이 계정에 magi 가 하나도 안 남는다 |
| 다시 켠다 | ✓ 다시 뜬다(`start.log` 에 둘째 줄) |
| 로그인 등록 | ✓ 하나도 없다(`Run` 키에 magi 없음) |

이 표가 사용자 요구 둘을 그대로 잰 것입니다 — **상주 프로세스도 시작 프로그램 등록도 없을 것**, 그리고 **Office 를
켜는 것 말고 사람이 따로 켜거나 관리할 것이 없을 것**.

## 아직 안 잰 것

- **32비트 Office.** 이 머신은 x64 라 비트가 갈리는 자리를 안 밟았습니다.
- **Microsoft 365.** 이 머신은 볼륨 판 2021 입니다.
- **Mac.** Office for Mac 은 COM 추가 기능을 안 받습니다 — 거기서는 이 길 자체가 없습니다(`../README.md`).
