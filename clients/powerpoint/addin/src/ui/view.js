// 얇은 뷰. **결정을 안 한다** — 유스케이스를 부르고 결과를 그린다.
//
// 그 말이 한동안 거짓이었다. 돌연변이 32개 중 30개가 살아남았고, 살아남은 줄은 DOM 을 쓰는
// 코드 안에 박힌 **결정**이었다. 그래서 결정을 전부 `screen.js` 로 옮겼다 — 여기 남은 것은
// 만들고·붙이고·대입하는 일뿐이고, 이 파일에 `if` 가 늘면 그건 다시 못 재는 자리가 늘었다는
// 뜻이다.
//
// **남은 것 11개.** 옮기고 나서 다시 재니 이 파일에 아직 11개가 산다. 둘로 갈린다.
// 하나 — 순수 함수가 준 답으로 **갈래를 고르는 줄**(`kind === 'lost'`, `shape === 'tool'`).
// 고르는 근거는 `screen.js` 에서 재지만 고른 뒤 무엇을 만드는지는 DOM 이라, 갈래를 잘못
// 골라도 스위트는 조용하다. 둘 — **아무도 안 부르는 메서드의 대입**(`note`/`clearNote`/
// `where` 의 `hidden`). 결정이 없는 줄이라 옮길 것이 없다. 둘 다 **가짜 DOM 을 세워야**
// 재지고, 목업에 그런 것이 없다는 게 지금의 사실이다 — 모르는 채로 두는 것과 알고 두는
// 것은 다르므로 여기 적는다.
import { foldAdvice, adviceNote } from '../domain/AdviceBoard.js';
import { SlideNumbers } from '../domain/Advice.js';
import { logShapeOf, sendNote } from '../usecase/SendTurn.js';
import { quoteNote } from '../usecase/QuoteSelection.js';
import { askSig } from '../usecase/WatchPrompt.js';
import { DECISIONS, WIDTH_NOTE, askArgs } from '../domain/Pending.js';
// 화면이 **정하는 것**은 전부 여기 있다 — 이 파일은 부르고 대입만 한다(`screen.js` 머리).
import {
  isSendKey, askAction, askKind, askHead, whatText, argsText, placeLine, doingLine,
  lastAskShape, decisionClass, failNote, noteLife, capsOf, capsText, capsSummary, brandState, streamLine,
  unknownLine, skippedLine, quoteBody, quoteMeta, rowClass, rowHead, rowShape, argsCell, endText,
  bodyText, adviceBoard, adviceTargetText, pretty, resultCell, permissionText, councilBody,
} from './screen.js';

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
     * 덱이 준 번호표와 **그게 몇 번째 물음의 답인가**. 「못 얻었다(`map === null`)」와 「아직
     * 안 물어봤다」를 안 뭉치는 것이 첫 이유고(뭉치려면 `slideNumbers` 가 빈 Map 을 줬어도
     * 됐다), 「이 안내에 대해선 아직 안 물어봤다」까지 가르는 것이 둘째 이유다.
     */
    this.slideNos = new SlideNumbers();
    /**
     * 마지막으로 그린 물음의 모양. 폴은 계속 도는데 그때마다 다시 그리면 사람이 고르던 것과
     * 적던 글이 지워진다 — `WatchPrompt`가 값에서 하는 일을 화면에서도 한 번 더 한다.
     */
    this.askSig = null;
    /**
     * 컴패니언에 붙었는가. **붙기 전의 창에 「스트림이 끊겼다」·「데몬에 안 닿는다」를 띄우지
     * 않기 위한 값**이다 — 고르라는 화면 위에 그 배너가 겹쳐 뜨면 사람은 고르기 전에 이미
     * 고장 난 줄 안다(실물에서 그 화면을 보고 넣었다, 2026-09-01).
     */
    this.bound = false;
  }

  /** 붙었다/떨어졌다를 창에 알린다. 위 두 배너의 조건이 이 값 하나다. */
  setBound(b) {
    this.bound = Boolean(b);
    this.renderAsk();
    if (this.readTranscript) this.onLog();
  }

  mount() {
    $('#adapter').textContent = this.deck.label;
    this.renderCaps();
    // 브랜드 줄은 **처음부터 사실을 말한다.** 비워 두면 「아직 안 골랐다」와 「골랐는데 화면이
    // 안 그렸다」가 같은 빈칸이 된다.
    this.brand({ companion: null, streamLive: false });
    $('#quote').addEventListener('click',
      () => this.guard(() => this.onQuote(), '인용을 못 붙였습니다'));
    // **누르기 전 읽기**(S14 의 대조군). 호버는 포커스를 안 옮기므로 여기서 읽은 선택이
    // 「작업창이 포커스를 가져가기 전」의 값이다. 들어올 때마다 덮어써서 낡지 않게 둔다.
    $('#quote').addEventListener('pointerenter', () => this.quoteSelection.sampleBeforeFocus());
    $('#send').addEventListener('click', () => this.guard(() => this.onSend(), '못 보냈습니다'));
    $('#input').addEventListener('keydown', (e) => {
      if (isSendKey(e)) {
        this.guard(() => this.onSend(), '못 보냈습니다');
      }
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
    // **붙기 전에는 「못 닿는다」가 아니다**(`askKind`). 그 사실을 뷰가 값에 실어 준다 —
    // 판정은 화면 밖에서 하고, 여기서는 넘기기만 한다.
    const v = { ...this.watchPrompt.view, bound: this.bound };
    this.renderDoing(v.doing, v.doingFresh);

    // 같은 것을 다시 그리지 않는다. 적던 글과 포커스가 이 한 줄에 달려 있다. 무엇이 서명에
    // 들고 무엇이 일부러 빠지는지는 `askSig` 가 안다 — 화면 밖이라야 잰다.
    const sig = askSig(v);
    if (askAction(sig, this.askSig) === 'refresh') {
      // **줄 하나는 이 문 밖이다.** 서명에 없는 것이 하나 있다 — 뒤에 쌓인 물음의 수다. 같은
      // 물음을 보는 동안에도 뒤가 늘면 「모두 2개」가 3개가 되고, 서명이 그대로라 여기서
      // 돌아가면 그 줄은 **영영 안 고쳐진다**(없다가 생기는 경우엔 아예 안 뜬다). 서명에 넣어
      // 해결하면 안 되는데, 그러면 뒤가 늘 때마다 판이 다시 서서 사람이 적던 답이 지워진다 —
      // 이 문이 있는 바로 그 이유다. 좁힐 때 신선해야 하는 것은 좁히는 지점 **밖**에 둔다.
      this.refreshPlace(v);
      return;
    }

    // **먼저 만들고 나중에 갈아 끼운다.** 만들다 터지면 직전 화면이 그대로 서 있고 다음 폴이
    // 다시 시도한다. 표시를 먼저 남기면 한 번 터진 물음은 **영영 안 그려지고**, 데몬은 바로
    // 그 물음에 막혀 있으므로 빈 칸 하나가 사람을 가둔다(§5.7). `null`은 쓸 말이 없다는 뜻이다.
    const kind = askKind(v);
    // `none` 은 **아직 아무 데도 안 붙었다**는 뜻이라 이 칸에 그릴 것이 없다. `null` 은 칸을
    // 접는다 — 「물음이 없다」와 「붙기 전이다」를 같은 빈칸으로 두는 것이 맞는 유일한 자리다.
    const el = kind === 'none' ? null
      : kind === 'lost' ? this.lostEl(v.lostNote)
        : kind === 'last' ? this.lastAskEl(v.clearedBy)
          : kind === 'known' ? this.askEl(v) : this.unknownAskEl(v);

    const box = $('#ask');
    box.replaceChildren();
    if (el) box.append(el);
    box.hidden = el == null;
    this.askSig = sig;
  }

  /** 자리 하나에 문장 하나. 두 곳이 같은 말을 짓지 않게 한 자리에 둔다. */
  fillPlace(pl, placement) {
    const line = placeLine(placement);
    pl.textContent = line.text;
    pl.hidden = line.hidden;
  }

  /** 판을 안 다시 세우고 그 줄만 고친다 — 글자만 만지므로 적던 답과 포커스가 안 다친다. */
  refreshPlace(v) {
    const pl = $('#ask')?.querySelector('.ask-place');
    if (pl) this.fillPlace(pl, v.pending?.placement ?? null);
  }

  /**
   * 데몬이 하는 일. **못 닿는 동안엔 현재형으로 안 적는다** — 그 말의 근거는 방금 읽은
   * status 뿐이라, 못 닿으면 근거 없이 「지금 …하는 중」이라고 말하는 것이 된다. 지우지도
   * 않는다: 마지막으로 뭘 하다 놓쳤는지는 사람이 알아야 할 것이고, 그건 지난 일이라 여전히
   * 참이다. 시제를 바꾸는 것이 곧 그 차이를 적는 일이다.
   */
  renderDoing(doing, fresh) {
    const el = $('#doing');
    const line = doingLine(doing, fresh);
    el.textContent = line.text;
    el.hidden = line.hidden;
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
    // 글은 `clearedNote` 가 짓는다 — 화면 밖이라야 잰다. `null` 은 **할 말이 없다**는 뜻이고
    // 그때만 이 줄이 안 선다. 모르는 사유는 조용히 숨는 대신 제 말을 갖고 온다.
    const shape = lastAskShape(clearedBy);
    if (!shape.show) return null;
    const el = document.createElement('p');
    el.className = 'ask-last';
    el.textContent = shape.text;
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
    h.textContent = askHead(p);
    box.append(h);

    const what = document.createElement('p');
    what.className = 'ask-what';
    what.textContent = whatText(p);
    box.append(what);

    // 정해진 것은 **도구 이름이 아니라 인자다.** "permission: bash"는 아무도 못 답한다.
    // 그래서 실린 게 없으면 **그 사실이 이 칸의 내용이다**(`askArgs`) — 칸을 없애면 사람은
    // 무엇을 허가하는지 모른다는 것조차 모른 채 누른다.
    const slot = askArgs(p);
    if (slot?.note) {
      const miss = document.createElement('p');
      miss.className = 'ask-args-missing';
      miss.textContent = slot.note;
      box.append(miss);
    } else if (slot) {
      const pre = document.createElement('pre');
      pre.className = 'ask-args';
      pre.textContent = argsText(slot);
      box.append(pre);
    }
    if (p.reason) {
      const r = document.createElement('p');
      r.className = 'ask-reason';
      r.textContent = `멈춘 이유: ${p.reason}`;
      box.append(r);
    }
    // **자리를 늘 만든다.** 뒤에 물음이 더 쌓이면 이 줄은 없다가 생기는데, 없는 자리는 갈아
    // 끼울 데가 없어서 판을 통째로 다시 세워야 하고, 그러면 사람이 적던 답이 지워진다.
    const pl = document.createElement('p');
    pl.className = 'ask-place';
    box.append(pl);
    this.fillPlace(pl, p.placement);
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
      b.className = decisionClass(d);
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
  async send(fn) { await this.guard(fn, '답을 못 보냈습니다'); }

  /**
   * 단추 하나가 하는 일 전체를 감싼다 — **단추는 조용히 죽지 않는다.**
   *
   * 안쪽의 유스케이스들은 던지는 대신 사유를 값에 실어 돌려주기로 돼 있다(`SendTurn` 의
   * `why`, `QuoteSelection` 의 `reason`, `PointAtAdvice` 의 `ok/reason` 셋 다 그렇다).
   * 그 약속이 지켜지는 동안 이 자리는 아무 일도 안 한다.
   *
   * 문제는 그것이 **약속일 뿐**이라는 것이었다. 한 군데서 깨진 날 — 덱이 선택을 안 내주면
   * `QuoteSelection.run` 이 그대로 던졌다 — 이벤트 리스너의 거절은 콘솔까지만 가고 누른
   * 사람에게는 **아무 일도 일어나지 않는다.** 그러면 그 침묵은 「안 골랐다」와 똑같이 생겨서
   * 사람이 제 탓으로 읽는다. 사유를 값에 싣는 것과 같은 이야기다: **안 실리면 없는 일이 된다.**
   *
   * 그래서 약속을 믿는 대신 깨져도 화면에 뜨게 한다. 이름을 가진 사유가 언제나 낫지만
   * (`readFailed` 처럼), 이름 없는 사유도 침묵보다는 낫다.
   */
  async guard(fn, what) {
    // **서 있던 사유를 여기서 물린다.** 사유는 「이번 한 번의 일」이므로 다음 누름까지가 수명인데,
    // `sticky` 는 스스로 안 사라지고 성공 갈래는 부를 `note` 가 없다(`onQuote` 가 그렇다). 그래서
    // 성공한 자리마다 「지워라」를 손으로 적으면 하나 빠뜨리는 날 화면이 거짓말을 한다 — 실제로
    // 읽기 실패 뒤 재시도가 성공해도 「덱이 답하지 않았습니다」가 그대로 서 있었다. 미는 자리가
    // 아니라 **누름이 반드시 지나는 자리**에 두면 빠뜨릴 곳이 없다.
    this.clearNote();
    try {
      await fn();
    } catch (e) {
      const n = failNote(what, e);
      this.note(n.text, { sticky: n.sticky });
    }
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
    const c = capsOf(this.deck);
    el.dataset.measured = c.measured ? 'yes' : 'no';
    const full = capsText(c);
    // 접히는 판이라 자리가 둘이다. **요약도 사실을 말한다** — 「다 좋다」로 접어 두면 접힌
    // 채로 거짓말을 하게 되므로, 안 쟀거나 빠진 것이 있으면 요약이 그것을 적는다.
    const summary = $('#caps-summary');
    const detail = $('#caps-detail');
    if (summary && detail) {
      summary.textContent = capsSummary(c);
      detail.textContent = full;
    } else {
      el.textContent = full;
    }
    console.log('[magi] ' + full);
  }

  /**
   * 브랜드 줄의 상태 한 마디. **붙은 곳과 손이 늘 보이는 자리**이고, 그것이 아래에 있는 이유는
   * 세로 391px 짜리 판에서 대화가 제일 넓어야 하기 때문이다(MS 지침의 크기 표).
   */
  brand(state) {
    const el = $('#brand-state');
    if (el) el.textContent = brandState(state);
  }

  async onQuote() {
    const { added, skipped, empty, reason, beforeCount } = await this.quoteSelection.run();
    if (empty) {
      // **사유를 뭉개지 않는다.** 넷이 다른 말이다 — 못 읽었다 / 날아갔다 / 모른다 / 안 골랐다.
      // 글은 `quoteNote` 가 짓는다: 화면 밖이라야 잰다.
      const n = quoteNote({ reason, beforeCount });
      this.note(n.text, { sticky: n.sticky });
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
    // 글은 `sendNote` 가 짓는다 — 화면 밖이라야 잰다. `null` 만 조용하다.
    const n = sendNote(r);
    if (n) this.note(n.text, { sticky: n.sticky });
    if (!r.sent) return;
    this.renderPending();
    this.renderSent();
  }

  /** 지금 로그에서 보이는 것. 셈은 `logShapeOf` 가 한다 — 화면 밖이라야 잰다. */
  logShape() { return logShapeOf(this.readTranscript?.view); }

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
    this.renderUnknown(v.unknownNote, v.skippedNote);
    this.renderAdviceFrom(v.rows);
    this.renderSent();
  }

  /**
   * 스트림 자체에 대한 한 줄. **조용한 대화와 죽은 스트림을 가른다** — 문은 깨끗한 끝을
   * 에러로 안 주므로, 이 줄이 없으면 사람은 안 오는 답을 영원히 기다린다.
   */
  renderStream(v) {
    const el = $('#stream');
    const line = streamLine({ ...v, bound: this.bound });
    el.textContent = line.text;
    el.hidden = line.hidden;
  }

  /**
   * 못 그리는 것과 **안 그리기로 한 것**을 같은 칸에, 다른 줄로 적는다. 한 문장으로 합치면
   * 「이 창을 고쳐야 한다」와 「이대로가 맞다」가 같은 말이 되고, 그런 줄은 곧 안 읽힌다.
   */
  renderUnknown(note, skipped) {
    const el = $('#unknown');
    el.replaceChildren();
    const un = unknownLine(note);
    const sk = skippedLine(skipped);
    if (!un.hidden) {
      const d = document.createElement('div');
      d.textContent = un.text;
      el.append(d);
    }
    if (!sk.hidden) {
      const d = document.createElement('div');
      d.className = 'skipped';
      d.textContent = sk.text;
      el.append(d);
    }
    el.hidden = un.hidden && sk.hidden;
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
   * **방금 누른 것이 어떻게 됐는가.** `sticky` 면 저 혼자 안 사라진다.
   *
   * 사라지는 알림은 **읽고 나면 볼일이 끝나는** 말이고(「3개는 이미 인용돼 있습니다」), 안
   * 사라지는 것은 **화면이 지금 거짓말을 하고 있다**는 말이다. 둘을 같은 수명으로 두면 뒤엣것이
   * 4초 만에 없어진다.
   *
   * 「안 사라진다」의 끝은 **다음 누름**이다(`guard` 가 물린다). 사유가 말하는 조건은 사람이
   * 다시 누르는 순간 더는 재 본 것이 아니기 때문이다 — 「잠시 뒤 다시 눌러 주세요」가 성공한
   * 재시도 뒤에도 서 있으면, 그 문장은 자기가 부른 행동에 의해 거짓이 된다.
   *
   * 이 판이 무엇인지는 여기 안 쓴다 — `where` 가 다른 자리에 쓴다.
   */
  note(text, opts) {
    const el = $('#note');
    el.textContent = text;
    el.hidden = false;
    clearTimeout(this._noteTimer);
    const life = noteLife(opts);
    if (life !== null) this._noteTimer = setTimeout(() => { el.hidden = true; }, life);
  }

  /** 서 있던 사유를 물린다. 누름이 시작될 때 `guard` 가 부른다. */
  clearNote() {
    clearTimeout(this._noteTimer);
    const el = $('#note');
    el.textContent = '';
    el.hidden = true;
  }

  /**
   * **이 판이 무엇인가.** 어느 호스트에 붙었는지, 못 붙었으면 왜인지.
   *
   * `note` 와 **자리가 다르다.** 한 칸을 같이 쓰면 첫 누름의 사유가 이 사실을 덮고, 그 뒤로
   * 사람은 자기가 PowerPoint 안이 아니라는 걸 영영 알 수 없다. 반대 방향으로도 샌다: 위
   * `guard` 가 누름마다 사유를 물리는데, 같은 칸이면 그 물림이 판 사실까지 같이 지운다.
   * 수명이 다른 두 말이라 자리를 나눈다 — 이건 창이 사는 동안 계속 참이다.
   *
   * 그래서 **지우는 문이 없다.** 늦게 풀린 호스트처럼 사실이 바뀌면 새 문장으로 덮는다.
   */
  where(text) {
    const el = $('#where');
    el.textContent = text;
    el.hidden = false;
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
    // 「글이 없다」와 「글을 못 읽었다」는 다른 문장이다 — 뒤엣것을 앞엣것으로 적으면 사람도
    // 모델도 빈 상자를 고치러 간다.
    body.textContent = quoteBody(q);
    const meta = document.createElement('div');
    meta.className = 'quote-meta';
    meta.textContent = quoteMeta(q);
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
    // 스크롤을 **가운데 영역이 갖는다.** 대화 칸이 자기 스크롤을 갖던 시절의 코드라, 자리를
    // 옮긴 뒤에도 같은 계산이 서게 상자를 골라 쓴다 — 없으면 예전처럼 대화 칸이다.
    // **자리가 둘이다 — 재는 상자와 담는 상자.** 스크롤은 가운데 영역이 갖고(`#scroll`), 줄이
    // 들어가는 것은 대화 칸이다(`#turns`). 한 변수로 뭉쳤더니 `replaceChildren` 이 **스크롤
    // 영역을 통째로 비웠고**, 화면에서는 요구 집합도 컴패니언 카드도 사라진 것으로 보였다
    // (실물에서 그 화면을 보고 갈랐다, 2026-09-01).
    const box = $('#turns');
    const scroller = $('#scroll') ?? box;
    const atEnd = scroller.scrollHeight - scroller.scrollTop - scroller.clientHeight < 40;
    box.replaceChildren();
    for (const r of rows) box.append(this.rowEl(r));
    // 위로 올려 읽는 중이면 **안 끌어내린다.** 도구가 줄줄이 도는 턴에서 읽던 자리를 뺏는다.
    if (atEnd) scroller.scrollTop = scroller.scrollHeight;
  }

  rowEl(r) {
    const el = document.createElement('div');
    // 종류를 **접두사와 함께** 적는다. 그냥 `turn ${r.kind}` 로 적으면 끝난 턴이
    // `class="turn turn"` 이 되고, `.turn.turn` 은 CSS 에서 그냥 `.turn` 이라
    // 그 한 줄에 준 모양이 **모든 줄에** 걸린다. 실제로 사용자 말이 가운데 정렬됐었다.
    el.className = rowClass(r);
    const head = rowHead(r);
    if (head) {
      const h = document.createElement('div');
      h.className = 'turn-head';
      h.textContent = head;
      el.append(h);
    }
    const shape = rowShape(r);
    if (shape === 'tool') {
      // **인자를 적는다.** 「set_text 를 불렀다」는 무엇이 바뀌었는지 안 알려 준다.
      if (r.kind === 'tool') {
        const pre = document.createElement('pre');
        pre.className = 'turn-args';
        pre.textContent = argsCell(r);
        el.append(pre);
      }
      // 허락과 답은 **같은 줄에** 붙는다(`Transcript.append` 가 `callId` 로 접었다). 따로
      // 세우면 도구가 줄줄이 도는 턴에서 「무엇을 불렀나」와 「어떻게 됐나」의 짝이 안 맞는다.
      const perm = permissionText(r);
      if (perm) {
        const p = document.createElement('div');
        p.className = 'turn-perm';
        p.textContent = perm;
        el.append(p);
      }
      const res = resultCell(r);
      if (res) {
        // **`isError` 하나로 ✗ 를 찍지 않는다** — `advisory` 는 「했는데 읽을 것이 붙었다」다
        // (`ToolResult.Advisory`). 이 제품에서 그 오독은 「슬라이드가 안 바뀌었다」로 읽힌다.
        el.classList.toggle('failed', res.failed);
        el.classList.toggle('advisory', res.advisory);
        const h = document.createElement('div');
        h.className = 'turn-result-head';
        h.textContent = `${res.mark} ${res.head}`;
        el.append(h);
        if (res.text) {
          const pre = document.createElement('pre');
          pre.className = 'turn-result';
          pre.textContent = res.text;
          el.append(pre);
        }
      } else if (r.kind === 'tool') {
        // 답이 아직 안 왔다. **비워 두는 것이 사실이다** — 「됐습니다」를 미리 적으면 실패한
        // 호출이 성공으로 보이는 구간이 생긴다.
        const h = document.createElement('div');
        h.className = 'turn-result-head pending';
        h.textContent = '⋯ 답을 기다립니다';
        el.append(h);
      }
      return el;
    }
    if (shape === 'council') {
      // 종료 게이트. **머리만으로 뜻이 서는 줄이 있다**(소집·결론) — 없는 몸통을 「(글 없음)」
      // 으로 채우면 화면이 빈 칸을 결함처럼 보이게 한다.
      const body = councilBody(r);
      if (body) {
        const p = document.createElement('p');
        p.textContent = body;
        el.append(p);
      }
      return el;
    }
    if (shape === 'turn') {
      // 끝난 턴. **검증 못 한 착지를 보통 끝처럼 그리지 않는다**(`TurnFinishedData`).
      el.classList.toggle('unverified', r.unverified);
      const p = document.createElement('p');
      p.textContent = endText(r);
      el.append(p);
      return el;
    }
    const p = document.createElement('p');
    // 사용자 줄에는 인용이 **글로 접혀** 들어 있다(`promptOf`). 예쁘게 걷어 내지 않는다 —
    // 모델이 받은 것이 이것이고, 걷어 내면 화면이 모델보다 덜 아는 것을 감추게 된다.
    p.textContent = bodyText(r);
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
    this.adviceNote = adviceNote({ strays, dropped });
    // 이 안내들의 슬라이드를 **언제 처음 봤는지** 먼저 적는다. 물음보다 앞이라야 「그 뒤에 던진
    // 물음의 답」이라는 말이 성립한다.
    for (const a of items) this.slideNos.note(a.slideId);
    this.renderAdvice();
    if (items.length) {
      const token = this.slideNos.ask();
      this.deck.slideNumbers()
        .then((m) => { if (this.slideNos.answer(token, m)) this.renderAdvice(); })
        // 던진 것도 **답이다** — "못 준다"는 답. 삼키면 목록이 영영 「확인 중」으로 남는다.
        .catch(() => { if (this.slideNos.answer(token, null)) this.renderAdvice(); });
    }
  }

  renderAdvice() {
    const box = $('#advice');
    box.replaceChildren();
    const board = adviceBoard(this.advices, this.adviceNote);
    $('#advice-wrap').hidden = board.wrapHidden;
    const note = $('#advice-strays');
    note.textContent = board.noteText;
    note.hidden = board.noteHidden;
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
      where.textContent = adviceTargetText(a, this.slideNos.map, this.slideNos.answered(a.slideId));
      el.append(where);
      // **누를 때만 선택을 옮긴다**(§6.1) — 자동으로는 절대 안 한다.
      //
      // `guard` 를 지난다. `PointAtAdvice.run` 은 오늘 약속을 지키지만(던지는 대신 `ok/reason`
      // 을 싣는다), **그 약속을 못 믿겠다는 것이 `guard` 가 있는 이유**다 — 그 문서주석이
      // 이름을 댄 셋 중 하나가 바로 이 유스케이스인데, 셋 중 둘만 감싸여 있었다. 여기서 깨지면
      // async 리스너의 거절이라 갈 곳이 없다: 콘솔에만 남고 누른 사람에게는 **아무 일도 안
      // 일어난다.** 실측했다 — 쪽지가 안 바뀌어서, 마침 서 있던 앞 쪽지가 이 누름의 답인 척
      // 자리를 지켰다. 침묵보다 나쁘다.
      el.addEventListener('click', () => this.guard(async () => {
        const { ok, reason } = await this.pointAt.run(a);
        if (!ok) this.note(reason);
      }, '안내를 못 따라갔습니다'));
      box.append(el);
    }
  }
}
