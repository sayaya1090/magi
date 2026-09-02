/**
 * 애니메이션 — `<p:timing>` 을 짓는다.
 *
 * # 지어내지 않았다
 *
 * 이 파일의 모양은 **PowerPoint 가 직접 쓴 것을 읽어서** 왔다(2026-09-03). COM 으로 효과를
 * 걸고, 저장하고, 압축을 풀어 나온 XML 이 아래 함수들의 원본이다. 규격서만 보고 지었으면
 * `grpId` 나 `presetSubtype` 처럼 없어도 파일은 열리는데 PowerPoint 가 조용히 무시하는 칸을
 * 틀리게 채웠을 것이다.
 *
 * # 왜 spid 에 우리 도형 id 를 그대로 쓰나
 *
 * PowerPoint 에서는 **Office.js 의 `shape.id` 가 OOXML 의 `<p:cNvPr id>` 와 같다**(실측:
 * 같은 장에서 Office.js 가 2·3·4 를 주고 XML 도 2·3·4 였다). 그래서 짝지을 표가 필요 없다.
 * Excel 과 다르므로 여기 적어 둔다.
 *
 * # 들어오기만 있다
 *
 * 나가기·강조·이동 경로는 안 만든다. 사람이 부탁하는 것의 거의 전부가 「나타나게 해 줘」이고,
 * 나머지는 각각 다른 XML 을 요구한다 — **안 재 본 모양을 지어 넣느니 없다고 말한다.**
 */

import { removeElements, endOfElement } from './chartxml.js';

const esc = (s) => String(s)
  .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');

/**
 * 이 손이 아는 효과. 값은 PowerPoint 가 쓴 그대로다.
 *
 * `subtype` 은 방향 같은 것을 뜻하는데 효과마다 뜻이 달라서 이름을 못 붙인다 — **우리가 잰
 * 조합만** 둔다.
 */
export const ANIM_EFFECTS = [
  { id: 'appear', ko: '나타내기', presetID: 1, subtype: 0, kind: 'none' },
  { id: 'fade', ko: '흐리게', presetID: 10, subtype: 0, kind: 'filter', filter: 'fade' },
  { id: 'wipe', ko: '닦아내기', presetID: 22, subtype: 4, kind: 'filter', filter: 'wipe(down)' },
  { id: 'zoom', ko: '확대', presetID: 23, subtype: 16, kind: 'grow' },
];

export const EFFECT_NAMES = ANIM_EFFECTS.map((s) => s.id);
export const START_KINDS = ['on_click', 'with_previous', 'after_previous'];

/** 이름을 효과로. **모르는 이름은 아는 것을 알려 주고 던진다.** */
export function effectSpec(name) {
  const want = String(name ?? 'fade').trim().toLowerCase();
  const hit = ANIM_EFFECTS.find((s) => s.id === want || s.ko === String(name ?? '').trim());
  if (!hit) {
    throw new Error(`${name} 는 이 손이 아는 효과가 아닙니다 — 아는 것: `
      + `${ANIM_EFFECTS.map((s) => `${s.id}(${s.ko})`).join(', ')}. `
      + '들어오기 효과만 있습니다 — 나가기·강조·이동 경로는 못 겁니다');
  }
  return hit;
}

/** presetID 를 이름으로. **모르는 번호는 모른다고 말한다.** */
export function effectOfPreset(presetID) {
  return ANIM_EFFECTS.find((s) => s.presetID === Number(presetID))?.id ?? null;
}

const target = (spid, para) => (para == null
  ? `<p:spTgt spid="${esc(spid)}"/>`
  : `<p:spTgt spid="${esc(spid)}"><p:txEl><p:pRg st="${para}" end="${para}"/></p:txEl></p:spTgt>`);

const setVisible = (nid, tgt) => `<p:set><p:cBhvr><p:cTn id="${nid}" dur="1" fill="hold">`
  + '<p:stCondLst><p:cond delay="0"/></p:stCondLst></p:cTn>'
  + `<p:tgtEl>${tgt}</p:tgtEl>`
  + '<p:attrNameLst><p:attrName>style.visibility</p:attrName></p:attrNameLst></p:cBhvr>'
  + '<p:to><p:strVal val="visible"/></p:to></p:set>';

const filterBody = (nid, tgt, filter, dur) =>
  `<p:animEffect transition="in" filter="${esc(filter)}"><p:cBhvr>`
  + `<p:cTn id="${nid}" dur="${dur}"/><p:tgtEl>${tgt}</p:tgtEl></p:cBhvr></p:animEffect>`;

const growBody = (nid, tgt, attr, dur) =>
  '<p:anim calcmode="lin" valueType="num"><p:cBhvr>'
  + `<p:cTn id="${nid}" dur="${dur}" fill="hold"/><p:tgtEl>${tgt}</p:tgtEl>`
  + `<p:attrNameLst><p:attrName>${attr}</p:attrName></p:attrNameLst></p:cBhvr>`
  + '<p:tavLst><p:tav tm="0"><p:val><p:fltVal val="0"/></p:val></p:tav>'
  + `<p:tav tm="100000"><p:val><p:strVal val="#${attr}"/></p:val></p:tav></p:tavLst></p:anim>`;

/**
 * 걸음들을 **클릭 묶음**으로 나눈다.
 *
 * PowerPoint 의 모양이 그렇다: 한 번의 클릭에 같이 도는 것들이 한 묶음이고, 묶음마다
 * `<p:par>` 가 하나다. 「이전과 함께」는 앞 묶음에 얹히고, 「클릭」과 「이전 다음」은 새 묶음을
 * 연다.
 *
 * **첫 걸음은 언제나 제 묶음을 연다** — 앞이 없는데 「이전과 함께」라고 하면 얹을 데가 없다.
 */
export function clickGroups(steps) {
  const groups = [];
  for (const step of steps) {
    const start = step.start ?? 'on_click';
    if (groups.length === 0) {
      groups.push({ start: start === 'with_previous' ? 'on_click' : start, steps: [step] });
    } else if (start === 'with_previous') {
      groups[groups.length - 1].steps.push(step);
    } else {
      groups.push({ start, steps: [step] });
    }
  }
  return groups;
}

/**
 * `<p:timing>` 한 덩이. 걸음이 없으면 **빈 글**을 준다 — 그게 「애니메이션 없음」이다.
 *
 * 각 걸음: `{ spid, spec, start, duration, paragraph }`.
 */
export function timingXml(steps) {
  if (!steps || steps.length === 0) return '';
  let next = 3; // 1 = tmRoot, 2 = mainSeq
  const nid = () => { const n = next; next += 1; return n; };

  const groups = clickGroups(steps);
  const bodies = [];
  let prevDur = 0;
  for (const group of groups) {
    // 「이전 다음」의 기다림은 **앞 묶음이 도는 시간**이다. PowerPoint 가 그 값을 그렇게 적는다.
    const delay = group.start === 'after_previous' ? String(prevDur) : 'indefinite';
    const outer = nid();
    const inner = nid();
    const pars = group.steps.map((step, i) => {
      const { spec } = step;
      const dur = step.duration;
      const tgt = target(step.spid, step.paragraph);
      const node = i > 0 ? 'withEffect'
        : (group.start === 'after_previous' ? 'afterEffect' : 'clickEffect');
      const eid = nid();
      let body = setVisible(nid(), tgt);
      if (spec.kind === 'filter') body += filterBody(nid(), tgt, spec.filter, dur);
      else if (spec.kind === 'grow') {
        body += growBody(nid(), tgt, 'ppt_w', dur);
        body += growBody(nid(), tgt, 'ppt_h', dur);
      }
      return `<p:par><p:cTn id="${eid}" presetID="${spec.presetID}" presetClass="entr"`
        + ` presetSubtype="${spec.subtype}" fill="hold" grpId="0" nodeType="${node}">`
        + '<p:stCondLst><p:cond delay="0"/></p:stCondLst>'
        + `<p:childTnLst>${body}</p:childTnLst></p:cTn></p:par>`;
    }).join('');
    prevDur = Math.max(...group.steps.map((s) => s.duration));
    bodies.push(`<p:par><p:cTn id="${outer}" fill="hold">`
      + `<p:stCondLst><p:cond delay="${delay}"/></p:stCondLst><p:childTnLst>`
      + `<p:par><p:cTn id="${inner}" fill="hold">`
      + '<p:stCondLst><p:cond delay="0"/></p:stCondLst>'
      + `<p:childTnLst>${pars}</p:childTnLst></p:cTn></p:par>`
      + '</p:childTnLst></p:cTn></p:par>');
  }

  // 어느 도형이 문단별로 도는지 적어 두는 자리. PowerPoint 의 애니메이션 창이 이걸 읽는다.
  const byShape = new Map();
  for (const step of steps) {
    byShape.set(step.spid, (byShape.get(step.spid) ?? false) || step.paragraph != null);
  }
  const bld = [...byShape.entries()].map(([spid, byPara]) => (byPara
    ? `<p:bldP spid="${esc(spid)}" grpId="0" build="p"/>`
    : `<p:bldP spid="${esc(spid)}" grpId="0" animBg="1"/>`)).join('');

  return '<p:timing><p:tnLst><p:par>'
    + '<p:cTn id="1" dur="indefinite" restart="never" nodeType="tmRoot"><p:childTnLst>'
    + '<p:seq concurrent="1" nextAc="seek"><p:cTn id="2" dur="indefinite" nodeType="mainSeq">'
    + `<p:childTnLst>${bodies.join('')}</p:childTnLst></p:cTn>`
    + '<p:prevCondLst><p:cond evt="onPrev" delay="0"><p:tgtEl><p:sldTgt/></p:tgtEl></p:cond></p:prevCondLst>'
    + '<p:nextCondLst><p:cond evt="onNext" delay="0"><p:tgtEl><p:sldTgt/></p:tgtEl></p:cond></p:nextCondLst>'
    + '</p:seq></p:childTnLst></p:cTn></p:par></p:tnLst>'
    + `<p:bldLst>${bld}</p:bldLst></p:timing>`;
}

/**
 * 장 XML 에서 옛 타이밍을 걷어 내고 새것을 끼운다.
 *
 * `<p:timing>` 은 `</p:sld>` 바로 앞이다(규격의 차례가 cSld → clrMapOvr → transition → timing).
 * **넣을 것이 없으면 걷어 내기만 한다** — 그게 「애니메이션 지우기」다.
 */
export function withTiming(slideXml, timing) {
  const cleaned = String(slideXml)
    .replace(/<p:timing>[\s\S]*?<\/p:timing>/, '')
    .replace(/<p:timing\s*\/>/, '');
  if (!timing) return cleaned;
  // **`extLst` 앞이다.** 규격의 차례는 cSld → clrMapOvr → transition → timing → **extLst** 이고,
  // `</p:sld>` 바로 앞에 두면 그 마지막 칸을 넘어선다. 장에 `<p:extLst>` 가 있는 덱에서만
  // 어긋나는데, 거기 사는 것이 하필 M365 주석의 되짚기(`p188:commentRel`)다 — 주석 단 장에
  // 애니메이션을 걸면 다음에 열 때 「복구」 대화창이 뜬다(리뷰가 짚었다, 2026-09-03).
  const end = cleaned.lastIndexOf('</p:sld>');
  if (end < 0) throw new Error('슬라이드 XML 이 </p:sld> 로 안 끝납니다 — 애니메이션을 못 넣습니다');
  // **장의 `extLst` 만 본다.** `<p:extLst>` 는 `<p:cSld>` 안에도 있다(PowerPoint 가 거기에
  // `p14:creationId` 를 적는다). 아무거나 먼저 만난 것 앞에 끼우면 타이밍이 `cSld` 안으로
  // 들어가고, PowerPoint 는 **거절도 않고 조용히 버린다** — 실물에서 그 화면을 봤다
  // (2026-09-03): 「효과 1개를 걸었습니다」라고 답했는데 파일에는 타이밍이 없었다.
  // `<p:cSld/>` 로 닫힌 것도 있다 — 닫는 태그 글자로 찾으면 그런 장에서 못 찾고, 못 찾으면
  // 장의 `extLst` 를 통째로 놓친다. 짝을 맞춰 세는 쪽으로 끝을 잡는다.
  const open = cleaned.indexOf('<p:cSld');
  const body = open >= 0 ? endOfElement(cleaned, open, 'p:cSld') : -1;
  const ext = body > 0 ? cleaned.indexOf('<p:extLst>', body) : -1;
  const at = ext >= 0 && ext < end ? ext : end;
  return cleaned.slice(0, at) + timing + cleaned.slice(at);
}

/**
 * 장 XML 에서 **한 도형의 문단 수**를 센다.
 *
 * 그 도형의 `<p:cNvPr id="N"` 부터 다음 `<p:cNvPr id=` 까지를 그 도형의 몫으로 보고 `<a:p>` 를
 * 센다. **빈 문단도 센다** — `pRg` 의 번호는 빈 문단을 건너뛰지 않으므로, 안 세면 번호가 밀려
 * 엉뚱한 줄이 나타난다.
 */
export function paragraphCount(slideXml, spid) {
  const box = shapeBody(slideXml, spid);
  if (!box.kind || box.kind !== 'p:sp') return 0;
  // **글 상자의 몸통 안만 센다.** 앞 판본은 이 도형의 `cNvPr` 부터 다음 `cNvPr` 까지를 창으로
  // 삼았는데, 표(`p:graphicFrame`)는 `cNvPr` 이 하나라 모든 칸의 문단이 그 창에 들어왔고
  // (2×2 표에 걸음 넷을 걸고 「넷을 걸었습니다」라고 답했다), 묶음(`p:grpSp`)은 제 `cNvPr` 뒤에
  // 곧바로 자식들의 `cNvPr` 이 와서 글이 가득해도 0 이었다(리뷰, 2026-09-03).
  const body = box.xml.match(/<p:txBody>[\s\S]*<\/p:txBody>/);
  if (!body) return 0;
  const all = (body[0].match(/<a:p(?=[\s/>])/g) ?? []).length;
  // **글이 한 자도 없으면 문단이 없는 것으로 센다.** 빈 도형에도 `<a:p/>` 하나가 있어서, 안
  // 가르면 아무것도 안 나타나는 걸음을 하나 걸고 「걸었습니다」라고 답한다.
  const words = (body[0].match(/<a:t>([^<]*)<\/a:t>/g) ?? []).join('').replace(/<[^>]*>/g, '').trim();
  return words ? all : 0;
}

/**
 * 그 도형의 **원소 하나**를 통째로 떠 온다. 종류(`p:sp`·`p:graphicFrame`·`p:grpSp`·`p:pic`)도
 * 같이 준다 — 무엇이냐에 따라 할 수 있는 일이 다르고, **모르면 지어내지 않는다.**
 */
export function shapeBody(slideXml, spid) {
  const xml = String(slideXml);
  const kinds = ['p:sp', 'p:pic', 'p:graphicFrame', 'p:grpSp', 'p:cxnSp'];
  // **뒤로 훑어 찾지 않는다.** `lastIndexOf('<p:sp')` 는 `<p:spTree>` 를 잡는다 — 이름이
  // 접두사로 겹치는 태그가 있어서, 자리로만 찾으면 엉뚱한 원소를 뜬다.
  const tag = new RegExp(`<(${kinds.join('|')})(?=[\\s/>])`, 'g');
  const mark = `<p:cNvPr id="${spid}"`;
  let best = null;
  let m = tag.exec(xml);
  while (m !== null) {
    const kind = m[1];
    const end = endOfElement(xml, m.index, kind);
    if (end > 0) {
      const body = xml.slice(m.index, end);
      // **제일 안쪽 것이 그 도형이다.** 묶음도 자식의 이름표를 품으므로, 안 가르면 묶음 안의
      // 글 상자가 묶음으로 읽힌다.
      if (body.includes(mark) && (!best || body.length < best.xml.length)) best = { kind, xml: body };
    }
    m = tag.exec(xml);
  }
  return best ?? { kind: null, xml: '' };
}
/**
 * 걸려 있는 것을 읽는다.
 *
 * **모르는 것을 아는 척하지 않는다.** 우리가 짓지 않은 트리도 여기로 들어온다 — 사람이 손으로
 * 건 나가기 효과, 다른 도구가 만든 것. 아는 번호는 이름으로, 모르는 번호는 번호 그대로 준다.
 */
export function readTiming(slideXml) {
  const block = String(slideXml).match(/<p:timing>[\s\S]*?<\/p:timing>/);
  // 애니메이션이 없는 장에도 PowerPoint 는 빈 `<p:timing>`(tmRoot 하나)을 적어 둔다. 그래서
  // **덩이가 있다는 것만으로는 애니메이션이 있다고 못 말한다.**
  if (!block) return { has: false, steps: [], unparsed: 0 };
  // **조건부 껍데기의 대체본을 먼저 걷는다.** 같은 효과가 `mc:Choice` 와 `mc:Fallback` 에
  // 둘 다 실려 오면 두 번 세어져, 사람이 보는 것의 두 배를 모델에게 말하게 된다.
  const xml = removeElements(block[0], 'mc:Fallback');

  const steps = [];
  // **속성 차례에 안 매인다.** 앞 판본은 `id → presetID → presetClass → … → nodeType` 순서를
  // 그대로 정규식에 박아서, 줄바꿈이 하나만 끼어도·`nodeType` 이 없어도·차례가 달라도 **하나도**
  // 못 읽었다. 그러고는 「애니메이션 없음」이라고 답했고, 이어진 `animate_slide` 가 그것을
  // 지우면서 「지운 것 0개」라고 적었다(리뷰, 2026-09-03). PowerPoint 말고 다른 것이 만든 덱은
  // 얼마든지 다른 차례로 적는다.
  const open = /<p:cTn\b((?:"[^"]*"|'[^']*'|[^>"'])*)>/g;
  let m = open.exec(xml);
  let seen = 0;
  while (m !== null) {
    const attrs = m[1];
    // 효과 마디는 `presetClass` 를 든 것이다. 묶음 마디(`fill="hold"`)와 뿌리는 안 든다.
    const cls = attrs.match(/presetClass="([^"]*)"/)?.[1];
    if (cls) {
      seen += 1;
      const end = endOfElement(xml, m.index, 'p:cTn');
      const body = end > 0 ? xml.slice(m.index, end) : '';
      const tgt = body.match(/<p:tgtEl>([\s\S]*?)<\/p:tgtEl>/)?.[1] ?? '';
      const node = attrs.match(/nodeType="([^"]*)"/)?.[1] ?? null;
      const preset = attrs.match(/presetID="(\d+)"/)?.[1] ?? null;
      const para = tgt.match(/<p:pRg st="(\d+)"/)?.[1];
      // **길이도 준다.** 안 주면 `read_animation` 의 답을 그대로 `animate_slide` 에 되먹였을 때
      // 모든 효과가 기본 500ms 로 초기화된다 — 읽고 다시 걸라고 시켜 놓고 값을 안 주는 셈이다.
      const durs = [...body.matchAll(/\sdur="(\d+)"/g)].map((d) => Number(d[1])).filter((n) => n > 1);
      steps.push({
        effect: effectOfPreset(preset),
        preset_id: preset == null ? null : Number(preset),
        // `entr` 만 우리가 만든다. 다른 것이 보이면 **다른 것이라고 말한다.**
        kind: cls,
        start: { clickEffect: 'on_click', withEffect: 'with_previous', afterEffect: 'after_previous' }[node]
          ?? node ?? null,
        duration_ms: durs.length ? Math.max(...durs) : null,
        shape_id: tgt.match(/spid="([^"]+)"/)?.[1] ?? null,
        paragraph: para == null ? null : Number(para),
      });
    }
    m = open.exec(xml);
  }
  return { has: true, steps, unparsed: Math.max(0, seen - steps.length) };
}
