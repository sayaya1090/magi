// 저장소의 마크다운에 든 mermaid 도표가 아직 파싱되는가.
//
// 경위. 형제 문서의 도표 하나가 라벨 따옴표를 안 닫아 통째로 파스 에러를 내고 있었는데, 아무도
// 몰랐다 — 렌더된 그림을 보는 사람이 없으면 깨진 도표는 조용하다. 눈으로 훑는 것으로는 못 잡는
// 부류라(따옴표 하나다) 파서에게 묻는다.
//
// **플레이그라운드 문법과 다르다.** 이 검사가 쓰는 것은 mermaid 자신의 `parse()` 이고, 그것이
// 실제로 렌더링 전에 도는 것이다. 정규식으로 괄호를 세는 검사는 통과시키는 것을 이것은 잡는다.
//
// ⚠ **에러가 대는 줄 번호는 한 칸 밀릴 수 있다.** 여는 따옴표만 있으면 파서가 라벨을 닫는 `|` 를
// 문자열 안으로 삼키고 다음 줄에 가서야 못 맞춘다 — 실측된 사례에서 12행 결함을 13행으로
// 보고했다. 그래서 아래는 줄 번호를 그대로 옮기되 **블록의 첫 줄 위치를 같이 적는다**: 사람이
// 세어 볼 앵커가 필요하다.
//
// 브라우저를 안 띄운다. mermaid 11 의 `parse()` 는 Node 에서 DOM 없이 돈다(실측). 앞선 판본은
// Playwright 로 헤드리스 크로미움을 띄우고 CDN 에서 mermaid 를 받았는데, 그것은 몇 초짜리
// 레인에 못 붙는다 — 네트워크를 타고 브라우저를 받는다.
import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";

// DOM 을 하나 세운 뒤에 mermaid 를 들인다. 순서가 중요하다 — mermaid 는 임포트 시점에 DOMPurify 를
// 잡고, DOMPurify 는 그 시점의 `window` 로 자기를 만든다. 나중에 window 를 주면 이미 늦어서
// `DOMPurify.addHook is not a function` 이 난다(실측: 114개 중 92개가 그렇게 실패했다 — 도표가
// 아니라 환경이 낸 실패였다).
let mermaid;
try {
  const { JSDOM } = await import("jsdom");
  const dom = new JSDOM("<!doctype html><body></body>", { pretendToBeVisual: true });
  // `defineProperty` 로 넣는다. Node 24 의 `globalThis.navigator` 는 getter 뿐이라 대입이 던진다.
  for (const name of ["window", "document", "navigator", "HTMLElement", "DOMParser", "Node"]) {
    Object.defineProperty(globalThis, name, {
      value: name === "window" ? dom.window : dom.window[name],
      configurable: true,
      writable: true,
    });
  }
  mermaid = (await import("mermaid")).default;
} catch (e) {
  // 조용히 통과시키지 않고, **사유를 그대로 보인다.** 처음엔 메시지만 찍었는데 그러면 "의존이
  // 없다"가 모든 실패의 이름이 된다 — 실제로는 다른 것이 깨져도 그렇게 보였다.
  console.error("mermaidcheck: 준비가 안 됐다 — `cd tools/mermaidcheck && npm i`");
  console.error("  원인:", String(e.message ?? e).split("\n")[0]);
  process.exit(2);
}

// git 이 추적하는 것만 본다. 트리를 그냥 걸으면 bench 의 생성물이 딸려 온다 — citecheck.py 가
// 같은 데서 한 번 속았다.
const files = execFileSync("git", ["ls-files", "-z", "*.md"], { encoding: "utf8" })
  .split("\0")
  .filter(Boolean);

mermaid.initialize({ startOnLoad: false });

let blocks = 0;
const bad = [];
for (const file of files) {
  const src = readFileSync(file, "utf8");
  // 여는 펜스의 줄 번호를 같이 센다. "세 번째 블록"보다 "N행부터"가 찾기 쉽다.
  const re = /```mermaid[^\n]*\n([\s\S]*?)```/g;
  for (let m; (m = re.exec(src)); ) {
    blocks++;
    const line = src.slice(0, m.index).split("\n").length;
    try {
      await mermaid.parse(m[1]);
    } catch (e) {
      bad.push({ file, line, err: String(e.message ?? e).split("\n").slice(0, 3).join(" ") });
    }
  }
}

for (const b of bad) console.log(`  !! ${b.file}:${b.line} 부터의 블록 — ${b.err}`);
console.log(`마크다운 ${files.length}개 · mermaid 블록 ${blocks}개 → 파싱 실패 ${bad.length}`);
process.exit(bad.length ? 1 : 0);
