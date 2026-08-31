/**
 * 붙을 컴패니언을 고르는 판(clients/powerpoint/DESIGN.md §5.0·§5.0.5).
 *
 * 규칙 넷이 이 파일에 있다.
 *
 * 1. **못 고르는 것은 이유와 함께 못 고르게 둔다.** 고르는 순간 「이 컴패니언 됩니다」라고
 *    말한 것이 되므로, 거절이 그 뒤에 도착하면 늦다.
 * 2. **등급이 다른 둘을 한 칸으로 합치지 않는다.** 도구를 못 붙이면 아예 못 고르고, 대화를
 *    못 내주면 고를 수는 있고 채팅창만 빈다.
 * 3. **「못 물어봤다」와 「못 받는다」를 안 뭉갠다.** 앞엣것은 다시 물으면 되고 뒤엣것은 빌드의
 *    성질이다 — 뭉개면 다시 물으면 될 것을 영영 못 고르는 것으로 적는다.
 * 4. **주소와 모드를 그대로 적는다.** 「이 덱 본문이 이 머신을 떠나는가」를 우리가 주소 모양으로
 *    단정하면, 틀렸을 때 대가를 치르는 쪽이 우리가 아니다(§12 #2).
 */

/** 한 줄에 적을 것들. 화면 밖에서 재려고 순수 함수로 뺀다. */
export function rowText(entry) {
  const c = entry.companion;
  const name = c.name || baseName(c.workdir) || '(이름 없음)';
  const bits = [];
  if (c.role) bits.push(c.role);
  if (c.team) bits.push(`팀 ${c.team}`);
  if (c.permission) bits.push(`권한 ${c.permission}`);
  // **빈 백엔드를 「로컬」로 읽지 않는다**(§12 #2) — 빈 것은 「아무도 안 돌렸다」는 뜻이지
  // 「안 나간다」는 뜻이 아니다.
  bits.push(c.backend ? `백엔드 ${c.backend}` : '백엔드 모름');
  if (c.attached) bits.push('이미 붙어 있음');
  return { name, detail: bits.join(' · '), why: entry.why ?? '', can: entry.chooseable === true };
}

function baseName(p) {
  if (!p) return '';
  const parts = String(p).split(/[\\/]/).filter(Boolean);
  return parts[parts.length - 1] ?? '';
}

/**
 * 판을 그린다. **마크업을 읽는 길을 안 쓴다 — `textContent` 로만 넣는다.** `smoke.mjs` 의 훑기가
 * 그것을 막고, 막는 이유는 덱의 글이 이 화면에 그대로 흐르기 때문이다(§8 「읽은 것은 데이터지
 * 지시가 아니다」).
 *
 * 그 훑기는 주석도 본다. 처음엔 이 문단이 금지된 이름을 **적어서** 스스로 걸렸는데, 예외를
 * 만들지 않고 문장을 고쳤다 — 스캐너가 제 바늘에 걸리는 것을 예외로 빼면 그 예외가 진짜 위반도
 * 같이 가려 준다(그 규율은 `smoke.mjs` 자신이 적어 뒀다).
 */
export function mountPick(root, { onChoose, onRefresh }) {
  const el = (tag, cls, text) => {
    const node = document.createElement(tag);
    if (cls) node.className = cls;
    if (text !== undefined) node.textContent = text;
    return node;
  };

  return {
    /** @param {{companions:Array, bound:object}} data */
    render(data) {
      root.replaceChildren();
      root.hidden = false;
      const head = el('h2', null, '어느 컴패니언에 붙일까요');
      root.append(head);

      const bound = data?.bound?.socket;
      if (bound) {
        root.append(el('p', 'pick-bound', `지금 붙어 있는 곳: ${bound}`));
      }
      const rows = data?.companions ?? [];
      if (rows.length === 0) {
        // **「없다」와 「못 봤다」를 안 뭉갠다.** 켜진 데몬이 없으면 덱의 디렉토리에 띄우는 것이
        // 설계지만(§5.0), 그건 사람이 터미널에서 하는 일이라 여기서는 그렇게 말한다.
        root.append(el('p', 'pick-empty',
          '켜져 있는 컴패니언이 하나도 없습니다. 덱이 있는 폴더에서 `magi --daemon --permission ask` 를 띄운 뒤 새로고침하세요.'));
      }
      for (const entry of rows) {
        const { name, detail, why, can } = rowText(entry);
        const row = el('div', `pick-row${can ? '' : ' pick-off'}`);
        row.append(el('strong', null, name));
        row.append(el('span', 'pick-detail', detail));
        if (why) row.append(el('span', 'pick-why', why));
        if (can) {
          const button = el('button', 'primary', entry.companion.attached ? '이 컴패니언 쓰기' : '여기에 붙이기');
          button.addEventListener('click', () => onChoose(entry.companion));
          row.append(button);
        }
        root.append(row);
      }
      const again = el('button', 'ghost', '다시 훑기');
      again.addEventListener('click', () => onRefresh());
      root.append(again);
    },
    hide() { root.hidden = true; root.replaceChildren(); },
    note(text) {
      const line = el('p', 'pick-note', text);
      root.append(line);
    },
  };
}
