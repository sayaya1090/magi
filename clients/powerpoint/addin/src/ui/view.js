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
import { mdToDom, looksLikeMd } from './md.js';
import { DECISIONS, WIDTH_NOTE, askArgs } from '../domain/Pending.js';
// 화면이 **정하는 것**은 전부 여기 있다 — 이 파일은 부르고 대입만 한다(`screen.js` 머리).
import {
  isSendKey, askAction, askReveal, askKind, askHead, whatText, argsText, placeLine, doingLine,
  lastAskShape, decisionClass, failNote, noteLife, capsOf, capsText, capsSummary, brandState, streamLine,
  unknownLine, skippedLine, quoteBody, quoteMeta, rowClass, rowHead, rowShape, argsCell, endText,
  bodyText, adviceBoard, adviceTargetText, pretty, resultCell, permissionText, councilBody,
  fixBoard, adapterText, readyText, planBoard, changedLines,
  planAnchor, reviewAsk, appendAsk, confirmAsk, thinkHead, turnRunning,
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
     * 덱에 저장된 제안. **손이 있어야 읽을 수 있다** — 없으면 이 층은 아예 안 뜬다(빈 목록을
     * 그리면 「제안이 없다」와 「못 읽었다」가 같은 화면이 된다).
     */
    this.hand = null;
    this.fixes = [];
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
    // **예상 밖일 때만 적는다**(`adapterText`). 진짜 호스트면 빈 칸이라 판 한 줄이 는다.
    const adapter = $('#adapter');
    const label = adapterText(this.deck);
    adapter.textContent = label;
    adapter.hidden = label === '';
    // **줄 자체를 지운다.** 안쪽만 감추면 테두리 한 줄이 28px 를 그대로 먹는다 — 이름은 위에서
    // PowerPoint 가 그리므로 이 줄에 남은 말은 이것 하나뿐이고, 그것이 없으면 줄도 없다.
    const head = $('#head');
    if (head) head.hidden = label === '';
    this.renderCaps();
    // 브랜드 줄은 **처음부터 사실을 말한다.** 비워 두면 「아직 안 골랐다」와 「골랐는데 화면이
    // 안 그렸다」가 같은 빈칸이 된다.
    this.brand({ companion: null, streamLive: false });
    $('#quote').addEventListener('click',
      () => this.guard(() => this.onQuote(), '인용을 못 붙였습니다'));
    // **누르기 전 읽기**(S14 의 대조군). 호버는 포커스를 안 옮기므로 여기서 읽은 선택이
    // 「작업창이 포커스를 가져가기 전」의 값이다. 들어올 때마다 덮어써서 낡지 않게 둔다.
    $('#quote').addEventListener('pointerenter', () => this.quoteSelection.sampleBeforeFocus());
    const review = $('#review');
    if (review) {
      review.addEventListener('click',
        () => this.guard(() => this.onReview(), '지금 보는 장을 못 읽었습니다'));
      // 인용 단추와 **같은 이유로** 들어올 때 미리 읽는다(S14). 저기서 잰 것은 도형이었고
      // 장은 아직 안 재 봤다 — 그래서 「재 봤으니 안 그런다」가 아니라 **옆 단추가 겪은 일을
      // 안 겪는 쪽**으로 둔다. 마우스가 아닌 손(키보드)에는 이 표본이 없으므로 그때는 즉석에서
      // 읽는다.
      review.addEventListener('pointerenter', () => {
        this.deck.selection().then((sel) => { this.reviewSample = sel; }).catch(() => {});
      });
    }
    $('#send').addEventListener('click', () => this.guard(() => this.onSend(), '못 보냈습니다'));
    $('#input').addEventListener('keydown', (e) => {
      if (isSendKey(e)) {
        this.guard(() => this.onSend(), '못 보냈습니다');
      }
    });
    this.renderPending();
    if (this.readTranscript) {
      this.readTranscript.onChange = () => this.onLogSoon();
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
  /** 물음 판도 스크롤 영역 **안에** 선다 — 서고 나면 바닥이 밀린다. 그래서 같이 감싼다. */
  renderAsk() { this.keepingEnd(() => this.drawAsk()); }

  drawAsk() {
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
    // **막힌 물음은 화면 안으로 끌어온다.** 이 칸은 대화 아래에 서므로 대화가 길면 접힌 자리
    // 밖이고, 그러면 데몬은 답을 기다리고 사람은 물음을 못 본다(§5.7). 실물에서 그 화면을
    // 봤다(2026-09-01) — 권한 확인 요청이 떴는데 휠을 굴려야 나왔다. 무엇을 끌어올지는
    // `askReveal` 이 정한다(화면 밖이라야 잰다).
    if (el && askReveal(kind, askAction(sig, this.askSig))) {
      box.scrollIntoView({ block: 'nearest' });
    }
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
   * 직전 확인 요청이 **왜** 내려갔는지 한 줄. 「없다」만 남기면 이 창이 답한 것과 남이 답한 것이
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
    h.textContent = '이 창이 답할 수 없는 확인 요청';
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
      sent.textContent = '답을 보냈습니다 — 요청이 내려가기를 기다립니다.';
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
    // 잰 것을 **헬퍼에도 넘긴다.** 실패해도 화면은 그리던 대로 그린다 — 이 값은 부수적이다.
    try { this.tellCaps?.(capsOf(this.deck)); } catch { /* 화면이 먼저다 */ }
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
   * **지금 보고 있는 장을 봐 달라.**
   *
   * **안 보낸다 — 컴포저에 채워만 둔다.** 누름 하나로 나가면 사람이 안 읽은 말이 나가고,
   * 「특히 색 대비를」 같은 한 줄을 덧붙일 자리가 사라진다. 이 제품에서 한 번의 부탁은
   * 도구 수십 번이라, 잘못 나간 말의 값이 비싸다.
   *
   * 적던 글도 안 지운다(`appendAsk`) — 단추 하나가 쓰던 문단을 날리면 그 단추는 다시 안 눌린다.
   */
  async onReview() {
    const sel = this.reviewSample ?? await this.deck.selection();
    this.reviewSample = null;
    // 글은 `reviewAsk` 가 짓는다: 화면 밖이라야 잰다.
    const r = reviewAsk(sel);
    if (!r.text) {
      this.note(r.note, { sticky: true });
      return;
    }
    const input = $('#input');
    input.value = appendAsk(input.value, r.text);
    input.focus();
    // 덧붙인 뒤 이어 적을 수 있게 커서를 끝으로.
    if (input.setSelectionRange) input.setSelectionRange(input.value.length, input.value.length);
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
  /**
   * **한 프레임에 한 번만 다시 그린다.**
   *
   * `onChange` 는 글자 한 조각마다 뛴다 — 긴 턴 하나에 수천 번이다. 그리고 `renderRows` 는
   * 대화 칸을 통째로 비우고 **모든 줄을 새로 만든다.** 줄이 N 개, 사건이 M 개면 DOM 을
   * N×M 번 짓는 셈이라, 2만 5천 프레임짜리 대화에서는 수백만 번이 된다.
   *
   * 그러면 창이 느려지다 끝내 답을 못 하게 되고, **작업창이 죽으면 모델의 조작이 전부
   * 45초씩 죽는다.** 실물에서 그 사슬을 봤다(2026-09-03): 긴 판마다 작업창이 사라졌고,
   * 도구가 죽자 모델은 PowerShell COM 으로 우회했으며, 그건 사람이 쓰던 PowerPoint 를
   * 닫는다. 화면 하나가 느린 것으로 끝나지 않는다.
   *
   * 그림은 사람이 보는 것이라 **한 프레임에 한 번이면 충분하다.** 값은 그대로 쌓이고
   * 그리기만 모은다 — 마지막 상태는 같다.
   */
  onLogSoon() {
    if (this.#drawing) return;
    this.#drawing = true;
    // **`requestAnimationFrame` 은 안 쓴다.** 작업창이 접히거나 가려지면 그 콜백은 아예
    // 안 뛴다 — 브라우저에서 쟀다(2026-09-03): `visibilityState` 가 `hidden` 이면 800ms 를
    // 기다려도 한 번도 안 뛰었다. 그러면 화면이 느려지는 것이 아니라 **영영 안 그려진다.**
    // 사람이 창을 다시 펼 때까지 보낸 말도, 온 답도 안 보인다. 모아 그리려다 안 그리는 것과
    // 바꿀 수는 없다. 타이머는 가려져도 뛴다(느려질 뿐이고, 모아 그리기에는 그걸로 족하다).
    setTimeout(() => {
      this.#drawing = false;
      this.onLog();
    });
  }

  #drawing = false;

  onLog() {
    const v = this.readTranscript.view;
    if (this.sendTurn.settle(this.logShape().userRows)) {
      // 메아리가 왔다 — 이제 지운다.
      $('#input').value = '';
      this.renderPending();
    }
    // 판을 **전부 그린 뒤에** 바닥을 붙인다 — 계획·안내·요약 어느 것이 서든 스크롤 영역의
    // 높이가 그때 바뀐다.
    this.keepingEnd(() => {
      this.renderStream(v);
      this.renderRows(v.rows);
      this.renderPlan(v.todos);
      this.renderReady(v.rows.length);
      this.renderUnknown(v.unknownNote);
      this.renderAdviceFrom(v.rows);
      this.renderSent();
      this.renderBusy(v.rows);
    });
  }

  /**
   * 스트림 자체에 대한 한 줄. **조용한 대화와 죽은 스트림을 가른다** — 문은 깨끗한 끝을
   * 에러로 안 주므로, 이 줄이 없으면 사람은 안 오는 답을 영원히 기다린다.
   */
  renderStream(v) {
    const el = $('#stream');
    const line = streamLine({ ...v, bound: this.bound });
    el.textContent = line.text;
    el.classList.toggle('info', line.kind === 'info');
    el.hidden = line.hidden;
  }

  /**
   * **못 그리는 것만 적는다.**
   *
   * 두 줄이었다: 「그릴 줄 모르는 N건」과 「일부러 안 그린 N건」. 뜻이 달라서 칸을 나눴었는데,
   * 실제로 화면에 서 보니 **뒤엣것은 사람이 할 일이 없는 줄**이었다 — `context.usage` 는 턴마다
   * 수십 건 오고 `session.created` 는 대화의 첫 줄일 뿐이다. 348×391 에서 그 줄은 대화를
   * 밀어내는 값만 한다.
   *
   * 앞엣것은 남긴다. 그 줄의 뜻은 **「이 창을 고쳐야 한다」**이고, 실제로 그 줄을 보고
   * `todos.changed` 를 그리게 됐다.
   *
   * 세는 것을 그만두지는 않는다 — `skippedCounts` 는 그대로 돌고 시험이 그것을 본다. 화면에
   * 안 적을 뿐이다. 안 세면 나중에 「무엇을 안 그리기로 했었나」를 알 길이 없다.
   */
  renderUnknown(note) {
    const el = $('#unknown');
    el.replaceChildren();
    const un = unknownLine(note);
    if (!un.hidden) {
      const d = document.createElement('div');
      d.textContent = un.text;
      el.append(d);
    }
    el.hidden = un.hidden;
  }

  /** 냈는데 아직 로그에 안 뜬 것. **나가는 문을 같이 준다** — 없으면 잠금이 사람을 가둔다. */
  renderSent() {
    const el = $('#sent');
    el.replaceChildren();
    el.hidden = !this.composer.waiting;
    if (!this.composer.waiting) return;
    const p = document.createElement('span');
    p.textContent = '보냈습니다 — 대화 기록에 오르기를 기다립니다.';
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

  /**
   * 붙은 컴패니언 이름을 쥔다. **그리는 것은 `renderReady` 가 매 로그 변화마다 다시 한다** —
   * 여기서 그려 두면 조건(대화가 비었는가)이 바뀌어도 문장이 남는다.
   */
  ready(bound, session = '') {
    this.boundName = bound || null;
    this.boundSession = session || '';
    this.renderReady(this.readTranscript ? this.readTranscript.view.rows.length : 0);
  }

  /**
   * 계획 판. 결정은 `planBoard` 가 하고 여기서는 그리기만 한다.
   *
   * **기본은 펼침이다.** 이 판이 답하는 것이 「지금 어디까지 왔나」인데 접혀 있으면 그 답을
   * 누르기 전엔 못 본다. 도구 줄의 인자와 반대다 — 저건 가끔 필요한 값이라 접고, 이건 도는
   * 동안 계속 보는 값이라 편다.
   *
   * **다만 펴는 것은 계획이 처음 설 때 한 번뿐이다.** 매 로그 변화마다 열면 사람이 접어 둔
   * 것이 글자 한 조각마다 다시 열린다 — `onChange` 는 토큰마다 뛴다. 그래서 「안 보이던 것이
   * 보이게 된 순간」에만 열고, 그 뒤로는 사람이 정한다. 계획이 끝나 판이 사라지면 그 기억도
   * 지워서, 다음 계획은 다시 펴진 채로 선다.
   */
  renderPlan(todos) {
    const el = $('#plan');
    if (!el) return;
    const b = planBoard(todos);
    el.hidden = b.hidden;
    if (b.hidden) {
      // 다음 계획은 다시 펴진 채로 선다.
      this.planShown = false;
      this.planAt = null;
      return;
    }
    if (!this.planShown) {
      el.open = true;
      this.planShown = true;
    }
    const sum = $('#plan-summary');
    if (sum) sum.textContent = `${b.headText} · ${b.doneText}`;
    const list = $('#plan-list');
    if (!list) return;
    list.replaceChildren();
    for (const r of b.rows) {
      const row = document.createElement('div');
      row.className = `plan-row plan-${r.known ? r.state : 'unknown'}`;
      const m = document.createElement('span');
      m.className = 'plan-mark';
      m.textContent = r.mark;
      const t = document.createElement('span');
      t.textContent = r.text;
      row.append(m, t);
      list.append(row);
    }
    // **바뀐 자리를 따라간다.** 목록은 96px 이라 항목이 예닐곱을 넘으면 지금 도는 것이 그
    // 밖으로 밀린다. 고르는 것은 `planAnchor` 가 하고(화면 밖이라야 잰다) 여기서는 민다.
    //
    // **키가 바뀐 때만** 민다. 로그는 글자 한 조각마다 뛰므로 그릴 때마다 끌면 사람이 목록을
    // 제 손으로 넘겨 볼 수가 없다 — 도구 줄에서 이미 한 번 겪은 자리다(`planShown`).
    const at = planAnchor(b);
    if (at && at.key !== this.planAt) {
      this.planAt = at.key;
      this.scrollWithin(list, list.children[at.index]);
    }
  }

  /**
   * **한 번 더 묻는다.** 답이 올 때까지 기다리는 약속을 돌려준다.
   *
   * `window.confirm` 을 대신한다. 그게 이 판에서 위험한 이유는 **안 뜰 수 있어서**인데, 안
   * 뜨면 `undefined` 가 돌아오고 부르는 쪽은 그것을 「아니오」로 읽는다 — 그러면 지우기가
   * 거절도 실패도 아닌 채로 조용히 아무 일도 안 한다. Office 작업창은 우리가 고른 브라우저가
   * 아니라서 「대개 뜬다」를 근거로 삼을 수 없다.
   *
   * **포커스는 덜 위험한 쪽에 준다.** 판이 뜨자마자 엔터를 치는 손이 지우는 일이 없어야 한다.
   * Escape 도 그만두는 쪽이다 — 판을 닫는 관습적인 키가 파괴로 이어지면 안 된다.
   */
  ask(what, name) {
    const box = $('#confirm');
    const text = confirmAsk(what, name);
    // 글을 못 지으면 **묻지 않고 거절한다.** 여기서 참을 돌려주면 안 물어보고 지운다.
    if (!box || !text) return Promise.resolve(false);
    $('#confirm-head').textContent = text.head;
    $('#confirm-body').textContent = text.body;
    const ok = $('#confirm-ok');
    const cancel = $('#confirm-cancel');
    ok.textContent = text.ok;
    cancel.textContent = text.cancel;
    ok.classList.toggle('text-danger', Boolean(text.danger));
    box.hidden = false;
    cancel.focus();
    return new Promise((resolve) => {
      const done = (v) => {
        box.hidden = true;
        ok.removeEventListener('click', yes);
        cancel.removeEventListener('click', no);
        box.removeEventListener('keydown', esc);
        resolve(v);
      };
      const yes = () => done(true);
      const no = () => done(false);
      const esc = (e) => { if (e.key === 'Escape') done(false); };
      ok.addEventListener('click', yes);
      cancel.addEventListener('click', no);
      box.addEventListener('keydown', esc);
    });
  }

  /**
   * **연 판을 보이는 데까지 데려온다.**
   *
   * 「늘 지킬 것」·「가이드」는 스크롤 영역 **안**에 서는데 그것을 여는 단추는 이제 화면
   * 아래(브랜드 줄 → `⋯`)에 있다. 대화가 길면 판은 저 위에 열리고, 사람이 보기에는
   * **아무 일도 안 일어난 것**이다 — 단추를 아래로 내린 것이 오히려 안 눌리는 단추를 만든다.
   */
  reveal(el) {
    this.scrollWithin(this.scroller(), el);
  }

  /**
   * 상자 **안에서만** 민다.
   *
   * `scrollIntoView` 를 안 쓰는 이유가 있다 — 그건 조상 스크롤러까지 같이 민다. 여기서 그
   * 조상은 대화 영역(`#scroll`)이라, 계획 한 줄을 보이게 하려다 **읽던 대화가 튄다.**
   * 방금 고친 바닥 고정과 정면으로 부딪히는 동작이다.
   */
  scrollWithin(box, el) {
    if (!box || !el) return;
    // 가운데에 세운다 — 위아래로 무엇이 더 있는지가 같이 보여야 진척으로 읽힌다.
    const mid = el.offsetTop - (box.clientHeight - el.offsetHeight) / 2;
    box.scrollTop = Math.max(0, mid);
  }

  /** 도는 중이라는 것 하나. 판정은 `turnRunning` 이 한다 — 화면 밖이라야 잰다. */
  renderBusy(rows) {
    const running = turnRunning(rows);
    const el = $('#busy');
    if (el) el.hidden = !running;
    // **세우는 손은 세울 것이 있을 때만.** 그리고 부를 문이 있을 때만 — 가짜 갈래에는 없다.
    const stop = $('#stop');
    if (stop) stop.hidden = !(running && this.canStop);
  }

  /** 처음 뜰 때 이 창이 어느 대화인지 적는다. 첫 줄이 서면 사라진다 — 그때부턴 대화가 증거다. */
  renderReady(rowCount) {
    const el = $('#ready');
    if (!el) return;
    const text = readyText(this.boundName, rowCount, this.boundSession);
    el.textContent = text;
    el.hidden = text === '';
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
    // **바닥 고정은 여기서 안 한다.** 재고 붙이는 것은 `keepingEnd` 가 감싸는 자리다 — 이
    // 함수가 그린 뒤에도 스크롤 영역의 **높이가 더 바뀌기 때문이다.** 계획 판이 서면 `#scroll`
    // 이 그만큼 줄어드는데 여기서 붙여 둔 바닥은 그 줄어듦을 못 봐서 그대로 풀린다 — 실물에서
    // 그 화면을 봤다(2026-09-04). 판이 하나 늘 때마다 같은 일이 난다.
    const box = $('#turns');
    box.replaceChildren();
    for (const r of rows) box.append(this.rowEl(r));
  }

  /** 스크롤은 **가운데 영역이 갖는다.** 없으면 예전처럼 대화 칸이다. */
  scroller() { return $('#scroll') ?? $('#turns'); }

  /**
   * 지금 바닥에 있는가. 위로 올려 읽는 중이면 **안 끌어내린다** — 도구가 줄줄이 도는 턴에서
   * 읽던 자리를 뺏는다.
   */
  atEnd() {
    const s = this.scroller();
    if (!s) return false;
    return s.scrollHeight - s.scrollTop - s.clientHeight < 40;
  }

  toEnd() {
    const s = this.scroller();
    if (s) s.scrollTop = s.scrollHeight;
  }

  /**
   * **바닥에 있었으면 다시 바닥으로.** 재는 것은 그리기 전, 붙이는 것은 **모든 판이 선 뒤**다.
   *
   * 감싸는 모양인 것이 요점이다. 「마지막에 부르세요」로 두면 판을 하나 더 그리는 날 그 줄이
   * 뒤에 붙고 고정이 조용히 풀린다 — 순서를 지키라고 적는 것보다 **뒤에 못 오게 만드는 것**이
   * 싸다.
   */
  keepingEnd(draw) {
    const stick = this.atEnd();
    draw();
    if (stick) this.toEnd();
  }

  rowEl(r) {
    const el = document.createElement('div');
    // 종류를 **접두사와 함께** 적는다. 그냥 `turn ${r.kind}` 로 적으면 끝난 턴이
    // `class="turn turn"` 이 되고, `.turn.turn` 은 CSS 에서 그냥 `.turn` 이라
    // 그 한 줄에 준 모양이 **모든 줄에** 걸린다. 실제로 사용자 말이 가운데 정렬됐었다.
    el.className = rowClass(r);
    const head = rowHead(r);
    const shape = rowShape(r);
    const res = shape === 'tool' ? resultCell(r) : null;
    if (res) {
      // **`isError` 하나로 ✗ 를 찍지 않는다** — `advisory` 는 「했는데 읽을 것이 붙었다」다
      // (`ToolResult.Advisory`). 이 제품에서 그 오독은 「슬라이드가 안 바뀌었다」로 읽힌다.
      el.classList.toggle('failed', res.failed);
      el.classList.toggle('advisory', res.advisory);
    }
    if (shape === 'think') {
      // **혼잣말도 접는다.** 도구 줄과 같은 손잡이 하나짜리 모양이다 — 규칙이 하나면 사람이
      // 무엇이 어디 접혀 있는지 안 외운다.
      //
      // 요약에 **첫 줄을 미리 보여 준다**(`thinkHead`). 손잡이만 있으면 열기 전에는 무슨
      // 생각인지 모르고, 모르면 안 열게 된다. 웹 콘솔이 같은 자리를 같은 모양으로 그린다.
      const fold = document.createElement('details');
      fold.className = 'turn-fold';
      const sum = document.createElement('summary');
      sum.className = 'turn-line';
      const name = document.createElement('span');
      name.className = 'turn-head';
      name.textContent = thinkHead(r);
      sum.append(name);
      fold.append(sum);
      const body = document.createElement('div');
      body.className = 'turn-think';
      // **글 그대로 둔다.** 혼잣말은 모델이 자기에게 쓴 글이라 줄바꿈이 뜻을 갖는다.
      body.textContent = r.text ?? '';
      fold.append(body);
      el.append(fold);
      return el;
    }
    if (shape === 'tool' && r.kind === 'tool') {
      // **도구 한 번이 줄 하나다.** 이름과 결과가 **같은 줄**에 서고, 그 줄을 누르면 보낸 것과
      // 받은 것이 **같이** 펴진다.
      //
      // 접힘이 둘이었다(이름 밑에 인자, 결과 밑에 답). 좁은 판에서 그건 손잡이가 둘이라는 뜻이고,
      // 무엇이 어디 접혀 있는지를 사람이 외워야 한다. 하나로 합치면 규칙이 한 줄로 선다 —
      // **접혀 있을 때는 「무엇을 불렀고 어떻게 됐나」, 펴면 「무엇을 보내고 무엇을 받았나」.**
      //
      // 늘 보이는 것은 됐는가(`✓/✗/⚠`)이고, 나머지는 궁금할 때 여는 값이다. 지우지는 않는다 —
      // 모델이 정확히 무엇을 주고받았는지 사람이 확인할 유일한 자리다.
      const fold = document.createElement('details');
      fold.className = 'turn-fold';
      const sum = document.createElement('summary');
      sum.className = 'turn-line';
      const name = document.createElement('span');
      name.className = 'turn-head';
      name.textContent = head ?? '';
      const mark = document.createElement('span');
      // 답이 아직 안 왔으면 **비워 두는 것이 사실이다** — 「완료」를 미리 적으면 실패한 호출이
      // 성공으로 보이는 구간이 생긴다.
      mark.className = res ? 'turn-result-head' : 'turn-result-head pending';
      // 셋이 **짝을 이룬다**: `완료` · `실패` · `대기`. 길이가 갈리면 줄마다 오른쪽 끝이 흔들리고,
      // 수십 줄이 서는 판에서 그 흔들림이 읽기를 방해한다.
      mark.textContent = res ? `${res.mark} ${res.head}` : '⋯ 대기';
      sum.append(name, mark);
      fold.append(sum);

      if (r.args != null) {
        const pre = document.createElement('pre');
        pre.className = 'turn-args';
        pre.textContent = argsCell(r);
        fold.append(pre);
      }
      // 허락은 이 제품에서 **덱을 고치게 뒀는가**다. 접힘 안에 둔다 — 줄을 하나 더 세우지 않고,
      // 궁금할 때 이름과 인자와 함께 한자리에서 읽힌다.
      const perm = permissionText(r);
      if (perm) {
        const p = document.createElement('div');
        p.className = 'turn-perm';
        p.textContent = perm;
        fold.append(p);
      }
      for (const line of changedLines(r)) {
        const d = document.createElement('div');
        d.className = 'turn-changed';
        d.textContent = line;
        fold.append(d);
      }
      if (res?.text) {
        const pre = document.createElement('pre');
        pre.className = 'turn-result';
        pre.textContent = res.text;
        fold.append(pre);
      }
      el.append(fold);
      return el;
    }
    if (head) {
      const h = document.createElement('div');
      h.className = 'turn-head';
      h.textContent = head;
      el.append(h);
    }
    if (shape === 'tool') {
      // 짝을 못 찾은 답·허락만 여기로 온다(`Transcript.append` 가 호출 줄을 못 찾은 경우).
      // **버리지 않는다** — 이 창이 로그 중간부터 읽기 시작했다는 사실이다.
      const perm = permissionText(r);
      if (perm) {
        const p = document.createElement('div');
        p.className = 'turn-perm';
        p.textContent = perm;
        el.append(p);
      }
      if (res) {
        const h = document.createElement('div');
        h.className = 'turn-result-head';
        h.textContent = `${res.mark} ${res.head}`;
        el.append(h);
      }
      return el;
    }
    if (shape === 'council') {
      // 종료 게이트. **머리만으로 뜻이 서는 줄이 있다**(소집·결론) — 없는 몸통을 「(글 없음)」
      // 으로 채우면 화면이 빈 칸을 결함처럼 보이게 한다.
      const body = councilBody(r);
      if (body) el.append(this.proseEl(body));
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
    // **사람의 말은 글자 그대로, 나머지는 마크다운으로.** 모델의 답·플러그인이 넣은 줄은
    // 마크다운으로 오는데 앞 판본은 전부 `textContent` 라 `**굵게**`·`|---|`·백틱이 그대로
    // 찍혔다(사용자 지적 2026-09-05). 사람이 친 글은 그 사람이 친 그대로 보여야 한다.
    // 사용자 줄에는 인용이 **글로 접혀** 들어 있다(`promptOf`). 예쁘게 걷어 내지 않는다 —
    // 모델이 받은 것이 이것이고, 걷어 내면 화면이 모델보다 덜 아는 것을 감추게 된다.
    if (r.kind === 'user') {
      const p = document.createElement('p');
      p.textContent = bodyText(r);
      el.append(p);
      return el;
    }
    el.append(this.proseEl(bodyText(r)));
    return el;
  }

  /** 마크다운 표식이 있으면 그려서, 없으면 문단 하나로. 마크업을 읽는 길은 없다(`md.js`). */
  proseEl(text) {
    if (looksLikeMd(text)) return mdToDom(document, text);
    const p = document.createElement('p');
    p.textContent = text;
    return p;
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

  /** 손을 나중에 받는다 — 손은 덱이 정해진 뒤에 서고, 화면은 그보다 먼저 뜬다. */
  useHand(hand) {
    this.hand = hand;
    void this.loadFixes();
  }

  /**
   * 덱에서 제안을 읽어 다시 그린다.
   *
   * **조용히 실패하지 않는다.** 못 읽으면 그 사실을 쪽지로 적는다 — 안 적으면 사람은
   * 제안이 없는 덱이라고 읽는다.
   */
  async loadFixes() {
    if (!this.hand) return;
    try {
      const out = await this.hand.run('read_suggestions', {});
      this.fixes = out?.result?.suggestions ?? [];
    } catch (e) {
      this.fixes = [];
      this.note(`덱의 제안을 못 읽었습니다 — ${e?.message ?? e}`);
    }
    this.renderFixes();
  }

  renderFixes() {
    const box = $('#fixes');
    if (!box) return;
    box.replaceChildren();
    const board = fixBoard(this.fixes);
    $('#fix-wrap').hidden = board.wrapHidden;
    $('#fix-head').textContent = board.headText;
    for (const c of board.cards) {
      const el = document.createElement('div');
      el.className = 'fix';
      // **덱에서 온 글이다.** `textContent` 로만 넣는다 — 남이 준 덱의 제안이 이 창에
      // 표시를 그리게 두면 안 된다.
      const what = document.createElement('div');
      what.className = 'fix-what';
      what.textContent = c.what;
      el.append(what);
      const why = document.createElement('div');
      why.className = 'fix-why';
      why.textContent = c.whyText;
      why.hidden = c.whyHidden;
      el.append(why);
      const where = document.createElement('div');
      where.className = 'fix-where';
      where.textContent = c.whereText;
      el.append(where);
      // 무엇이 일어나는지. **제안의 글이 아니라 제안의 손에서 왔다.**
      const does = document.createElement('div');
      does.className = 'fix-does';
      does.textContent = c.doesText;
      el.append(does);

      const buttons = document.createElement('div');
      buttons.className = 'fix-buttons';
      const apply = document.createElement('button');
      apply.className = 'fix-apply';
      apply.textContent = c.applyText;
      apply.disabled = !c.canApply;
      apply.addEventListener('click', () => this.guard(
        () => this.applyFix(c.key), '제안을 적용하지 못했습니다'));
      const drop = document.createElement('button');
      drop.textContent = '무시';
      drop.addEventListener('click', () => this.guard(
        () => this.dropFix(c.key), '제안을 떼지 못했습니다'));
      buttons.append(apply, drop);
      el.append(buttons);
      box.append(el);
    }
  }

  /**
   * 「적용」. **고치고 나서 뗀다** — 먼저 떼면 고치기가 실패했을 때 제안까지 잃는다.
   *
   * 고치는 손이 장을 다시 짓는 것이면(`set_notes`) **장 id 가 바뀐다.** 그래서 뗄 때는
   * 옛 id 가 아니라 **방금 받은 id** 로 뗀다.
   */
  async applyFix(key) {
    const row = this.fixes.find((f) => f.key === key);
    if (!row || !row.fix) { this.note('그 제안을 못 찾았습니다'); return; }
    const args = { slide_id: row.slide_id, ...(row.fix.args ?? {}) };
    if (row.shape_id && args.shape_id == null) args.shape_id = row.shape_id;
    const done = await this.hand.run(row.fix.tool, args);
    const slideId = done?.result?.slide_id ?? row.slide_id;
    await this.hand.run('drop_suggestion', {
      slide_id: slideId, shape_id: row.shape_id ?? undefined, key,
    });
    this.note((done?.changed ?? []).join(' ') || '적용했습니다');
    await this.loadFixes();
  }

  /** 「무시」. 덱은 안 고치고 제안만 뗀다. */
  async dropFix(key) {
    const row = this.fixes.find((f) => f.key === key);
    if (!row) { this.note('그 제안을 못 찾았습니다'); return; }
    await this.hand.run('drop_suggestion', {
      slide_id: row.slide_id, shape_id: row.shape_id ?? undefined, key,
    });
    this.note('제안을 뗐습니다 — 덱은 안 고쳤습니다');
    await this.loadFixes();
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
