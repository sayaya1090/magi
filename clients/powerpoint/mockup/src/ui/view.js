// 얇은 뷰. **결정을 안 한다** — 유스케이스를 부르고 결과를 그린다.
import { Advice } from '../domain/Advice.js';
import { DECISIONS, WIDTH_NOTE, CLEARED } from '../domain/Pending.js';

const $ = (sel) => document.querySelector(sel);

export class View {
  constructor({ conversation, quoteSelection, pointAt, sendTurn, chat, deck, watchPrompt }) {
    this.conversation = conversation;
    this.quoteSelection = quoteSelection;
    this.pointAt = pointAt;
    this.sendTurn = sendTurn;
    this.chat = chat;
    this.deck = deck;
    /** 없을 수도 있다(문이 없는 자리). 없으면 그 칸은 **안 그린다** — 빈 칸을 지어내지 않는다. */
    this.watchPrompt = watchPrompt ?? null;
    this.advices = [];
    this.slideNos = null;   // null = 안 물어봤거나 못 얻었다. 그때는 id 로 적는다.
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
    this.chat.subscribe((ev) => this.onEvent(ev));
    this.renderPending();
    this.renderTurns();
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

  async onSend() {
    const text = $('#input').value;
    const turn = await this.sendTurn.run(text);
    if (!turn) return;
    $('#input').value = '';
    this.renderPending();
    this.renderTurns();
  }

  onEvent(ev) {
    if (ev.kind === 'thinking') { this.setThinking(true); return; }
    if (ev.kind === 'say') {
      this.setThinking(false);
      this.conversation.hear(ev.text);
      this.renderTurns();
      return;
    }
    if (ev.kind === 'advise') {
      this.advices.push(new Advice(ev.advice));
      this.renderAdvice();
      // 번호는 덱에 물어야 안다. 먼저 id 로 그려 놓고, 답이 오면 다시 그린다 — 목록이 늦게 뜨는
      // 것보다 늦게 예뻐지는 쪽이 낫다. 순서가 바뀌었을 수 있으니 **매번 다시 묻는다.**
      this.deck.slideNumbers().then((m) => {
        this.slideNos = m;
        this.renderAdvice();
      }).catch(() => {});
    }
  }

  setThinking(on) { $('#thinking').hidden = !on; }

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
    for (const q of this.conversation.pending) {
      box.append(this.quoteEl(q, true));
    }
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
        this.conversation.detach(q.shapeId);
        this.renderPending();
      });
      el.append(x);
    }
    return el;
  }

  renderTurns() {
    const box = $('#turns');
    box.replaceChildren();
    for (const t of this.conversation.turns) {
      const el = document.createElement('div');
      el.className = `turn ${t.role}`;
      for (const q of t.quotes) el.append(this.quoteEl(q, false));
      const p = document.createElement('p');
      p.textContent = t.text;
      el.append(p);
      box.append(el);
    }
    box.scrollTop = box.scrollHeight;
  }

  renderAdvice() {
    const box = $('#advice');
    box.replaceChildren();
    $('#advice-wrap').hidden = this.advices.length === 0;
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
      if (a.pointable) {
        const where = document.createElement('div');
        where.className = 'advice-target';
        const no = this.slideNos?.get(a.slideId);
        where.textContent = [`슬라이드 ${no ?? a.slideId}`, ...a.shapeIds].join(' · ');
        el.append(where);
      }
      // **누를 때만 선택을 옮긴다**(§6.1) — 자동으로는 절대 안 한다.
      el.addEventListener('click', async () => {
        const { ok, reason } = await this.pointAt.run(a);
        if (!ok) this.note(reason);
      });
      box.append(el);
    }
  }
}
