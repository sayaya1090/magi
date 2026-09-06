# 설치 — Excel 판

[매뉴얼](MANUAL.ko.md) · [아키텍처](ARCHITECTURE.ko.md) · [파워포인트 판 INSTALL](../../powerpoint/docs/INSTALL.ko.md)

> Mac(Excel 16.112.3)에서는 2026-09-06 에 §3.1 그대로 깔려 붙었다. Windows 절차는 파워포인트 판이 실측한 것을 엑셀 말로
> 옮긴 것이라 아직 안 잰 것이다.
>
> **2026-09-06, LTSC 2021(16.0.14334)에서 끝까지 됐다.** `install.ps1` 한 줄 → 공유 폴더에 Magi(AI Assistant) → 추가 → 홈 탭
> 「AI Assistant › Magi」(아이콘 포함) → 창 「준비됐습니다 — 도구 61 개」(지원 API 줄 숨음 = 전부 ✓) → 창의 보내기로
> 「A5 에 '설치 확인', B5 에 B2:B3 합계」를 시키니 A5 와 `=SUM(B2:B3)`→8 이 들어갔다(COM 으로 읽어 확인). 다만
> 그 전에 반나절 막힌 것이 하나 있다 — §3.3 의 「키는 하나」.

## 0. 한 번에 깔기 — `clients/office/install.ps1`(파워포인트·엑셀·워드 한 번에)

저장소를 받은 Windows 머신에서 이 한 줄이면 된다(Go 가 있어야 한다):

```powershell
& .\clients\office\install.ps1
```

하는 일 — Office 판을 읽고(M365 인가 볼륨 판인가), `magi.exe` 하나를 빌드해 놓고(헬퍼는 `magi office` 안),
애드인 파일을 그 옆에 복사하고, 데몬 권한 모드를 **allow** 로 두고(`~/.magi/config.toml`, 사용자 결정), 헬퍼(3001)를
띄우고, 인증서를 이 계정의 신뢰 저장소에 넣고(Windows 가 한 번 묻는다), 애드인을 등록하고(M365 는 개발자 키, 볼륨 판은
신뢰 카탈로그 — 파워포인트 판과 **같은 폴더 `~/.magi/catalog`, 같은 키**, §3.3), 로그인 때 헬퍼가 같이 뜨게 한다.
파워포인트 판 설치기와 같이 돌려도 된다 — 포트·인증서·Run 키 이름이 다르다. 다시 돌려도 된다; `-NoAutostart` ·
`-SkipBuild` · `-Clean` · `-CatalogUnc` 는 파워포인트 판과 같은 뜻이다.

끝나면 Excel 을 껐다 켜고, 볼륨 판은 **삽입 → 내 추가 기능 → 공유 폴더 → Magi(AI Assistant) → 추가** 를 한 번 한다.

## 1. 판 고르기 — 하나뿐이다

| Excel | ExcelApi | 작업창 |
|---|---|---|
| Microsoft 365 | 1.21 | 손 (전부 된다) |
| 2021 LTSC · 2024 | 1.14 | 손 (전부 된다) |
| 2019 | 1.7 | 손 — 찾기·그림 넣기(1.9)·메모(1.10)·유효성·피벗(1.8)은 **이름을 대고 거절** |
| 2016 이하 | ≤1.4 | 화면만 — 편집 못 함, 그렇게 말한다 |

파워포인트 판의 「2021 은 COM 손」 갈림이 **없다.**

## 2. 공통 — 헬퍼·인증서·데몬

```bash
go build -o magi ./cmd/magi
./magi office -cert-hint    # 인증서를 신뢰 저장소에 넣는 법(첫 기동이 <config>/office-helper-cert.pem 하나를 만든다 — 세 프로그램 공용)
magi --daemon               # 통합 문서가 있는 디렉토리에서 — 없어도 헬퍼가 하나 띄운다
./magi office               # 127.0.0.1:3000 — /xl 아래(파워포인트 /ppt, 워드 /word 와 한 프로세스)
```

포트는 매니페스트 `<SourceLocation>` 과 같아야 한다(3001). 파워포인트 헬퍼(3000)와 같이 떠도 된다 — 인증서 이름도
포트도 다르다.

## 3. 애드인을 Excel 에 넣는 길

### 3.1 macOS

```bash
cp clients/excel/addin/manifest.xml ~/Library/Containers/com.microsoft.Excel/Data/Documents/wef/
```

Excel 을 완전히 끝냈다(⌘Q) 다시 연다. 홈 탭 오른쪽 끝 **AI Assistant › Magi**. 안 서면 `~/Library/Containers/
com.microsoft.Excel/Data/Library/Application Support/Microsoft/Office/16.0/Wef/` 캐시를 옆으로 치우고 다시.

### 3.2 Windows — Microsoft 365

`HKCU\Software\Microsoft\Office\16.0\WEF\Developer` 에 매니페스트 경로 한 줄(파워포인트 판 §3.1). 껐다 켜면
홈 → 추가 기능 → 개발자 추가 기능에 선다.

### 3.3 Windows — 2019·2021 LTSC

**실측(2026-09-06, Excel 2021 16.0.14334) — 신뢰 카탈로그 키는 하나여야 한다.** Excel 은 뜰 때 `WEF\TrustedCatalogs`
아래에 키가 **둘 이상이면** 신뢰 센터에 「설정을 읽는 도중 문제가 발생하여 다시 설정해야 합니다」를 띄우고 **전부 지운다**
— 자기가 만든 키, 빈 폴더를 가리키는 키, 파워포인트 판의 키, 가리지 않고. 하나면 산다. 같은 머신의 PowerPoint 2021 은
둘을 받는다. 처음에는 관리 공유(`\\localhost\C$`)·GUID 대소문자·매니페스트 내용을 의심해 하나씩 걷어냈는데 전부
아니었고, 남은 것이 개수였다. 그래서 두 설치기가 **한 폴더** `~/.magi/catalog` 에 각자의 매니페스트(`magi-ppt-manifest.xml`,
`magi-xl-manifest.xml`)를 놓고 **키 하나**를 같이 쓴다. 옛 자리(ppt-catalog·xl-catalog)를 가리키는 키는 설치기가 지운다.

UNC 는 카탈로그 폴더를 덮는 진짜 공유가 있으면 그것(`\\<컴퓨터>\magi\catalog`), 없으면 관리 공유다(`-CatalogUnc` 로 직접
줄 수도 있다). 이 머신은 관리자 PowerShell 에서 `New-SmbShare -Name magi -Path "$env:USERPROFILE\.magi" -ReadAccess
$env:USERNAME` 로 만든 공유를 썼다 — 관리 공유로도 키가 하나면 Excel 이 받는지는 다시 안 쟀다. 개발자 키
(`WEF\Developer`)는 파워포인트 판처럼 무시한다.



개발자 키를 무시하므로 **신뢰 카탈로그(공유 폴더)** 다(파워포인트 판 §3.2 절차 그대로, 매니페스트만 이것):
`HKCU\Software\Microsoft\Office\16.0\WEF\TrustedCatalogs\{guid}` 에 `Id`·`Url`(UNC)·`Flags=1` → 삽입 → 내 추가 기능
→ 공유 폴더 → Magi(AI Assistant) → 추가.

한 줄 설치는 `clients/office/install.ps1`(§0)이 이것을 다 한다 — 헬퍼 하나(`magi office`)에 세 애드인을 등록한다(2026-09-06 에 엑셀 판 설치기에서 옮겨 적었고 Windows 에서 아직 안 돌렸다).

## 4. 확인

작업창이 **「준비됐습니다 — 도구 61 개.」** 로 끝나면 깔린 것이다. 「지원 API」 줄은 2021/365 에서 숨어 있어야
한다(전부 ✓). 하얀 창이면 인증서(§2), 「연결된 손이 없습니다」면 헬퍼·데몬(매뉴얼 §10).

## 5. 지우기

- macOS: `wef/manifest.xml` 삭제 + Excel 재시작.
- Windows: `clients/office/install.ps1 -Clean` 이 세 판의 등록과 Office 애드인 캐시를 걷어 낸다(카탈로그 키는 파워포인트 판과 같이 쓰는
  것이라 그쪽도 다시 추가해야 한다). 손으로 하려면 `WEF\TrustedCatalogs` 의 키(볼륨 판) 또는 `WEF\Developer\magi-xl`(M365),
  `Run\magi-xl`, 설치 폴더 `%LOCALAPPDATA%\magi\xl`, 카탈로그의 `magi-xl-manifest.xml`.
- 인증서: 신뢰 저장소에서 `magi office helper` 제거(다른 판도 같이 쓰는 것이라 전부 내릴 때만), `<config>/office-helper-*.pem` 삭제.
- 헬퍼·데몬 내리기: `Stop-Process -Name magi`(헬퍼도 magi 다). PowerPoint 를 다 꺼도 이것들은 안 꺼진다 — 파워포인트 판 INSTALL §6.
