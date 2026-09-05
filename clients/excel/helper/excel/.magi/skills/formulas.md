---
name: formulas
description: 수식·이름·유효성 — 자주 쓰는 수식 꼴과 함정(텍스트 숫자, 순환 참조, 절대 참조). 계산이 들어가는 부탁에 읽는다.
---

# 수식 규약

- **수식은 `formulas` 로** 넣는다(`write_range{formulas:[["=SUM(B2:B9)"]]}`). `values` 에 넣어도 "=" 로 시작하면 수식이다.
- **절대 참조**: 채우기 방향으로 고정할 축에 `$` — 단가 셀은 `$B$1`, 행별 값은 `B2`.
- **오류를 감싼다**: 나눗셈은 `=IFERROR(B2/C2, "")`. 찾기는 `XLOOKUP`(2021 이상) 또는 `INDEX/MATCH`.
- **텍스트 숫자**: `read_range` 가 `"1,234"` 같은 문자열을 주면 계산이 안 된다 — 사람에게 알리고, 부탁이 있으면 `VALUE()` 로 옮긴 열을 새로 둔다.
- **이름**: 반복되는 상수·범위는 `set_name`. `read_names` 로 이미 있는 이름을 먼저 본다.
- **유효성**: 입력 시트의 선택지는 `set_validation{validation_kind:"list", values:[…]}`. 숫자 범위는 `whole_number` + `Between`.
- **되읽기**: 수식을 넣은 범위는 `read_range{formulas:true}` 로 되읽어 `#REF!`·`#NAME?`·`#DIV/0!` 이 없는지 본다. 있으면 그 셀 주소를 대고 고친다.
- 순환 참조는 넣지 않는다 — 합계가 제 범위를 포함하는 실수가 흔하다(`=SUM(C2:C14)` 를 C14 에).
