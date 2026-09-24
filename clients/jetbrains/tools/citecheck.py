#!/usr/bin/env python3
"""클라이언트 문서 및 코드 주석에 포함된 소스 코드(Go/Kotlin 등) 참조의 유효성을 정적 검증합니다.

[도입 배경]
초기 설계 문서는 소스 코드의 특정 행 번호(`file:line`)를 인용하여 작성되었습니다.
그러나 코드 변경으로 인해 13개 인용이 즉시 불일치 상태가 되었습니다:
- `daemon.go:587` (WorkspaceKey 참조 의도) -> 실제 `typ = event.TypePermissionRequested`를 가리킴
- `main.go:1097` (디태치 주석 참조 의도) -> 실제 `if bound != nil`을 가리킴
인용된 기술적 사실 자체는 유효했으나 행 번호 지정 방식의 취약점이 노출되었습니다.
이에 따라 참조 방식을 '심볼(식별자) 명세'로 전면 전환하였으며, 심볼 이름 변경에 따른 불일치를 방지하기 위해 본 정적 검증 도구를 구축했습니다.

[CI 워크플로 연동]
본 스크립트는 `.github/workflows/ci.yml`과 `.github/workflows/test-jetbrains.yml` 양쪽 파이프라인에서 모두 실행됩니다.
Go 코어 변경 시 심볼명이 변경될 경우 `clients/jetbrains/**` 필터만 동작하는 `test-jetbrains.yml`에서는 이를 감지할 수 없으므로, 코어 변경을 감시하는 `ci.yml`에서도 검증을 병행합니다.

[4가지 검증 항목]
1. 행 번호 인용 금지: 본문 내 `파일명:행번호` 형식 인용 부재 확인 (규칙 자체를 설명하는 첫 `##` 이전 머리말 제외)
2. 심볼 존재 여부: `파일명`의 `심볼` 형태 참조 시 해당 파일 내 실제 식별자 정의 존재 검증
3. 인용구 일치 여부: `파일명`, "문장" 형태 인용 시 해당 파일 내 인용문(한 줄 기준) 존재 검증 (말줄임표는 각 조각 개별 검증)
4. 단독 파일명 해소: 경로 없는 파일명이 저장소 내에서 유일하게 식별되는지 검증 (`.js`/`.mjs`는 모호성 방지를 위해 경로 명시 필수)

[동일 파일명 충돌 방지]
`search.go`와 같이 복수 디렉터리(`clients/web/server`, `internal/app`)에 동일 파일명이 존재하는 경우 오탐을 방지하기 위해 경로 명시를 강제합니다.

[검증 누락 방지]
정규식 매핑에서 제외되거나 단독 언급된 파일 목록을 검사 종료 시점에 '검사 제외(Unchecked)'로 명시 출력하여, 실제 검증 범위와 단순 누락을 명확히 구분합니다.

오류 발견 시 종료 코드 1을 반환합니다.
"""
import os
import re
import subprocess
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__)))))
HERE = os.path.join(ROOT, "clients/jetbrains")

# Git 추적 대상 소스 파일만 검사합니다. 디렉터리 하드코딩 대신 git 메타데이터를 사용하여 신규 패키지 추가 시에도 자동으로 검증 대상에 포함됩니다.
SKIP_DIRS = {".git", "build", "node_modules", ".gradle", "vendor", "scratchpad", ".agents"}

# 파일명 백틱(`) 표기는 선택 사항입니다(마크다운은 백틱 사용, KDoc은 미사용 패턴 지원).
# `.js`/`.mjs`는 외부 라이브러리(예: `Office.js`)와의 식별자 충돌 방지를 위해 경로가 포함된 인용만 수집합니다.
# 룩비하인드에 `.`을 포함하여 `./UI.ko.md` 같은 상대 링크에서 `ko.md`가 유효하지 않은 파일로 오인식되는 오류를 방지합니다.
BARE = (r'(?<![\w:/.])((?:[A-Za-z_][A-Za-z_/.-]*\.(?:go|md|kt)'
        r'|[A-Za-z_][A-Za-z_.-]*(?:/[A-Za-z_.-]+)+\.(?:js|mjs)))(?![\w])')
# 심볼 정규식: 숫자를 포함하는 식별자(예: `sha256Of`)도 누락 없이 수집하도록 `\w`를 허용합니다.
# 쉼표(,)는 파일 목록 구분자이므로 파일명과 심볼 사이의 연결어에서 제외합니다.
SYMBOL = re.compile(r'`?' + BARE + r'`?[^`\n,]{0,14}`([A-Za-z_][\w.]*)`')
QUOTE = re.compile(r'`?' + BARE + r'`?[^`\n"]{0,30}"([^"\n]{12,120})"')
MENTION = re.compile(r'`?' + BARE + r'`?')
LINENO = re.compile(r'(?<![\w:/])[A-Za-z_][A-Za-z_/.-]*\.(?:go|md|kt|ts|js|mjs|yml):\d+')


def sources():
    """저장소의 소스 파일을 (basename, [경로…]) 형태로 색인합니다.

    Git이 추적하는 파일만 수집합니다. 디렉터리 트리를 직접 순회할 경우 `bench/harbor/state` 등 빌드/벤치마크 생성물이
    포함되어 `README.md`가 11개로 집계되고 정상적인 인용이 모호성 오류로 실패하는 문제를 방지합니다.
    """
    index = {}
    tracked = subprocess.run(["git", "ls-files", "-z"], cwd=ROOT, capture_output=True, text=True)
    if tracked.returncode != 0 or not tracked.stdout.strip():
        # git ls-files 실행 실패 시 즉시 종료합니다. 색인이 비어 있으면 모든 인용이 파일 미존재 오류로 실패하여 원인 파악이 왜곡되기 때문입니다.
        raise SystemExit(f"citecheck: git ls-files 가 아무것도 못 냈다 (rc={tracked.returncode})")
    for rel in tracked.stdout.split("\0"):
        if rel.endswith((".go", ".md", ".kt", ".js", ".mjs")):
            index.setdefault(os.path.basename(rel), []).append(os.path.join(ROOT, rel))
    return index


INDEX = sources()


def resolve(name):
    """인용 대상 파일 경로를 반환합니다. 반환값: (절대경로, 오류메시지) — 실패 시 경로는 None."""
    if "/" in name:
        p = os.path.join(ROOT, name)
        if os.path.isfile(p):
            return p, None
        # 접미 일치도 허용합니다: `clients/web/server/mcp.go` 처럼 저장소 루트 기준이 아닌 상대 경로 인용 지원.
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
        # 존재하지 않는 파일 참조는 실패로 처리합니다. 예외 처리로 조용히 넘길 경우
        # 파일 이동/삭제 시 해당 파일의 모든 심볼 인용이 검증 대상에서 누락되어 오류가 은폐되기 때문입니다.
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
    """검사 대상 텍스트를 추출합니다. Kotlin(.kt) 파일은 주석(KDoc 및 라인 주석)만 검사합니다.

    소스 코드 전체를 스캔할 경우 테스트 픽스처의 문자열 리터럴(예: `completeCode("a.kt", …)`)이
    파일 인용으로 오탐되는 현상을 방지하기 위함입니다.
    """
    body = read(path)
    if not path.endswith(".kt"):
        return body
    return "\n".join(m.group(0) for m in COMMENT.finditer(body))


def documents():
    # 연계 문서(`clients/powerpoint/DESIGN.md`) 및 메인 `README.md`를 검사 대상에 포함합니다.
    # 문서 간 절 번호 및 절 제목 인용의 일치성을 검사하여 번호 재할당이나 제목 변경 시의 불일치를 탐지합니다
    # (도입 시점 실측: BAD 0, UNCHECKED 18로 시작하여 잘못 인용된 절 제목 "5.4 죽으면 다시"를 즉시 탐지 및 수정).
    out = [os.path.join(ROOT, "clients/powerpoint/DESIGN.md"), os.path.join(HERE, "README.md")]
    # `docs/` 디렉터리 내 설계 문서들도 동일한 검증 대상에 포함합니다.
    docs = os.path.join(HERE, "docs")
    if os.path.isdir(docs):
        out += [os.path.join(docs, n) for n in sorted(os.listdir(docs)) if n.endswith(".md")]
    for base, dirs, names in os.walk(os.path.join(HERE, "plugin")):
        dirs[:] = [d for d in dirs if d not in SKIP_DIRS]
        out += [os.path.join(base, n) for n in sorted(names) if n.endswith(".kt")]
    return out


FENCE = re.compile(r"^```.*?^```", re.S | re.M)
BAREJS = re.compile(r'(?<![\w:/.-])([A-Za-z_][A-Za-z_.-]*\.(?:js|mjs))(?![\w])')


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
        if "…" in quote or "..." in quote:
            parts = [p.strip() for p in re.split(r"…|\.\.\.", quote) if p.strip()]
            ok = len(parts) > 0 and all(p in read(path) for p in parts)
        else:
            ok = quote in read(path)
        if not ok:
            if "…" in quote or "..." in quote:
                bad.append(f'{where}: {name} 에 이 인용 조각이 없다: "{quote[:60]}"')
            else:
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
    # 검출 결과 0건은 '파일명을 명시한 인용(심볼 및 인용구)'에 대해서만 검증 완료를 의미합니다.
    # 파일명 없이 식별자 이름만 단독 작성된 경우 [SYMBOL] 검사 대상(파일명+심볼 짝)에서 제외됩니다.
    # (문서 전체 백틱 식별자는 947개[2026-08-29 실측]에 달하나 대부분 `true`, 지역 변수, JSON 키 등 비인용 토큰이므로 전수 대조 시 노이즈 발생).
    # 저장소 전체 심볼 자동 매칭 방식 역시 115건의 오탐(`Office.EventType` 등 외부 API) 대비 실효 결함 탐지 0건이었으므로,
    # 파일명이 명시된 심볼만 엄격히 대조하고 검사 대상의 한계를 명문화합니다.
    print(f"파일을 댄 심볼 {syms}개 · 인용문 {quotes}개 검사 → 못 찾은 것 {len(bad)}"
          f" — 파일 없이 이름만 적은 인용은 검사 대상에서 제외")
    return 1 if bad else 0


if __name__ == "__main__":
    sys.exit(main())
