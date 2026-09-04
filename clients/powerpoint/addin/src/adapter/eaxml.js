/**
 * 동아시아 서체를 슬라이드 XML 에 쓴다.
 *
 * **Office.js 로는 못 한다.** `font.name` 은 라틴 서체(`a:latin`)만 바꾸고, 한글·중일 글자는
 * 테마의 동아시아 서체(`a:ea`)를 따른다. 그래서 한국어 덱에서는 글꼴을 몇 번을 걸어도 **눈에
 * 보이는 한글이 안 바뀐다** — 오늘 그 화면을 세 번 봤다(2026-09-04: 사람이 「왜 기본포맷이
 * 남아있냐」고 물었고, 되읽은 값은 `latin=Arial, hangul=맑은 고딕` 이었다).
 *
 * ⚠ **테마를 갈아 끼우는 길과 다르다.** 테마 부분을 통째로 바꾸는 것은 PowerPoint 가 되돌려
 * 놓는 것을 앞서 실측했다(TESTING §7b). 이 파일은 테마가 아니라 **슬라이드의 런 속성**에 쓴다 —
 * 명시적 서식이라 테마보다 세다. 되는지는 실물에서 재고 나서 쓴다.
 */

/** 런 속성이 열리는 자리들. 문단 기본(`a:defRPr`)과 빈 문단 끝(`a:endParaRPr`)까지 봐야 한다. */
const RPR = /<a:(rPr|defRPr|endParaRPr)\b([^>]*?)(\/>|>)/g;

/**
 * 이 XML 의 모든 런에 `<a:ea typeface="…"/>` 를 건다.
 *
 * - 이미 `a:ea` 가 있으면 **갈아 끼운다**(둘을 남기면 PowerPoint 가 앞의 것을 쓴다).
 * - 자기 닫는 태그(`<a:rPr … />`)는 열어서 자식을 넣는다.
 * - `a:latin` 이 있으면 **그 바로 뒤**에 둔다. OOXML 스키마의 자식 순서가 정해져 있어서
 *   아무 데나 넣으면 파일이 열리다 고쳐지거나 통째로 거부된다.
 *
 * @param {string} xml 슬라이드 파트 원문
 * @param {string} face 동아시아 서체 이름(예: "맑은 고딕", "본고딕")
 * @returns {{xml:string, runs:number}} 고친 원문과 손댄 런 수
 */
export function withEastAsianFont(xml, face) {
  const name = String(face ?? '').trim();
  if (!name) return { xml, runs: 0 };
  const tag = `<a:ea typeface="${escapeAttr(name)}"/>`;
  let runs = 0;

  const out = String(xml).replace(RPR, (whole, kind, attrs, close) => {
    runs += 1;
    if (close === '/>') {
      // 자식이 없던 런. 열어서 이것 하나만 넣는다.
      return `<a:${kind}${attrs}>${tag}</a:${kind}>`;
    }
    return `<a:${kind}${attrs}>`; // 여는 태그는 그대로 두고, 아래에서 자식을 손본다
  });

  // 자식은 태그 단위로 못 고치므로 블록으로 다시 훑는다. **여는 태그를 먼저 정규화해 두었으니**
  // 여기서는 열린 것만 상대하면 된다.
  const fixed = out.replace(/<a:(rPr|defRPr|endParaRPr)\b([^>]*)>([\s\S]*?)<\/a:\1>/g,
    (whole, kind, attrs, body) => {
      let inner = body.replace(/<a:ea\b[^>]*\/>/g, ''); // 옛것을 걷어낸다
      if (/<a:latin\b[^>]*\/>/.test(inner)) {
        inner = inner.replace(/(<a:latin\b[^>]*\/>)/, `$1${tag}`);
      } else {
        inner = tag + inner;
      }
      return `<a:${kind}${attrs}>${inner}</a:${kind}>`;
    });
  return { xml: fixed, runs };
}

/** 이 서체가 실제로 걸려 있는 런 수. 되읽어 확인하는 자리에서 쓴다. */
export function eastAsianRuns(xml, face) {
  const want = `typeface="${escapeAttr(String(face ?? '').trim())}"`;
  return (String(xml).match(/<a:ea\b[^>]*\/>/g) ?? []).filter((t) => t.includes(want)).length;
}

function escapeAttr(s) {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/"/g, '&quot;');
}
