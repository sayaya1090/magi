# magi-word — Word 애드인의 헬퍼

[사용자 매뉴얼](../docs/MANUAL.ko.md) · [무엇을 어디서 재나](../docs/TESTING.ko.md) · [아키텍처](../docs/ARCHITECTURE.ko.md) · [애드인](../addin/README.md) · [엑셀 판 헬퍼](../../excel/helper/README.md)

엑셀 판 헬퍼(`magi-xl`)를 **복사해 이름과 도구만 바꿨다** — 얼굴 셋과 그것을 무는 시험은 저쪽 README 그대로다.

```sh
go build -o magi-word ./clients/word/helper
./magi-word                     # 127.0.0.1:3002, 애드인은 clients/word/addin
./magi-word -allow-rules        # 읽기 14개의 허용 규칙
./magi-word -cert-hint
```

| 파일 | 무엇 |
|---|---|
| `names.go` | `word` · 3002 · `word-helper-cert` · `magi-word` · 워크스페이스 `word` · 문서 키 `wd-` · 쿼리 `doc` |
| `tools.go` | 도구 44개(읽기 14 · 쓰기 30). `withFromTo`·`paraProp`·`tableProp`. 별칭: `paragraph`←`para`, `text`←`content`, `color`←`font_color` |
| `enums.go` | 정렬·밑줄·형광·목록·구분·머리글·추적 모드·검토·내장 스타일 28·표 스타일 105. `suggest.what` 은 열거 예외 |
| `instructions.go` · `word/skills/` | 브리프 7단계 · 가이드 셋 |

## 아직 아닌 것

- **실물 Word 에 한 번도 안 붙여 봤다**(2026-09-06).
- 공용 패키지로 갈라야 한다(헬퍼 셋이 복사).
