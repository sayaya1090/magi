/**
 * **수정 제안** — 덱 안에 남고, 누르면 고쳐지고, 고쳐지면 사라진다.
 *
 * # 왜 이게 따로 있나
 *
 * 이미 셋이 있다. 헷갈리기 쉬우니 한 자리에 적어 둔다.
 *
 * | | 어디 사나 | 누가 보나 | 언제까지 |
 * |---|---|---|---|
 * | `advise`(포스트잇) | 작업창 | 사람 | **이 대화 동안만** |
 * | 발표자 노트 | 덱 | 사람 — 발표자 화면·유인물 | 영영 |
 * | 태그 | 덱 | **아무도** — 에이전트의 기억 | 영영 |
 * | **제안** | 덱(태그로) | 사람 — 작업창의 카드 | **고칠 때까지** |
 *
 * 사람이 원한 것은 워드의 주석이다: 「여기 이렇게 고치는 게 좋겠습니다」가 **문서에 붙어 있고**,
 * 나중에 열어도 있고, 받아들이면 고쳐지면서 없어진다. 포스트잇은 대화가 끝나면 사라지고, 노트는
 * 발표할 때 화면에 뜬다. 그래서 새 자리가 필요했다.
 *
 * # 왜 태그에 담나
 *
 * PowerPoint 주석은 Office.js 에 문이 아예 없다. 태그는 **파일에 남고, 화면에 안 나오고,
 * 저장·종료·재열기를 넘어 남는다**(§6.18 에서 실측). 제안이 필요로 하는 성질이 정확히 그것이다.
 *
 * # 무엇을 고칠지는 **제안의 글이 아니라 제안의 손이 정한다**
 *
 * 카드에 적히는 「무엇을 합니다」는 \`fix\` 의 도구와 인자에서 **우리가 지어낸다.** 제안이 스스로
 * 적어 둔 설명(\`what\`)은 그 자리에 안 쓴다.
 *
 * 남이 준 덱에는 남이 넣은 제안이 들어 있을 수 있고, 그 글은 우리 도구에게 말을 거는 글일 수
 * 있다(§6.13). 「제목을 크게 하겠습니다」라고 적어 놓고 \`delete_slide\` 를 달아 둘 수 있다는
 * 뜻이다. 설명을 손에서 뽑으면 그 거짓말이 **카드에 그대로 드러난다.**
 */

/** 제안 키의 앞머리. 워드에서는 settings(WordApi 1.4)에 산다 — 2021(1.3)에는 제안이 없다. */
export const FIX_PREFIX = 'MAGI.FIX.';

/**
 * 제안이 달 수 있는 손.
 *
 * **좁게 연다.** 여기 없는 것은 제안으로 못 건다 — 덱 하나를 통째로 바꾸는 것(`apply_style`),
 * 장을 지우는 것(`delete_slide`), 장을 더 만드는 것은 누름 한 번에 일어나면 안 되는 일이다.
 * 사람이 카드를 꼼꼼히 읽지 않는다는 전제로 고른 목록이다.
 */
export const FIXABLE = new Map([
  ['replace_paragraph', (a) => `문단 ${a.paragraph ?? '?'} 의 글을 바꿉니다 — 「${short(a.text ?? a.content)}」`],
  ['format_text', (a) => `${at(a)} 의 글자 서식을 바꿉니다 (${styleWords(a)})`],
  ['format_paragraph', (a) => `${at(a)} 의 문단 서식을 바꿉니다 (${paraWords(a)})`],
  ['set_style', (a) => `${at(a)} 에 스타일 「${a.style ?? a.builtin ?? '?'}」 를 겁니다`],
  ['replace_all', (a) => `「${short(a.find)}」 를 「${short(a.replace)}」 로 바꿉니다${a.from ? ` (${at(a)})` : ''}`],
  ['insert_paragraphs', (a) => `${a.after ? `문단 ${a.after} 뒤에` : a.before ? `문단 ${a.before} 앞에` : '본문 끝에'} 문단 ${Array.isArray(a.lines) ? a.lines.length : '?'}개를 넣습니다`],
]);
const at = (a) => (a.from == null ? '본문 전체' : a.to == null || a.to === a.from ? `문단 ${a.from}` : `문단 ${a.from}–${a.to}`);
const paraWords = (a) => [a.align && `정렬 ${a.align}`, a.space_before != null && `앞 ${a.space_before}pt`, a.space_after != null && `뒤 ${a.space_after}pt`,
  a.line_spacing != null && `줄 간격 ${a.line_spacing}`, a.left_indent != null && `왼쪽 들여쓰기 ${a.left_indent}pt`].filter(Boolean).join(', ') || '바꿀 것 없음';

const short = (s, n = 30) => {
  const t = String(s ?? '').replace(/\s+/g, ' ').trim();
  return t.length > n ? `${t.slice(0, n)}…` : t;
};

const styleWords = (a) => [a.bold != null && (a.bold ? '굵게' : '굵게 해제'), a.italic != null && (a.italic ? '기울임' : '기울임 해제'),
  a.underline && `밑줄 ${a.underline}`, a.strike != null && '취소선', a.size != null && `크기 ${a.size}`, a.color && `색 ${a.color}`,
  a.highlight && `형광 ${a.highlight}`, a.font && `글꼴 ${a.font}`, a.text && `「${short(a.text)}」 만`].filter(Boolean).join(', ') || '바꿀 것 없음';


/**
 * **카드에 적을 한 줄.** 제안의 글이 아니라 **손**에서 뽑는다.
 *
 * 손이 없으면 「고칠 손이 안 달렸습니다」라고 적는다 — 그 카드는 읽히기만 하고 안 눌린다.
 * 아는 손이 아니면 **이름을 그대로** 적는다. 지어내면 사람은 그것을 우리가 아는 일로 읽는다.
 */
export function fixLabel(fix) {
  if (!fix || !fix.tool) return { text: '고칠 손이 안 달렸습니다 — 읽고 직접 고치세요', can: false };
  const make = FIXABLE.get(fix.tool);
  if (!make) {
    return {
      text: `이 제안은 '${fix.tool}' 을 부르려 합니다 — 제안으로 누를 수 있는 손이 아닙니다`,
      can: false,
    };
  }
  try {
    return { text: make(fix.args ?? {}), can: true };
  } catch {
    // 인자가 우리가 기대한 모양이 아니다. **그 사실이 카드에 뜬다** — 눌러 보고 알면 늦다.
    return { text: `'${fix.tool}' 의 인자를 못 읽었습니다 — 누를 수 없습니다`, can: false };
  }
}

/** 새 제안의 이름. 대문자·숫자만 쓴다 — PowerPoint 가 어차피 대문자로 바꾼다. */
export function freeFixKey(taken, now = Date.now(), rand = Math.random) {
  const seed = `${now.toString(36)}${Math.floor(rand() * 1e6).toString(36)}`.toUpperCase();
  let key = FIX_PREFIX + seed;
  let n = 1;
  while (taken.includes(key)) { key = `${FIX_PREFIX + seed}-${n}`; n += 1; }
  return key;
}

/**
 * 제안 하나를 태그 값으로.
 *
 * **읽을 수 있게 담는다.** 사람이 PowerPoint 없이 파일을 열어 봐도 무엇이 적혀 있는지 알아야
 * 한다 — 짧게 줄인 키(`w`·`y`)로 담으면 몇 바이트를 아끼고 그 성질을 잃는다. 태그 값에는
 * 실질적인 길이 한계가 없다(2026-09-03 실측: 2만 자가 그대로 왕복했다).
 */
export function encodeFix({ what, why, fix }) {
  const body = { what: String(what ?? '').trim() };
  if (why) body.why = String(why).trim();
  if (fix && fix.tool) body.fix = { tool: String(fix.tool), args: fix.args ?? {} };
  return JSON.stringify(body);
}

/**
 * 태그 값을 제안으로. **못 읽으면 못 읽었다고 말한다.**
 *
 * 남이 넣은 것, 손으로 고친 것, 옛 판본이 넣은 것이 여기로 들어온다. 던지면 그 장의 제안이
 * 통째로 안 보이므로, 한 줄로 만들어 카드에 올린다 — 사람이 그 태그를 지울 수 있어야 한다.
 */
export function decodeFix(key, value, where = {}) {
  const base = { key, paragraph: where.paragraph ?? null };
  let body = null;
  try { body = JSON.parse(String(value)); } catch { body = null; }
  if (!body || typeof body !== 'object' || typeof body.what !== 'string' || !body.what.trim()) {
    return { ...base, what: '읽을 수 없는 제안입니다', why: '', fix: null, broken: true };
  }
  return {
    ...base,
    what: body.what.trim(),
    why: typeof body.why === 'string' ? body.why.trim() : '',
    fix: body.fix && typeof body.fix === 'object' && body.fix.tool
      ? { tool: String(body.fix.tool), args: body.fix.args ?? {} }
      : null,
    broken: false,
  };
}

/** 이 키가 제안인가. */
export const isFixKey = (key) => String(key ?? '').toUpperCase().startsWith(FIX_PREFIX);

/**
 * `read_tags` 한 판을 제안 목록으로.
 *
 * 슬라이드에 붙은 것과 도형에 붙은 것을 **같이** 낸다 — 사람에게는 「이 장의 제안」 하나다.
 */
export function suggestionsOf(read) {
  const out = [];
  const where = { slide: read?.slide ?? null, slideId: read?.slide_id ?? null };
  for (const t of read?.tags ?? []) {
    if (isFixKey(t.key)) out.push(decodeFix(t.key, t.value, where));
  }
  for (const sh of read?.shapes ?? []) {
    for (const t of sh.tags ?? []) {
      if (isFixKey(t.key)) out.push(decodeFix(t.key, t.value, { ...where, shapeId: sh.shape_id }));
    }
  }
  return out;
}
