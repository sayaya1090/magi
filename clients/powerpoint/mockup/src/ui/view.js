// 얇은 뷰. **결정을 안 한다** — 유스케이스를 부르고 결과를 그린다.
import { foldAdvice } from '../domain/AdviceBoard.js';
import { targetLabel } from '../domain/Advice.js';
import { DECISIONS, WIDTH_NOTE, CLEARED } from '../domain/Pending.js';

const $ = (sel) => document.querySelector(sel);

export class View {
  constructor({ composer, quoteSelection, pointAt, sendTurn, deck, watchPrompt, readTranscript }) {
    this.composer = composer;
    this.quoteSelection = quoteSelection;
    this.pointAt = pointAt;
    this.sendTurn = sendTurn;
    this.deck = deck;
    /** 없을 수도 있다(문이 없는 자리). 없으면 그 칸은 **안 그린다** — 빈 칸을 지어내지 않는다. */
    this.watchPrompt = watchPrompt ?? null;
    /** 대화가 흘러 들어오는 자리. **화면의 대화는 전부 여기서 나온다**(§5.7). */
    this.readTranscript = readTranscript ?? null;
    this.advices = [];
    this.adviceNote = '';
    /**
     * 덱이 준 번호표(Map) 또는 못 얻었다는 뜻의 `null`. **둘을 「안 물어봤다」와 안 뭉친다** —
     * 뭉치려면 `slideNumbers` 가 빈 Map 을 줬어도 됐고, 포트가 굳이 null 을 고른 이유가 없어진다.
     */
    this.slideNos = null;
    /** 물어보고 답을 받았는가. 이게 없으면 `slideNos === null` 이 두 가지 뜻을 갖는다. */
    this.slideNosAnswered = false;
    /**
     * 마지막으로 그린 물음의 모양. 폴은 계속 도는데 그때마다 다시 그리면 사람이 고르던 것과
     * 적던 글이 지워진다 — `WatchPrompt`가 값에서 하는 일을 화면에서도 한 번 더 한다.
     */
    this.askSig = null;
  }

  mount() {
    $('#adapter').textContent = this.deck.label;
    this.renderCaps();
    $('#quote').addEventListener('click', () => this.onQuote());
    // **누르기 전 읽기**(S14 의 대조군). 호버는 포커스를 안 옮기므로 여기서 읽은 선택이
    // 「작업창이 포커스를 가져가기 전」의 값이다. 들어올 때마다 덮어써서 낡지 않게 둔다.
    $('#quote').addEventListener('pointerenter', () => this.quoteSelection.sampleBeforeFocus());
    $('#send').addEventListener('click', () => this.onSend());
    $('#input').addEventListener('keydown', (e) => {
      if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) this.onSend();
    });
    this.renderPending();
    if (this.readTranscript) {
      this.readTranscript.onChange = () => this.onLog();
      this.onLog();
    }
    if (this.watchPrompt) {
      this.watchPrompt.onChange = () => this.renderAsk();
      this.renderAsk();
    }
  }

  /**
   * 데몬이 막혀서 묻는 자리.
   *
   * 여기서 정하는 것 셋. **하나 — 모르는 종류에는 단추를 안 준다**(§5.7). 그린 단추는 눌리고,
   * 누른 답은 코어에서 떨어지되 *"이미 결정됐거나 만료됐다"*는 **틀린 사유**로 떨어진다.
   * **둘 — 보낸 뒤에도 물음을 안 내린다.** 내려가는
   * 것은 다음 `status`가 말하는 것이고, 미리 내리면 실패한 답이 성공처럼 보인다. 대신 단추를
   * 잠근다. **셋 — 폭을 단추 문구에 적는다.** 「허용」이라고만 쓰면 세션 전체를 여는 줄 모르고
   * 누른다.
   */
  renderAsk() {
    if (!this.watchPrompt) return;
    const v = this.watchPrompt.view;
    this.renderDoing(v.doing);

    // 같은 것을 다시 그리지 않는다. 적던 글과 포커스가 이 한 줄에 달려 있다.
    const sig = [v.pending?.id ?? '', v.pending?.kind ?? '', v.answered ? '1' : '0',
      v.reachable ? '1' : '0', v.clearedBy ?? ''].join('|');
    if (sig === this.askSig) return;

    // **먼저 만들고 나중에 갈아 끼운다.** 만들다 터지면 직전 화면이 그대로 서 있고 다음 폴이
    // 다시 시도한다. 표시를 먼저 남기면 한 번 터진 물음은 **영영 안 그려지고**, 데몬은 바로
    // 그 물음에 막혀 있으므로 빈 칸 하나가 사람을 가둔다(§5.7). `null`은 쓸 말이 없다는 뜻이다.
    const el = !v.reachable ? this.lostEl(v.lostNote)
      : !v.pending ? this.lastAskEl(v.clearedBy)
      : v.pending.known ? this.askEl(v) : this.unknownAskEl(v);

    const box = $('#ask');
    box.replaceChildren();
    if (el) box.append(el);
    box.hidden = el == null;
    this.askSig = sig;
  }

  renderDoing(doing) {
    const el = $('#doing');
    el.textContent = doing ?? '';
    el.hidden = !doing;
  }

  lostEl(text) {
    const el = document.createElement('div');
    el.className = 'ask-lost';
    el.textContent = text;
    return el;
  }

  /**
   * 직전 물음이 **왜** 내려갔는지 한 줄. 「없다」만 남기면 이 창이 답한 것과 남이 답한 것이
   * 화면에서 똑같이 생긴다. 「무엇으로」는 안 적는다 — 남이 답한 것을 이 창은 모른다.
   */
  lastAskEl(clearedBy) {
    const text = {
      [CLEARED.answered]: '직전 물음: 답을 보냈고 내려갔습니다.',
      [CLEARED.elsewhere]: '직전 물음: 다른 곳에서 답했습니다 — 무엇으로 답했는지는 모릅니다.',
      [CLEARED.unreachable]: '직전 물음: 데몬에 못 닿아 내려갔습니다 — 끝난 것이 아닙니다.',
    }[clearedBy];
    if (!text) return null;
    const el = document.createElement('p');
    el.className = 'ask-last';
    el.textContent = text;
    return el;
  }

  unknownAskEl(v) {
    const box = document.createElement('div');
    box.className = 'ask-box unknown';
    const h = document.createElement('h2');
    h.textContent = '이 창이 답할 수 없는 물음';
    const p = document.createElement('p');
    p.className = 'ask-unknown';
    p.textContent = v.unknownKindNote;
    box.append(h, p);   // **단추는 없다.** 없는 것이 이 칸의 내용이다.
    return box;
  }

  askEl(v) {
    const p = v.pending;
    const box = document.createElement('div');
    box.className = 'ask-box';
    const h = document.createElement('h2');
    h.textContent = p.isPermission ? '권한을 묻고 있습니다' : '묻고 있습니다';
    box.append(h);

    const what = document.createElement('p');
    what.className = 'ask-what';
    what.textContent = p.what || '(무엇인지 안 실렸습니다)';
    box.append(what);

    // 정해진 것은 **도구 이름이 아니라 인자다.** "permission: bash"는 아무도 못 답한다.
    if (p.args != null) {
      const pre = document.createElement('pre');
      pre.className = 'ask-args';
      pre.textContent = typeof p.args === 'string' ? p.args : this.pretty(p.args);
      box.append(pre);
    }
    if (p.reason) {
      const r = document.createElement('p');
      r.className = 'ask-reason';
      r.textContent = `멈춘 이유: ${p.reason}`;
      box.append(r);
    }
    if (p.placement) {
      const pl = document.createElement('p');
      pl.className = 'ask-place';
      pl.textContent = `${p.placement} — 이걸 답하면 다음 물음이 옵니다.`;
      box.append(pl);
    }
    // 근거는 **접지 않는다.** 접힌 근거는 안 읽히고, 안 읽힌 근거로 누른 것은 판단이 아니다.
    if (p.report.length) {
      const dl = document.createElement('dl');
      dl.className = 'ask-report';
      for (const sec of p.report) {
        const dt = document.createElement('dt');
        dt.textContent = sec.key;
        const dd = document.createElement('dd');
        dd.textContent = sec.text;
        dl.append(dt, dd);
      }
      box.append(dl);
    }

    box.append(p.isPermission ? this.decisionsEl(v) : this.choicesEl(v));
    if (p.isPermission) {
      const note = document.createElement('p');
      note.className = 'ask-note';
      note.textContent = WIDTH_NOTE;
      box.append(note);
    }
    if (v.answered) {
      const sent = document.createElement('p');
      sent.className = 'ask-note';
      sent.textContent = '답을 보냈습니다 — 물음이 내려가기를 기다립니다.';
      box.append(sent);
    }
    return box;
  }

  decisionsEl(v) {
    const row = document.createElement('div');
    row.className = 'ask-picks';
    for (const d of DECISIONS) {
      const b = document.createElement('button');
      b.className = d.width === 'call' ? 'ghost' : 'ghost wide';
      b.textContent = d.label;   // 문구가 **폭을 말한다**(§5.7).
      b.disabled = v.answered;
      b.addEventListener('click', () => this.send(() => this.watchPrompt.answer(d.value)));
      row.append(b);
    }
    return row;
  }

  choicesEl(v) {
    const wrap = document.createElement('div');
    const picks = document.createElement('div');
    picks.className = 'ask-picks';
    for (const opt of v.pending.options) {
      const b = document.createElement('button');
      b.className = 'ghost';
      b.textContent = opt;
      b.disabled = v.answered;
      b.addEventListener('click', () => this.send(() => this.watchPrompt.choose(opt)));
      picks.append(b);
    }
    if (v.pending.options.length) wrap.append(picks);

    // 고를 것이 있어도 **적는 칸을 남긴다** — 코어는 글을 그대로 받고, 보기 넷이 다 틀린
    // 경우가 물음이 사람에게 온 이유일 때가 있다.
    const row = document.createElement('div');
    row.className = 'ask-text';
    const input = document.createElement('input');
    input.type = 'text';
    input.placeholder = '답을 적습니다';
    input.disabled = v.answered;
    const b = document.createElement('button');
    b.className = 'primary';
    b.textContent = '보내기';
    b.disabled = v.answered;
    const go = () => {
      const t = input.value.trim();
      if (!t) return;
      this.send(() => this.watchPrompt.choose(t));
    };
    input.addEventListener('keydown', (e) => { if (e.key === 'Enter') go(); });
    b.addEventListener('click', go);
    row.append(input, b);
    wrap.append(row);
    return wrap;
  }

  /**
   * 답 보내기 한 번. **실패를 삼키지 않는다** — 유스케이스가 거절한 것(종류가 다르다, 이미
   * 보냈다)도 문이 죽은 것도 여기서는 똑같이 「안 갔다」이고, 안 간 것을 조용히 두면 사람은
   * 답이 간 줄 안다.
   */
  async send(fn) {
    try {
      await fn();
    } catch (e) {
      this.note(`답을 못 보냈습니다: ${e.message}`, { sticky: true });
    }
  }

  pretty(v) {
    try { return JSON.stringify(v, null, 2); } catch { return String(v); }
  }

  /**
   * 호스트가 무엇을 지원한다고 말했는지 한 줄.
   *
   * **안 잰 것을 잰 것처럼 안 적는다.** 가짜 덱이면 사유를 그대로 띄운다. 화면에도 쓰고 콘솔에도
   * 한 줄 남기는데, 작업창은 닫히면 사라지지만 이 값이 필요한 순간은 대개 뭔가 안 될 때라 그때
   * 남아 있는 쪽이 콘솔이다.
   */
  renderCaps() {
    const el = $('#caps');
    if (!el) return;
    const c = (typeof this.deck.capabilities === 'function')
      ? this.deck.capabilities()
      : { measured: false, note: '어댑터가 안 답한다', sets: [] };
    if (!c.measured) {
      el.dataset.measured = 'no';
      el.textContent = `요구 집합: ${c.note || '안 잼'}`;
    } else {
      el.dataset.measured = 'yes';
      // ok 가 null 인 것은 "아니오"가 아니라 **물어보다 던졌다**이므로 `?` 로 갈라 둔다.
      el.textContent = '요구 집합: ' + c.sets
        .map((s) => `${s.name} ${s.version} ${s.ok === true ? '✓' : s.ok === false ? '✗' : '?'}`)
        .join(' · ');
    }
    console.log('[magi] ' + el.textContent);
  }

  async onQuote() {
    const { added, skipped, empty, reason, beforeCount } = await this.quoteSelection.run();
    if (empty) {
      // **사유를 뭉개지 않는다.** 셋이 다른 말이고, 셋째는 「모른다」다 — 앞 읽기가 없는 채로
      // 「안 골랐다」라고 적으면 그게 S14 를 못 재게 만드는 그 뭉갬이다.
      if (reason === 'lostFocus') {
        this.note(`선택이 날아갔습니다 — 누르기 직전엔 ${beforeCount}개를 잡고 있었습니다. (S14)`);
      } else if (reason === 'unknown') {
        this.note('잡힌 도형이 없습니다 — 누르기 전 읽기가 없어 '
          + '"안 골랐다"와 "포커스가 가져갔다"를 못 가릅니다.');
      } else {
        this.note('잡힌 도형이 없습니다 — 캔버스에서 도형을 클릭한 뒤 다시 눌러 주세요.');
      }
      return;
    }
    if (skipped) this.note(`${skipped}개는 이미 인용돼 있습니다.`);
    this.renderPending();
    if (added.length) $('#input').focus();
  }

  /**
   * 보낸다. **화면에 미리 붙이지 않는다**(§5.7).
   *
   * 낸 것을 그 자리에서 대화에 붙이면 로그가 같은 말을 다시 실어 올 때 두 벌이 되고, 신원으로
   * 걸러 낼 방법이 없다 — `submit` 은 식별자를 안 돌려주고 밖에서 붙은 창은 전부 `attach` 로
   * 찍힌다(`Composer` 주석). 그래서 컴포저는 **쥔 채 잠기고**, 지우는 것은 로그의 메아리다.
   */
  async onSend() {
    const log = this.logShape();
    const r = await this.sendTurn.run($('#input').value, log);
    if (!r.sent) {
      if (r.why === 'failed') this.note(`못 보냈습니다: ${r.error.message}`, { sticky: true });
      if (r.why === 'waiting') this.note('앞서 낸 말이 아직 로그에 안 떴습니다.');
      return;
    }
    if (r.blind) {
      // 갔지만 확인할 길이 없다. **글을 안 지운다** — 지우면 「갔다」를 말한 셈이 된다.
      this.note('보냈습니다. 이 창이 로그를 못 읽고 있어 갔는지 확인은 못 합니다 — '
        + '적은 글은 그대로 뒀습니다.', { sticky: true });
    }
    this.renderPending();
    this.renderSent();
  }

  /** 지금 로그에서 보이는 것. 없으면 **읽는 중이 아니다**(`live:false`). */
  logShape() {
    const v = this.readTranscript?.view;
    if (!v) return { userRows: 0, live: false };
    return { userRows: v.rows.filter((r) => r.kind === 'user').length, live: v.live };
  }

  /** 로그가 움직였다. 여기 하나로 대화·안내·컴포저가 다 따라간다. */
  onLog() {
    const v = this.readTranscript.view;
    if (this.sendTurn.settle(this.logShape().userRows)) {
      // 메아리가 왔다 — 이제 지운다.
      $('#input').value = '';
      this.renderPending();
    }
    this.renderStream(v);
    this.renderRows(v.rows);
    this.renderUnknown(v.unknownNote);
    this.renderAdviceFrom(v.rows);
    this.renderSent();
  }

  /**
   * 스트림 자체에 대한 한 줄. **조용한 대화와 죽은 스트림을 가른다** — 문은 깨끗한 끝을
   * 에러로 안 주므로, 이 줄이 없으면 사람은 안 오는 답을 영원히 기다린다.
   */
  renderStream(v) {
    const el = $('#stream');
    const parts = [];
    if (v.refusal) parts.push(`서버가 이 창의 커서를 안 받았습니다: ${v.refusal}`);
    if (!v.live) parts.push('대화 스트림이 끊겼습니다 — 새 말이 안 옵니다.');
    el.textContent = parts.join(' · ');
    el.hidden = parts.length === 0;
  }

  renderUnknown(note) {
    const el = $('#unknown');
    el.textContent = note ?? '';
    el.hidden = !note;
  }

  /** 냈는데 아직 로그에 안 뜬 것. **나가는 문을 같이 준다** — 없으면 잠금이 사람을 가둔다. */
  renderSent() {
    const el = $('#sent');
    el.replaceChildren();
    el.hidden = !this.composer.waiting;
    if (!this.composer.waiting) return;
    const p = document.createElement('span');
    p.textContent = '보냈습니다 — 로그에 뜨기를 기다립니다.';
    const b = document.createElement('button');
    b.className = 'ghost';
    b.textContent = '그만 기다리기';
    b.title = '잠금만 풉니다. 적은 글은 안 지웁니다 — 갔는지는 여전히 모릅니다.';
    b.addEventListener('click', () => {
      this.composer.release();
      this.renderSent();
      this.renderPending();
    });
    el.append(p, b);
  }

  /**
   * 한 줄 알림. `sticky` 면 안 사라진다.
   *
   * 사라지는 알림은 **방금 누른 사람**에게 하는 말이고, 안 사라지는 것은 **화면이 지금 거짓말을
   * 하고 있다**는 말이다. 둘을 같은 수명으로 두면 뒤엣것이 4초 만에 없어진다.
   */
  note(text, { sticky = false } = {}) {
    const el = $('#note');
    el.textContent = text;
    el.hidden = false;
    clearTimeout(this._noteTimer);
    if (!sticky) this._noteTimer = setTimeout(() => { el.hidden = true; }, 4000);
  }

  renderPending() {
    const box = $('#pending');
    box.replaceChildren();
    // 기다리는 동안은 **빼지도 못한다.** 이미 나간 글에 붙어 나간 인용이라, 여기서 빼면
    // 화면과 모델이 본 것이 갈린다.
    const locked = this.composer.waiting;
    for (const q of this.composer.pending) {
      box.append(this.quoteEl(q, !locked));
    }
    box.classList.toggle('locked', locked);
    $('#send').disabled = locked;
  }

  quoteEl(q, removable) {
    const el = document.createElement('div');
    el.className = 'quote';
    const head = document.createElement('div');
    head.className = 'quote-head';
    head.textContent = `${q.where} · ${q.headline}`;
    const body = document.createElement('div');
    body.className = 'quote-body';
    body.textContent = q.text ? `"${q.preview()}"` : '(글 없음)';
    const meta = document.createElement('div');
    meta.className = 'quote-meta';
    meta.textContent = [q.type, q.sizeLabel].filter(Boolean).join(' · ');
    el.append(head, body, meta);
    if (removable) {
      const x = document.createElement('button');
      x.className = 'quote-x';
      x.textContent = '×';
      x.title = '인용 빼기';
      x.addEventListener('click', () => {
        this.composer.detach(q.shapeId);
        this.renderPending();
      });
      el.append(x);
    }
    return el;
  }

  /**
   * 대화. **로그가 그린다** — 이 창이 따로 쌓아 두는 대화는 없다.
   *
   * 종류마다 다르게 그리는 것이 이 칸의 일이다. 다 말풍선으로 그리면 모델의 혼잣말이 답이
   * 되고, 정책이 밀어 넣은 줄이 사람이 한 말이 되고, 슬라이드를 고친 도구 호출이 안 보인다
   * (§5.7). 매번 통째로 다시 그리는데, 여기엔 사람이 적던 것이 없어서 그래도 된다.
   */
  renderRows(rows) {
    const box = $('#turns');
    const atEnd = box.scrollHeight - box.scrollTop - box.clientHeight < 40;
    box.replaceChildren();
    for (const r of rows) box.append(this.rowEl(r));
    // 위로 올려 읽는 중이면 **안 끌어내린다.** 도구가 줄줄이 도는 턴에서 읽던 자리를 뺏는다.
    if (atEnd) box.scrollTop = box.scrollHeight;
  }

  rowEl(r) {
    const el = document.createElement('div');
    // 종류를 **접두사와 함께** 적는다. 그냥 `turn ${r.kind}` 로 적으면 끝난 턴이
    // `class="turn turn"` 이 되고, `.turn.turn` 은 CSS 에서 그냥 `.turn` 이라
    // 그 한 줄에 준 모양이 **모든 줄에** 걸린다. 실제로 사용자 말이 가운데 정렬됐었다.
    el.className = `turn kind-${r.kind}`;
    const head = ROW_HEAD[r.kind];
    if (head) {
      const h = document.createElement('div');
      h.className = 'turn-head';
      h.textContent = r.kind === 'tool' ? `⚙ ${r.tool ?? '(이름 없음)'}` : head;
      el.append(h);
    }
    if (r.kind === 'tool') {
      // **인자를 적는다.** 「set_text 를 불렀다」는 무엇이 바뀌었는지 안 알려 준다.
      const pre = document.createElement('pre');
      pre.className = 'turn-args';
      pre.textContent = r.args == null ? '(인자 없음)' : clip(this.pretty(r.args), 300);
      el.append(pre);
      return el;
    }
    if (r.kind === 'turn') {
      // 끝난 턴. **검증 못 한 착지를 보통 끝처럼 그리지 않는다**(`TurnFinishedData`).
      el.classList.toggle('unverified', r.unverified);
      const p = document.createElement('p');
      p.textContent = r.unverified
        ? `검증되지 않은 끝${r.reason ? ` — ${r.reason}` : ''}`
        : '— 턴 끝 —';
      el.append(p);
      return el;
    }
    const p = document.createElement('p');
    // 사용자 줄에는 인용이 **글로 접혀** 들어 있다(`promptOf`). 예쁘게 걷어 내지 않는다 —
    // 모델이 받은 것이 이것이고, 걷어 내면 화면이 모델보다 덜 아는 것을 감추게 된다.
    p.textContent = r.text || '(글 없음)';
    el.append(p);
    return el;
  }

  /**
   * 안내 층. **로그의 도구 호출에서 유도한다** — 따로 쌓아 두지 않는다(`AdviceBoard`).
   *
   * 번호는 덱에 물어야 안다. 먼저 id 로 그려 놓고, 답이 오면 다시 그린다 — 목록이 늦게 뜨는
   * 것보다 늦게 예뻐지는 쪽이 낫다. 순서가 바뀌었을 수 있으니 **매번 다시 묻는다.**
   */
  renderAdviceFrom(rows) {
    const { items, strays, dropped } = foldAdvice(rows);
    this.advices = items;
    const notes = [];
    // 이름이 우리 서버가 아니라서 못 붙인 것. **조용히 안 끝낸다** — 설정 한 줄이 기능을
    // 껐다는 사실이 화면 어딘가엔 있어야 한다.
    if (strays.length) {
      notes.push(`안내를 부른 도구가 이 창이 아는 이름이 아닙니다: ${strays.join(', ')}`);
    }
    if (dropped) notes.push(`안내 ${dropped}건은 무엇을 말하는지 안 실려 못 붙였습니다.`);
    this.adviceNote = notes.join(' · ');
    this.renderAdvice();
    if (items.length) {
      this.deck.slideNumbers().then((m) => {
        this.slideNos = m;
        this.slideNosAnswered = true;
        this.renderAdvice();
      }).catch(() => {
        // 던진 것도 **답이다** — "못 준다"는 답. 삼키면 목록이 영영 「확인 중」으로 남는다.
        this.slideNos = null;
        this.slideNosAnswered = true;
        this.renderAdvice();
      });
    }
  }

  renderAdvice() {
    const box = $('#advice');
    box.replaceChildren();
    $('#advice-wrap').hidden = this.advices.length === 0 && this.adviceNote === '';
    const note = $('#advice-strays');
    note.textContent = this.adviceNote;
    note.hidden = this.adviceNote === '';
    for (const a of this.advices) {
      const el = document.createElement('button');
      el.className = 'advice';
      el.disabled = !a.pointable;
      const what = document.createElement('div');
      what.textContent = a.message;
      el.append(what);
      // **가리킬 곳을 글로도 적는다**(§6.1 층 1: 슬라이드 · 도형 id · 무엇을 · 왜).
      // 목업은 축소판을 안 그리므로 여기가 유일하게 "어느 슬라이드냐"가 사는 곳이다.
      // 이게 없으면 사람이 알아내는 길이 **눌러 보는 것**뿐인데, 누르면 잡고 있던 선택을 뺏는다.
      // 안 눌리는 항목에는 **왜 안 눌리는지**가 그 자리에 온다. 회색으로만 두면 "모델이 어딜
      // 말 안 했다"와 "이 창이 고장났다"가 같은 화면이 된다.
      const where = document.createElement('div');
      where.className = 'advice-target';
      where.textContent = a.pointable
        ? targetLabel(a, this.slideNos, this.slideNosAnswered)
        : a.unpointableReason;
      el.append(where);
      // **누를 때만 선택을 옮긴다**(§6.1) — 자동으로는 절대 안 한다.
      el.addEventListener('click', async () => {
        const { ok, reason } = await this.pointAt.run(a);
        if (!ok) this.note(reason);
      });
      box.append(el);
    }
  }
}

/** 줄머리. 없는 종류는 머리 없이 글만 — 사용자와 모델의 말이 그렇다. */
const ROW_HEAD = {
  think: '혼잣말 (사용자에게 한 말이 아님)',
  note: '⟳ 사람이 아닌 배우가 넣은 줄',
  tool: '⚙',
  error: '오류',
};

function clip(s, n) { return s.length > n ? s.slice(0, n - 1) + '…' : s; }
