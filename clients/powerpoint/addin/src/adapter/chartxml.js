/**
 * 네이티브 차트 하나를 **OOXML 로 짓는다.**
 *
 * # 왜 이 파일이 있나
 *
 * 1.8 의 객체 모델에는 차트를 넣는 문이 없다. 매뉴얼은 한동안 그것을 「불가능」이라고 적었는데,
 * 절반만 맞았다 — 덱에 **슬라이드를 통째로 넣는 문**은 열려 있고(`insertSlidesFromBase64`),
 * `.pptx` 는 zip 이고, 우리는 이제 zip 을 쓸 줄 안다(`zipwrite.js`). 실물에서 왕복이 통하는 것도
 * 확인했다(2026-09-02).
 *
 * 그래서 남은 일은 **차트 부품을 짓는 것**뿐이고, 그게 이 파일이다.
 *
 * # 왜 그림이 아니라 차트인가
 *
 * 사각형을 여러 개 그려도 막대그래프처럼 보이게 만들 수는 있다. 그런데 그건 **덫**이다 — 나중에
 * 숫자 하나를 고치려는 사람이 사각형을 손으로 끌어야 하고, 그때야 그게 차트가 아니었다는 것을
 * 안다. 이 저장소가 제일 싫어하는 모양(그럴듯한데 아닌 것)이라, 할 거면 진짜를 넣는다.
 *
 * # 데이터 시트를 안 넣는다
 *
 * PowerPoint 의 차트는 보통 `.xlsx` 를 하나 품고 다니고, 「데이터 편집」이 그것을 연다. 우리는
 * 그것을 안 만든다 — 대신 **값을 차트 안에 캐시로 박는다**(`c:numCache`·`c:strCache`). 차트는
 * 제대로 그려지고 서식도 다 만질 수 있지만, 「데이터 편집」은 품은 시트가 없다고 말한다.
 *
 * **그 사실을 결과가 적는다.** 안 적으면 사람은 편집을 눌러 보고 나서야 알게 되고, 그때는 이미
 * 그 차트로 발표 자료를 다 만든 뒤다.
 */

/** XML 에 넣을 수 있게 다듬는다. 다섯만 바꾸면 된다. */
export function xmlText(s) {
  return String(s ?? '')
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&apos;');
}

/** 이 파일이 아는 차트 종류. **모르는 것은 갈음하지 않는다.** */
export const CHART_KINDS = new Map([
  ['bar', { tag: 'barChart', dir: 'col', ko: '세로 막대' }],
  ['column', { tag: 'barChart', dir: 'col', ko: '세로 막대' }],
  ['막대', { tag: 'barChart', dir: 'col', ko: '세로 막대' }],
  ['세로막대', { tag: 'barChart', dir: 'col', ko: '세로 막대' }],
  ['hbar', { tag: 'barChart', dir: 'bar', ko: '가로 막대' }],
  ['가로막대', { tag: 'barChart', dir: 'bar', ko: '가로 막대' }],
  ['line', { tag: 'lineChart', ko: '꺾은선' }],
  ['꺾은선', { tag: 'lineChart', ko: '꺾은선' }],
  ['선', { tag: 'lineChart', ko: '꺾은선' }],
  ['pie', { tag: 'pieChart', ko: '원' }],
  ['원', { tag: 'pieChart', ko: '원' }],
  ['파이', { tag: 'pieChart', ko: '원' }],
]);

/** 이름을 종류로. 못 알아들으면 **아는 것을 알려 주고 던진다.** */
export function chartKind(name) {
  const key = String(name ?? '').toLowerCase().replace(/[\s_-]/g, '');
  const got = CHART_KINDS.get(key);
  if (!got) {
    // 아는 이름을 한 번씩만 보여 준다 — 별명을 다 늘어놓으면 읽히지 않는다.
    const shown = [...new Set([...CHART_KINDS.values()].map((v) => v.ko))].join(', ');
    throw new Error(`${name} 는 이 손이 아는 차트가 아닙니다 — 아는 것: ${shown}`
      + ' (bar/column·hbar·line·pie 로도 부를 수 있습니다)');
  }
  return got;
}

/** 값 하나를 캐시 점으로. */
function pt(i, v) {
  return `<c:pt idx="${i}"><c:v>${xmlText(v)}</c:v></c:pt>`;
}

/**
 * 계열 하나.
 *
 * `c:f` 는 값이 원래 있던 자리를 가리키는 주소다. 품은 시트가 없어도 **주소는 있어야** 한다 —
 * 없으면 PowerPoint 가 계열을 못 읽는다. 있지도 않은 시트를 가리키는 것이 이상해 보이지만,
 * 이게 캐시만 든 차트의 정상적인 모양이다.
 */
function series(i, name, cats, vals, kind) {
  const col = String.fromCharCode(66 + i);   // B, C, D…
  const n = cats.length;
  const catRef = `<c:cat><c:strRef><c:f>Sheet1!$A$2:$A$${n + 1}</c:f>`
    + `<c:strCache><c:ptCount val="${n}"/>${cats.map((c, k) => pt(k, c)).join('')}</c:strCache>`
    + `</c:strRef></c:cat>`;
  const valRef = `<c:val><c:numRef><c:f>Sheet1!$${col}$2:$${col}$${n + 1}</c:f>`
    + `<c:numCache><c:formatCode>General</c:formatCode><c:ptCount val="${n}"/>`
    + `${vals.map((v, k) => pt(k, Number(v))).join('')}</c:numCache>`
    + `</c:numRef></c:val>`;
  const tx = `<c:tx><c:strRef><c:f>Sheet1!$${col}$1</c:f>`
    + `<c:strCache><c:ptCount val="1"/>${pt(0, name)}</c:strCache></c:strRef></c:tx>`;
  // 원 차트에는 축이 없고 `invertIfNegative` 도 안 쓴다.
  const barOnly = kind.tag === 'barChart' ? '<c:invertIfNegative val="0"/>' : '';
  const lineOnly = kind.tag === 'lineChart' ? '<c:smooth val="0"/>' : '';
  return `<c:ser><c:idx val="${i}"/><c:order val="${i}"/>${tx}${barOnly}${catRef}${valRef}${lineOnly}</c:ser>`;
}

/**
 * 차트 부품(`ppt/charts/chart1.xml`) 한 벌.
 *
 * @param {{kind: string, title?: string, categories: string[],
 *          series: {name: string, values: number[]}[]}} spec
 */
export function chartPart(spec) {
  const kind = chartKind(spec.kind);
  const cats = (spec.categories ?? []).map((c) => String(c));
  const sets = spec.series ?? [];
  if (cats.length === 0) throw new Error('가로축에 놓을 항목이 하나도 없습니다 — categories 를 주세요');
  if (sets.length === 0) throw new Error('그릴 값이 하나도 없습니다 — series 를 주세요');
  for (const s of sets) {
    if ((s.values ?? []).length !== cats.length) {
      // **길이가 다르면 지어내지 않는다.** 모자란 자리를 0 으로 채우면 그래프에 없는 골이 생기고,
      // 사람은 그것을 자료로 읽는다.
      throw new Error(`계열 "${s.name}" 의 값이 ${(s.values ?? []).length}개인데 `
        + `항목은 ${cats.length}개입니다 — 수가 같아야 합니다`);
    }
  }
  if (kind.tag === 'pieChart' && sets.length > 1) {
    throw new Error('원 차트는 계열을 하나만 그립니다 — 여럿을 견주려면 막대나 꺾은선을 쓰세요');
  }

  const title = spec.title
    ? `<c:title><c:tx><c:rich><a:bodyPr/><a:lstStyle/><a:p><a:r><a:t>${xmlText(spec.title)}</a:t></a:r></a:p>`
      + `</c:rich></c:tx><c:overlay val="0"/></c:title><c:autoTitleDeleted val="0"/>`
    : '<c:autoTitleDeleted val="1"/>';

  const sers = sets.map((s, i) => series(i, s.name ?? `계열 ${i + 1}`, cats, s.values, kind)).join('');

  // 축 id 는 이 문서 안에서만 유일하면 된다.
  const axes = kind.tag === 'pieChart' ? '' : '<c:axId val="111111111"/><c:axId val="222222222"/>';
  const body = kind.tag === 'barChart'
    ? `<c:barChart><c:barDir val="${kind.dir}"/><c:grouping val="clustered"/>`
      + `<c:varyColors val="0"/>${sers}<c:gapWidth val="150"/>${axes}</c:barChart>`
    : kind.tag === 'lineChart'
      ? `<c:lineChart><c:grouping val="standard"/><c:varyColors val="0"/>${sers}`
        + `<c:marker val="1"/>${axes}</c:lineChart>`
      : `<c:pieChart><c:varyColors val="1"/>${sers}<c:firstSliceAng val="0"/></c:pieChart>`;

  const axisParts = kind.tag === 'pieChart' ? '' : (
    '<c:catAx><c:axId val="111111111"/><c:scaling><c:orientation val="minMax"/></c:scaling>'
    + '<c:delete val="0"/><c:axPos val="b"/><c:crossAx val="222222222"/></c:catAx>'
    + '<c:valAx><c:axId val="222222222"/><c:scaling><c:orientation val="minMax"/></c:scaling>'
    + '<c:delete val="0"/><c:axPos val="l"/><c:majorGridlines/><c:crossAx val="111111111"/></c:valAx>'
  );

  return '<?xml version="1.0" encoding="UTF-8" standalone="yes"?>'
    + '<c:chartSpace xmlns:c="http://schemas.openxmlformats.org/drawingml/2006/chart"'
    + ' xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"'
    + ' xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">'
    + '<c:chart>'
    + title
    + `<c:plotArea><c:layout/>${body}${axisParts}</c:plotArea>`
    + (sets.length > 1 || kind.tag === 'pieChart'
      ? '<c:legend><c:legendPos val="b"/><c:overlay val="0"/></c:legend>' : '')
    + '<c:plotVisOnly val="1"/><c:dispBlanksAs val="gap"/>'
    + '</c:chart></c:chartSpace>';
}

/**
 * 슬라이드에 놓을 **틀**(`p:graphicFrame`). 차트는 도형이 아니라 이 틀에 담긴다.
 *
 * 단위는 EMU 다 — 1pt = 12700 EMU. 사람과 도구는 pt 로 말하므로 여기서만 바꾼다.
 */
export function chartFrame({ id, name, relId, left, top, width, height }) {
  const emu = (pt2) => Math.round(Number(pt2) * 12700);
  return `<p:graphicFrame><p:nvGraphicFramePr>`
    + `<p:cNvPr id="${id}" name="${xmlText(name)}"/><p:cNvGraphicFramePr/><p:nvPr/>`
    + `</p:nvGraphicFramePr>`
    + `<p:xfrm><a:off x="${emu(left)}" y="${emu(top)}"/>`
    + `<a:ext cx="${emu(width)}" cy="${emu(height)}"/></p:xfrm>`
    + `<a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/chart">`
    + `<c:chart xmlns:c="http://schemas.openxmlformats.org/drawingml/2006/chart"`
    + ` xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"`
    + ` r:id="${relId}"/>`
    + `</a:graphicData></a:graphic></p:graphicFrame>`;
}

/**
 * 아직 안 쓰인 차트 부품 이름.
 *
 * 뼈대로 뜬 장에 **이미 차트가 있을 수 있다.** 실물에서 그 화면을 봤다(2026-09-02): 차트를
 * 하나 넣고 나면 그 장이 「보고 있는 장」이 되고, 다음 차트가 그 장을 뼈대로 뜨는데 —
 * 꾸러미에 이미 `chart1.xml` 이 있으므로 같은 이름을 하나 더 넣으면 **zip 에 같은 이름이 둘**
 * 생긴다. PowerPoint 는 그것을 `InvalidArgument` 로 되받는다.
 *
 * 「첫 차트는 되는데 둘째부터 안 된다」는 사람이 원인을 짚을 수 없는 종류의 고장이다.
 */
export function freeChartName(names) {
  const used = new Set(names);
  for (let i = 1; i < 1000; i += 1) {
    const name = `ppt/charts/chart${i}.xml`;
    if (!used.has(name)) return { part: name, target: `../charts/chart${i}.xml`, at: `/${name}` };
  }
  throw new Error('이 장에 차트가 너무 많아 새 이름을 못 지었습니다');
}

/**
 * 아직 안 쓰인 관계 id.
 *
 * 같은 이유다 — 뼈대에 이미 `rId3` 이 있는데 우리도 `rId3` 을 쓰면 관계가 하나로 뭉개진다.
 * 고정된 이름(`rIdChart1`)은 두 번째 차트에서 그대로 부딪힌다.
 */
export function freeRelId(relsXml) {
  for (let i = 1; i < 1000; i += 1) {
    const id = `rId${i}`;
    if (!relsXml.includes(`Id="${id}"`)) return id;
  }
  throw new Error('이 장에 관계가 너무 많아 새 id 를 못 지었습니다');
}

/** 관계 하나를 관계 파일에 끼운다. **이미 있으면 안 넣는다.** */
export function withRelationship(xml, relId, target) {
  if (xml.includes(`Id="${relId}"`)) return xml;
  const rel = `<Relationship Id="${relId}"`
    + ` Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/chart"`
    + ` Target="${target}"/>`;
  if (!xml.includes('</Relationships>')) {
    throw new Error('관계 파일의 모양이 예상과 다릅니다 — </Relationships> 를 못 찾았습니다');
  }
  return xml.replace('</Relationships>', `${rel}</Relationships>`);
}

/** 콘텐츠 형식에 차트를 등록한다. 없으면 PowerPoint 가 그 부품을 아예 안 읽는다. */
export function withContentType(xml, partName) {
  if (xml.includes(`PartName="${partName}"`)) return xml;
  const over = `<Override PartName="${partName}"`
    + ` ContentType="application/vnd.openxmlformats-officedocument.drawingml.chart+xml"/>`;
  if (!xml.includes('</Types>')) {
    throw new Error('[Content_Types].xml 의 모양이 예상과 다릅니다 — </Types> 를 못 찾았습니다');
  }
  return xml.replace('</Types>', `${over}</Types>`);
}

/**
 * 슬라이드의 도형 나무에 틀을 끼운다.
 *
 * **있던 도형을 안 지운다.** 부르는 쪽이 빈 장을 원하면 그쪽에서 정하는 것이고, 여기서 지우면
 * 「차트를 넣어 달랬더니 있던 것이 사라졌다」가 된다.
 */
export function withFrame(slideXml, frameXml) {
  const at = slideXml.lastIndexOf('</p:spTree>');
  if (at < 0) {
    throw new Error('슬라이드 XML 의 모양이 예상과 다릅니다 — </p:spTree> 를 못 찾았습니다');
  }
  return slideXml.slice(0, at) + frameXml + slideXml.slice(at);
}
