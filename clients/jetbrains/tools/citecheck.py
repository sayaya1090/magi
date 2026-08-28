#!/usr/bin/env python3
"""README 가 Go 원본을 가리키는 손가락이 아직 무언가를 가리키는지 검사한다.

경위. 이 문서는 처음에 줄 번호로 원본을 인용했다. 하루 만에 일곱 개가 밀렸고 — `daemon.go:587`
(WorkspaceKey)은 이제 `typ = event.TypePermissionRequested` 를, `main.go:1097`(디태치 주석)은
`if bound != nil` 을 가리키고 있었다. 인용한 사실은 전부 맞았다. 썩은 것은 손가락뿐이었다.
그래서 전부 심볼로 바꿨는데, 심볼도 이름이 바뀌면 똑같이 썩는다. 다른 점은 심볼은 **기계가 확인할
수 있다**는 것뿐이고, 그 확인을 사람의 성실성에 맡기면 확인은 안 일어난다. 그래서 이 파일이 있다.

검사하는 것 둘.
  1. `path/file.go` 의 `Symbol`   → 그 파일에 그 식별자가 있는가
  2. `path/file.go`, ... "문장"    → 그 파일에 그 문장이 한 줄로 들어 있는가

두 번째가 한 줄이어야 하는 이유는 grep 이 줄 단위이기 때문이다. 실제로 이 검사기를 처음 돌렸을 때
방금 쓴 인용문 하나가 걸렸다 — 원문은 "25 **out of** 300" 인데 "25 of 300" 으로 적었고, 고친 뒤에도
그 문장이 주석에서 줄바꿈을 넘어가 여전히 안 찾혔다. 두 번 다 사람 눈으로는 안 보이는 종류다.

실패하면 1 로 나간다. CI(.github/workflows/test-jetbrains.yml)가 이것을 본다.
"""
import os
import re
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__)))))
HERE = os.path.join(ROOT, "clients/jetbrains")

# 검사 대상은 설계 문서 하나가 아니다. Kotlin 주석도 Go 원본을 짚고, SocketPath.kt 는 그것이
# 존재 이유인 파일이다 — 그리고 실제로 DaemonClient.kt 와 Wire.kt 에 밀린 줄 번호가 일곱 개
# 남아 있었다. 문서만 고치고 코드 주석을 두면 다음 사람은 두 규칙을 보게 된다.
def documents():
    out = [os.path.join(HERE, "README.md")]
    for base, _, names in os.walk(os.path.join(HERE, "plugin")):
        if "build" in base.split(os.sep):
            continue
        out += [os.path.join(base, n) for n in sorted(names) if n.endswith(".kt")]
    return out

# 파일 이름만 적은 인용(`daemon.go`)을 풀어 줄 후보 디렉토리. 앞에 있는 것이 이긴다.
# 동명 파일이 실재한다 — search.go 는 cmd/magi-web 과 internal/app 에 하나씩 있고, 이 순서를
# 잘못 두면 검사기가 엉뚱한 파일을 열고 멀쩡한 인용을 썩었다고 보고한다. 실제로 그랬다.
BASES = [
    "cmd/magi", "cmd/magi-web",
    "internal/adapter/daemon", "internal/adapter/mcp",
    "internal/adapter/plugin/lua", "internal/adapter/platform",
    "internal/graceful", "internal/app",
    "docs", "web/ui", ".",
]

# 파일 이름의 백틱은 있어도 되고 없어도 된다. 마크다운은 두르고 KDoc 은 안 두른다.
SYMBOL = re.compile(r'`?([A-Za-z_/.-]+\.(?:go|md|kt))`?[^`\n]{0,14}`([A-Za-z_.]+)`')
QUOTE = re.compile(r'`?([A-Za-z_/.-]+\.(?:go|md|kt))`?[^`\n"]{0,30}"([^"\n]{12,120})"')
# 앞 백틱은 필수가 아니다. 마크다운은 두르지만 KDoc 은 `(daemon.go:980)` 처럼 맨몸으로 적는다.
# 처음엔 백틱을 요구했고, 그래서 Kotlin 주석에 줄 번호를 되돌리는 시험이 통과해 버렸다.
LINENO = re.compile(r'(?<![\w:/])[A-Za-z_][A-Za-z_/.-]*\.(?:go|md|kt|ts|yml):\d+')


def candidates(name):
    return [p for p in (os.path.join(ROOT, b, name) for b in BASES) if os.path.isfile(p)]


def read(path):
    with open(path, encoding="utf-8", errors="replace") as f:
        return f.read()


def main():
    bad = []
    syms, quotes = 0, 0
    for path in documents():
        n_s, n_q = check(read(path), os.path.relpath(path, ROOT), bad)
        syms += n_s
        quotes += n_q
    for line in bad:
        print(f"  !! {line}")
    print(f"심볼 {syms}개 · 인용문 {quotes}개 검사 → 못 찾은 것 {len(bad)}")
    return 1 if bad else 0


def check(doc, where, bad):

    # 줄 번호로 되돌아가는 것을 막는다. 한 번 겪은 실패다.
    #
    # 첫 `##` 앞의 머리말은 빼고 본다. 규칙 자체를 적는 자리라 "이렇게 적으면 이렇게 썩는다"의
    # 증거로 밀린 줄 번호를 그대로 인용해야 하는데, 그것까지 잡으면 검사기가 자기 설명문을 잡는다.
    # 실제로 처음 돌렸을 때 그렇게 됐다. 경계를 흐리는 대신 구역으로 나눈다.
    first_section = doc.find("\n## ")
    body = doc[first_section:] if first_section > 0 else doc
    for m in LINENO.finditer(body):
        bad.append(f"{where}: 줄 번호 인용이 다시 들어왔다: {m.group(0)} — 심볼이나 한 줄 인용문으로")

    seen, checked_syms = set(), 0
    for name, sym in SYMBOL.findall(doc):
        sym = sym.split(".")[-1]
        if not re.match(r"^[A-Za-z_]\w*$", sym) or (name, sym) in seen:
            continue
        seen.add((name, sym))
        found = candidates(name)
        if not found:
            continue
        checked_syms += 1
        if not any(re.search(rf"\b{re.escape(sym)}\b", read(p)) for p in found):
            bad.append(f"{where}: {name} 에 `{sym}` 가 없다 — 이름이 바뀌었거나 지워졌다")

    checked_quotes = 0
    for name, quote in QUOTE.findall(doc):
        found = candidates(name)
        if not found:
            continue
        checked_quotes += 1
        if not any(quote in read(p) for p in found):
            bad.append(f'{where}: {name} 에 이 문장이 한 줄로 없다: "{quote[:60]}"')

    return checked_syms, checked_quotes


if __name__ == "__main__":
    sys.exit(main())
