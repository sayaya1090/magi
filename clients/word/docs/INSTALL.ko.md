# 설치 — Word 판

[매뉴얼](MANUAL.ko.md) · [엑셀 판 INSTALL](../../excel/docs/INSTALL.ko.md)

> Mac 에서 한 번 깔았다(2026-09-06): 인증서를 키체인에 넣고 매니페스트를 `wef` 에 두면, Word 홈 탭 「추가 기능 › 개발자 추가 기능 › Magi」에서 한 번 누른 뒤 리본에 **Magi** 단추가 생긴다. Windows 는 아직.

## 1. 판

| Word | WordApi | 작업창 |
|---|---|---|
| Microsoft 365 · 2024 | 1.4~1.9 | 손 (전부 된다) |
| 2019 · 2021 LTSC | 1.3 | 손 — 메모·책갈피·변경 추적(1.4)·변경 검토(1.6)·제안(settings 1.4)은 이름을 대고 거절 |
| 2016 이하 | ≤1.2 | 화면만 |

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
