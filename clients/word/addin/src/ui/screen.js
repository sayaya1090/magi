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
  // Enter 가 보내고 Shift+Enter 가 줄을 바꾼다 — 채팅 창의 흔한 약속(사용자 2026-09-05). ⌘/Ctrl+Enter
  // 도 여전히 보낸다. **한글 조합 중의 Enter 는 조합을 끝내는 키라 보내지 않는다**(isComposing).
  if (e.key !== 'Enter' || e.isComposing) return false;
  if (e.metaKey || e.ctrlKey) return true;
  return !e.shiftKey && !e.altKey;
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
 * 대기」가 화면 안에서 그대로 재현된다. 실물에서 그 화면을 봤다(2026-09-01): 권한 확인 요청이
 * 떴는데 판에는 안 보였고, 마우스 휠을 굴려야 나왔다.
 *
 * **끌어오는 것은 막힌 물음뿐이다.** `lost`(못 닿음)·`last`(직전 확인 요청이 내려감)는 사람이
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
  return p.what || '(무엇인지 전달되지 않았습니다)';
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
    text: placement ? `${placement} — 이걸 답하면 다음 요청이 옵니다.` : '',
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
 * 직전 확인 요청이 **왜** 내려갔는지. `show:false` 는 **할 말이 없다**는 뜻이고 그때만 줄이 안
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
    : { measured: false, note: 'Word 연결이 답하지 않습니다', sets: [] };
}

/**
 * 접힌 채로 늘 보이는 한 칸.
 *
 * 작업창은 PowerPoint 에서 348×391 이라(MS 지침의 크기 표) 세로가 귀하고, 요구 집합 여섯 줄은
 * 뭔가 안 될 때만 읽는 값이다. 그래서 접되 **요약은 사실을 말한다** — 안 쟀으면 안 쟀다고
 * 하고, 하나라도 ✗ 면 그 수를 적는다. 「다 좋다」로 접어 두면 그 창은 접힌 채로 거짓말을 한다.
 */
/**
 * 이 줄을 **숨겨도 되는가.** 다 지원이면 읽을 사람이 없다 — 348×391 판에서 한 줄을 먹을 뿐이다
 * (사용자 2026-09-05: 「어디 꼭 필요할 때만 참고하게 숨기는 게 좋겠다」). 못 쟀거나 하나라도 빠지면
 * 보인다: 숨은 채로 거짓말을 하는 것은 접힌 채로 거짓말을 하는 것보다 나쁘다. 전문은 늘 콘솔에 남는다.
 */
export function capsQuiet(c) {
  if (!c.measured) return false;
  return c.sets.every((s) => s.ok === true);
}

export function capsSummary(c) {
  if (!c.measured) return '지원 API: 재지 못했습니다';
  const no = c.sets.filter((s) => s.ok === false).length;
  const unknown = c.sets.filter((s) => s.ok !== true && s.ok !== false).length;
  if (no === 0 && unknown === 0) return `API ${c.sets.length}종 모두 지원`;
  const bits = [];
  if (no > 0) bits.push(`${no}개 없음`);
  if (unknown > 0) bits.push(`${unknown}개 모름`);
  return `지원 API: ${bits.join(' · ')}`;
}

/**
 * 브랜드 줄에 같이 서는 상태 한 마디. **붙은 곳과 손을 한 줄에** 적는다 — 둘 다 「지금 이
 * 창이 무엇에 닿아 있나」이고, 위쪽에 두면 세로 391px 에서 대화가 그만큼 줄어든다.
 */
export function brandState({ companion, streamLive, hands, session }) {
  if (!companion) return '컴패니언 미선택';
  const bits = [companion];
  // **어느 대화인가.** 창을 둘 띄우면 이것이 두 창을 가르는 유일한 값이고, 오늘 그것이 없어서
  // 한나절을 썼다(2026-09-05: 빈 작업창, 남의 덱에 간 호출, 아무도 못 듣는 권한 확인 요청).
  //
  // 브랜드 줄에 두는 이유는 **수명이 여기 맞기 때문**이다. 처음 뜰 때만 적는 자리(`#ready`)에
  // 두었더니 「첫 줄 전까지만」 규칙에 걸려, 이미 오간 대화에 붙은 창에서는 영영 안 보였다 —
  // 사람이 세 번 「똑같다」고 한 자리다. 이 값은 창이 그 대화에 붙어 있는 동안 계속 참이다.
  // **대화가 없는 것과 대화가 끊긴 것은 다른 말이다.** 붙기 전·대화가 아직 안 열린 창에도
  // 「대화 끊김」이 섰다 — 끊길 대화가 없는데 끊겼다고 하니 사람이 고장으로 읽었다(2026-09-05).
  // 「끊김」은 대화가 있는데 스트림이 죽은 것에만 쓴다.
  if (session) {
    bits.push(`대화 ${session}`);
    bits.push(streamLive ? '대화 연결됨' : '대화 끊김');
  } else {
    bits.push('대화 없음');
  }
  if (typeof hands === 'number') bits.push(hands === 1 ? '문서 1' : `문서 ${hands}`);
  return bits.join(' · ');
}

/** 그 값을 한 줄로. `ok` 가 `null` 인 것은 "아니오"가 아니라 **물어보다 던졌다**라 `?` 로 가른다. */
export function capsText(c) {
  if (!c.measured) return `지원 API: ${c.note || 'Word 연결이 사유를 알려 주지 않았습니다'}`;
  return '지원 API: ' + c.sets
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
  if (v.refusal) parts.push(`서버가 이 창의 이어 읽기 위치를 받지 않았습니다: ${v.refusal}`);
  // 죽은 스트림과 「아직 아무 요청도 안 보낸 빈 대화」는 다르다. 빈 대화는 경고가 아니라 안내다
  // (사용자 지적 2026-09-05).
  if (!v.live && v.empty && !v.refusal) {
    return { text: '아직 대화가 없습니다 — 첫 요청을 보내면 그때부터 옵니다.', hidden: false, kind: 'info' };
  }
  if (!v.live) parts.push('대화 스트림이 끊겼습니다 — 새 말이 안 옵니다.');
  return { text: parts.join(' · '), hidden: parts.length === 0, kind: 'warn' };
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
 * 인용 한 조각의 몸통. 「빈 문단」과 「글을 못 읽었다」는 **다른 문장**이다 — 뒤엣것을
 * 앞엣것으로 적으면 사람도 모델도 빈 칸을 채우러 간다.
 */
export function quoteBody(q) {
  if (q.text) return `"${q.preview()}"`;
  return q.textUnavailable ? '(글을 못 읽었습니다)' : '(빈 문단)';
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
  // 사용자 말풍선은 상태로 모양이 갈린다 — 대기 중인 말은 옅게(점선·흐린 글자), 처리 중인 말은 그대로.
  return `turn kind-${r.kind}` + (r.kind === 'user' && r.status ? ` status-${r.status}` : '');
}

/**
 * 이 줄에 붙일 머리. 없으면 머리 없이 글만 — 사용자와 모델의 말이 그렇다.
 *
 * `note` 만 줄을 들여다본다. 「사람이 아닌 배우가 넣었다」는 **배우를 밝혔을 때만** 할 수 있는
 * 말이고, 안 밝힌 줄에 그렇게 적으면 모르는 것을 아는 것처럼 적는 것이다(`Row.attributed`).
 */
/** 사용자 말풍선에 붙는 상태 표시. 끝난 말엔 아무것도 안 붙인다. */
export function userBadge(status) {
  if (status === 'running') return { kind: 'running', text: '처리 중' };
  if (status === 'queued') return { kind: 'queued', text: '대기 중 — 지금 일이 끝나면 이어서 처리합니다' };
  return null;
}

export function headOf(r) {
  if (r.kind !== 'note') return ROW_HEAD[r.kind];
  return r.attributed ? noteHead(r.actor) : '⟳ 누가 넣었는지 안 밝힌 줄';
}

/**
 * 사람이 아닌 쪽이 대화에 끼운 줄의 머리 — **누가, 무엇을** 끼웠는지로 적는다. 앞 판본의
 * 「사람이 아닌 배우가 넣은 줄」은 코어의 actor 필드를 직역한 말이라 사람이 읽을 말이 아니었다
 * (사용자 지적 2026-09-05: 「용어 좀 잘 골라봐라」). 코어의 배우 id 는 `steer`·`interject`·
 * `council`·`loop`·`compact`·`plugin`·`hook`·`handoff`·`orchestrator` 등이다(internal/app).
 */
export function noteHead(actor) {
  const kind = String(actor?.kind ?? '');
  const id = String(actor?.id ?? '');
  if (kind === 'agent') return `⟳ 다른 에이전트(${id || '이름 없음'})가 넣은 줄`;
  switch (id) {
    case 'steer': return '⟳ 중간에 보낸 지시 — 사용자의 말을 magi 가 대화에 끼웠습니다';
    case 'interject': return '⟳ 끼어든 말 — magi 가 대화에 끼웠습니다';
    case 'council': return '⟳ 카운슬이 넣은 줄';
    case 'compact': return '⟳ 대화 압축 — magi 가 앞부분을 요약해 넣었습니다';
    case 'plugin': return '⟳ 플러그인이 넣은 줄';
    case 'hook': return '⟳ 훅이 넣은 줄';
    case 'handoff': return '⟳ 넘겨받은 일 — magi 가 넣었습니다';
    default: return `⟳ magi 가 넣은 줄${id ? ` (${id})` : ''}`;
  }
}

/** 종류별 줄머리. */
const ROW_HEAD = {
  think: 'Thinking (사용자에게 한 말이 아님)',
  note: '⟳ magi 가 넣은 줄',   // 실제 머리는 noteHead(actor) 가 배우별로 고른다
  tool: '⚙',
  error: '오류',
  // 짝이 되는 호출 줄을 못 찾은 것들. 보통은 호출 줄에 접히므로(`Transcript.append`) 이
  // 머리가 보인다는 것은 **이 창이 로그 중간부터 읽기 시작했다**는 뜻이고, 그건 사실이라
  // 적는다 — 「왜 답만 덩그러니 있나」를 사람이 이 줄로 안다.
  result: '⚙ 앞부분을 못 본 도구 호출의 결과',
  permission: '⚙ 앞부분을 못 본 도구 호출의 권한 결정',
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
  if (r.kind === 'fold') return 'fold';
  if (r.kind === 'council') return 'council';
  // **혼잣말은 접힌다.** 사용자에게 한 말이 아닌데 답풍선과 같은 자리를 통째로 먹고 있었다 —
  // 도형 하나에 호출 하나인 이 제품에서 그 글은 길고, 348×391 에서는 답을 밀어낸다.
  if (r.kind === 'think') return 'think';
  return 'text';
}

/**
 * **지금 도는 중인가.** 상단 진행 막대가 이 값으로 뜨고 진다.
 *
 * 상태를 따로 묻지 않고 **로그에서 유도한다.** 사람이 말을 냈고 그 뒤로 끝난 턴이 없으면 도는
 * 중이다 — 이 저장소가 화면 값을 다루는 규칙이 그렇다: 적어 두면 조건이 사라져도 남고,
 * 유도하면 같이 사라진다.
 *
 * 도구가 도는 동안 글이 몇 분에 한 줄뿐인 제품이라(도형마다 한 호출) 이 막대가 없으면
 * 「멈춘 것」과 「도는 중」이 화면에서 같아 보인다. 실제로 그 물음을 받았다(2026-09-04).
 */
export function turnRunning(rows) {
  const list = Array.isArray(rows) ? rows : [];
  let lastUser = -1;
  for (let i = list.length - 1; i >= 0; i -= 1) {
    if (list[i]?.kind === 'user') { lastUser = i; break; }
  }
  if (lastUser < 0) return false;
  // 그 뒤에 **끝난 턴**이 있으면 끝난 것이다.
  for (let i = lastUser + 1; i < list.length; i += 1) {
    if (list[i]?.kind === 'turn') return false;
  }
  return true;
}

/**
 * 접힌 혼잣말의 **요약 한 줄**. 웹 콘솔과 같은 모양이다 —
 * `ConversationElement` 가 `tr("row.reasoning") + " · " + Rows.oneLine(text, 80)` 으로 짓는다.
 *
 * **첫 줄을 미리 보여 주는 것이 요점이다.** 손잡이만 있으면 열기 전에는 무슨 생각인지 모르고,
 * 열어 보지 않으면 안 열게 된다. 미리보기가 있으면 열지 말지를 열기 전에 정한다.
 */
export function thinkHead(r) {
  const one = oneLine(r?.text ?? '');
  return one ? `Thinking · ${clip(one, 80)}` : 'Thinking';
}

/** 줄바꿈과 이어진 공백을 한 칸으로. 요약은 **한 줄**이어야 줄 높이가 안 흔들린다. */
export function oneLine(s) {
  return String(s ?? '').replace(/\s+/g, ' ').trim();
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
  return { done: '완료', continue: '계속', abstain: '보류' }[d] ?? (d || '(정보 없음)');
}

/**
 * 표결 수. **다시 세지 않는다** — 규칙(과반·만장일치·가중치)을 이 창이 한 벌 더 가지면,
 * 둘이 어긋나는 날 화면이 로그와 다른 결론을 적는다.
 */
export function tallyText(t) {
  if (!t) return '';
  const bits = [];
  for (const [k, label] of [['done', '완료'], ['continue', '계속'], ['abstain', '보류']]) {
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
    const who = c.members.length ? c.members.join(' · ') : '(구성원 정보 없음)';
    return `⚖ ${c.round}회차 판정 — ${who}${c.rule ? ` (${c.rule})` : ''}`;
  }
  if (c.stage === 'verdict') {
    const who = c.member || '(누군지 정보 없음)';
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
/** 접은 줄의 글. 줄어든 것은 대화다 — 시스템·도구 목록은 접히지 않는다. */
export function foldText(r) {
  const bytes = Number(r?.fold?.bytes) || 0;
  if (bytes > 0) return `도구 결과 하나를 덜어냈습니다 — ${kilo(Math.round(bytes / 4))} 토큰쯤 · 다시 읽으면 돌아옵니다`;
  const b = Number(r?.fold?.before) || 0; const a = Number(r?.fold?.after) || 0;
  if (b <= 0 && a <= 0) return '컨텍스트를 접었습니다';
  const shed = b - a;
  return `컨텍스트를 접었습니다 — ${kilo(b)} → ${kilo(a)} 토큰${shed > 0 ? ` · ${kilo(shed)} 덜어냄` : ''}`;
}

export function endText(r) {
  return r.unverified
    ? `검증 없이 끝남${r.reason ? ` — ${r.reason}` : ''}`
    : '— 응답 끝 —';
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
        r.paragraph ? `문단 ${r.paragraph}` : '어느 문단인지 전달되지 않았습니다',
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
 * 처음 뜰 때 **이 창이 어느 대화인가**를 적는다.
 *
 * 앞 판본은 「…에 붙어 있습니다 — 바로 시키시면 됩니다」였다. 사용자가 지웠다(2026-09-05):
 * 붙었다는 것은 아래 브랜드 줄이 이미 말하고, 시키면 된다는 것은 입력창이 있다는 사실이 말한다.
 * **아무것도 더 안 알려 주는 문장**이었다.
 *
 * 대신 **대화 이름**을 적는다. 창을 둘 띄우면 어느 창이 어느 대화인지가 화면 어디에도 없었고,
 * 오늘 그것 때문에 한나절을 썼다 — 빈 작업창, 남의 덱에 간 호출, 아무도 못 듣는 권한 확인 요청.
 * 사람이 그 이름을 볼 수 있으면 그 셋이 전부 눈으로 갈린다.
 *
 * 첫 줄이 서면 사라지는 것은 그대로다 — 그때부터는 대화 자체가 증거다.
 *
 * @param {string|null} bound 붙은 컴패니언 이름. 없으면 아직 안 붙은 것이다.
 * @param {number} rowCount 대화에 선 줄 수.
 * @param {string} [session] 이 창이 붙은 대화 이름.
 */
export function readyText(bound, rowCount, session = '') {
  // **컴패니언 이름은 이 문장에 안 쓰인다.** 그런데 조건에는 남아 있었고, 첫 폴이 그 이름보다
  // 먼저 오면 그리는 쪽은 빈 문자열을 받았다 — 그리고 부르는 자리의 걸쇠는 이미 잠겨서 다시
  // 안 그렸다. 사람이 세 번 「똑같다」고 한 자리다(2026-09-05).
  //
  // 적을 것은 **대화 이름**이다. 그것이 있으면 적고, 첫 줄이 서면 사라진다.
  if (rowCount > 0) return '';
  return session ? `대화 ${session}` : '';
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
      // **켜고 끄는 것은 스위치다 — 아이콘 단추가 아니다.**
      //
      // 앞 판본은 `◉`/`○` 아이콘 단추였다. M3 가 그 셋을 갈라 두는 기준이 명시적이다:
      // 체크박스는 목록에서 **여럿**, 라디오는 **하나**, 스위치는 **독립적인 설정**. 가이드는
      // 서로 무관하고 하나씩 켜고 꺼지며 **저장 없이 즉시** 먹으므로 스위치가 그 자리다.
      // 그리고 `◉`/`○` 는 읽는 사람에게 **라디오의 모양**이라, 「이 중 하나만」으로 읽힌다.
      //
      // 툴팁은 **동작을 적는다 — 아이콘 이름이 아니라**(M3 icon-buttons: "a tooltip describing
      // its action, rather than the name of the icon itself"). 켜짐/꺼짐은 **두 속성**으로
      // 말한다: 자리(핸들이 왼쪽/오른쪽)와 **핸들 크기**(16→24). 색 하나로만 가르지 않는다 —
      // 같은 문서의 "at least two properties, rather than just color".
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
 * **되돌릴 수 없는 것을 누르기 전에 한 번 더 묻는 판.**
 *
 * `window.confirm` 을 쓰던 자리다. 그게 이 판에서 위험한 이유는 안 뜰 수 있어서인데, 안 뜨면
 * `undefined` 가 돌아오고 그러면 **지우기가 조용히 아무 일도 안 한다** — 거절도 아니고 실패도
 * 아닌, 눌렀는데 아무 말이 없는 단추가 된다. Office 작업창은 우리가 고른 브라우저가 아니다.
 *
 * 글을 여기서 짓는 것은 늘 같은 이유다: 화면 밖이라야 잰다.
 */
export function confirmAsk(what, name) {
  if (what === 'council') {
    // **컴패니언을 다시 띄우는 일이다** — 이 창의 대화만이 아니라 같은 데몬에 붙은 다른 창·클라이언트·플러그인이
    // 다 끊겼다 다시 붙는다(사용자 2026-09-06: 「그 데몬 쓰는 다른 플러그인도 다 영향을 받을거잖아」). 그 사실을
    // 누르기 전에 적는다. 문서는 안 건드린다.
    const on = name === 'on';
    return {
      head: `카운슬을 ${on ? '켭니다' : '끕니다'} — 컴패니언을 다시 띄웁니다`,
      body: '이 컴패니언(magi 데몬)이 다시 뜹니다. 지금 대화는 새로 시작되고 도는 턴은 중단됩니다. 같은 데몬에 붙어 있는 '
        + '다른 창·터미널·IDE·플러그인도 모두 끊겼다 다시 붙습니다. 문서는 그대로입니다.',
      ok: '다시 띄웁니다',
      cancel: '그만둡니다',
      danger: true,
    };
  }
  if (what === 'delete-guide') {
    return {
      head: `${name} 를 지웁니다`,
      // **끄는 길이 바로 옆에 있다**는 것을 여기서 말한다. 그 말이 없으면 「잠깐 안 쓰려고」
      // 지우는 사람이 생기고, 그건 되돌릴 곳이 없다.
      body: '되돌릴 수 없습니다. 잠시 안 쓸 것이면 지우지 말고 스위치를 끄세요 — 글은 그대로 남습니다.',
      ok: '지웁니다',
      cancel: '그만둡니다',
      danger: true,
    };
  }
  return null;
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
  const from = Number(sel?.from) || 0;
  if (from < 1) {
    return { text: '', note: '어느 문단인지 못 읽었습니다. 문단 번호를 적어서 시키세요.' };
  }
  const to = Number(sel?.to) || from;
  const where = to > from ? `문단 ${from}–${to}` : `문단 ${from}`;
  return {
    text: `${where} 을 검토해 주세요. read_html 로 모양을 확인하고, 고칠 것을 짚어 주세요. `
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
  list_paragraphs: '문단 목차 읽기', read_paragraphs: '문단 읽기', read_document: '문서 살펴보기', find: '찾기', read_table: '표 읽기',
  read_html: '모양 보기(HTML)', read_comments: '메모 읽기', read_footnotes: '각주 읽기', list_images: '그림 목록', render_page: '쪽 그림 보기', read_content_controls: '콘텐츠 컨트롤 읽기', list_shapes: '도형 목록', read_tracked_changes: '변경 내역 읽기', describe_style: '이 문서 서식 읽기',
  snapshot_paragraphs: '되돌릴 지점 만들기', read_tags: '기록 읽기', read_suggestions: '제안 읽기', advise: '안내 붙이기', clear_advice: '안내 지우기',
  insert_paragraphs: '문단 넣기', replace_paragraph: '문단 글 바꾸기', delete_paragraphs: '문단 지우기', set_style: '스타일 걸기',
  format_text: '글자 서식', format_paragraph: '문단 서식', insert_table: '표 넣기', set_table_cells: '표 칸 쓰기', add_table_rows: '표 행 넣기',
  delete_table: '표 지우기', format_table: '표 서식', format_table_cells: '표 칸 서식', edit_table: '표 행·열 고치기', insert_list: '목록 넣기', set_list: '목록으로', insert_image: '그림 넣기', format_image: '그림 크기·설명', delete_image: '그림 지우기',
  insert_break: '나누기 넣기', insert_field: '필드 넣기', insert_footnote: '각주 달기', delete_footnote: '각주 지우기', set_style_format: '스타일 정의', set_page_setup: '쪽 설정', insert_content_control: '콘텐츠 컨트롤 넣기', set_content_control: '콘텐츠 컨트롤 채우기', delete_content_control: '콘텐츠 컨트롤 떼기', insert_shape: '도형 넣기', format_shape: '도형 서식', delete_shape: '도형 지우기', move_paragraphs: '문단 옮기기', insert_file: '문서 파일 넣기', set_header_footer: '머리글·바닥글', set_hyperlink: '링크', replace_all: '찾아 바꾸기',
  add_comment: '메모 달기', reply_comment: '메모 답글', resolve_comment: '메모 해결', add_bookmark: '책갈피 넣기', delete_bookmark: '책갈피 지우기',
  set_track_changes: '변경 추적', review_changes: '변경 수락·거부', set_properties: '문서 속성', restore_paragraphs: '되돌리기',
  set_tag: '기록 남기기', suggest: '제안 붙이기', drop_suggestion: '제안 떼기', land: '끝 신고',
}));

export function toolLabel(name) {
  if (!name) return '(이름 없음)';
  const short = name.startsWith('mcp__word__') ? name.slice('mcp__word__'.length) : name;
  return TOOL_LABELS.get(short) ?? name;
}

/** 표시 이름을 가진 도구들. `smoke` 가 카탈로그와 견주는 데 쓴다. */
export function labelledTools() { return [...TOOL_LABELS.keys()]; }

/**
 * 카운슬 단추의 글. **동작을 적는다** — 지금 켜져 있으면 「끕니다」, 꺼져 있으면 「켭니다」. 그리고 값을 적는다:
 * 누르면 컴패니언이 다시 뜨고 대화가 새로 시작된다(helper/council.go). 그 말이 title 에 없으면 사람은
 * 대화가 왜 사라졌는지 모른다. 모르는 상태(null)면 켜는 쪽으로 적되 「모름」을 붙인다.
 */
/** 컨텍스트를 이루는 다섯 조각 — 요청에 실리는 순서(웹 콘솔 DetailElement.MAKEUP 과 같은 순서·같은 이름). */
export const CONTEXT_PARTS = Object.freeze([
  ['system', '시스템'], ['tools', '도구 목록'], ['talk', '대화'], ['calls', '호출'], ['results', '결과'],
]);
const commas = (n) => String(Math.round(Number(n) || 0)).replace(/\B(?=(\d{3})+(?!\d))/g, ',');

/**
 * 컨텍스트 띠 — 얼마나 찼고 **무엇으로** 찼나.
 *
 * 총량만 보여 주면 사람은 대화를 줄이러 간다. 이 하네스에서 대화는 대개 작은 쪽이고, 매 요청에 실려 가는 도구
 * 목록이 가장 크다. 그래서 띠의 조각은 장식이 아니라 「무엇을 줄여야 하는가」의 답이다(웹 콘솔과 같은 이유).
 * 다섯 조각이 전부 0 이면 측정이 아니라 **모름**이라 띠를 안 그린다 — 창을 모를 때 퍼센트를 안 적는 것과 같다.
 */
/** 토큰은 k 단위로 — 30,861 은 「31k」, 5,703 은 「5.7k」, 500 은 그대로(사용자 요청 2026-09-06). */
export function kilo(n) {
  const v = Number(n) || 0;
  if (v < 1000) return String(v);
  if (v < 10000) return `${(v / 1000).toFixed(1).replace(/\.0$/, '')}k`;
  return `${Math.round(v / 1000)}k`;
}

export function contextMeter(st) {
  if (!st || typeof st !== 'object') return { hidden: true, text: '', title: '', segments: [], keys: [], pct: null, compactDisabled: true };
  const used = Number(st.used) || 0; const window = Number(st.window) || 0;
  if (used <= 0 && window <= 0) return { hidden: true, text: '', title: '', segments: [], keys: [], pct: null, compactDisabled: true };
  const pct = window > 0 ? Math.min(100, Math.round(used * 100 / window)) : null;
  const parts = st.parts && typeof st.parts === 'object' ? st.parts : {};
  const sum = CONTEXT_PARTS.reduce((a, [k]) => a + (Number(parts[k]) || 0), 0);
  // **눈금은 모델의 창이다.** 조각을 합에 맞춰 늘이면 띠가 늘 가득 차 보여 「얼마나 남았나」가 안 보인다
  // (사용자 지적 2026-09-06). 창을 알면 창이 100% 고 안 찬 자리는 빈 채로 둔다; 창을 모르면 합(또는
  // 제공자가 센 값 중 큰 쪽)에 맞춘다 — 그때는 가득 찬 띠가 「모른다」의 모양이다.
  const scale = window > 0 ? window : Math.max(sum, used);
  const segments = sum > 0 && scale > 0
    ? CONTEXT_PARTS.filter(([k]) => (Number(parts[k]) || 0) > 0)
      .map(([k, label]) => ({ kind: k, label, tokens: Number(parts[k]), pct: Math.min(100, Number(parts[k]) * 100 / scale), title: `${label} · ${kilo(parts[k])}` }))
    : [];
  const keys = segments.map((s) => ({ kind: s.kind, text: `${s.label} ${kilo(s.tokens)}` }));
  const text = `${st.estimated ? '~' : ''}${kilo(used)}${window > 0 ? ` / ${kilo(window)}` : ''} 토큰`
    + (pct != null ? ` · ${pct}%` : '') + (Number(st.messages) > 0 ? ` · 메시지 ${commas(st.messages)}` : '');
  const folds = Number(st.compactions) || 0;
  const note = folds > 0 ? `접기 ${folds}회 · ${kilo(st.shed)} 토큰 덜어냄` : '';
  return {
    hidden: false, pct, text, note, segments, keys,
    title: [text, ...segments.map((s) => s.title), note].filter(Boolean).join(' · '),
    // 도구 목록·시스템은 접어도 안 준다 — 대화가 없으면 접을 것이 없다.
    compactDisabled: (Number(parts.talk) || 0) + (Number(parts.calls) || 0) + (Number(parts.results) || 0) === 0 && sum > 0,
  };
}

/**
 * 프로바이더·모델 고르기. 프로바이더 목록은 지금 카탈로그를 답하는 심들이고, 모델 목록은 고른 프로바이더의 것
 * (없으면 데몬이 답한 것). **지금 것은 목록에 없어도 선다** — 안 서면 「고른 적 없음」과 「고른 것을 못 그림」이
 * 같은 빈칸이 된다.
 */
export function modelPicker(m) {
  const providers = Array.isArray(m?.providers) ? m.providers : [];
  const backend = String(m?.backend ?? ''); const current = String(m?.model ?? '');
  const on = providers.find((p) => p.base === backend);
  const provOptions = providers.map((p) => ({ value: p.base, text: p.name || p.base, selected: p.base === backend }));
  if (backend && !on) provOptions.unshift({ value: backend, text: `${backend} (명단 밖)`, selected: true });
  let names = (on && Array.isArray(on.models) && on.models.length > 0) ? on.models : (Array.isArray(m?.models) ? m.models : []);
  names = [...new Set(names)];
  if (current && !names.includes(current)) names = [current, ...names];
  const modelOptions = names.map((n) => ({ value: n, text: n, selected: n === current }));
  const empty = provOptions.length === 0 && modelOptions.length === 0;
  return {
    providers: provOptions, models: modelOptions, empty,
    note: empty ? (m?.warning || m?.error || '고를 것이 없습니다 — 답하는 프로바이더가 없습니다') : (m?.warning || ''),
    title: `${on?.name || backend || '백엔드 모름'} · ${current || '모델 모름'}`,
  };
}

export function councilButton(on) {
  const known = typeof on === 'boolean';
  const pressed = on === true;
  const verb = pressed ? '끕니다' : '켭니다';
  const state = known ? (pressed ? '지금 켜짐' : '지금 꺼짐') : '지금 상태 모름';
  return {
    pressed,
    title: `카운슬을 ${verb} — ${state}. 누르면 컴패니언이 다시 뜨고 새 대화로 시작합니다(문서는 그대로)`,
  };
}
