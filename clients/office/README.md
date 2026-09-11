# magi office — 파워포인트·엑셀·워드 애드인의 헬퍼, 단일 통합 프로세스

[↑ 저장소](../../README.md) · [파워포인트 매뉴얼](../powerpoint/docs/MANUAL.ko.md) · [엑셀 매뉴얼](../excel/docs/MANUAL.ko.md) · [워드 매뉴얼](../word/docs/MANUAL.ko.md) · [클라이언트 연동 계약](../../docs/CLIENTS.ko.md)

`magi office`는 `magi` 바이너리의 하위 명령입니다. 2026-09-06 이전까지 개별 프로세스로 분리되어 있던 세 헬퍼(`magi-ppt`·`magi-xl`·`magi-word`)를 단일 프로세스로 통합하였습니다. 이를 통해 사용자 시스템의 신뢰 저장소에 등록되는 인증서, 자동 시작 구성, 볼륨 라이선스 판 Excel의 신뢰할 수 있는 카탈로그 레지스트리 키, 배포 바이너리(`magi`)를 각각 단 하나로 일원화하였습니다.

## 1. 설치 방법

```powershell
irm https://raw.githubusercontent.com/sayaya1090/magi/main/clients/office/install.ps1 | iex
```

저장소 클론이나 별도 개발 툴체인 없이 단일 명령줄로 설치할 수 있습니다. 스크립트 파일을 로컬에 내려받아 실행할 수도 있으며, 이 경우 다음 매개변수를 지원합니다(`irm … | iex` 파이프 실행 시에는 인자 전달이 불가하여 기본 설치만 수행됩니다).

```powershell
.\install.ps1                 # 기본 설치
.\install.ps1 -Uninstall      # 설치 제거
.\install.ps1 -Clean          # 레지스트리 등록 및 Office 캐시 초기화 후 재설치
```

⚠ **로컬 스크립트 실행 시 보안 정책 및 인터넷 다운로드 플래그 해제 필요:**
Windows 클라이언트의 기본 PowerShell 실행 정책(`Restricted`)과 인터넷에서 다운로드된 파일에 부여되는 Zone.Identifier(Mark of the Web)로 인해 실행이 차단될 수 있습니다. 2026-09-08 기준 클린 LTSC 2021 환경 실측 결과, `.\install.ps1` 직접 실행 시 `PSSecurityException`(`이 시스템에서 스크립트를 실행할 수 없으므로 … 로드할 수 없습니다`)이 발생하며 즉시 중단되었습니다.
전역 시스템 정책을 변경하지 않고 현재 실행에 한하여 정책을 우회하려면 다음과 같이 차단을 해제하고 실행합니다:

```powershell
Unblock-File .\install.ps1
powershell -NoProfile -ExecutionPolicy Bypass -File .\install.ps1
```

원격 파이프(`irm … | iex`) 방식은 파일 시스템에 스크립트를 저장하지 않으므로 위 두 제약에 걸리지 않아 기본 권장 경로로 채택되었습니다. 로컬 파일 실행은 설치 플래그 지정이 필요한 경우에 사용하며, 이때는 위 두 명령을 함께 실행해야 합니다.

**빌드 툴체인 독립성:**
설치 스크립트는 컴파일 환경 없이 GitHub 릴리스 자산을 직접 다운로드합니다. 코어 바이너리는 `v*` 레인에서, Office 관련 자산 2종은 `office-v*` 레인에서 가져옵니다. 최신 버전 판별은 `badges` 브랜치에 유지되는 단일 행 파일(`core-latest.txt`·`office-latest.txt`)을 참조합니다. GitHub API의 `/releases/latest`를 직접 참조하지 않는 이유는 저장소 내 4개 배포 레인이 혼재되어 있어 콘솔 릴리스가 더 최신인 경우 Office 자산이 404를 반환하거나 불일치하는 `checksums.txt`를 수신하는 오류를 방지하기 위함입니다.

COM 추가 기능(`addin-com`)은 COM 호스팅 구조상 자체 포함(Self-contained) 배포가 지원되지 않아 .NET 데스크톱 런타임이 필수이며, 미설치 시 설치 스크립트가 런타임을 자동으로 내려받아 설치합니다. 어댑터 바이너리는 자체 포함 형태로 빌드되므로 별도 런타임에 의존하지 않습니다.

⚠ **32비트 Office 환경 제약:**
COM 추가 기능은 호스트 Office 프로세스 내부에서 인프로세스(In-process)로 로드되므로 Office 아키텍처와 비트 수가 일치해야 합니다. 공식 릴리스는 x64 자산만 제공하므로, 32비트 Office 환경에서는 설치기가 사유를 안내하고 중단됩니다. 32비트 환경에서는 아래의 `-FromSource` 플래그를 사용하여 로컬에서 직접 빌드해야 합니다.

### 1.1 소스 코드 직접 빌드

```powershell
.\install.ps1 -FromSource     # Go + .NET 9 SDK 필요
```

소스 코드 수정 사항을 즉시 반영하여 검증할 때 사용합니다.

### 1.2 헬퍼 수동 실행

```sh
go build -o magi ./cmd/magi
./magi office -cert-hint         # <config>/office-helper-cert.pem 을 신뢰 저장소에 등록 (최초 1회)
./magi office                    # 127.0.0.1:26411 — /ppt·/xl·/word 3종 엔드포인트 제공
./magi office -allow-rules=xl    # 해당 프로그램의 읽기 도구 허용 규칙 출력 (config.toml 반영용)
```

볼륨 라이선스(Office LTSC 2021) 환경의 PowerPoint는 웹 작업창(Office.js)을 통한 슬라이드 편집 API를 지원하지 않으므로 COM 어댑터(`magi-ppt-hand`)가 직접 편집을 수행합니다. 설치 환경이 볼륨 라이선스일 경우 설치기가 해당 어댑터를 함께 내려받으며, 어댑터의 구동 수명 주기는 헬퍼 프로세스(`helper/adapter.go`)가 관리합니다. 2026-09-07 이전에는 개별 파워포인트 설치기에 위임되어 통합 설치기만 실행한 2021 환경에서 어댑터 부재 오류가 발생했으나, 현재는 헬퍼가 어댑터를 자동 기동하도록 개선되었습니다.

Windows 환경에서 세 Office 컴패니언은 일반 magi 데몬과 동일한 설정 트리(`%APPDATA%\magi` 내 `config.toml`, `plugins`)를 공유합니다. 반면 Unix 도메인 소켓 및 컴패니언 명단 파일은 `~/.magi` 디렉토리에 배치되며, 설치기가 사용자 환경 변수 `MAGI_SOCKET_DIR`로 해당 경로를 등록합니다. 이러한 분리가 필요한 기술적 사유는 두 가지입니다:
1. Windows의 AF_UNIX 소켓 바인딩 경로는 최대 108바이트로 제한되므로, 긴 사용자명을 가진 시스템에서 `%APPDATA%\magi` 하위 경로는 버퍼 오버플로를 유발합니다.
2. 신뢰할 수 있는 카탈로그(Trusted Catalog)는 네트워크 공유로 노출 가능한 디렉토리여야 합니다.

2026-09-07 이전에는 설정 디렉토리 자체를 `~/.magi`로 고정하여 플러그인 로드 후 백엔드 설정이 누락되거나 API 키 미인식 오류가 발생하였으며, 현재는 설정 경로를 유지한 채 소켓 경로만 분리(`daemon.SocketDir`)하여 문제를 해결하였습니다.

## 2. 엔드포인트 및 워크스페이스 배치

| 구분 | 파워포인트 | 엑셀 | 워드 |
|---|---|---|---|
| 작업창 HTML | `/ppt/taskpane.html` | `/xl/taskpane.html` | `/word/taskpane.html` |
| MCP 서버 | `/ppt/mcp` (`ppt`, 도구 49개 — 조작 도구 48개 + `list_documents`) | `/xl/mcp` (`xl`, 도구 76개) | `/word/mcp` (`word`, 도구 66개) |
| 조작 스트림 (SSE) | `/ppt/hand/stream?presentation=` | `/xl/hand/stream?workbook=` | `/word/hand/stream?doc=` |
| 문서 식별 키 | `pid-…` | `wb-…` | `wd-…` |
| 컴패니언 워크스페이스 | `<config>/powerpoint` | `<config>/excel` | `<config>/word` |
| 도구 명세·열거형·지침·스킬 | `ppt_*.go`, `skills/powerpoint` | `xl_*.go`, `skills/excel` | `word_*.go`, `skills/word` |

세 프로그램은 TLS 인증서, 세션 토큰, HTTP 포트(26411)만을 공유합니다. 조작 허브, MCP 서버, REST API, 컴패니언 인스턴스는 각 프로그램별로 완전히 격리되어 마운트됩니다(`serve.go`의 `mount`). 프로그램 간 차이점은 `app.go`의 `App` 구조체 정의에 집약되어 있으며, 신규 오피스 호스트 확장 시 `App` 정의 1건과 `*_tools.go`, `*_enums.go`, `*_instructions.go`, 스킬 디렉토리를 추가하는 구조로 설계되었습니다.

## 3. 프로세스 수명 주기 연동 (Office 실행 시 기동, 종료 시 자동 정리)

시스템 로그인 시 불필요한 백그라운드 프로세스 상주를 배제하고, 사용자가 Office 프로그램을 실행 중일 때만 헬퍼 및 컴패니언 데몬이 기동되며 활성 인스턴스가 모두 종료되면 함께 종료되도록 수명 주기를 일치시켰습니다(2026-09-07 설계 반영).

| 시점 | 동작 상세 |
|---|---|
| Office 애플리케이션 기동 | COM 추가 기능(`addin-com`)이 Office 프로세스 내부에서 로드되며 헬퍼를 기동합니다 (로그인 시 자동 시작 등록 불필요). |
| 작업창(Taskpane) 개방 | 헬퍼가 해당 오피스 호스트 전용 컴패니언 데몬(`magi --daemon`)을 구동합니다. |
| PowerPoint 2021 활성화 | 헬퍼가 COM 편집 어댑터(`magi-ppt-hand`)를 기동합니다(`helper/adapter.go`). |
| 특정 Office 프로그램 종료 | 해당 컴패니언 인스턴스가 60초 유휴(idle) 감시 후 자동으로 종료됩니다(`helper/idle.go`). |
| 모든 Office 프로그램 종료 | 어댑터가 자체 종료되며, 헬퍼 역시 60초 유휴 감시 후 종료됩니다(`officeWatch`). |

프로세스 감시는 OS 표준 도구(Windows `tasklist`, macOS `pgrep`)를 통해 수행됩니다. 프로세스 상태를 조회할 수 없는 운영체제에서는 오판으로 인한 서비스 중단을 방지하기 위해 프로세스를 강제 종료하지 않습니다. 개발 환경에서 Office 없이 헬퍼를 상시 유지하려면 `-keep-running` 옵션을 사용합니다.

**실측 검증 (2026-09-07, LTSC 2021 16.0.14334 · x64):**
위 수명 주기 흐름을 실측 검증하였습니다. PowerPoint 실행 시 COM 추가 기능이 헬퍼를 정상 실행(`start.log`)하였으며, 3개 프로그램을 동시 실행해도 헬퍼 프로세스는 단일 인스턴스로 유지되었습니다. 프로그램을 모두 종료한 후 60초 이내에 헬퍼, 컴패니언, 어댑터가 모두 자체 종료되어 계정 내 magi 잔류 프로세스가 0건임을 확인하였습니다. 상세 측정 지표는 [`addin-com/README.md`](addin-com/README.md) §실측에 기술되어 있습니다.

**배포본 클린 설치 검증 (2026-09-08, `office-v0.0.4`):**
저장소 및 툴체인이 없는 클린 환경에서 설치 스크립트만으로 자산 4종 수신, 컴패니언 설정 3종 구성, 인증서 설치, 카탈로그 등록, COM 추가 기능 등록까지 오류 0건으로 완료되었습니다. 모든 프로세스를 종료한 뒤 PowerPoint만 단독 실행하였을 때 정상 기동을 확인하였습니다:

```
종료 후 상태: magi = 0  →  PowerPoint 기동 후: ppt = 1, magi = 4
start.log: [POWERPNT] 헬퍼를 띄웠습니다: …\magi.exe office -config-dir … -socket-dir …
```

POWERPNT 프로세스가 시작 로그를 직접 기록하였으며, 대화 완료 시 카운슬 판정(⚖) 없이 정상 완료되어 승인 모드가 비활성화된 상태로 정상 배포되었음을 확인하였습니다.

설정 분리 검증 역시 동일 환경에서 확인되었습니다. 전역 `config.toml`은 활성 행이 없는 기본 주석 템플릿 상태를 유지하며, 개별 오피스 컴패니언 워크스페이스에만 전용 설정이 적용됩니다:

```toml
# <config>\{powerpoint,excel,word}\.magi\config.toml
permission = "allow"
[council]
enabled = false
```

이를 통해 Office 확장을 설치하더라도 사용자의 일반 터미널 magi, 웹 콘솔 세션, 예약 작업 등 다른 환경에는 무단으로 설정이 전파되지 않음을 확인하였습니다.

**COM 추가 기능 vtable 정렬 결함 수정 이력:**
초기 버전에서 `IDTExtensibility2`를 `InterfaceIsIDispatch`로 선언하여 vtable 오프셋이 4슬롯 어긋났고, Office 호스트가 `OnConnection`을 호출하는 순간 `AccessViolationException`으로 프로세스가 비정상 종료되는 결함이 있었습니다. 상세 원인과 해결책은 [`addin-com/README.md`](addin-com/README.md)에 기술되어 있으며, 회귀 방지 검증은 `helper/addin_com_vtable_test.go`에서 소스 코드 정적 분석으로 수행합니다.

**헬퍼 자체 종료 조건:**
헬퍼는 외부에서 자신을 다시 기동해 줄 메커니즘이 보장될 때만 스스로 종료합니다(`wakesUpAgain`). Windows는 COM 추가 기능이, macOS는 launchd가 해당 역할을 수행합니다. 수동 실행 환경이나 `-NoAutostart`로 설치된 환경에서는 재기동 메커니즘이 없으므로 자동 종료되지 않습니다.

### 3.1 플랫폼별 지원 현황 및 제약

Windows 환경은 인프로세스 COM 추가 기능을 통해 "상주 프로세스 및 시작 프로그램 등록 배제"와 "호스트 실행 외 사용자 수동 개입 배제"라는 두 가지 요구사항을 충족합니다.

- **macOS 환경 제약:**
  Office for Mac은 COM 추가 기능을 지원하지 않으며(웹 추가 기능 및 VBA만 지원), 웹 추가 기능 HTML 페이지는 헬퍼가 제공하므로 헬퍼보다 먼저 로드될 수 없습니다. 대안인 로그인 항목 등록이나 launchd 데몬 등록은 무상주 원칙에 위배되며, 사용자가 수동으로 `magi office`를 실행하는 것은 자동화 요구사항에 위배됩니다. 따라서 macOS는 작업창 렌더링 및 도구 인터페이스 검증을 위한 개발 환경으로 분류하며, 일반 사용자 배포 환경으로는 권장하지 않습니다. macOS 환경에서는 헬퍼가 유휴 시 자동 종료되지 않습니다(`wakesUpAgain`).
- **Linux 환경:**
  데스크톱 Office 제품군이 존재하지 않으므로 본 확장 클라이언트는 설치를 지원하지 않습니다. 코어 데몬, TUI, 웹 콘솔 등 일반 magi 환경은 정상 동작합니다.

## 4. 문서 간 및 프로그램 간 연동

**동일 프로그램 내 다중 문서 연동 (서식 복제 등):**
모든 도구는 `document` 매개변수를 지원하며, 명시적으로 전달된 인자가 URL 쿼리의 기본 문서를 우선합니다. PowerPoint의 `list_documents`를 통해 열려 있는 덱 목록과 키를 조회할 수 있습니다. 서식 복제는 `matchstyle.go`를 통해 도구 레벨에서 처리됩니다. `apply_style` 및 `set_theme_colors{scope:"master"}` 도구에 `match_document` 인자로 참조 덱 키를 넘기면, 헬퍼가 대상 문서의 `describe_style`에서 서체·크기·색상을, `read_theme_colors`에서 12종 테마 색상을 읽어와 호출 요청의 빈 필드만 자동으로 보충합니다. 사용자가 직접 지정한 값은 덮어쓰지 않습니다. 개별 편집 세션(손)은 특정 문서에 종속되므로 이 작업이 불가능하며, 다중 문서를 중계하는 헬퍼 허브를 통해서만 처리가 가능합니다.

- **실측 검증 (2026-09-08, LTSC 2021 COM 손, 2개 덱 동시 실행):**
  "참조 덱 서식을 유지하며 3개 슬라이드 생성" 작업을 검증하였습니다. 도구 수준 보충 기능 도입 전에는 모델이 참조 덱의 실제 스타일을 조회하지 않고 글꼴을 임의 생성(`ea_font: "본고딕"`, 원본은 맑은 고딕)하여 카운슬에서 2회 반려 후 승인되었으나, 도입 후에는 툴 호출 횟수가 16회에서 8회로 감소하고 카운슬 첫 번째 심의에서 즉시 승인되었습니다 (세부 지표: [파워포인트 매뉴얼 §6.22](../powerpoint/docs/MANUAL.ko.md)).

**프로그램 간 연동 (Excel 표 데이터를 활용한 PowerPoint 슬라이드 생성 등):**
프로그램별 컴패니언 인스턴스가 분리되어 있으므로 Excel 대화 세션에는 PowerPoint 도구가 직접 노출되지 않습니다. 작업 전환은 `hand_off{to: "powerpoint"}` 도구를 통해 수행됩니다(워크스페이스 명칭: `powerpoint`, `excel`, `word`).
전달된 작업은 대상 컴패니언의 신규 대화에서 실행됩니다. 개별 문서 등록 도구는 해당 문서 세션에만 바인딩되므로, 헬퍼는 개별 문서 등록 외에 컴패니언 전역 등록을 추가로 유지합니다(`serve.go`의 `settle`, 2026-09-07). `document` 인자가 생략된 호출은 허브 규칙(문서가 단일 개체이면 해당 문서 지정, 2개 이상이면 문서 목록 안내)을 따릅니다. 인계 브리프에는 텍스트 형식으로 데이터를 직렬화하여 전달하며, 대상 세션은 소스 세션의 내부 시트에 직접 접근하지 않습니다. 해당 연동 시나리오는 향후 실물 환경 검증이 예정되어 있습니다.

## 5. 테스트 및 검증

```sh
go test ./clients/office/helper/
```

총 66개 테스트 파일로 구성되어 있습니다. 각 프로그램별 매뉴얼에 명시된 허용 규칙, 도구 목록 수, 매니페스트 URL 등 문서 일관성 검증을 3개 프로그램 전체에 걸쳐 수행합니다(`Apps`). 웹 작업창 관련 스모크 테스트는 각 애드인의 `tools/` 디렉토리에 유지되며, 헬퍼 소스 코드는 `../../../office/helper/<app>_tools.go` 경로를 통해 참조됩니다.
