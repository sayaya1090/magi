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
  const at = cleaned.lastIndexOf('</p:sld>');
  if (at < 0) throw new Error('슬라이드 XML 이 </p:sld> 로 안 끝납니다 — 애니메이션을 못 넣습니다');
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
  const xml = String(slideXml);
  const head = xml.indexOf(`<p:cNvPr id="${spid}"`);
  if (head < 0) return 0;
  const nextAt = xml.indexOf('<p:cNvPr id=', head + 1);
  const window = xml.slice(head, nextAt < 0 ? xml.length : nextAt);
  // **빈 문단은 `<a:p/>` 로 쓰인다.** 여는 태그만 찾으면 그것을 놓치고, 놓치면 번호가 밀려
  // 엉뚱한 줄이 나타난다 — 시험이 이걸 잡았다(2026-09-03).
  return (window.match(/<a:p(?=[\s/>])/g) ?? []).length;
}

/**
 * 걸려 있는 것을 읽는다.
 *
 * **모르는 것을 아는 척하지 않는다.** 우리가 짓지 않은 트리도 여기로 들어온다 — 사람이 손으로
 * 건 나가기 효과, 다른 도구가 만든 것. 아는 번호는 이름으로, 모르는 번호는 번호 그대로 준다.
 */
export function readTiming(slideXml) {
  const block = String(slideXml).match(/<p:timing>[\s\S]*?<\/p:timing>/);
  if (!block) return { has: false, steps: [] };
  const steps = [];
  const re = /<p:cTn id="\d+" presetID="(\d+)" presetClass="(\w+)"[^>]*nodeType="(\w+)"[^>]*>([\s\S]*?)<p:tgtEl>([\s\S]*?)<\/p:tgtEl>/g;
  let m = re.exec(block[0]);
  while (m !== null) {
    const [, preset, cls, node, , tgt] = m;
    const para = tgt.match(/<p:pRg st="(\d+)"/)?.[1];
    steps.push({
      effect: effectOfPreset(preset),
      preset_id: Number(preset),
      // `entr` 만 우리가 만든다. 다른 것이 보이면 **다른 것이라고 말한다.**
      kind: cls,
      start: { clickEffect: 'on_click', withEffect: 'with_previous', afterEffect: 'after_previous' }[node] ?? node,
      shape_id: tgt.match(/spid="([^"]+)"/)?.[1] ?? null,
      paragraph: para == null ? null : Number(para),
    });
    m = re.exec(block[0]);
  }
  return { has: true, steps };
}
