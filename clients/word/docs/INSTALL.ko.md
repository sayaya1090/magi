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
go build -o magi-word ./clients/word/helper
./magi-word -cert-hint      # <config>/word-helper-cert.pem 을 신뢰 저장소에 — 데몬도 이것으로 헬퍼에 붙는다
magi --daemon
./magi-word                 # 127.0.0.1:3002
```

## 3. 애드인 넣기

- **macOS**: `cp clients/word/addin/manifest.xml ~/Library/Containers/com.microsoft.Word/Data/Documents/wef/` 뒤 Word 재시작.
- **Windows**: 엑셀 판 §3 과 같은 절차(M365 개발자 키 / 볼륨 판 신뢰 카탈로그). 한 줄 설치기는 아직 없다.

## 4. 확인 · 5. 지우기

「준비됐습니다 — 도구 44 개.」 · macOS 는 wef 의 manifest.xml 삭제, 인증서는 `word-helper-cert`.
