#!/usr/bin/env python3
"""이 클라이언트의 문서와 주석이 Go 원본을 가리키는 손가락을 검사한다.

경위. 설계 문서는 처음에 줄 번호로 원본을 인용했다. 하루 만에 열셋이 밀렸고 — `daemon.go:587`
(WorkspaceKey)은 `typ = event.TypePermissionRequested` 를, `main.go:1097`(디태치 주석)은
`if bound != nil` 을 가리키고 있었다. 인용한 사실은 하나도 틀리지 않았다. 썩은 것은 손가락뿐이다.
그래서 전부 심볼로 바꿨는데, 심볼도 이름이 바뀌면 똑같이 썩는다. 다른 점은 **기계가 확인할 수
있다**는 것뿐이고, 그 확인을 성실성에 맡기면 확인은 안 일어난다. 그래서 이 파일이 있다.

`.github/workflows/ci.yml` 과 `.github/workflows/test-jetbrains.yml` 이 **둘 다** 부른다. 한쪽만
으로는 안 되는데, 썩음이 Go 쪽 rename 에서 오는 반면 test-jetbrains 는 `clients/jetbrains/**`
필터라 그 rename 을 아예 못 보기 때문이다.

검사하는 것 넷.
  1. 줄 번호 인용이 돌아오지 않았는가        (본문에 한해서 — 규칙 자체를 적는 머리말은 예외)
  2. `파일` 의 `심볼`  → 그 파일에 그 식별자가 있는가
  3. `파일`, ... "문장"  → 그 파일에 그 문장이 **한 줄로** 들어 있는가
  4. 맨몸 파일 이름이 저장소에서 하나로 풀리는가

세 번째가 한 줄이어야 하는 이유는 grep 이 줄 단위이기 때문이다. 네 번째가 있는 이유는 이 검사기
자신이 한 번 속아서다 — `search.go` 는 `cmd/magi-web` 과 `internal/app` 에 하나씩 있고, 검사기가
엉뚱한 쪽을 열어 멀쩡한 인용을 썩었다고 보고했다. 사람도 같은 것에 속는다.

"못 찾은 것 0" 이 "전부 확인했다"는 뜻이 되도록, **검사에서 빠진 짝은 마지막에 나열한다.** 인용
모양인데 정규식에 안 걸리는 것들이 있고(연결어가 창을 넘거나, 이 트리 밖 파일을 짚거나), 그것을
조용히 버리면 인용 하나가 한 단어 늘어난 순간 검사에서 빠지면서 총계는 그대로 0 이 된다.

실패하면 1 로 나간다.
"""
import os
import re
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__)))))
HERE = os.path.join(ROOT, "clients/jetbrains")

# 이 트리의 소스만 본다. 후보 디렉토리 목록을 손으로 유지하는 대신 저장소를 걸어서, 새 패키지가
# 생겨도 검사기가 따라오게 한다. 대신 같은 이름이 둘 이상이면 인용 쪽에 경로를 요구한다.
SKIP_DIRS = {".git", "build", "node_modules", ".gradle", "vendor", "scratchpad", ".agents"}

# 파일 이름의 백틱은 있어도 되고 없어도 된다. 마크다운은 두르고 KDoc 은 안 두른다. 처음엔
# 백틱을 요구했고, 그래서 Kotlin 주석에 줄 번호를 되돌리는 시험이 통과해 버렸다.
BARE = r'(?<![\w:/])([A-Za-z_][A-Za-z_/.-]*\.(?:go|md|kt))'
# 심볼에 숫자를 허용한다. `[A-Za-z_.]+` 이었을 때 `sha256Of` 같은 이름이 전부 조용히 빠졌다.
SYMBOL = re.compile(r'`?' + BARE + r'`?[^`\n]{0,14}`([A-Za-z_][\w.]*)`')
QUOTE = re.compile(r'`?' + BARE + r'`?[^`\n"]{0,30}"([^"\n]{12,120})"')
MENTION = re.compile(r'`?' + BARE + r'`?')
LINENO = re.compile(r'(?<![\w:/])[A-Za-z_][A-Za-z_/.-]*\.(?:go|md|kt|ts|yml):\d+')


def sources():
    """저장소의 소스 파일을 (basename, [경로…]) 로 색인한다."""
    index = {}
    for base, dirs, names in os.walk(ROOT):
        dirs[:] = [d for d in dirs if d not in SKIP_DIRS and not d.startswith(".")]
        for n in names:
            if n.endswith((".go", ".md", ".kt")):
                index.setdefault(n, []).append(os.path.join(base, n))
    return index


INDEX = sources()


def resolve(name):
    """인용이 가리키는 파일. (경로, 문제) — 경로가 None 이면 검사 못 한 것이다."""
    if "/" in name:
        p = os.path.join(ROOT, name)
        if os.path.isfile(p):
            return p, None
        # 접미 일치도 허용한다: `cmd/magi-web/mcp.go` 처럼 저장소 루트 기준이 아닌 인용.
        hits = [c for c in INDEX.get(os.path.basename(name), []) if c.endswith(name)]
        if len(hits) == 1:
            return hits[0], None
        return None, None if not hits else f"{name}: 같은 꼬리를 가진 파일이 {len(hits)}개다"
    hits = INDEX.get(name, [])
    if len(hits) == 1:
        return hits[0], None
    if not hits:
        return None, None  # 이 트리 밖 파일이거나 예시. 검사 못 한 것으로 센다.
    return None, (
        f"`{name}` 는 저장소에 {len(hits)}개라 어느 것인지 모른다 — 경로를 붙여 적을 것 "
        f"({', '.join(os.path.relpath(h, ROOT) for h in sorted(hits)[:4])})"
    )


def read(path):
    with open(path, encoding="utf-8", errors="replace") as f:
        return f.read()


def documents():
    out = [os.path.join(HERE, "README.md")]
    for base, dirs, names in os.walk(os.path.join(HERE, "plugin")):
        dirs[:] = [d for d in dirs if d not in SKIP_DIRS]
        out += [os.path.join(base, n) for n in sorted(names) if n.endswith(".kt")]
    return out


def check(doc, where, bad, unchecked):
    # 규칙 자체를 적는 머리말(첫 `##` 앞)은 줄 번호 검사에서 뺀다. "이렇게 적으면 이렇게 썩는다"의
    # 증거로 밀린 번호를 인용해야 하는데, 그것까지 잡으면 검사기가 자기 설명문을 잡는다.
    cut = doc.find("\n## ")
    for m in LINENO.finditer(doc[cut:] if cut > 0 else doc):
        bad.append(f"{where}: 줄 번호 인용이 돌아왔다: {m.group(0)} — 심볼이나 한 줄 인용문으로")

    checked = set()
    for name, sym in SYMBOL.findall(doc):
        path, problem = resolve(name)
        if problem:
            bad.append(f"{where}: {problem}")
            continue
        if not path:
            unchecked.append(f"{where}: `{name}` 의 `{sym}` — 이 트리에 없는 파일")
            continue
        checked.add((name, sym))
        body = read(path)
        if sym in body:
            continue
        # 한정 인용(`Client.SetPermission`)을 마지막 조각만 보고 넘기면 `\bSetPermission\b` 가
        # 아무 데나 걸려 검사가 무의미해진다. 그렇다고 문자열 그대로 찾으면 **Go 에는 그런
        # 문자열이 없다** — 메서드는 `func (c *Client) SetPermission` 으로 적히고 `Client.
        # SetPermission` 은 부르는 이름일 뿐이다. 그래서 리시버와 이름을 같이 본다.
        if "." in sym:
            recv, tail = sym.rsplit(".", 1)
            if re.search(rf"func\s*\([^)]*\b{re.escape(recv)}\s*\)\s*{re.escape(tail)}\b", body):
                continue
            # Kotlin 쪽(`object X { fun y }`)과 Go 의 타입 필드는 두 이름이 다 있으면 통과시키되,
            # 사람이 볼 수 있게 남긴다.
            if re.search(rf"\b{re.escape(recv)}\b", body) and re.search(rf"\b{re.escape(tail)}\b", body):
                unchecked.append(f"{where}: {name} 의 `{sym}` — 둘 다 있지만 한 선언에서 못 봤다")
                continue
        bad.append(f"{where}: {name} 에 `{sym}` 가 없다 — 이름이 바뀌었거나 지워졌다")

    quotes = 0
    for name, quote in QUOTE.findall(doc):
        path, problem = resolve(name)
        if problem or not path:
            continue
        quotes += 1
        if quote not in read(path):
            bad.append(f'{where}: {name} 에 이 문장이 한 줄로 없다: "{quote[:60]}"')

    # 검사에서 빠진 파일 언급을 센다. 0 이 "다 봤다"는 뜻이 되게 하는 부분이다.
    mentioned = {m.group(1) for m in MENTION.finditer(doc)}
    covered = {n for n, _ in checked} | {n for n, _ in QUOTE.findall(doc)}
    for name in sorted(mentioned - covered):
        # 문서가 문서를 가리키는 것(`docs/ARCHITECTURE.md`)은 파일 전체가 대상이라 정상이다.
        # 소스 파일을 심볼 없이 가리키는 것만 약한 손가락으로 본다.
        if name.endswith(".md"):
            continue
        path, problem = resolve(name)
        if path or problem:
            unchecked.append(f"{where}: `{name}` 를 심볼 없이 가리킨다")
    return len(checked), quotes


def main():
    bad, unchecked = [], []
    syms = quotes = 0
    for path in documents():
        s, q = check(read(path), os.path.relpath(path, ROOT), bad, unchecked)
        syms += s
        quotes += q
    for line in bad:
        print(f"  !! {line}")
    if unchecked:
        print(f"  -- 검사 못 한 언급 {len(unchecked)}개 (사람이 볼 것, 실패는 아니다):")
        for line in unchecked:
            print(f"       {line}")
    print(f"심볼 {syms}개 · 인용문 {quotes}개 검사 → 못 찾은 것 {len(bad)}")
    return 1 if bad else 0


if __name__ == "__main__":
    sys.exit(main())
