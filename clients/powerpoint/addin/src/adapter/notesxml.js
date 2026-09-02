/**
 * 발표자 노트를 **OOXML 로 짓는다.**
 *
 * # 왜 이 파일이 있나
 *
 * 객체 모델에는 노트를 만지는 문이 없다 — 1.8 에도, 1.10 에도. 매뉴얼은 그것을 「못 읽고 못
 * 쓴다」로 적어 왔고 그 절반(못 읽는다)은 여전히 맞다. 그런데 **쓰는 쪽은 길이 있었다**:
 * 차트·그림과 같은 길이다(§6.14).
 *
 * # 실물에서 잰 것
 *
 * 슬라이드를 뜨면(`exportAsBase64`) **노트가 없는 장에도 `notesMaster` 가 따라온다**
 * (2026-09-03 실측). 덱이 그것을 갖고 있기 때문이다. 그래서 우리가 지을 것은 노트 조각 하나와
 * 관계 두 줄뿐이고, 마스터·테마 같은 큰 것은 손댈 필요가 없다.
 *
 * 노트 조각의 모양도 PowerPoint 자신이 만든 것을 그대로 읽어 왔다 — 지어내지 않았다.
 *
 * # 세 자리표시자
 *
 * 노트 화면에는 셋이 있다: 슬라이드 그림(`sldImg`), 노트 본문(`body`), 쪽 번호(`sldNum`).
 * 본문만 우리 것이고 나머지 둘은 **자리만 잡아 둔다** — 빼면 노트 화면이 이상해진다.
 */

import { xmlText } from './chartxml.js';

/** 한 줄을 문단으로. 빈 줄도 문단이다 — 사람이 띄운 자리를 우리가 없애지 않는다. */
function para(line) {
  if (line === '') return '<a:p><a:endParaRPr lang="ko-KR"/></a:p>';
  return `<a:p><a:r><a:rPr lang="ko-KR" altLang="en-US" dirty="0"/>`
    + `<a:t>${xmlText(line)}</a:t></a:r></a:p>`;
}

/**
 * 노트 조각(`ppt/notesSlides/notesSlideN.xml`) 한 벌.
 *
 * @param {string} text 노트 본문. 줄바꿈이 문단이 된다.
 */
export function notesPart(text) {
  const lines = String(text ?? '').replace(/\r\n?/g, '\n').split('\n');
  const body = lines.map(para).join('');
  return '<?xml version="1.0" encoding="UTF-8" standalone="yes"?>'
    + '<p:notes xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"'
    + ' xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"'
    + ' xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main">'
    + '<p:cSld><p:spTree>'
    + '<p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr>'
    + '<p:grpSpPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="0" cy="0"/>'
    + '<a:chOff x="0" y="0"/><a:chExt cx="0" cy="0"/></a:xfrm></p:grpSpPr>'
    // 슬라이드 그림 자리. 우리 것이 아니지만 **자리는 잡아 둔다.**
    + '<p:sp><p:nvSpPr><p:cNvPr id="2" name="Slide Image Placeholder 1"/>'
    + '<p:cNvSpPr><a:spLocks noGrp="1" noRot="1" noChangeAspect="1"/></p:cNvSpPr>'
    + '<p:nvPr><p:ph type="sldImg"/></p:nvPr></p:nvSpPr><p:spPr/></p:sp>'
    // 노트 본문 — 여기만 우리가 채운다.
    + '<p:sp><p:nvSpPr><p:cNvPr id="3" name="Notes Placeholder 2"/>'
    + '<p:cNvSpPr><a:spLocks noGrp="1"/></p:cNvSpPr>'
    + '<p:nvPr><p:ph type="body" idx="1"/></p:nvPr></p:nvSpPr><p:spPr/>'
    + `<p:txBody><a:bodyPr/><a:lstStyle/>${body}</p:txBody></p:sp>`
    // 쪽 번호 자리.
    + '<p:sp><p:nvSpPr><p:cNvPr id="4" name="Slide Number Placeholder 3"/>'
    + '<p:cNvSpPr><a:spLocks noGrp="1"/></p:cNvSpPr>'
    + '<p:nvPr><p:ph type="sldNum" sz="quarter" idx="5"/></p:nvPr></p:nvSpPr><p:spPr/></p:sp>'
    + '</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:notes>';
}

/**
 * 노트 조각이 가리켜야 할 것들 — 마스터와 자기 슬라이드.
 *
 * 실물에서 읽은 그대로다(2026-09-03): `notesMaster` 와 `slide` 둘.
 */
export function notesRels(slideName, masterName) {
  return '<?xml version="1.0" encoding="UTF-8" standalone="yes"?>'
    + '<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">'
    + '<Relationship Id="rId1"'
    + ' Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/notesMaster"'
    + ` Target="../notesMasters/${masterName}"/>`
    + '<Relationship Id="rId2"'
    + ' Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide"'
    + ` Target="../slides/${slideName}"/>`
    + '</Relationships>';
}

/**
 * 이미 있는 노트 조각의 **본문만** 갈아 끼운다.
 *
 * 장에 노트가 이미 있으면 조각을 새로 짓지 않는다 — PowerPoint 가 만든 것에는 우리가 모르는
 * 서식이 붙어 있을 수 있고, 통째로 갈아 치우면 그것이 조용히 사라진다. 본문만 바꾸는 쪽이
 * **사람이 해 둔 것을 덜 부순다.**
 */
export function withNotesText(notesXml, text) {
  const lines = String(text ?? '').replace(/\r\n?/g, '\n').split('\n');
  const body = lines.map(para).join('');
  // 본문 자리표시자(`type="body"`)를 담은 `p:sp` 안의 `p:txBody` 를 찾는다.
  const at = notesXml.indexOf('<p:ph type="body"');
  if (at < 0) {
    throw new Error('이 노트 조각에 본문 자리가 없습니다 — 모양이 예상과 다릅니다');
  }
  const open = notesXml.indexOf('<p:txBody>', at);
  const close = notesXml.indexOf('</p:txBody>', open);
  if (open < 0 || close < 0) {
    throw new Error('이 노트 조각의 본문 모양이 예상과 다릅니다 — p:txBody 를 못 찾았습니다');
  }
  return notesXml.slice(0, open)
    + `<p:txBody><a:bodyPr/><a:lstStyle/>${body}`
    + notesXml.slice(close);
}

/**
 * 노트 조각에서 **글만** 읽어 온다.
 *
 * 읽기는 원래 「못 하는 것」에 적혀 있었다. 객체 모델로는 지금도 못 하지만, 뜬 꾸러미에는
 * 들어 있으므로 **꺼내 읽을 수는 있다** — 그 사실을 「없다」로 적으면 안 된다.
 */
export function notesTextOf(notesXml) {
  const at = notesXml.indexOf('<p:ph type="body"');
  if (at < 0) return '';
  const open = notesXml.indexOf('<p:txBody>', at);
  const close = notesXml.indexOf('</p:txBody>', open);
  if (open < 0 || close < 0) return '';
  const body = notesXml.slice(open, close);
  // 문단마다 한 줄. 한 문단이 여러 조각(`a:r`)으로 쪼개져 있을 수 있으므로 이어 붙인다 —
  // PowerPoint 는 한글과 영문이 섞이면 조각을 나눈다(실물에서 본 그대로).
  return body.split('<a:p>').slice(1).map((p) => {
    const runs = [...p.matchAll(/<a:t>([\s\S]*?)<\/a:t>/g)].map((m) => m[1]);
    return runs.join('')
      .replace(/&lt;/g, '<').replace(/&gt;/g, '>')
      .replace(/&quot;/g, '"').replace(/&apos;/g, "'")
      .replace(/&amp;/g, '&');
  }).join('\n').replace(/\n+$/, '');
}

/** 아직 안 쓰인 노트 조각 이름. 차트·그림과 같은 이유다(zip 에 같은 이름이 둘이면 안 열린다). */
export function freeNotesName(names) {
  const used = new Set(names);
  for (let i = 1; i < 1000; i += 1) {
    const name = `ppt/notesSlides/notesSlide${i}.xml`;
    if (!used.has(name)) {
      return { part: name, rels: `ppt/notesSlides/_rels/notesSlide${i}.xml.rels`, target: `../notesSlides/notesSlide${i}.xml`, at: `/${name}` };
    }
  }
  throw new Error('이 장에 노트 조각이 너무 많아 새 이름을 못 지었습니다');
}
