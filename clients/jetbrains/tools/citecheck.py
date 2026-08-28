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
  4. 맨몸 파일 이름이 저장소에서 하나로 풀리는가 (`.js`/`.mjs` 는 예외 — `BARE` 주석)

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
import subprocess
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__)))))
HERE = os.path.join(ROOT, "clients/jetbrains")

# 이 트리의 소스만 본다. 후보 디렉토리 목록을 손으로 유지하는 대신 저장소를 걸어서, 새 패키지가
# 생겨도 검사기가 따라오게 한다. 대신 같은 이름이 둘 이상이면 인용 쪽에 경로를 요구한다.
SKIP_DIRS = {".git", "build", "node_modules", ".gradle", "vendor", "scratchpad", ".agents"}

# 파일 이름의 백틱은 있어도 되고 없어도 된다. 마크다운은 두르고 KDoc 은 안 두른다. 처음엔
# 백틱을 요구했고, 그래서 Kotlin 주석에 줄 번호를 되돌리는 시험이 통과해 버렸다.
# `.js`/`.mjs` 는 **경로가 있는 인용만** 잡는다. 맨몸으로 넓히면 `Office.js` 가
# `그런 파일이 없다` 로 떨어진다 — 남의 라이브러리 이름과 우리 파일 이름은 맨몸에서 구별이
# 안 되고, go/md/kt 는 이 트리 밖 이름이 산문에 안 나와서 여태 그 문제가 없었을 뿐이다.
# 그렇다고 js 만 "없는 파일은 조용히 넘김"으로 두면 목업 파일 이름을 바꾸는 순간 그 인용
# 전부가 검사에서 빠진다 — 가장 큰 썩음이 가장 조용해지는 그 모양(아래 resolve 주석).
# 그래서 규칙을 바꾸는 대신 **적는 쪽에 경로를 요구한다.** 맨몸 js 는 안 잡는 대신
# 아래에서 반드시 나열한다 — 안 보는 것과 못 보는 것을 가른다.
BARE = (r'(?<![\w:/])((?:[A-Za-z_][A-Za-z_/.-]*\.(?:go|md|kt)'
        r'|[A-Za-z_][A-Za-z_.-]*(?:/[A-Za-z_.-]+)+\.(?:js|mjs)))(?![\w])')
# 심볼에 숫자를 허용한다. `[A-Za-z_.]+` 이었을 때 `sha256Of` 같은 이름이 전부 조용히 빠졌다.
SYMBOL = re.compile(r'`?' + BARE + r'`?[^`\n]{0,14}`([A-Za-z_][\w.]*)`')
QUOTE = re.compile(r'`?' + BARE + r'`?[^`\n"]{0,30}"([^"\n]{12,120})"')
MENTION = re.compile(r'`?' + BARE + r'`?')
LINENO = re.compile(r'(?<![\w:/])[A-Za-z_][A-Za-z_/.-]*\.(?:go|md|kt|ts|js|mjs|yml):\d+')


def sources():
    """저장소의 소스 파일을 (basename, [경로…]) 로 색인한다.

    git 이 추적하는 것만 담는다. 처음엔 트리를 그냥 걸었더니 `bench/harbor/state` 의 생성물이
    딸려 와 `README.md` 가 11 개가 됐고, 문서의 멀쩡한 인용이 "모호하다"로 실패했다. 무시되는
    파일은 이 저장소의 사실이 아니다.
    """
    index = {}
    tracked = subprocess.run(["git", "ls-files", "-z"], cwd=ROOT, capture_output=True, text=True)
    if tracked.returncode != 0 or not tracked.stdout.strip():
        # 여기서 죽는다. 색인이 비면 모든 인용이 "그런 파일이 없다"로 떨어지는데, 시끄럽기만 하고
        # 사인이 거짓이다 — 문서가 아니라 이 검사기가 고장난 것이다.
        raise SystemExit(f"citecheck: git ls-files 가 아무것도 못 냈다 (rc={tracked.returncode})")
    for rel in tracked.stdout.split("\0"):
        if rel.endswith((".go", ".md", ".kt", ".js", ".mjs")):
            index.setdefault(os.path.basename(rel), []).append(os.path.join(ROOT, rel))
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
        if not hits:
            return None, f"`{name}`: 그런 파일이 없다 — 옮겨졌거나 이름이 바뀌었다"
        return None, f"`{name}`: 같은 꼬리를 가진 파일이 {len(hits)}개다"
    hits = INDEX.get(name, [])
    if len(hits) == 1:
        return hits[0], None
    if not hits:
        # 없는 파일은 **실패**다. 한때 조용히 넘겼는데, 그러면 `daemon.go` 를 다른 이름으로
        # 옮기는 순간 이 문서의 그 파일 인용 전부가 검사에서 빠지면서 총계는 그대로 0 이 된다 —
        # 썩음의 가장 큰 형태가 제일 조용해진다.
        return None, f"`{name}`: 그런 파일이 없다 — 옮겨졌거나 이름이 바뀌었다"
    return None, (
        f"`{name}` 는 저장소에 {len(hits)}개라 어느 것인지 모른다 — 경로를 붙여 적을 것 "
        f"({', '.join(os.path.relpath(h, ROOT) for h in sorted(hits)[:4])})"
    )


def read(path):
    with open(path, encoding="utf-8", errors="replace") as f:
        return f.read()


COMMENT = re.compile(r"/\*\*?.*?\*/|//[^\n]*", re.S)


def prose(path):
    """검사 대상이 되는 글. Kotlin 은 **주석만** 본다.

    코드까지 보면 문자열 리터럴이 인용으로 잡힌다 — `completeCode("a.kt", …)` 의 `a.kt` 는 시험
    픽스처이지 원본을 가리키는 손가락이 아니다. 그것을 관용으로 넘기려다 "없는 파일"이라는 가장
    큰 썩음까지 같이 삼켰다(동료 실측). 인용은 산문에 살지 리터럴에 안 사니까, 관용을 넓히는
    대신 보는 범위를 좁힌다.
    """
    body = read(path)
    if not path.endswith(".kt"):
        return body
    return "\n".join(m.group(0) for m in COMMENT.finditer(body))


def documents():
    # 형제 문서도 본다. 둘이 서로의 **절 번호**를 짚고 있었고, 절 번호는 줄 번호와 같은 성질이다 —
    # 위치이지 이름이라 재배치하면 조용히 딴 데를 가리킨다. 답도 같다: 절의 이름은 **제목**이고,
    # 제목을 번호까지 인용하면 재번호와 개제가 둘 다 걸린다(검사 3이 그대로 그 검사다).
    #
    # 켜기 전에 쟀다 — 그쪽 문서는 BAD 0, UNCHECKED 18(전부 "심볼 없이 가리킨다"). 켜는 순간
    # 빨개지지 않는다. 그리고 켜자마자 값이 났다: 이쪽이 그쪽 절 제목 하나를 기억으로 지어냈고
    # (실제는 "5.4 죽으면 다시") 검사기가 그것을 잡았다.
    out = [os.path.join(ROOT, "clients/powerpoint/DESIGN.md"), os.path.join(HERE, "README.md")]
    for base, dirs, names in os.walk(os.path.join(HERE, "plugin")):
        dirs[:] = [d for d in dirs if d not in SKIP_DIRS]
        out += [os.path.join(base, n) for n in sorted(names) if n.endswith(".kt")]
    return out


FENCE = re.compile(r"^```.*?^```", re.S | re.M)
BAREJS = re.compile(r'(?<![\w:/])([A-Za-z_][A-Za-z_.-]*\.(?:js|mjs))(?![\w])')


def check(doc, where, bad, unchecked):
    # 규칙 자체를 적는 머리말(첫 `##` 앞)을 통째로 뺀다. "이렇게 적으면 이렇게 썩는다"의 증거로
    # 밀린 번호를 인용해야 하는데, 그것까지 검사하면 검사기가 자기 설명문을 잡는다.
    #
    # 코드 펜스도 뺀다. 디렉토리 트리 그림의 `README.md          # 이 문서` 는 목록의 한 줄이지
    # 원본을 가리키는 손가락이 아니다. Kotlin 을 주석만 보는 것과 같은 이유다 — 관용을 넓히는
    # 대신 보는 범위를 좁힌다.
    cut = doc.find("\n## ")
    doc = FENCE.sub("", doc[cut:] if cut > 0 else doc)
    for m in LINENO.finditer(doc):
        bad.append(f"{where}: 줄 번호 인용이 돌아왔다: {m.group(0)} — 심볼이나 한 줄 인용문으로")

    checked = set()
    said = set()  # 같은 이름을 여러 번 인용해도 한 번만 말한다
    for name, sym in SYMBOL.findall(doc):
        path, problem = resolve(name)
        if problem:
            if name not in said:
                said.add(name)
                bad.append(f"{where}: {problem}")
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
        if problem:
            if name not in said:
                said.add(name)
                bad.append(f"{where}: {problem}")
            continue
        quotes += 1
        if quote not in read(path):
            bad.append(f'{where}: {name} 에 이 문장이 한 줄로 없다: "{quote[:60]}"')

    # 검사에서 빠진 파일 언급을 센다. 0 이 "다 봤다"는 뜻이 되게 하는 부분이다.
    mentioned = {m.group(1) for m in MENTION.finditer(doc)}
    covered = {n for n, _ in checked} | {n for n, _ in QUOTE.findall(doc)}
    for name in sorted(mentioned - covered):
        path, problem = resolve(name)
        # 이름이 없어졌거나 모호한 것은 **실패**다. 심볼을 안 달았다는 이유로 이것까지 조용히
        # 넘기면, 코퍼스의 인용 대부분이 그 모양이라 가장 큰 썩음이 가장 조용해진다.
        if problem:
            if name not in said:
                said.add(name)
                bad.append(f"{where}: {problem}")
            continue
        # 문서가 문서를 가리키는 것(`docs/ARCHITECTURE.md`)은 파일 전체가 대상이라 정상이다.
        # 소스 파일을 심볼 없이 가리키는 것만 약한 손가락으로 남긴다.
        if path and not name.endswith(".md"):
            unchecked.append(f"{where}: `{name}` 를 심볼 없이 가리킨다")

    # 경로 없는 js 언급. [BARE] 가 일부러 안 잡는 것들인데, **안 잡는 것과 못 보는 것은
    # 다르다** — 그냥 두면 목업이 커질수록 사각지대가 같이 커지면서 총계는 그대로 0 이 된다.
    # 여기 한 줄로 서면 남의 라이브러리는 사람이 한 번 보고 넘기고, 우리 파일이면 경로를
    # 붙이라는 말이 된다. 실측: 지금 코퍼스에서 이 목록은 `Office.js` 한 줄이다.
    for name in sorted({m.group(1) for m in BAREJS.finditer(doc)}):
        unchecked.append(f"{where}: `{name}` 를 경로 없이 가리킨다 — 우리 파일이면 경로를 붙일 것")
    return len(checked), quotes


def main():
    bad, unchecked = [], []
    syms = quotes = 0
    for path in documents():
        s, q = check(prose(path), os.path.relpath(path, ROOT), bad, unchecked)
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
