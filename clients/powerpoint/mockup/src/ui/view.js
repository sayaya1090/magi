// 얇은 뷰. **결정을 안 한다** — 유스케이스를 부르고 결과를 그린다.
import { Advice } from '../domain/Advice.js';

const $ = (sel) => document.querySelector(sel);

export class View {
  constructor({ conversation, quoteSelection, pointAt, sendTurn, chat, deck }) {
    this.conversation = conversation;
    this.quoteSelection = quoteSelection;
    this.pointAt = pointAt;
    this.sendTurn = sendTurn;
    this.chat = chat;
    this.deck = deck;
    this.advices = [];
    this.slideNos = null;   // null = 안 물어봤거나 못 얻었다. 그때는 id 로 적는다.
  }

  mount() {
    $('#adapter').textContent = this.deck.label;
    this.renderCaps();
    $('#quote').addEventListener('click', () => this.onQuote());
    $('#send').addEventListener('click', () => this.onSend());
    $('#input').addEventListener('keydown', (e) => {
      if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) this.onSend();
    });
    this.chat.subscribe((ev) => this.onEvent(ev));
    this.renderPending();
    this.renderTurns();
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
    const { added, skipped, empty } = await this.quoteSelection.run();
    if (empty) {
      // **사유를 뭉개지 않는다.** 아무것도 안 잡혔을 수도 있고, 포커스가 창으로 오면서
      // 선택이 날아갔을 수도 있다(S14). 목업은 그 둘을 구분 못 한다는 사실 자체를 말한다.
      this.note('잡힌 도형이 없습니다 — 캔버스에서 도형을 클릭한 뒤 다시 눌러 주세요.');
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

  note(text) {
    const el = $('#note');
    el.textContent = text;
    el.hidden = false;
    clearTimeout(this._noteTimer);
    this._noteTimer = setTimeout(() => { el.hidden = true; }, 4000);
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
