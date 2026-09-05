# 설치 — Excel 판

[매뉴얼](MANUAL.ko.md) · [아키텍처](ARCHITECTURE.ko.md) · [파워포인트 판 INSTALL](../../powerpoint/docs/INSTALL.ko.md)

> **아직 실물에서 안 깔아 봤다(2026-09-06).** 아래는 파워포인트 판이 실측한 절차를 엑셀 말로 옮긴 것이고, 처음
> 깐 날의 기록이 이 문서를 고친다.

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
go build -o magi-xl ./clients/excel/helper
./magi-xl -cert-hint        # 인증서를 신뢰 저장소에 넣는 법(첫 기동이 <config>/xl-helper-cert.pem 을 만든다)
magi --daemon               # 통합 문서가 있는 디렉토리에서 — 없어도 헬퍼가 하나 띄운다
./magi-xl                   # 127.0.0.1:3001
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

개발자 키를 무시하므로 **신뢰 카탈로그(공유 폴더)** 다(파워포인트 판 §3.2 절차 그대로, 매니페스트만 이것):
`HKCU\Software\Microsoft\Office\16.0\WEF\TrustedCatalogs\{guid}` 에 `Id`·`Url`(UNC)·`Flags=1` → 삽입 → 내 추가 기능
→ 공유 폴더 → Magi(AI Assistant) → 추가.

한 줄 설치(`install.ps1`)는 아직 없다 — 파워포인트 판 것에서 COM 손 절을 빼고 이름을 바꾸면 된다.

## 4. 확인

작업창이 **「준비됐습니다 — 도구 61 개.」** 로 끝나면 깔린 것이다. 「지원 API」 줄은 2021/365 에서 숨어 있어야
한다(전부 ✓). 하얀 창이면 인증서(§2), 「연결된 손이 없습니다」면 헬퍼·데몬(매뉴얼 §10).

## 5. 지우기

- macOS: `wef/manifest.xml` 삭제 + Excel 재시작.
- Windows: 레지스트리 키(3.2 또는 3.3) 삭제 + Excel 재시작.
- 인증서: 신뢰 저장소에서 `xl-helper-cert` 제거, `<config>/xl-helper-*.pem` 삭제.
