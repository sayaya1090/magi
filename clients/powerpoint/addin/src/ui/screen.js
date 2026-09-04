// 화면이 **정하는 것**만 모은 자리. DOM 을 안 만지고 값만 답한다 — 그래서 잴 수 있다.
//
// 왜 갈랐나. `view.js` 머리에는 「결정을 안 한다」고 적혀 있었는데 실측이 그 말을 거짓으로
// 만들었다: 뷰에 심은 돌연변이 32개 중 **30개가 살아남았고**, 살아남은 줄은 거의 전부
// DOM 을 쓰는 코드 **안에 박힌 결정**이었다 — 「Cmd/Ctrl+Enter 라야 보낸다」, 「sticky 면
// 4초를 안 건다」, 「call 폭이면 좁은 단추」. 시험이 뷰에서 부를 수 있는 것은 `headOf`
// 하나뿐이라 나머지 결정에는 손이 못 닿았다. 가짜 DOM 을 지어 **재는 자리를 늘리는 것**보다
// 결정을 **재지는 자리로 옮기는 것**이 싸다.
//
// 규칙 하나. 여기의 함수는 `document` 를 모른다. 알기 시작하면 다시 못 재는 자리가 된다.
// 뷰가 하는 일은 이제 **부르고 대입하기**뿐이다.
import { clearedNote } from '../domain/Pending.js';
import { targetLabel } from '../domain/Advice.js';

/** 보내는 키. **Enter 혼자로는 안 보낸다** — 여러 줄을 적는 칸이라 줄바꿈이 발송이 되면 안 된다. */
export function isSendKey(e) {
  return e.key === 'Enter' && (e.metaKey || e.ctrlKey);
}

/**
 * 물음 판을 **다시 세울 것인가**. 서명이 같으면 안 세운다 — 적던 글과 포커스가 이 한 줄에
 * 달려 있다. `'refresh'` 는 「판은 그대로 두고 늙는 줄만 고친다」는 뜻이다.
 */
export function askAction(sig, prevSig) {
  return sig === prevSig ? 'refresh' : 'rebuild';
}

/**
 * 새로 선 물음을 **화면 안으로 끌어와야 하는가.**
 *
 * 물음 칸은 대화 아래에 선다. 대화가 길면 그 칸은 접힌 자리 밖이고, 그러면 **데몬은 답을
 * 기다리고 사람은 물음을 못 본다** — §5.7 이 이름 대어 피하려는 「아무도 안 보는 곳에서
 * 대기」가 화면 안에서 그대로 재현된다. 실물에서 그 화면을 봤다(2026-09-01): 권한 물음이
 * 떴는데 판에는 안 보였고, 마우스 휠을 굴려야 나왔다.
 *
 * **끌어오는 것은 막힌 물음뿐이다.** `lost`(못 닿음)·`last`(직전 물음이 내려감)는 사람이
 * 답할 것이 없으므로, 읽던 자리를 뺏으면서까지 보여 줄 이유가 없다. `unknown` 은 단추가
 * 없지만 **데몬은 여전히 막혀 있고**, 왜 아무 일도 안 일어나는지를 그 칸만 말한다.
 *
 * 그리고 **새로 선 것일 때만.** 폴은 1초마다 도는데 그때마다 끌어오면 사람이 위로 올려
 * 읽던 것이 매초 도로 내려간다 — `askAction` 이 이미 같은 물음을 가려낸다.
 */
export function askReveal(kind, action) {
  return action === 'rebuild' && (kind === 'known' || kind === 'unknown');
}

/** 어떤 물음 판인가. 넷은 서로 다른 화면이고, **모르는 종류에는 단추가 없다**(§5.7). */
export function askKind(v) {
  // **아직 아무 컴패니언에도 안 붙었으면 「못 닿는다」가 아니다.** 붙기 전의 창에 그 배너를
  // 띄우면 고르라는 화면 위에 「데몬에 안 닿습니다」가 겹쳐 뜨고, 사람은 고르기 전에 이미
  // 고장 난 줄 안다 — 실물에서 그 화면을 보고 고쳤다(2026-09-01).
  if (v.bound === false) return 'none';
  if (!v.reachable) return 'lost';
  if (!v.pending) return 'last';
  return v.pending.known ? 'known' : 'unknown';
}

/** 물음 판 머리. 권한인지 아닌지가 **머리에서** 갈려야 사람이 무게를 안다. */
export function askHead(p) {
  return p.isPermission ? '권한을 묻고 있습니다' : '묻고 있습니다';
}

/** 무엇을 묻는가. 안 실렸으면 **안 실렸다고 적는다** — 빈 칸은 다 읽었다는 뜻이 된다. */
export function whatText(p) {
  return p.what || '(무엇인지 안 실렸습니다)';
}

/** 실린 인자. 글이면 그대로, 아니면 펴서. */
export function argsText(slot) {
  return typeof slot.args === 'string' ? slot.args : pretty(slot.args);
}

/**
 * 「이걸 답하면 다음이 온다」 한 줄. `placement` 가 없으면 **자리는 서 있되 말이 없다** —
 * 자리를 없애면 뒤에 물음이 쌓일 때 갈아 끼울 데가 없어 판을 통째로 다시 세우게 된다.
 */
export function placeLine(placement) {
  return {
    text: placement ? `${placement} — 이걸 답하면 다음 물음이 옵니다.` : '',
    hidden: !placement,
  };
}

/**
 * 데몬이 하는 일 한 줄. **못 닿는 동안엔 현재형으로 안 적는다** — 근거가 방금 읽은 status
 * 뿐이라, 못 닿으면 근거 없이 「지금 …하는 중」이라고 말하는 것이 된다.
 */
export function doingLine(doing, fresh) {
  return {
    text: !doing ? '' : fresh ? doing : `마지막으로 읽었을 때: ${doing}`,
    hidden: !doing,
  };
}

/**
 * 직전 물음이 **왜** 내려갔는지. `show:false` 는 **할 말이 없다**는 뜻이고 그때만 줄이 안
 * 선다 — 모르는 사유는 조용히 숨는 대신 제 말을 갖고 온다(`clearedNote`).
 */
export function lastAskShape(clearedBy) {
  const text = clearedNote(clearedBy);
  return { show: text !== null, text: text ?? '' };
}

/** 결정 단추의 모양. **폭이 넓은 결정은 넓게 생겼다** — 문구만으로는 안 읽고 누른다(§5.7). */
export function decisionClass(d) {
  return d.width === 'call' ? 'ghost' : 'ghost wide';
}

/**
 * 터진 것을 사람에게 옮기는 한 줄. **안 사라진다**(`sticky`) — 「화면이 지금 거짓말을 하고
 * 있다」는 말이라, 4초 뒤 없어지면 사람은 그 말을 못 보고 다음을 누른다.
 */
export function failNote(what, e) {
  return { text: `${what}: ${e?.message ?? String(e)}`, sticky: true };
}

/**
 * 쪽지의 수명(ms). `null` 이면 **저 혼자 안 사라진다.** 사라지는 알림은 읽고 나면 볼일이
 * 끝나는 말이고, 안 사라지는 것은 화면이 거짓말을 하고 있다는 말이다. 둘을 같은 수명으로
 * 두면 뒤엣것이 4초 만에 없어진다.
 */
export function noteLife({ sticky = false } = {}) {
  return sticky ? null : 4000;
}

/**
 * 호스트가 무엇을 지원한다고 말했는가. **안 잰 것을 잰 것처럼 안 적는다** — 어댑터가 아예
 * 안 답하는 경우까지 여기서 값으로 만든다.
 */
export function capsOf(deck) {
  return (typeof deck.capabilities === 'function')
    ? deck.capabilities()
    : { measured: false, note: '어댑터가 안 답한다', sets: [] };
}

/**
 * 접힌 채로 늘 보이는 한 칸.
 *
 * 작업창은 PowerPoint 에서 348×391 이라(MS 지침의 크기 표) 세로가 귀하고, 요구 집합 여섯 줄은
 * 뭔가 안 될 때만 읽는 값이다. 그래서 접되 **요약은 사실을 말한다** — 안 쟀으면 안 쟀다고
 * 하고, 하나라도 ✗ 면 그 수를 적는다. 「다 좋다」로 접어 두면 그 창은 접힌 채로 거짓말을 한다.
 */
export function capsSummary(c) {
  if (!c.measured) return '요구 집합: 못 쟀습니다';
  const no = c.sets.filter((s) => s.ok === false).length;
  const unknown = c.sets.filter((s) => s.ok !== true && s.ok !== false).length;
  if (no === 0 && unknown === 0) return `요구 집합 ${c.sets.length}개 모두 지원`;
  const bits = [];
  if (no > 0) bits.push(`${no}개 없음`);
  if (unknown > 0) bits.push(`${unknown}개 모름`);
  return `요구 집합: ${bits.join(' · ')}`;
}

/**
 * 브랜드 줄에 같이 서는 상태 한 마디. **붙은 곳과 손을 한 줄에** 적는다 — 둘 다 「지금 이
 * 창이 무엇에 닿아 있나」이고, 위쪽에 두면 세로 391px 에서 대화가 그만큼 줄어든다.
 */
export function brandState({ companion, streamLive, hands }) {
  if (!companion) return '컴패니언 미선택';
  const bits = [companion];
  bits.push(streamLive ? '대화 연결됨' : '대화 끊김');
  if (typeof hands === 'number') bits.push(hands === 1 ? '덱 1' : `덱 ${hands}`);
  return bits.join(' · ');
}

/** 그 값을 한 줄로. `ok` 가 `null` 인 것은 "아니오"가 아니라 **물어보다 던졌다**라 `?` 로 가른다. */
export function capsText(c) {
  if (!c.measured) return `요구 집합: ${c.note || '어댑터가 사유를 안 실었다'}`;
  return '요구 집합: ' + c.sets
    .map((s) => `${s.name} ${s.version} ${s.ok === true ? '✓' : s.ok === false ? '✗' : '?'}`)
    .join(' · ');
}

/**
 * 스트림 자체에 대한 한 줄. **조용한 대화와 죽은 스트림을 가른다** — 문은 깨끗한 끝을
 * 에러로 안 주므로, 이 줄이 없으면 사람은 안 오는 답을 영원히 기다린다.
 */
export function streamLine(v) {
  // 아직 안 붙었으면 스트림에 대해 할 말이 없다 — 「끊겼다」는 붙어 있던 것에 대한 말이다.
  if (v.bound === false) return { text: '', hidden: true };
  const parts = [];
  if (v.refusal) parts.push(`서버가 이 창의 커서를 안 받았습니다: ${v.refusal}`);
  if (!v.live) parts.push('대화 스트림이 끊겼습니다 — 새 말이 안 옵니다.');
  return { text: parts.join(' · '), hidden: parts.length === 0 };
}

/** 일부러 안 그린 것. 못 그리는 것과 **다른 줄**이라 문장도 따로 만든다. */
export function skippedLine(note) {
  return { text: note ?? '', hidden: !note };
}

/** 로그가 못 읽은 것. 없으면 칸이 안 선다. */
export function unknownLine(note) {
  return { text: note ?? '', hidden: !note };
}

/**
 * 인용 한 조각의 몸통. 「글이 없다」와 「글을 못 읽었다」는 **다른 문장**이다 — 뒤엣것을
 * 앞엣것으로 적으면 사람도 모델도 빈 상자를 고치러 간다.
 */
export function quoteBody(q) {
  if (q.text) return `"${q.preview()}"`;
  return q.textUnavailable ? '(글을 못 읽었습니다)' : '(글 없음)';
}

/** 인용 한 조각의 꼬리표. 빈 것은 안 적는다 — ` · ` 만 남은 줄이 생긴다. */
export function quoteMeta(q) {
  return [q.type, q.sizeLabel].filter(Boolean).join(' · ');
}

/**
 * 줄의 class. 종류를 **접두사와 함께** 적는다. 그냥 `turn ${r.kind}` 로 적으면 끝난 턴이
 * `class="turn turn"` 이 되고, `.turn.turn` 은 CSS 에서 그냥 `.turn` 이라 그 한 줄에 준
 * 모양이 **모든 줄에** 걸린다. 실제로 사용자 말이 가운데 정렬됐었다.
 */
export function rowClass(r) {
  return `turn kind-${r.kind}`;
}

/**
 * 이 줄에 붙일 머리. 없으면 머리 없이 글만 — 사용자와 모델의 말이 그렇다.
 *
 * `note` 만 줄을 들여다본다. 「사람이 아닌 배우가 넣었다」는 **배우를 밝혔을 때만** 할 수 있는
 * 말이고, 안 밝힌 줄에 그렇게 적으면 모르는 것을 아는 것처럼 적는 것이다(`Row.attributed`).
 */
export function headOf(r) {
  if (r.kind !== 'note') return ROW_HEAD[r.kind];
  return r.attributed ? ROW_HEAD.note : '⟳ 누가 넣었는지 안 밝힌 줄';
}

/** 종류별 줄머리. */
const ROW_HEAD = {
  think: '혼잣말 (사용자에게 한 말이 아님)',
  note: '⟳ 사람이 아닌 배우가 넣은 줄',
  tool: '⚙',
  error: '오류',
  // 짝이 되는 호출 줄을 못 찾은 것들. 보통은 호출 줄에 접히므로(`Transcript.append`) 이
  // 머리가 보인다는 것은 **이 창이 로그 중간부터 읽기 시작했다**는 뜻이고, 그건 사실이라
  // 적는다 — 「왜 답만 덩그러니 있나」를 사람이 이 줄로 안다.
  result: '⚙ 앞을 못 본 호출의 답',
  permission: '⚙ 앞을 못 본 호출의 권한 결정',
};

/**
 * 화면에 실제로 적히는 머리. 도구 줄은 **이름까지** 적는다 — `⚙` 하나로는 무엇이
 * 슬라이드를 고쳤는지 모른다. 이름이 안 실린 것도 그 사실을 적는다.
 */
export function rowHead(r) {
  if (r.kind === 'council') return councilHead(r);
  const head = headOf(r);
  if (!head) return '';
  return r.kind === 'tool' ? `⚙ ${toolLabel(r.tool)}` : head;
}

/** 줄을 어떤 모양으로 그리는가. 다 말풍선으로 그리면 도구 호출이 사람 말이 된다(§5.7). */
export function rowShape(r) {
  if (r.kind === 'tool' || r.kind === 'result' || r.kind === 'permission') return 'tool';
  if (r.kind === 'turn') return 'turn';
  if (r.kind === 'council') return 'council';
  return 'text';
}

/**
 * 도구 줄에 붙는 **결과 칸**. 아직 답이 안 왔으면 `null` 이고, 그것도 사실이다 — 호출만 있고
 * 답이 없는 줄은 「도는 중」이라는 뜻이라, 없는 답을 지어내는 것보다 비워 두는 편이 맞다.
 *
 * **`isError` 하나로 ✗ 를 찍지 않는다.** 코어의 `ToolResult.Advisory` 주석이 그 사고를 적어
 * 뒀다: 한 일은 했는데 읽을 것이 붙은 호출도 `IsError` 를 세우고, 그래서 창 둘이 **성공한
 * 쓰기를 실패로 그렸다.** 이 제품에서 그 오독은 사람에게 「슬라이드가 안 바뀌었다」로 읽히고,
 * 사람은 이미 바뀐 것을 다시 시킨다.
 */
export function resultCell(r) {
  const res = r.result;
  if (!res) return null;
  if (res.advisory) {
    return { mark: '⚠', head: '완료 — 읽을 것 있음', text: resultText(res),
      failed: false, advisory: true };
  }
  if (res.isError) {
    return { mark: '✗', head: '실패', text: resultText(res), failed: true, advisory: false };
  }
  return { mark: '✓', head: '완료', text: resultText(res), failed: false, advisory: false };
}

/** 답의 몸통. 글일 때도 객체일 때도 있어서(`json.RawMessage`) 편 뒤에 자른다. */
function resultText(res) {
  const c = res.content;
  const s = typeof c === 'string' ? c : (c == null ? '' : pretty(c));
  // 그림은 **참조로만** 온다(`ImageRef`). 이 창은 아직 못 여는데, 몇 장인지도 안 적으면
  // 「도구가 아무것도 안 냈다」와 「낸 것을 우리가 못 그린다」가 같은 화면이 된다.
  const img = res.images > 0 ? `\n(그림 ${res.images}장은 이 창이 아직 안 그립니다)` : '';
  return clip(s.trim(), 400) + img;
}

/**
 * 답에서 **사람이 읽을 줄**만 뽑는다 — 우리 도구가 `changed` 에 한국어로 적어 보내는 것.
 *
 * 실물에서 판이 이것 때문에 찼다(2026-09-04, 스크린샷): `read_slide` 한 번의 답이
 * `"revision"`·`"shapes"` 를 통째로 펴서 화면을 덮었다. 인자는 접어 뒀는데 **답은 안 접고
 * 있었다** — 그리고 이 창에서 값이 있는 것은 그 JSON 이 아니라 그 안의 한두 줄이다.
 *
 * 그래서 화면은 이 줄만 펴 두고 **원문은 접는다.** 못 뽑으면 빈 배열이고, 그때는 접힘만 남는다 —
 * 지어내지 않는다.
 */
export function changedLines(r) {
  const c = r?.result?.content;
  if (typeof c !== 'string') return [];
  try {
    const got = JSON.parse(c);
    const list = got?.changed;
    return Array.isArray(list) ? list.filter((x) => typeof x === 'string') : [];
  } catch {
    return [];
  }
}

/** 허락 한 줄. 이 제품에서 이 답은 **덱을 고치게 뒀는가**다. */
export function permissionText(r) {
  if (!r.permission) return '';
  const word = { allow: '이번 한 번 허용', always: '앞으로도 허용', deny: '거절' }[r.permission];
  // 모르는 결정을 아는 척 옮기지 않는다 — 글자 그대로 적는다.
  return word ? `권한: ${word}` : `권한: ${r.permission}`;
}

/** 표결의 말. 코어의 이름(`done|continue|abstain`)을 사람 말로. */
function decisionWord(d) {
  return { done: '끝났다', continue: '더 하라', abstain: '기권' }[d] ?? (d || '(안 실림)');
}

/**
 * 표결 수. **다시 세지 않는다** — 규칙(과반·만장일치·가중치)을 이 창이 한 벌 더 가지면,
 * 둘이 어긋나는 날 화면이 로그와 다른 결론을 적는다.
 */
export function tallyText(t) {
  if (!t) return '';
  const bits = [];
  for (const [k, label] of [['done', '끝났다'], ['continue', '더 하라'], ['abstain', '기권']]) {
    if (typeof t[k] === 'number') bits.push(`${label} ${t[k]}`);
  }
  return bits.length ? ` — ${bits.join(' · ')}` : '';
}

/**
 * 종료 게이트의 줄머리.
 *
 * **이 줄이 없으면 사람은 모델이 왜 같은 일을 또 하는지 모른다.** 게이트가 「다 했다」를
 * 거절하면 턴이 계속 도는데, 화면에는 도구 호출만 줄줄이 서고 그 사유는 로그에만 남는다 —
 * 실물에서 본 화면이다(2026-09-01).
 */
export function councilHead(r) {
  // **줄을 받는다** — 이 파일의 다른 줄 함수들과 같은 모양이라야 부르는 쪽이 헷갈리지 않는다.
  const c = r?.council;
  if (!c) return '⚖';
  if (c.stage === 'convened') {
    const who = c.members.length ? c.members.join(' · ') : '(구성원 안 실림)';
    return `⚖ ${c.round}회차 판정 — ${who}${c.rule ? ` (${c.rule})` : ''}`;
  }
  if (c.stage === 'verdict') {
    const who = c.member || '(누군지 안 실림)';
    const lens = c.lens ? ` (${c.lens})` : '';
    // **말 없는 표를 「기권했다」로 적지 않는다**(`CouncilVerdictData.Silent`) — 백엔드가
    // 죽었거나 답을 못 읽은 것이라, 판단해서 기권한 것과 다른 사실이다.
    return c.silent ? `⚖ ${who}${lens}: 답이 없었습니다`
      : `⚖ ${who}${lens}: ${decisionWord(c.decision)}`;
  }
  return `⚖ ${c.round}회차 결론: ${decisionWord(c.decision)}${tallyText(c.tally)}`;
}

/** 게이트가 덧붙인 말. 없으면 빈 글 — 머리만으로 뜻이 서는 줄이 있다. */
export function councilBody(r) {
  const c = r?.council;
  if (!c) return '';
  if (c.stage === 'verdict') return c.rationale ?? '';
  if (c.stage === 'decided') return [c.note, c.feedback].filter(Boolean).join('\n');
  return '';
}

/** 도구 줄의 인자 칸. **인자를 적는다** — 「set_text 를 불렀다」는 무엇이 바뀌었는지 안 알려 준다. */
export function argsCell(r) {
  return r.args == null ? '(인자 없음)' : clip(pretty(r.args), 300);
}


/** 끝난 턴의 한 줄. **검증 못 한 착지를 보통 끝처럼 그리지 않는다**(`TurnFinishedData`). */
export function endText(r) {
  return r.unverified
    ? `검증되지 않은 끝${r.reason ? ` — ${r.reason}` : ''}`
    : '— 턴 끝 —';
}

/**
 * 말 줄의 몸통. 사용자 줄에는 인용이 **글로 접혀** 들어 있다(`promptOf`). 예쁘게 걷어 내지
 * 않는다 — 모델이 받은 것이 이것이고, 걷어 내면 화면이 모델보다 덜 아는 것을 감추게 된다.
 */
export function bodyText(r) {
  return r.text || '(글 없음)';
}

/**
 * 안내 층이 서는가. **사유만 있어도 선다** — 안내가 0개인데 「몇 개를 못 읽었다」가 있으면
 * 그 말이 갈 곳이 없어진다.
 */
export function adviceBoard(advices, note) {
  return {
    wrapHidden: advices.length === 0 && note === '',
    noteText: note,
    noteHidden: note === '',
  };
}

/**
 * 안내 하나가 **어디를 가리키는가**. 안 눌리는 항목에는 **왜 안 눌리는지**가 그 자리에 온다 —
 * 회색으로만 두면 "모델이 어딜 말 안 했다"와 "이 창이 고장났다"가 같은 화면이 된다.
 */
export function adviceTargetText(a, map, answered) {
  return a.pointable ? targetLabel(a, map, answered) : a.unpointableReason;
}

/** 값을 사람이 읽게 편다. 못 펴는 것(순환 참조)도 **뭔가는 적는다.** */
export function pretty(v) {
  try { return JSON.stringify(v, null, 2); } catch { return String(v); }
}

/** 너무 길면 자른다. 자른 표시(`…`)까지가 `n` 이다 — 자르고도 `n` 을 넘으면 자른 뜻이 없다. */
export function clip(s, n) { return s.length > n ? s.slice(0, n - 1) + '…' : s; }

/**
 * 제안 판. **이 파일의 다른 것들과 같은 규칙**이다 — 갈래를 여기서 정하고 뷰는 그리기만 한다.
 *
 * 카드가 말하는 것 셋을 가른다:
 *
 * 1. **제안의 글** — 덱에서 온 것이다. 남이 준 덱이면 남이 쓴 글이고, 우리 도구에게 말을
 *    거는 글일 수도 있다(§6.13). 그대로 보여 주되 **그것이 무엇을 하는지의 근거로는 안 쓴다.**
 * 2. **무엇을 합니다** — `fix` 에서 뽑은 말이다. 이게 사람이 믿을 수 있는 유일한 줄이다.
 * 3. **누를 수 있나** — 손이 없거나 우리가 모르는 손이면 카드는 읽히기만 한다.
 */
export function fixBoard(rows) {
  const list = rows ?? [];
  return {
    wrapHidden: list.length === 0,
    headText: list.length === 1 ? '제안 1건' : `제안 ${list.length}건`,
    cards: list.map((r) => ({
      key: r.key,
      what: r.what,
      whyText: r.why || '',
      whyHidden: !r.why,
      whereText: [
        r.slide != null ? `슬라이드 ${r.slide}` : '어느 장인지 안 실렸습니다',
        r.shape_id ? `도형 ${r.shape_id}` : null,
      ].filter(Boolean).join(' · '),
      doesText: r.does,
      // **못 누르는 이유가 그 자리에 온다.** 회색 버튼만 두면 「손이 안 달렸다」와 「이 창이
      // 고장났다」가 같은 화면이 된다.
      canApply: Boolean(r.appliable),
      applyText: r.appliable ? '적용' : '적용 불가',
      broken: Boolean(r.broken),
    })),
  };
}

/**
 * 헤더의 어댑터 이름. **예상 밖일 때만 적는다.**
 *
 * PowerPoint 안에서 「PowerPoint (Office.js)」는 사람이 이미 보고 있는 것을 한 줄 더 적는
 * 것이고, 작업창은 348×391 이라 그 한 줄이 비싸다. 가짜 덱과 모르는 어댑터는 **반드시**
 * 적힌다 — 조용히 진짜인 척하지 않는 것이 그 갈래의 요점이다.
 *
 * @param {{label?:string, isHost?:boolean}} deck
 */
export function adapterText(deck) {
  if (deck?.isHost) return '';
  return deck?.label || 'unknown';
}

/**
 * 「붙었으니 시키면 된다」가 아직 참인가.
 *
 * 대화가 하나라도 서면 그 문장은 **증명된 것**이라 자리만 먹는다. 값으로 재는 이유는 이 판이
 * 계속 세는 것과 같다 — 적어 두면 조건이 사라져도 문장이 남고, 유도하면 같이 사라진다.
 *
 * @param {string|null} bound 붙은 컴패니언 이름. 없으면 아직 안 붙은 것이다.
 * @param {number} rowCount 대화에 선 줄 수.
 */
export function readyText(bound, rowCount) {
  if (!bound || rowCount > 0) return '';
  return `${bound} 에 붙어 있습니다 — 바로 시키시면 됩니다.`;
}

/**
 * 가이드 판. **결정은 여기서 하고 뷰는 그리기만 한다**(§4 — 뷰에 심은 돌연변이 32개 중 30개가
 * 살아남은 뒤로 이 레인이 지키는 규칙).
 *
 * 세 가지를 값으로 정한다.
 *
 * 하나 — **꺼 둔 것도 목록에 남는다.** 빼면 다시 켤 길이 없고, 사라진 것과 지워진 것이 화면에서
 * 같아진다.
 *
 * 둘 — **설명이 없으면 없다고 적는다.** 빈 줄로 두면 「설명이 없는 가이드」와 「설명을 못 읽은
 * 가이드」가 같은 회색 한 줄이 된다. 그리고 그 설명은 **모델이 부를지 말지를 정하는 글**이라,
 * 비어 있다는 사실 자체가 사람이 고쳐야 할 것이다.
 *
 * 셋 — **하나도 없을 때 빈 판을 보이지 않는다.** 「아직 없다」와 「못 읽었다」는 다른 말이고,
 * 뒤엣것은 사유가 있어야 한다.
 *
 * @param {{guides?:Array, error?:string}} state
 */
export function guideBoard(state) {
  if (state?.error) {
    return { failed: true, headText: '가이드를 못 읽었습니다', note: state.error, rows: [] };
  }
  const list = state?.guides ?? [];
  const on = list.filter((g) => g.enabled).length;
  return {
    failed: false,
    headText: list.length === 0
      ? '아직 가이드가 없습니다'
      : `가이드 ${list.length}벌 · 켜짐 ${on}`,
    // 하나도 안 켜져 있으면 그 사실을 적는다 — 적어 두고 다 꺼 놓은 것은 흔한 상태이고,
    // 그때 모델은 아무것도 안 읽는다.
    note: list.length > 0 && on === 0 ? '전부 꺼져 있어 모델이 아무것도 안 읽습니다.' : '',
    rows: list.map((g) => ({
      name: g.name,
      descText: g.description ? g.description : '설명이 없습니다 — 모델이 이걸 보고 부를지 정합니다',
      descMissing: !g.description,
      enabled: Boolean(g.enabled),
      toggleText: g.enabled ? '켜짐' : '꺼짐',
      // 아이콘 단추 셋. **툴팁은 동작을 적는다 — 아이콘 이름이 아니라**(M3 icon-buttons:
      // "a tooltip describing its action, rather than the name of the icon itself").
      // 그리고 켜짐/꺼짐은 **두 속성**으로 말한다: 글리프(◉/○)와 굵기. 색 하나로만 가르면
      // 못 가리는 사람이 있다 — 같은 문서의 "at least two properties, rather than just color".
      toggleIcon: g.enabled ? '◉' : '○',
      toggleTip: g.enabled ? `${g.name} 를 끕니다 — 글은 그대로 둡니다` : `${g.name} 를 켭니다`,
      editTip: `${g.name} 를 고칩니다`,
      deleteTip: `${g.name} 를 지웁니다 — 되돌릴 수 없습니다`,
      sizeText: typeof g.chars === 'number' ? `${g.chars}자` : '',
    })),
  };
}

/**
 * 계획 판. **모델이 스스로 세운 할 일 목록**(`todowrite` → `todos.changed`).
 *
 * 왜 그리는가: 이 제품의 턴은 길고(도형마다 한 호출), 그 사이 화면에 서는 것은 도구 이름뿐이라
 * 사람은 **어디까지 왔는지**를 못 본다. 계획은 모델이 이미 세워 두고 있고, 우리는 안 그리고
 * 있었을 뿐이다 — 화면 아래에 「그릴 줄 모르는 이벤트 … todos.changed」로 세어지고 있었다.
 *
 * 쌓지 않는다. 계약이 매번 전량이라 마지막 것이 곧 지금이다.
 *
 * **다 끝나면 사라진다.** 접는 것으로는 모자란다 — 접힌 판도 이 크기(348×391)에서는 한 줄을
 * 계속 차지하고, 그 줄은 이미 지난 일이다. 그래서 이 판의 수명은 **계획이 도는 동안**이다.
 *
 * 취소도 끝이다. 「할 일이 남았는가」에 답하는 판이지 「무엇을 했는가」를 적는 판이 아니다 —
 * 지난 일은 대화 줄에 이미 있다.
 */
export function planBoard(todos) {
  const list = Array.isArray(todos) ? todos : [];
  if (list.length === 0) return { hidden: true, headText: '', doneText: '', rows: [] };
  const mark = { pending: '·', in_progress: '▸', completed: '✓', cancelled: '✗' };
  const done = list.filter((t) => t.status === 'completed' || t.status === 'cancelled').length;
  const now = list.find((t) => t.status === 'in_progress');
  const allDone = done === list.length;
  // 다 끝났으면 안 그린다. 「계획 7/7」은 사람이 할 일이 없는 줄이다.
  if (allDone) return { hidden: true, headText: '', doneText: '', rows: [] };
  return {
    hidden: false,
    headText: `계획 ${done}/${list.length}`,
    // **지금 하는 것을 머리에 적는다.** 목록을 접어 둬도 이 한 줄은 남아야 「멈춘 것」과
    // 「도는 중」이 갈린다.
    doneText: now ? now.content : '다음 항목을 아직 안 골랐습니다',
    rows: list.map((t) => ({
      text: t.content ?? '',
      // 모르는 상태를 **완료로 읽지 않는다** — 지어내면 다 된 것처럼 보인다.
      mark: mark[t.status] ?? '·',
      state: t.status ?? 'unknown',
      known: Object.prototype.hasOwnProperty.call(mark, t.status),
    })),
  };
}

/**
 * 계획 목록에서 **눈이 가 있어야 하는 줄.**
 *
 * 목록은 96px 짜리 제 스크롤을 갖는데(`.plan-list`), 항목이 예닐곱을 넘으면 지금 도는 것이
 * 그 밖으로 밀린다 — 그러면 이 판이 답하려던 「지금 어디까지 왔나」를 판을 열어 두고도 못 본다.
 *
 * 고르는 규칙은 둘이다. **도는 것이 있으면 그것**이고, 없으면 **마지막으로 끝난 것**이다 —
 * 하나를 끝내고 다음을 아직 안 고른 사이가 실제로 있고(`planBoard.doneText` 가 그 자리를
 * 적는다), 그때 방금 바뀐 자리는 끝난 쪽이다.
 *
 * `key` 를 같이 돌려주는 것이 요점이다. 로그는 **글자 한 조각마다** 뛰므로 그릴 때마다 끌면
 * 사람이 목록을 제 손으로 넘겨 볼 수가 없다 — 잡은 자리가 **바뀌었을 때만** 끈다. 글까지
 * 키에 넣는 것은 항목이 제자리에서 고쳐 쓰이는 경우가 있기 때문이다.
 */
export function planAnchor(board) {
  const rows = board?.rows ?? [];
  if (rows.length === 0) return null;
  let i = rows.findIndex((r) => r.state === 'in_progress');
  if (i < 0) {
    for (let k = rows.length - 1; k >= 0; k--) {
      if (rows[k].state === 'completed' || rows[k].state === 'cancelled') { i = k; break; }
    }
  }
  if (i < 0) i = 0;
  return { index: i, key: `${i}:${rows[i].state}:${rows[i].text}` };
}

/**
 * **「지금 이 장을 봐 달라」의 글.**
 *
 * 번호가 있어야 짓는다. 모델은 장을 **번호로** 짚으므로(`read_slide {"slide": 5}`) 번호가
 * 없으면 이 부탁은 가리키는 데가 없는 말이 된다 — 그때는 「지금 보고 있는 장」 같은 말로
 * 갈음하지 않는다. 그 말을 받은 모델은 자기가 마지막으로 만진 장을 고르고, 그건 사람이
 * 보고 있는 장이 아니다(§5.8 의 "못 찾은 것을 비슷한 것으로 갈음하지 않는다").
 */
export function reviewAsk(sel) {
  const n = sel?.slideNo;
  if (!Number.isInteger(n) || n < 1) {
    return { text: '', note: '몇 번째 장인지 못 읽었습니다. 번호를 적어서 시키세요.' };
  }
  return {
    text: `${n}번 슬라이드를 검토해 주세요. 그림으로 확인하고, 고칠 것을 짚어 주세요. `
      + `고치는 것은 제가 시킨 뒤에 해 주세요.`,
    note: '',
  };
}

/**
 * 컴포저에 **덧붙인다.** 적던 글을 안 지운다 — 단추 하나가 사람이 쓰던 문단을 날리면 그
 * 단추는 다시 안 눌린다.
 */
export function appendAsk(cur, add) {
  const c = String(cur ?? '');
  if (!add) return c;
  if (!c.trim()) return add;
  return `${c.replace(/\s+$/, '')}\n${add}`;
}

/**
 * 도구의 **표시 이름**. `mcp__ppt__set_text` 가 아니라 「글 바꾸기」.
 *
 * 화면에 기계 이름을 그대로 내면 사람이 매번 번역해서 읽어야 하고, 이 판은 한 턴에 그 줄이
 * 수십 개다. 다만 **모르는 것은 지어내지 않는다** — 표에 없으면 받은 이름을 그대로 적는다.
 * 지어낸 이름은 「이 창이 아는 도구」와 「모르는 도구」를 화면에서 같아 보이게 만든다.
 *
 * ⚠ **이 표는 `helper/tools.go` 의 카탈로그와 갈릴 수 있다.** 도구가 늘면 여기도 늘어야 하고,
 * 안 늘면 새 도구만 기계 이름으로 뜬다 — 조용히. 그래서 `smoke` 가 카탈로그를 읽어 **빠진
 * 이름을 세운다.**
 */
const TOOL_LABELS = new Map(Object.entries({
  // 읽기
  list_slides: '목차 읽기', read_slide: '슬라이드 읽기', list_layouts: '레이아웃 보기',
  describe_style: '이 덱 서식 읽기', find_shapes: '도형 찾기', render_slide: '그림으로 보기',
  export_slide_ooxml: '원본 XML 읽기', snapshot_slide: '되돌릴 자리 만들기',
  read_notes: '발표자 노트 읽기', read_tags: '메모 읽기', read_animation: '애니메이션 읽기',
  read_suggestions: '제안 읽기',
  // 안내
  advise: '안내 붙이기', clear_advice: '안내 걷기',
  // 장
  add_slide: '장 만들기', add_slides: '여러 장 만들기', delete_slide: '장 지우기',
  duplicate_slide: '장 복제', apply_layout: '레이아웃 바꾸기', reorder_slide: '장 순서 바꾸기',
  restore_slide: '되돌리기',
  // 글·서식
  set_text: '글 바꾸기', format_shape: '서식 바꾸기', apply_style: '서식 한 번에 바꾸기',
  move_shape: '자리 옮기기', align_shapes: '줄 세우기', add_shape: '도형 넣기',
  delete_shape: '도형 지우기', set_hyperlink: '링크 걸기',
  // 표·차트·그림
  add_table: '표 만들기', replace_table: '표 다시 짓기', set_table_cells: '표 칸 채우기',
  add_chart: '차트 넣기', add_image: '그림 넣기',
  // 덱에 남는 것
  set_notes: '발표자 노트 쓰기', set_tag: '메모 남기기', animate_slide: '애니메이션 걸기',
  suggest: '제안 붙이기', drop_suggestion: '제안 떼기',
  // 덱 밖 — magi 자신의 것
  websearch: '웹 검색', webfetch: '웹 페이지 읽기', todowrite: '계획 세우기',
  bash: '셸 명령', read: '파일 읽기', write: '파일 쓰기', edit: '파일 고치기',
  glob: '파일 찾기', grep: '파일 안 찾기', list: '폴더 보기', remember: '기억해 두기',
  skill: '스킬 읽기', ask_user: '사람에게 묻기', council: '완료 선언',
}));

export function toolLabel(name) {
  if (!name) return '(이름 없음)';
  const short = name.startsWith('mcp__ppt__') ? name.slice('mcp__ppt__'.length) : name;
  return TOOL_LABELS.get(short) ?? name;
}

/** 표시 이름을 가진 도구들. `smoke` 가 카탈로그와 견주는 데 쓴다. */
export function labelledTools() { return [...TOOL_LABELS.keys()]; }
