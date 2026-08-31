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
  return r.kind === 'tool' ? `⚙ ${r.tool ?? '(이름 없음)'}` : head;
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
    return { mark: '⚠', head: '됐습니다 — 읽을 것이 붙었습니다', text: resultText(res),
      failed: false, advisory: true };
  }
  if (res.isError) {
    return { mark: '✗', head: '실패했습니다', text: resultText(res), failed: true, advisory: false };
  }
  return { mark: '✓', head: '됐습니다', text: resultText(res), failed: false, advisory: false };
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
