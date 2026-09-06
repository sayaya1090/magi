# 설치 — Word 판

[매뉴얼](MANUAL.ko.md) · [엑셀 판 INSTALL](../../excel/docs/INSTALL.ko.md)

> Mac 에서 한 번 깔았다(2026-09-06): 인증서를 키체인에 넣고 매니페스트를 `wef` 에 두면, Word 홈 탭 「추가 기능 › 개발자 추가 기능 › Magi」에서 한 번 누른 뒤 리본에 **Magi** 단추가 생긴다. Windows 는 아직.

## 1. 판

| Word | WordApi | 작업창 |
|---|---|---|
| Microsoft 365 · 2024 | 1.4~1.9 | 손 (전부 된다) |
| 2019 · 2021 LTSC | 1.3 | 손 — 메모·책갈피·변경 추적(1.4)·변경 검토(1.6)·제안(settings 1.4)은 이름을 대고 거절 |
| 2016 이하 | ≤1.2 | 화면만 |

### 1.1 볼륨 판에 Word 가 빠져 있을 때 — Office 배포 도구로 더한다

LTSC 2021 을 Word 없이 깐 머신이 있다(Click-to-Run 설정의 `ProPlus2021Volume.ExcludedApps` 에 `word`). Microsoft 의 Office 배포
도구(ODT, 다운로드 센터 id=49117 의 `officedeploymenttool_*.exe`, 서명 확인 뒤 `/quiet /extract:폴더` 로 푼다)로 **같은 제품을 Word 만 뺀
제외 목록으로 다시 구성**하면 나머지는 그대로 두고 Word 만 더해진다(2026-09-07 실측, 약 2분):

```xml
<Configuration>
  <Add OfficeClientEdition="64" Channel="PerpetualVL2021">
    <Product ID="ProPlus2021Volume">
      <Language ID="ko-kr" />
      <ExcludeApp ID="Access" /><ExcludeApp ID="Groove" /><ExcludeApp ID="Lync" /><ExcludeApp ID="OneDrive" />
      <ExcludeApp ID="OneNote" /><ExcludeApp ID="Outlook" /><ExcludeApp ID="Publisher" /><ExcludeApp ID="Teams" />
    </Product>
  </Add>
  <Display Level="Full" AcceptEULA="FALSE" />
</Configuration>
```

`setup.exe /configure 위파일.xml` 을 **관리자 권한**으로 돈다(UAC). 제외 목록은 그 머신의 레지스트리
`HKLM\SOFTWARE\Microsoft\Office\ClickToRun\Configuration` 에서 읽어 그대로 옮기고 Word 만 뺀다 — 다르게 적으면 그 프로그램이 지워진다.
라이선스는 기존 Office 의 것을 그대로 따른다. 끝나면 이미 있는 카탈로그에서 「공유 폴더 → 추가」만 하면 된다(TESTING §5.3).

## 2. 공통

```bash
go build -o magi ./cmd/magi
./magi office -cert-hint    # <config>/office-helper-cert.pem 하나를 신뢰 저장소에(파워포인트·엑셀 공용) — 데몬도 이것으로 헬퍼에 붙는다
magi --daemon
./magi office               # 127.0.0.1:3000 — /ppt·/xl·/word 세 판
```

## 3. 애드인 넣기

- **macOS**: `cp clients/word/addin/manifest.xml ~/Library/Containers/com.microsoft.Word/Data/Documents/wef/` 뒤 Word 재시작.
- **Windows**: 엑셀 판 §3 과 같은 절차(M365 개발자 키 / 볼륨 판 신뢰 카탈로그). 한 줄 설치기는 `clients/office/install.ps1`(세 프로그램 공용; 2021 에서 돌았다 — 파워포인트 판 TESTING §5.5. Word 애드인을 그 머신의 Word 로 여는 것은 아직).

## 4. 확인 · 5. 지우기

「준비됐습니다 — 도구 44 개.」 · macOS 는 wef 의 manifest.xml 삭제, 인증서는 `magi office helper`(세 판 공용이라 다른 판을 아직 쓰면 두지 않는다).
