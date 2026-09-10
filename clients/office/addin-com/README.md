# magi-office-start — Office 실행 연동 COM 추가 기능

Office 애플리케이션 시작 시 헬퍼 프로세스(`magi office`)의 실행 여부를 감지하고, 미실행 상태일 경우 이를 자동으로 기동하는 인프로세스 COM 추가 기능(In-process COM Add-in)입니다.

## 1. 도입 배경 및 목적

Office 작업창(웹 애드인)의 HTML 페이지는 로컬 헬퍼 프로세스가 제공합니다. 따라서 헬퍼가 미리 실행되어 있지 않으면 리본 메뉴에서 Magi 버튼을 클릭하더라도 빈 작업창이 표시됩니다.
초기 버전에서는 이를 Windows 사용자 로그인 시 자동 시작(`HKCU\Software\Microsoft\Windows\CurrentVersion\Run`) 방식으로 처리하였으나, Office를 사용하지 않는 동안에도 백그라운드 프로세스가 상주하는 문제가 제기되었습니다.
이에 따라 2026-09-07 설계를 전면 개편하여, 시스템 로그인 등록 방식을 완전히 배제하고 Office 프로세스가 시작될 때 인프로세스 COM 추가 기능을 통해 헬퍼를 온디맨드로 구동하도록 구성하였습니다.
COM 추가 기능은 Office 프로세스 내부에서 직접 로드되므로 호스트 애플리케이션의 실행 수명 주기에 완전히 종속됩니다. 볼륨 라이선스 환경에서 .NET 런타임이 감지되지 않으면 설치기가 자동으로 런타임을 설치하며, Microsoft 365 환경에서 미설치 상태인 경우 사용자가 헬퍼를 직접 수동 기동하는 방식으로 대응합니다.

| 시점 / 상태 | 프로세스 상태 |
|---|---|
| Office 미실행 | 관련 프로세스 전무 (백그라운드 상주 없음) |
| Office 실행 | 본 추가 기능이 로드되며 헬퍼 프로세스 기동 |
| 작업창 개방 | 헬퍼가 해당 프로그램 전용 컴패니언 인스턴스 준비 |
| PowerPoint 2021 활성화 | 헬퍼가 슬라이드 편집 어댑터(`magi-ppt-hand`) 구동 |
| Office 전체 종료 | 컴패니언(60초 유휴 후) 및 어댑터 자동 종료, 헬퍼 프로세스 최종 종료 |

## 2. 연동 규약

- **레지스트리 등록**: 모든 등록은 현재 사용자 영역(`HKCU`)에 기록되므로 관리자(Administrator) 권한이 필요하지 않습니다. 설치 스크립트(`install.ps1`)가 다음 키를 생성합니다:
  - `Software\Classes\CLSID\{38162D7F-4C03-4B36-9F55-15D83EEA5EF3}\InprocServer32` → `magi-office-start.comhost.dll`
  - `Software\Classes\Magi.Office.Start\CLSID`
  - 호스트별 추가 기능 등록: `Software\Microsoft\Office\<PowerPoint|Excel|Word>\Addins\Magi.Office.Start` (`LoadBehavior = 3`)
- **실행 인자 전달**: 헬퍼의 기동 명령줄 인자는 설치 환경에 따라 머신별로 상이하므로, 설치 스크립트가 DLL과 동일한 디렉토리에 `helper-args.txt` 파일로 기록합니다.
- **중복 기동 방지**: 포트 26411이 이미 수신 대기 중인 경우 추가 동작 없이 즉시 반환합니다. 3개 프로그램(PowerPoint·Excel·Word)을 동시에 실행하더라도 네임드 뮤텍스(`Local\magi-office-start`)를 통해 헬퍼 프로세스는 단일 인스턴스만 유지됩니다.
- **예외 차단 원칙**: 추가 기능 내부에서 처리되지 않은 예외가 발생하면 Office 호스트는 안정성을 위해 `LoadBehavior`를 2(비활성화)로 강등하여 차후 추가 기능을 호출하지 않습니다. 따라서 모든 예외는 관리 코드 내부에서 철저히 격리되며, 세부 진단 로그는 DLL 인접 경로의 `start.log`에 기록됩니다.

## 3. 비트 수 및 빌드

COM 추가 기능은 Office 프로세스 내부에서 직접 로드되므로 **Office 프로세스의 바이너리 아키텍처와 동일한 비트(x64 / x86)**로 빌드되어야 합니다. 설치 스크립트는 `ClickToRun\Configuration`의 `Platform` 값을 감지하여 `-r win-x64` 또는 `-r win-x86` 대상 런타임 식별자(RID)를 결정합니다.

일반 사용자는 통합 설치 스크립트(`clients/office/install.ps1`)를 통해 사전 빌드된 바이너리를 설치합니다. 개발 환경에서 직접 빌드할 경우 다음 명령을 수행합니다:

```sh
cd clients/office/addin-com/src
dotnet build -r win-x64 --self-contained false     # comhost.dll 생성을 위해 Windows RID 명시 필요
```

빌드 완료 시 `bin/…/magi-office-start.comhost.dll`, `magi-office-start.dll`, `.runtimeconfig.json`, `.deps.json` 파일이 단일 세트로 산출됩니다.

## 4. IDTExtensibility2 vtable 정렬 및 메모리 보호 위반 결함 분석

`IDTExtensibility2`는 표준 Dual 인터페이스입니다. vtable 구조는 다음과 같이 12개 슬롯으로 구성됩니다:
- `IUnknown` (3개 슬롯: `QueryInterface`, `AddRef`, `Release`)
- `IDispatch` (4개 슬롯: `GetTypeInfoCount`, `GetTypeInfo`, `GetIDsOfNames`, `Invoke`)
- `IDTExtensibility2` 고유 메서드 (5개 슬롯: `OnConnection`, `OnDisconnection`, `OnAddInsUpdate`, `OnStartupComplete`, `OnBeginShutdown`)

초기 구현에서는 이 인터페이스를 `[InterfaceType(ComInterfaceType.InterfaceIsIDispatch)]`로 선언하였습니다. 이 경우 .NET CLR은 `IDispatch` 7개 슬롯으로만 구성된 축약 vtable을 생성합니다. 그러나 Office 호스트는 Dual 인터페이스 규약에 따라 8번째 슬롯에서 `OnConnection`을 직접 호출하므로, 잘못된 메모리 주소로 분기하여 프로세스 충돌(Crash)이 발생하였습니다.

2026-09-07 LTSC 2021 (16.0.14334) 환경 실측 결과, PowerPoint 실행 후 약 20초 시점에 `AccessViolationException`(`0xc0000005`, 보호된 메모리 읽기/쓰기 시도)이 발생하며 애플리케이션이 비정상 종료되었습니다:

```
Microsoft Office 16 : PowerPoint에서 'magi.office.start' 추가 기능을 사용할 경우 문제가 발생합니다…
.NET Runtime        : POWERPNT.EXE | CoreCLR 9.0.19 | terminated due to an unhandled exception
                      System.AccessViolationException: Attempted to read or write protected memory
Application Error   : POWERPNT.EXE  예외 코드: 0xc0000005
```

이 오류는 관리 코드 진입 전 언매니지드 COM vtable 디스패치 단계에서 발생하므로 C#의 `try/catch` 블록으로 차단할 수 없으며, 선언부의 vtable 배치를 올바르게 구성해야만 해결할 수 있습니다.
현재 구현에서는 인터페이스를 `InterfaceIsIUnknown`으로 선언하고, vtable 전면에 4개의 `IDispatch` 슬롯을 명시적으로 배치하였습니다. 모든 매개변수는 `IntPtr`로 선언하고 `[PreserveSig]`를 적용하여 HRESULT를 직접 반환하도록 구성하였습니다(기존 시그니처의 `object`, `ref Array`에 수반되는 불필요한 VARIANT/SAFEARRAY 마샬링 배제).

또한 호스트가 `IID_IDispatch`로 질의할 경우 `ICustomQueryInterface`를 구현하여 동일한 Dual vtable 포인터를 직접 반환하도록 처리하였습니다. 이를 통해 런타임 타입 라이브러리(TLB) 등록을 요구하는 CLR 기본 IDispatch 마샬러(`Typelib export: Type library is not registered`)로 분기하는 결함을 방지하였습니다(`.NET Core`의 `EnableComHosting` 메커니즘은 TLB를 생성하거나 등록하지 않음).

회귀 방지 검증은 `clients/office/helper/addin_com_vtable_test.go`에서 소스 코드 정적 분석을 통해 수행합니다. vtable 선언 복원 또는 IDispatch 슬롯 누락 변형 시 테스트가 즉각 실패하도록 불변식을 강제합니다.

## 5. 실측 검증 (2026-09-07 · Office LTSC 2021 16.0.14334 · x64)

| 검증 항목 | 실측 결과 |
|---|---|
| 등록 후 PowerPoint 실행 | ✓ 정상 기동 확인 (`start.log`에 `[POWERPNT] 헬퍼를 띄웠습니다 …` 기록) |
| Office 호스트의 `LoadBehavior` 강등 여부 | ✗ 강등 없음 (`LoadBehavior = 3` 유지) |
| 다중 프로그램 동시 실행 (PowerPoint·Excel·Word) | ✓ 헬퍼 프로세스 단일 인스턴스 유지 확인. 후속 프로그램은 포트 활성화를 감지하고 중복 기동 생략 |
| Office 프로그램 전체 종료 | ✓ **60초 이내에 헬퍼·컴패니언·어댑터가 모두 자체 종료**되어 계정 내 잔여 프로세스 0건 확인 |
| 재실행 | ✓ 정상 재기동 확인 (`start.log`에 2번째 라인 기록) |
| 로그인 시 자동 시작 등록 | ✓ 전무 (`HKCU\...\Run` 키에 magi 관련 항목 없음) |

위 실측을 통해 사용자 요구사항(상주 프로세스 및 시작 프로그램 등록 배제, 사용자 수동 개입 없는 호스트 중심 수명 주기 연동)이 완전히 충족됨을 입증하였습니다.

## 6. 미검증 환경

- **32비트 Office 환경**: x64 환경 실측 완료 상태이며 32비트 환경 분기는 추가 검증이 필요합니다.
- **Microsoft 365**: 볼륨 라이선스(Office LTSC 2021) 환경 위주로 측정되었으며 M365 환경에 대한 추가 검증이 필요합니다.
- **macOS 환경**: Office for Mac은 인프로세스 COM 추가 기능을 지원하지 않으므로 본 방식의 적용이 불가합니다([`../README.md`](../README.md) 참조).
