package dev.sayaya.magi.client.interfaces;

import dev.sayaya.magi.bridge.Markdown;
import dev.sayaya.magi.bridge.GoSharing;
import dev.sayaya.magi.component.Dialogs;
import dev.sayaya.magi.bridge.Icons;
import dev.sayaya.magi.bridge.May;
import dev.sayaya.magi.bridge.Stylesheet;
import dev.sayaya.magi.client.domain.Moves;
import dev.sayaya.magi.client.domain.Rows;
import dev.sayaya.magi.client.usecase.CompanionStore;
import elemental2.core.JsDate;
import elemental2.dom.DomGlobal;
import elemental2.dom.Element;
import elemental2.dom.HTMLElement;
import elemental2.dom.HTMLFormElement;
import elemental2.dom.HTMLLinkElement;
import jsinterop.base.Js;
import jsinterop.base.JsArrayLike;
import jsinterop.base.JsPropertyMap;

import javax.inject.Inject;
import javax.inject.Singleton;

import static dev.sayaya.magi.bridge.Labels.tr;

/**
 * 코딩 에이전트의 가운데: 전사 · 컴포저 — 그리고
 * 지난 일 층위(?past=): 빈 past는 목록(/history), 값은 그 세션의 전사(한 번의 읽기).
 * 층위에선 지금-대화의 판들이 물러난다: 과거를 보는 화면 밑에서 스트림이 그려지면
 * 보는 것과 닿는 것이 갈라진다(운영 규칙).
 *
 * 전사 행의 클래스(.row/.who/.txt, toolok…)는 기존 콘솔 page.js rowNode와 같은 계약 —
 * console.css 가 그대로 입힌다. 이 화면 자신의 것(사실 줄·컴포저 배치·턴 바)만
 * companion.css 가 말하고, 모듈이 로드될 때 스스로 <link>를 단다.
 *
 * 아직 아닌 것(대조표의 잔여): 접히는 사실판 전체·워크스페이스 판·툴 행 펼침(인자·출력)·
 * 마크다운·행 재사용 윈도우잉. 여기 없는 것은 없다고 그린다 — 반쯤 그리지 않는다.
 */
@Singleton
public class ConversationElement {
    private final CompanionStore store;
    private final Dialogs dialogs;
    private final dev.sayaya.magi.client.usecase.OpenCards open;
    private final HTMLElement root = el("section");
    private final HTMLElement log = el("div");
    private final HTMLElement past = el("section");
    private final HTMLFormElement form = Js.uncheckedCast(DomGlobal.document.createElement("form"));
    private final HTMLElement field = el("md-outlined-text-field");
    private final HTMLElement box = el("div");   // form 안쪽의 .composer — 줄을 이루는 것
    private String lastSig = null;
    private boolean wired = false;     // 재방문 마운트가 구독을 겹으로 쌓지 않게

    @Inject
    public ConversationElement(CompanionStore store, Dialogs dialogs,
                               dev.sayaya.magi.client.usecase.OpenCards open) {
        this.store = store;
        this.dialogs = dialogs;
        this.open = open;
        // 감싸는 상자를 두지 않는다: 부모가 준 자리는 이미 높이가 정해진 기둥이고, 그 안에서
        // 전사가 남는 높이를 받아 제 안에서 스크롤한다(운영 규칙). 사이에 상자가 하나라도 끼면
        // 그 사슬이 거기서 끊긴다 — 실측: 전사가 4190px로 자라 기둥 밖으로 흘렀다.
        root.id = "conversation";
        // 턴바는 여기 없다 — 창을 가로지르는 줄(운영 css의 left:0;right:0 fixed)은 이 기둥이
        // 기준 상자가 되는 순간 기둥 폭짜리가 된다. 그건 창의 것이고, 켜는 사실(turn 프레임)도
        // 이미 셸에 와 있다(shell-ui TurnbarElement).
        log.id = "log";
        past.id = "agentdetail";
        past.setAttribute("hidden", "");
        // 가운데 기둥에 놓는 것: 전사와, 그것을 대신하는 지난 일 층위. 사실판은 부모의 것이고
        // 한 마디 보내는 상자는 부모가 내준 도크 자리로 간다(mountComposer) — 그것이 창 바닥에
        // 고정된 상자라는 사실은 부모만 안다.
        // display:contents — 있는 셈 치지 않는 상자. 이 요소가 필요한 이유는 mount가 프레임을
        // 통째로 갈아끼우기 때문이고(자식 하나로 다루는 편이 안전하다), 배치에서는 없어야 한다.
        root.style.setProperty("display", "contents");
        root.append(log, past);
    }

    public void mount(HTMLElement frame) {
        // 시트도 언어 팩도 부모가 이미 들여놓았다 — 여기서는 그리기만 한다.
        frame.replaceChildren(root);
        if (wired) return;   // 재방문: 캐시된 렌더가 다시 앉는 것 — 구독은 이미 흐른다
        wired = true;
        store.start();
        store.onContext(ctx -> { lastSig = null; layer(ctx); });
        store.alive().subscribe(this::aliveIs);
        // 이름은 명단 조각에서 온다 — 그 행이 같은 말을 다시 하면 스토어가 흘리지 않는다.
        // 세션과 상태가 이 조각에서 온다 — 컴패니언이 다른 대화로 떠나거나 일을 시작하면
        // 컴포저가 말하는 것도 달라져야 한다.
        store.aimed().subscribe(row -> { aimed = row; reach(); });
        store.onRows(this::paintRows);
        store.onPast(this::paintPast);
        store.onSub(this::paintSub);
    }

    /** 한 마디 보내는 상자 — 부모가 내준 자리에 앉는다. 어느 자리인지는 묻지 않는다. */
    public void mountComposer(HTMLElement frame) {
        frame.replaceChildren(composer());
    }

    // ── 지난 일 층위 ─────────────────────────────────────────────────────────

    private String pastNow = null;

    /**
     * 층위가 정해지면 지금-대화의 판들이 물러난다 — 다만 <b>컴포저는 하나를 남긴다</b>.
     *
     * 물러나는 이유는 "과거라서"가 아니라 <b>보낼 대화가 화면에 없어서</b>다: 자식 하나를
     * 들여다보는 중이거나 지난 일 목록을 훑는 중에 누른 보내기는, 보이지 않는 대화에 말을
     * 넣는다. 한 세션의 전사를 열어 둔 화면은 그 시험을 통과한다 — 보고 있는 그 대화가 곧 말이
     * 갈 곳이고, 다만 컴패니언이 아직 거기 있지 않을 뿐이다. 그건 상자를 치울 이유가 아니라
     * 보내기 전에 한 번 묻는 이유다(운영 paintCompanionChrome의 onSession).
     */
    private void layer(dev.sayaya.magi.bridge.CompanionContext ctx) {
        pastNow = ctx == null ? null : ctx.past;
        subNow = ctx == null || ctx.sub == null || ctx.sub.isEmpty() ? null : ctx.sub;
        // 지난 일과 자식은 <b>같은 자리</b>를 대신한다 — 둘 다 지금 대화를 물리고 그 자리에 선다.
        boolean layered = pastNow != null || subNow != null;
        // 빈 past는 목록이다 — 고른 대화가 아직 없으니 보낼 곳도 없다.
        boolean onSession = subNow == null && pastNow != null && !pastNow.isEmpty();
        toggle(log, !layered);
        toggle(form, !layered || onSession);
        toggle(past, layered);
        // 화면을 옮기면 이 상자가 닿는 대화가 바뀐다 — 질문이 도착할 때만이 아니라 여기서도 알린다.
        reach();
    }

    private String subNow = null;

    /**
     * 자식 하나 — 무엇을 하라고 보내졌고, 무엇을 했나.
     *
     * 지난 일 층위와 같은 자리에 같은 모양으로 선다(같은 rowNode): 자식의 전사도 전사이고,
     * 이 콘솔에서 전사가 읽히는 방식은 하나여야 한다(운영 drawChild의 그 판단).
     */
    private void paintSub(Object rows) {
        if (subNow == null) return;
        past.replaceChildren();
        dev.sayaya.magi.bridge.Motion.enter(past);
        JsPropertyMap<Object> me = store.subMeta() == null ? null : Js.uncheckedCast(store.subMeta());
        String role = me == null ? "" : str2(me, "role");
        HTMLElement head = el("h2");
        head.className = "sectionhead";
        HTMLElement word = el("span");
        word.textContent = role.isEmpty() ? tr("detail.subagent") : role;
        head.append(word);
        HTMLElement back = el("md-text-button");
        back.className = "backpast";
        back.textContent = tr("action.back_to", "name", tr("nav.companions"));
        back.addEventListener("click", evt -> GoSharing.sub(null));
        head.append(back);
        past.append(head);
        if (me != null) {
            // 도는 중인지와 어느 모델인지 — 한 줄. 그 아이를 다시 부를 수는 없으니 사실만 적는다.
            boolean running = Js.isTruthy(me.get("running"));
            String model = str2(me, "model");
            past.append(cell("dnote", tr(running ? "detail.running" : "detail.finished")
                    + (model.isEmpty() ? "" : " · " + model)));
            String task = str2(me, "task");
            if (!task.isEmpty()) {
                past.append(cell("dk", tr("detail.asked")));
                past.append(cell("dv", task));
            }
        }
        past.append(cell("dk dhero", tr("detail.what_it_did")));
        HTMLElement dlog = el("div");
        dlog.className = "dlog";
        JsArrayLike<Object> list = rows == null ? null : Js.uncheckedCast(rows);
        for (int i = 0; list != null && i < list.getLength(); i++) {
            dlog.append(rowNode(Js.uncheckedCast(list.getAt(i))));
        }
        if (list == null || list.getLength() == 0) dlog.append(cell("dnote", tr("detail.nothing_yet")));
        past.append(dlog);
    }

    private void paintPast(Object data) {
        if (subNow != null) return;   // 자식 층위가 그 자리에 서 있다
        if (pastNow == null) { past.replaceChildren(); return; }
        past.replaceChildren();
        // 한 겹 들어간 층위도 들어온다 — 그 자리에 있던 전사를 대신하는 것이라, 아무 움직임 없이
        // 바뀌면 같은 판의 내용이 갑자기 딴것이 된 것처럼 읽힌다(운영 drawDeep 끝의 reveal).
        dev.sayaya.magi.bridge.Motion.enter(past);
        HTMLElement head = el("h2");
        head.className = "sectionhead";
        HTMLElement word = el("span");
        word.textContent = tr("field.history");
        head.append(word);
        // 돌아가는 길이 머리에 산다: 세션에서는 목록으로, 목록에서는 지금 대화로.
        HTMLElement back = el("md-text-button");
        back.className = "backpast";
        back.textContent = tr("action.back_to", "name",
                pastNow.isEmpty() ? tr("nav.companions") : tr("field.history"));
        back.addEventListener("click", evt -> GoSharing.past(pastNow.isEmpty() ? null : ""));
        head.append(back);
        past.append(head);
        if (data == null) return;   // 아직 — 빈 화면과 "없다"를 섞지 않는다
        JsArrayLike<Object> list = Js.uncheckedCast(data);
        if (pastNow.isEmpty()) {
            // 목록: 행 하나가 한 세션 — 여는 길은 그 행이다.
            if (list.getLength() == 0) { past.append(cell("dnote", tr("find.none"))); return; }
            for (int i = 0; i < list.getLength(); i++) {
                JsPropertyMap<Object> h = Js.uncheckedCast(list.getAt(i));
                HTMLElement row = el("button");
                row.setAttribute("type", "button");
                boolean current = Js.isTruthy(h.get("current"));
                row.className = "hs hit48" + (current ? " now" : "");
                double agoSec = h.get("ago") == null ? -1 : Js.coerceToDouble(h.get("ago"));
                row.append(cell("when", current ? tr("state.working")
                        : agoSec >= 0 ? tr("time.ago", "d", dur((int) agoSec)) : ""));
                String title = str2(h, "title");
                row.append(cell("what", title.isEmpty() ? tr("history.untitled") : title));
                final String id = str2(h, "id");
                row.addEventListener("click", evt -> GoSharing.past(id));
                past.append(row);
            }
            return;
        }
        // 한 세션의 전사 — 같은 rowNode, 다른 원천(fetch): 스트림이 아니다.
        HTMLElement dlog = el("div");
        dlog.className = "dlog";
        for (int i = 0; i < list.getLength(); i++) dlog.append(rowNode(Js.uncheckedCast(list.getAt(i))));
        if (list.getLength() == 0) dlog.append(cell("dnote", tr("detail.nothing_yet")));
        past.append(dlog);
    }

    private static void toggle(HTMLElement e, boolean show) {
        if (show) e.removeAttribute("hidden"); else e.setAttribute("hidden", "");
    }

    private static HTMLElement cell(String cls, String text) {
        HTMLElement d = el("div");
        d.className = cls;
        if (text != null) d.textContent = text;
        return d;
    }

    private static String str2(JsPropertyMap<Object> m, String k) {
        Object v = m.get(k);
        return v == null ? "" : String.valueOf(v);
    }

    /** s/m/h/d — 운영 dur()와 같은 축약: 단위는 언어를 타지 않는다. */
    private static String dur(int s) {
        if (s < 60) return s + "s";
        if (s < 3600) return Math.round(s / 60f) + "m";
        if (s < 86400) return Math.round(s / 3600f) + "h";
        return Math.round(s / 86400f) + "d";
    }


    private final HTMLElement sendBtn = el("md-filled-button");
    // 돌고 있는 턴을 멈추는 문 — 답하는 것과 같은 권한이라(운영 data-may="answer"), 볼 수만
    // 있는 사람에게는 남의 일을 끝내는 버튼을 내놓지 않는다.
    private final HTMLElement stopBtn = el("md-filled-tonal-button");
    private final HTMLElement note = el("div");   // 컴포저 아래 한 줄 — 지금 이 상자의 몫
    private final HTMLElement hint = el("div");   // 모델이 내민 다음 말(흐리게, Tab이 가져간다)
    private String suggested = "";
    private int suggestAt = 0;
    private double suggestTick = -1;
    private String parked = "";                   // 다른 몫으로 쓰던 초고는 지우지 않고 맡아 둔다
    private boolean wasAnswering = false;
    private boolean dressed = false;      // 옷을 한 번은 입혀야 한다 — 첫 상태가 기본값과 같아도
    private boolean composerBuilt = false;

    /**
     * 컴포저의 몫 — 부탁을 보내는 자리인가, 위 질문에 답하는 자리인가.
     *
     * 옷(라벨·버튼 낱말·아래 한 줄)은 <b>몫이 바뀔 때만</b> 갈아입는다: 명단은 몇 초마다
     * 흐르고, 그때마다 다시 쓰면 바뀐 적 없는 낱말 때문에 버튼이 분당 수십 번 다시 지어진다.
     *
     * 그리고 쓰던 글은 지우지 않고 <b>맡아 둔다</b>. 몫은 사람이 타이핑하는 중에도 바뀔 수
     * 있어서(질문이 도착하거나 남이 먼저 답하거나), 지우면 되돌릴 수 없는 삭제가 된다.
     * 몫마다 제 초고를 갖는다 — 어느 쪽도 남의 글을 제 것처럼 내놓지 않는다.
     */
    private void answerMode() {
        boolean now = store.answering();
        // 처음 한 번은 반드시 입힌다: 첫 상태가 기본값과 같다는 이유로 건너뛰면 버튼과 필드가
        // 이름 없이 선다(실측: 보내기 버튼의 접근 이름이 "send"라는 id였다).
        if (now == wasAnswering && dressed) return;
        wasAnswering = now;
        dressed = true;
        field.setAttribute("label", tr(now ? "label.answer" : "label.ask"));
        // 표도 몫을 따른다(운영 withMark의 그 삼항): 답하는 자리에서는 되돌려 주는 화살,
        // 묻는 자리에서는 종이비행기.
        Icons.say(sendBtn, tr(now ? "action.answer" : "action.send"), now ? "#i-sl-reply" : "#i-ss-paper-plane");
        String had = value();
        value(parked);
        parked = had;
        note.textContent = now ? tr("answer.instead") : "";
        if (now) note.removeAttribute("hidden"); else note.setAttribute("hidden", "");
        // 몫이 바뀌면 라벨을 새로 썼다 — 옮겨야 하는 화면이면 그 위에 다시 덮어야 한다.
        reach();
    }

    /**
     * 이 상자가 <b>어느 대화에 닿는가</b> — 그리고 닿지 못할 때 무엇이라 말하는가.
     *
     * 보통 화면 말고 두 경우가 더 있고, 둘 다 이것이 없던 시절의 결함이다. 컴패니언이 있지 않은
     * 세션에 서면 상자는 살아 있는 것과 똑같이 보였고, 거기 쓴 말은 컴패니언이 마침 있던 대화로
     * — 화면 밖으로 — 갔다. 그리고 턴이 도는 중인 컴패니언은 아예 옮길 수 없으니, 그 자리에서
     * 문장을 받는 상자는 배달하지 못할 것을 받는 상자였다.
     *
     * 씌운 라벨은 <b>씌운 쪽이 벗긴다</b>: 몫이 바뀔 때만 라벨을 쓰는 answerMode는 화면을 걸어
     * 나오는 일을 모른다. 그래서 옮길 것이 없어지는 순간 여기서 보통의 라벨을 되돌린다 —
     * 아니면 아무도 보고 있지 않은 세션의 이름을 단 상자가 그대로 남는다(운영이 겪은 그것).
     */
    private void reach() {
        if (!composerBuilt) return;
        // 능력 둘이 한 상자에서 만난다. 막힌 것을 <b>풀어 주는 것</b>과 <b>새 일을 주는 것</b>은
        // 일부러 다른 권한이다 — 컴패니언을 풀어 줘도 되는 사람이 그것이 무슨 일을 할지 정해도
        // 되는 사람은 아니다 — 그런데 사람이 손대는 컨트롤은 둘 다 이 상자다. 그래서 어느 하나만
        // 있어도 상자는 남고, 지금 몫이 마침 없는 그 능력일 때만 거절한다.
        //
        // 게이트는 서버가 진다(May의 규칙). 여기서 하는 일은 눌러서 거절에 닿을 컨트롤을 잠그고,
        // 왜 잠겼는지를 그 줄에 적는 것뿐이다 — 잠긴 채 이름이 "묻기"인 상자는 고장으로 읽힌다.
        boolean asked = store.answering();
        boolean canAnswer = May.can("answer"), canPrompt = May.can("prompt");
        // 물리기만 한다(운영의 `f.hidden = f.hidden || …`): 층위가 이미 물린 상자를 여기서 되살리면
        // 지난 일 목록 위에 보낼 곳 없는 상자가 다시 선다.
        if (!canAnswer && !canPrompt) toggle(form, false);
        if (asked ? !canAnswer : !canPrompt) {
            able(field, true);
            able(sendBtn, true);
            field.setAttribute("label", tr(asked ? "may.not_answer" : "may.not_prompt"));
            return;
        }
        String session = aimed == null ? null : aimed.session;
        String to = Moves.to(pastNow, session);
        boolean blocked = Moves.blocked(to, aimed == null ? null : aimed.state);
        able(field, blocked);
        able(sendBtn, blocked);
        if (to.isEmpty()) {
            field.setAttribute("label", tr(store.answering() ? "label.answer" : "label.ask"));
            // 질문이 서 있는 동안 그 한 줄은 answerMode의 것이다 — 옮기기가 빌린 자리만 돌려준다.
            if (!store.answering()) {
                if (!note.textContent.isEmpty()) note.textContent = "";
                note.setAttribute("hidden", "");
            }
            return;
        }
        field.setAttribute("label", blocked ? tr("move.busy") : tr("move.into", "to", to));
        note.textContent = blocked ? tr("move.busy_why") : tr("move.will_ask");
        note.removeAttribute("hidden");
    }

    private static void able(HTMLElement e, boolean no) {
        if (no) e.setAttribute("disabled", ""); else e.removeAttribute("disabled");
    }

    /**
     * 묻고, 옮기고, 보낸다 — 그 순서로, 그 순서로만.
     *
     * 먼저 보내면 그 말은 컴패니언이 <b>지금</b> 있는 대화로 들어간다 — 아무도 보고 있지 않은
     * 그 대화로. 묻지 않고 옮기면 지난주 일을 읽으려고 열어 본 사람이 그것을 조용히 지금
     * 대화로 만들어 버린다.
     *
     * 그리고 옮기기가 거부되면 <b>보내지 않는다</b>: 답은 거부 사유이고 성공은 빈 문자열이라
     * 판정은 "말이 있느냐"다(운영에서 이것을 거짓과 비교했을 때, 거부된 옮기기 뒤로 보내기가
     * 그대로 따라간 적이 있다). 쓰던 말은 돌려준다 — 타이핑을 잃는 쪽이 늘 더 나쁘다.
     */
    private void moveAndSend(String to, String text) {
        String session = aimed == null ? null : aimed.session;
        String from = session == null || session.isEmpty() ? tr("move.somewhere") : session;
        dialogs.confirm(tr("move.headline", "to", to),
                tr("move.body", "from", from, "to", to),
                tr("action.move_and_send"), "#i-ss-paper-plane", () -> {
                    value("");
                    clearSuggest();
                    store.resume(to, why -> {
                        if (why != null && !why.isEmpty()) { value(text); return; }
                        store.submit(text, w -> { });
                        // 옮겼으니 그 대화가 곧 지금 대화다 — 읽던 층위를 접고 돌아간다.
                        GoSharing.past(null);
                    });
                });
    }

    private HTMLElement composer() {
        // 한 번만 짓는다 — 도크 자리는 화면을 다시 찾을 때마다 다시 건네지고, 다시 지으면
        // 같은 폼에 제출 리스너가 겹으로 쌓인다(한 번 보낸 것이 두 번 간다).
        if (composerBuilt) return form;
        composerBuilt = true;
        // 운영의 그 두 겹이다: 바깥 <form>이 여백을 지고, 안쪽 .composer가 줄을 이룬다.
        // 한 겹으로 합치면(form에 .composer를 입히면) 여백이 줄 안으로 들어와 상자가 도크
        // 폭을 꽉 채운다 — 실측: 운영 1356px 자리에 1404px.
        box.className = "composer";
        // 운영 컴포저의 그 계약: .composer 안의 md-outlined-text-field#t — 구분선·간격·
        // flex 전부 console.css의 것이다.
        field.id = "t";
        field.setAttribute("type", "textarea");
        field.setAttribute("rows", "1");
        sendBtn.setAttribute("type", "submit");
        sendBtn.id = "send";
        HTMLElement send = sendBtn;
        // 버튼은 한 무리로 — 필드가 줄의 나머지를 갖는다(운영 .bgroup).
        HTMLElement group = el("div");
        group.className = "bgroup";
        stopBtn.id = "stop";
        stopBtn.setAttribute("type", "button");
        stopBtn.textContent = tr("action.interrupt");
        Icons.mark(stopBtn, "#i-ss-circle-stop");
        if (!May.can("answer")) stopBtn.setAttribute("hidden", "");
        stopBtn.addEventListener("click", evt -> {
            // 되돌릴 수 없는 일이라 이름을 대고 묻는다(운영 confirmStop) — 무엇이 멈추는지.
            // 이름은 명단이 안다 — 셸이 실어 보내는 그 창의 사실(RosterSharing). 이름을 모르면
            // 이름 자리를 비워 두지 않고 이름 없는 물음을 쓴다(운영의 두 문장 그대로).
            String who = nameOfAimed();
            dialogs.stop(who, () -> store.interrupt(why -> { }));
        });
        group.append(send, stopBtn);
        box.append(field, group);
        note.id = "cnote";
        note.setAttribute("hidden", "");
        // 제안은 컴포저 <b>줄 아래</b>에 선다: .composer는 감싸지 않는 flex 한 줄(칸+버튼)이라,
        // 그 사이에 두면 폰에서 칸을 눌러 버린다(운영이 이 자리를 고른 이유).
        hint.className = "sughint";
        hint.setAttribute("hidden", "");
        hint.setAttribute("aria-hidden", "true");
        form.append(box, hint, note);
        wireSuggest();
        // 이 상자가 지금 무엇을 하는 자리인지는 부모가 알린다 — 그 사실이 바뀔 때만 옷을 갈아입는다.
        store.listenForAsk(this::answerMode);
        answerMode();
        form.addEventListener("submit", evt -> {
            evt.preventDefault();
            String v = value().trim();
            if (v.isEmpty()) return;
            // 컴패니언이 있지 않은 대화라면, 보내기 전에 물어야 할 것이 하나 있다.
            String to = Moves.to(pastNow, aimed == null ? null : aimed.session);
            if (!to.isEmpty()) {
                if (Moves.blocked(to, aimed == null ? null : aimed.state)) return;
                moveAndSend(to, v);
                return;
            }
            // 비우고, 거부되면 되돌린다 — 타이핑을 잃는 쪽이 늘 더 나쁘다(기존 콘솔 규칙).
            value("");
            store.submit(v, why -> {
                if (why != null && !why.isEmpty() && value().trim().isEmpty()) value(v);
            });
        });
        return form;
    }

    /** 지금 보는 컴패니언의 이름 — 스토어가 잘라 준 그 행의 것(없으면 빈 문자열). */
    private String nameOfAimed() {
        return aimed == null || aimed.name == null ? "" : aimed.name;
    }

    /**
     * 복사 — 늘 서 있다(손끝이 올라올 때만 나타나는 컨트롤은 터치 화면에 영영 없는 컨트롤이고
     * 키보드가 닿지 못하는 컨트롤이다). 컴포넌트가 아니라 맨 button인 이유는 이것이 산문 행마다
     * 하나씩이기 때문이다 — 섀도 루트를 수백 개 더 짓지 않는다(운영 copyChip의 그 판단).
     */
    private HTMLElement copyChip(String text) {
        HTMLElement b = el("button");
        b.setAttribute("type", "button");
        b.className = "copy hit48";
        b.append(Icons.shape("#i-sl-copy", null));
        b.setAttribute("aria-label", tr("action.copy"));
        b.setAttribute("title", tr("action.copy"));
        b.addEventListener("click", evt -> {
            evt.preventDefault();
            evt.stopPropagation();
            // 조용히 실패한 복사는 시끄럽게 실패한 복사보다 나쁘다: 다음에 하는 일이 붙여넣기라
            // 그때는 이유가 사라지고 없다.
            copy(text, ok -> {
                if (!ok) { note.textContent = tr("copy.refused"); note.removeAttribute("hidden"); return; }
                b.textContent = "\u2713";
                b.classList.add("done");
                DomGlobal.setTimeout(a -> {
                    b.replaceChildren(Icons.shape("#i-sl-copy", null));
                    b.classList.remove("done");
                }, 1200);
            });
        });
        return b;
    }

    private interface Landed { void call(boolean ok); }

    private static native void copy(String text, Landed then) /*-{
        var ok = function (good) { then.@dev.sayaya.magi.client.interfaces.ConversationElement.Landed::call(Z)(good); };
        if (!$wnd.navigator.clipboard) { ok(false); return; }
        $wnd.navigator.clipboard.writeText(String(text || ''))
            .then(function () { ok(true); })["catch"](function () { ok(false); });
    }-*/;

    /**
     * 홈통에 적히는 이름 — <b>누가 말했는가</b>이지 어느 기계가 냈는가가 아니다.
     *
     * 카운슬 행은 그 자리의 이름을 쓴다("council"이 세 번 서 있으면 어느 자리인지 못 말한다),
     * 사람의 행은 "당신"(컴패니언이 이름을 붙였으면 그 이름), 시스템 행은 magi의 어느 부분이
     * 썼는지, 모델의 행은 magi다(운영 whoWord 그대로).
     */
    private static String whoWord(JsPropertyMap<Object> r, String who) {
        if ("council".equals(who) && !str(r, "member").isEmpty()) return str(r, "member");
        // 컴패니언이 사람을 달리 부르면 그 이름이지만(플러그인이 붙이는 사실), 그것은 로그에
        // 없고 데몬의 버스에만 있다 — 그 문이 생기기 전까지는 "당신"이다.
        if ("user".equals(who)) return tr("row.you");
        if ("system".equals(who)) {
            String by = str(r, "by");
            return by.isEmpty() ? tr("row.system") : by;
        }
        if ("assistant".equals(who)) return "magi";
        return who;
    }

    /**
     * 한 표결이 무엇을 보고 내려졌는가 — 가운데의 카드로 편다(사실판·파일과 같은 줄을 쓴다).
     *
     * 증거가 없는 라운드는 그렇다고 <b>말한다</b>: 소집이 접혀 나간 라운드의 증거는 정말로
     * 사라진 것이고, 그 자리를 조용히 비워 두면 못 읽은 것처럼 보여 사람이 다시 누르게 된다.
     */
    private void showVerdict(int round, String member, JsPropertyMap<Object> vote) {
        HTMLElement box = el("div");
        String key = "cr:" + round + ":" + member;
        box.id = key;
        box.setAttribute("title", member);
        box.style.setProperty("display", "contents");
        dev.sayaya.magi.bridge.CardSharing.closable(box, () -> { });
        HTMLElement bar = cell("filebar", null);
        HTMLElement who = cell("filedir", member.isEmpty() ? tr("council.outcome") : member);
        // 그 자리의 색 — 전사의 행이 쓰는 바로 그 토큰이라 둘이 어긋날 수가 없다. 아는 셋에만
        // 준다: 로그가 뭐라 적었든 그 문자열이 토큰 이름이 되지는 않는다.
        String seat = Rows.seatClass(member);
        if (!seat.isEmpty()) who.style.setProperty("color", "var(--magi-ref-" + member.toLowerCase() + ")");
        bar.append(who);
        String chip = voteChip(vote);
        if (!chip.isEmpty()) bar.append(cell("dchip", chip));
        HTMLElement body = cell("dinsp", null);
        body.append(cell("dnote", tr("detail.loading")));
        box.append(bar, body);
        // 이 카드는 자식이 만든 다른 카드들과 함께 부모에게 간다 — 자리를 아는 쪽은 부모다.
        cardsWith(box);
        store.councilEvidence(round, seen -> {
            body.replaceChildren();
            JsPropertyMap<Object> ev = seen == null ? null : Js.uncheckedCast(seen);
            if (ev == null) {
                body.append(cell("dk dhero", tr("detail.evidence")), cell("dnote", tr("detail.evidence_gone")));
            } else {
                body.append(cell("dk dhero", tr("detail.evidence")));
                section(body, "detail.task", str(ev, "task"), false);
                section(body, "detail.plan", str(ev, "plan"), false);
                section(body, "detail.report", str(ev, "report"), false);
                section(body, "detail.actions", str(ev, "actions"), true);
                section(body, "detail.changes", str(ev, "changes"), true);
                if (Js.isTruthy(ev.get("noChanges"))) body.append(cell("dnote", tr("detail.no_changes")));
            }
            // 그리고 표 자체 — 증거 다음이다. 순서가 논지다: 먼저 읽은 표는 믿는 수밖에 없고,
            // 나중에 읽은 표는 확인할 수 있다.
            section(body, "detail.rationale", str(vote, "why"), false);
            section(body, "detail.next", str(vote, "feedback"), false);
            // 수정이 <b>잃으면 안 되는 것</b>. 만들어지고, 자리마다 기록되고, 모델에게 되먹여지는데
            // 어디에도 그려지지 않았다 — 끝난 일을 지키는 그 한 줄만 아무도 못 읽고 있었다.
            section(body, "detail.keep", str(vote, "keep"), false);
            // 근거는 없는 두 경우까지 말한다: 보고의 내용으로 판단했다고 밝힌 표(NO-EVIDENCE)와,
            // 아무 것도 대지 않은 표. 아무 것도 딛지 않은 "done"은 그 자체가 봐야 할 사실이다.
            String cite = str(vote, "cite").trim();
            body.append(cell("dk", tr("detail.grounds")));
            body.append(cell("dbody", cite.isEmpty() ? tr("detail.no_grounds")
                    : "NO-EVIDENCE".equals(cite.toUpperCase()) ? tr("detail.judged_on_report")
                    : "\"" + cite + "\""));
        });
    }

    /**
     * 표를 한 줄로 — 결정어 · 렌즈 · 확신. 운영 detailHead의 그 칩과 같은 조립이다.
     *
     * <p>렌즈와 확신은 전선에 없어서 <b>매번</b> undefined였다(실측: 카운슬 행이 실어 온 것은
     * decision·member·round·text 넷뿐). 55%의 찬성과 95%의 찬성은 다른 표다.
     */
    private static String voteChip(JsPropertyMap<Object> v) {
        StringBuilder b = new StringBuilder();
        String decision = str(v, "decision");
        if (!decision.isEmpty()) b.append(councilWord(decision));
        String lens = str(v, "lens");
        if (!lens.isEmpty()) b.append(b.length() == 0 ? "" : " \u00b7 ").append(lens);
        double conf = v.get("confidence") == null ? 0 : Js.coerceToDouble(v.get("confidence"));
        if (conf > 0) b.append(b.length() == 0 ? "" : " \u00b7 ").append((int) Math.round(conf * 100)).append('%');
        return b.toString();
    }

    /** 표를 사람의 말로 — 운영 councilWordOf의 그 세 키(모르는 결정은 로그의 말 그대로). */
    private static String councilWord(String decision) {
        switch (decision) {
            case "done": return tr("council.accept");
            case "continue": return tr("council.reject");
            case "abstain": return tr("council.abstain");
            default: return decision;
        }
    }

    /** 이 화면이 연 카드들 — 파일과 같은 줄에 선다(부모가 그 줄을 그린다). */
    private final java.util.LinkedHashMap<String, HTMLElement> mine = new java.util.LinkedHashMap<>();

    private void cardsWith(HTMLElement card) {
        mine.put(card.id, card);
        dev.sayaya.magi.bridge.CardSharing.closable(card, () -> { mine.remove(card.id); pushCards(); });
        pushCards();
    }

    private void pushCards() {
        open.set("verdicts", new java.util.ArrayList<>(mine.values()));
    }

    private static void section(HTMLElement into, String key, String text, boolean pre) {
        if (text == null || text.trim().isEmpty()) return;
        into.append(cell("dk", tr(key)));
        if (!pre) { into.append(cell("dbody", text)); return; }
        // 감싸는 .dbody 안의 pre — console.css가 코드 블록을 입히는 자리가 거기다(.dbody pre).
        // 밖에 두었을 땐 등폭도, 가로 스크롤도, 배경도 없이 본문 글꼴로 흘렀다.
        HTMLElement wrap = cell("dbody", null);
        HTMLElement p = el("pre");
        p.textContent = text;
        wrap.append(p);
        into.append(wrap);
    }

    /**
     * 컴포저의 이어쓰기 — 칸 안에 유령을 끼워 넣지 않고 <b>아래에 흐리게</b> 적는다.
     *
     * 여기 칸은 컴포넌트(md-outlined-text-field)이고 그 속의 textarea는 섀도 루트 안이라,
     * 편집기가 하는 것처럼 같은 자리에 거울을 깔 수가 없다(운영이 이 화면에서 고른 그 타협).
     * Tab이 가져가고, Escape나 다음 타이핑이 지운다.
     */
    private void wireSuggest() {
        HTMLElement inner = field;
        inner.addEventListener("input", evt -> {
            clearSuggest();                      // 방금 친 것이 달라졌다 — 서 있던 제안은 낡았다
            if (suggestTick >= 0) DomGlobal.clearTimeout(suggestTick);
            suggestTick = DomGlobal.setTimeout(a -> askSuggest(), 400);
        });
        inner.addEventListener("keydown", evt -> {
            elemental2.dom.KeyboardEvent k = Js.uncheckedCast(evt);
            if ("Tab".equals(k.key) && !suggested.isEmpty() && !composing(k)) {
                evt.preventDefault();
                value(value() + suggested);      // 쓰다 만 자리에서 이어진다 — 대신하지 않는다
                clearSuggest();
                return;
            }
            if ("Escape".equals(k.key)) clearSuggest();
        });
        inner.addEventListener("blur", evt -> clearSuggest());
    }

    private void askSuggest() {
        if (!May.can("prompt")) return;
        String said = value();
        if (said.trim().isEmpty()) { clearSuggest(); return; }   // 빈 칸은 짐작할 자리가 아니다
        final int mine = ++suggestAt;
        store.suggest(said, text -> {
            if (mine != suggestAt) return;              // 더 새 요청이 앞질렀다
            if (!value().equals(said)) return;          // 기다리는 사이 계속 쳤다
            // 붙일 때는 온 그대로다(앞뒤 공백까지) — 이어붙는 글의 띄어쓰기는 그것을 쓴 쪽이
            // 정한다. 흐리게 보일 때만 다듬는다: 줄 앞 공백은 화면에서 빈칸으로만 보인다.
            suggested = text == null ? "" : text;
            if (suggested.trim().isEmpty()) { clearSuggest(); return; }
            hint.textContent = suggested.trim();
            hint.removeAttribute("hidden");
        });
    }

    private void clearSuggest() {
        suggestAt++;
        if (suggested.isEmpty() && hint.hasAttribute("hidden")) return;
        suggested = "";
        hint.textContent = "";
        hint.setAttribute("hidden", "");
    }

    private static native boolean composing(elemental2.dom.KeyboardEvent e) /*-{
        return !!(e.isComposing || e.keyCode === 229);
    }-*/;

    private String value() {
        Object v = Js.asPropertyMap(field).get("value");
        return v == null ? "" : String.valueOf(v);
    }

    private void value(String v) { Js.asPropertyMap(field).set("value", v); }




    /**
     * 이 소켓의 데몬이 아직 답하는가 — 명단에 없거나 있어도 live가 거짓이면 멈춘 것이다
     * (운영 companionAlive의 그 판정: 소켓이 치워졌거나, 남아 있어도 답하지 않거나).
     */
    private boolean alive = true;
    private dev.sayaya.magi.bridge.FleetAgent[] roster = null;

    private dev.sayaya.magi.bridge.FleetAgent aimed = null;

    private void aliveIs(boolean now) {
        if (alive == now) return;
        alive = now;
        if (!now) {
            // 대화가 있던 자리에 그 사정을 적는다. 상태줄 한 줄로는 왜 화면이 비었는지 알 수
            // 없고(운영 실측: 탭 띠와 컴포저 사이 378px의 빈 곳), 컴포저는 그대로 둔다 —
            // 다시 띄우면 이어서 말할 자리다.
            lastSig = null;
            forgetDrawn();
            HTMLElement empty = el("div");
            empty.className = "empty";
            empty.append(DomGlobal.document.createTextNode(tr("state.companion_gone")),
                    el("br"), DomGlobal.document.createTextNode(tr("state.gone_how")));
            log.replaceChildren(empty);
        } else {
            lastSig = null;   // 돌아왔다 — 다음 프레임이 전사를 통째로 다시 세운다.
        }
    }

    /** 지금 서 있는 행들과 그 말 — 자리로 견주기 위한 것(운영의 shownRows). */
    private final java.util.List<HTMLElement> drawn = new java.util.ArrayList<>();
    private final java.util.List<String> sigs = new java.util.ArrayList<>();

    private void forgetDrawn() { drawn.clear(); sigs.clear(); }

    private void paintRows(Object rowsOrNull) {
        if (!alive) return;   // 멈춘 컴패니언의 자리에는 그 사정이 서 있다.
        if (rowsOrNull == null) {
            // 아직 모른다 — 이전 컴패니언의 대화가 새 화면에 비치면 안 된다.
            lastSig = null;
            forgetDrawn();
            log.replaceChildren();
            return;
        }
        JsArrayLike<Object> rows = Js.uncheckedCast(rowsOrNull);
        String sig = rows.getLength() + "|" + rowSig(rows.getLength() == 0 ? null : rows.getAt(rows.getLength() - 1));
        if (sig.equals(lastSig)) return;
        lastSig = sig;
        boolean stick = atBottom();
        // 자리로 견주어 <b>달라진 행만</b> 짓는다(운영 draw()의 그 규칙).
        //
        // 전사는 한 턴씩 자라고, 통째로 다시 그리면 자란 것 하나 때문에 이미 읽고 있던 행이
        // 전부 새 노드가 된다 — 펼쳐 둔 툴 결과가 접히고, 고르던 글자가 풀리고, 브라우저는
        // 매 프레임 화면 전체를 다시 칠한다(실측: 10초에 49번, 그중 새 행은 여섯).
        for (int i = 0; i < rows.getLength(); i++) {
            JsPropertyMap<Object> row = Js.uncheckedCast(rows.getAt(i));
            String want = rowSig(row);
            HTMLElement had = i < drawn.size() ? drawn.get(i) : null;
            if (had != null && want.equals(sigs.get(i))) continue;   // 같은 말이면 그대로 둔다
            HTMLElement made = rowNode(row);
            if (had == null) {
                log.append(made);
                drawn.add(made);
                sigs.add(want);
            } else {
                had.replaceWith(made);
                drawn.set(i, made);
                sigs.set(i, want);
            }
        }
        // 남은 것은 이제 없는 행이다(전사가 짧아지는 자리: 컴패니언을 옮기거나 접었을 때).
        for (int i = drawn.size() - 1; i >= rows.getLength(); i--) {
            drawn.remove(i).remove();
            sigs.remove(i);
        }
        if (stick) toBottom();
    }

    private HTMLElement rowNode(JsPropertyMap<Object> r) {
        String who = str(r, "who");
        boolean hasOk = r.has("ok") && r.get("ok") != null;
        boolean ok = hasOk && Js.isTruthy(r.get("ok"));
        boolean pending = Js.isTruthy(r.get("pending"));
        HTMLElement d = el("div");
        d.className = Rows.rowClass(who, hasOk, ok,
                Js.isTruthy(r.get("note")), pending, Js.isTruthy(r.get("abandoned")),
                str(r, "decision"), str(r, "member"));
        HTMLElement w = el("div");
        w.className = "who";
        w.textContent = whoWord(r, who);
        // 카운슬 자리의 이름은 누를 수 있다 — 그 표결이 <b>무엇을 보고</b> 내려졌는지로 간다.
        // 표를 검증 가능하게 만드는 반쪽이고, 전사의 한 줄에는 그것이 들어갈 자리가 없다.
        String member = str(r, "member");
        double round = r.get("round") == null ? 0 : Js.coerceToDouble(r.get("round"));
        if ("council".equals(who) && !member.isEmpty() && round > 0) {
            HTMLElement name = el("button");
            name.setAttribute("type", "button");
            name.className = "who whoin hit48";
            name.textContent = whoWord(r, who);
            name.setAttribute("aria-label", tr("detail.evidence") + ": " + member);
            name.setAttribute("title", tr("detail.evidence"));
            final int at = (int) round;
            name.addEventListener("click", evt -> { evt.stopPropagation(); showVerdict(at, member, r); });
            w = name;
        }
        String at = str(r, "at");
        if (!at.isEmpty()) {
            HTMLElement when = el("div");
            when.className = "when";
            when.textContent = hhmm(at);
            w.append(when);
        }
        if (Rows.folded(who)) {
            d.append(w, foldNode(r, who, hasOk, ok, pending));
            return d;
        }
        if (pending) w.append(tag("row.working"));
        if (Js.isTruthy(r.get("abandoned"))) w.append(tag("row.abandoned"));
        // 사람이 화면에서 못 얻는 것 하나 — 적힌 그대로의 본문. 골라서 복사하면 <b>그려진</b>
        // 글이 나온다(표는 칸이 붙어 나오고 코드 울타리는 사라진다). 산문 두 행에만 둔다.
        String said = str(r, "text");
        if (("user".equals(who) || "assistant".equals(who)) && !said.trim().isEmpty()) w.append(copyChip(said));
        HTMLElement t = el("div");
        t.className = "txt";
        // 그리는 규칙은 누가 말했느냐로 갈린다(운영 rowNode의 그 세 갈래).
        //
        // 사람이 쓴 것은 <b>쓴 그대로</b> 보인다 — 파이프 표가 든 프롬프트를 마크다운으로 그리면
        // 자기가 치지 않은 모양으로 돌아온다. 에러는 색이 아니라 마크로 이끈다: 빨강만으로 말한
        // 상태는 잉크로만 말한 것이다. 나머지(모델의 말)는 마크다운으로 그린다 — 이 줄이 없던
        // 동안 표는 파이프의 벽으로, 펜스 블록은 백틱 셋과 붙은 본문으로 도착했다.
        String body = str(r, "text");
        if ("error".equals(who)) t.textContent = "\u2717 " + body;
        else if ("user".equals(who)) t.textContent = body;
        else Markdown.into(t, body);
        d.append(w, t);
        return d;
    }

    /**
     * 접힌 행 — 원본 rowNode 의 details.txt.fold 계약: 요약(마크+한 줄)이 닫혀 있어도 결말을
     * 말하고, 속은 fold.asked/fold.answered(디프면 fold.changed) 블록이다. 실패·주석은 열려서
     * 도착한다 — 읽으라고 온 행이라서. kind별 열림 선호는 localStorage, 프로그램 토글의
     * 메아리는 쓰지 않는다(원본에서 실측된 그 결함).
     */
    private HTMLElement foldNode(JsPropertyMap<Object> r, String who, boolean hasOk, boolean ok, boolean pending) {
        HTMLElement det = el("details");
        det.className = "txt fold";
        det.setAttribute("data-kind", who);
        boolean openNow = "failed".equals(who) || (hasOk && !ok) || "open".equals(stored("fold." + who));
        if (openNow) det.setAttribute("open", "");
        final boolean[] userToggle = {false};
        det.addEventListener("toggle", evt -> {
            if (!userToggle[0]) return;
            store("fold." + who, det.hasAttribute("open") ? "open" : "shut");
        });
        DomGlobal.setTimeout(a -> userToggle[0] = true, 0);

        HTMLElement head = el("summary");
        HTMLElement mk = mark(who, hasOk, ok, Js.isTruthy(r.get("note")));
        if (mk != null) head.append(mk, DomGlobal.document.createTextNode(" "));
        head.append(DomGlobal.document.createTextNode(summaryLine(r, who, hasOk)));
        det.append(head);

        HTMLElement body = el("div");
        body.className = "foldbody";
        String args = str(r, "args");
        String out = str(r, "out");
        String diff = str(r, "diff");
        if ("tool".equals(who) || "result".equals(who) || "failed".equals(who) || "shell".equals(who)) {
            int blocks = 0;
            if (!diff.isEmpty()) blocks = (pathOf(args).isEmpty() ? 0 : 1) + 1;
            else blocks = (args.isEmpty() ? 0 : 1) + (out.isEmpty() ? 0 : 1);
            if (!diff.isEmpty()) {
                String path = pathOf(args);
                if (!path.isEmpty()) { if (blocks > 1) body.append(foldKey("fold.asked")); body.append(pre(path, false)); }
                if (blocks > 1) body.append(foldKey("fold.changed"));
                body.append(diffPre(diff));
            } else if (!args.isEmpty() || !out.isEmpty()) {
                if (!args.isEmpty()) {
                    if (blocks > 1) body.append(foldKey("fold.asked"));
                    body.append(block(args));
                }
                if (!out.isEmpty()) {
                    if (blocks > 1) body.append(foldKey("fold.answered"));
                    body.append(block(out));
                }
            } else {
                body.append(pre(str(r, "text"), Rows.looksLikeDiff(str(r, "text"))));
            }
        } else {
            // thinking·council: 요약이 첫 줄을 이미 말했으니 속은 전체 본문이고, 그 본문은 모델이
            // 쓴 글이라 전사 행과 같은 규칙으로 그린다. 카운슬은 요약이 이미 말한 첫 줄을 빼고
            // 나머지를 그린다(운영의 그 두 갈래).
            HTMLElement t = el("div");
            String said = str(r, "text");
            if ("council".equals(who)) {
                String[] lines = said.split("\n", 2);
                said = lines.length > 1 ? lines[1].trim() : "";
            }
            body.append(Markdown.into(t, said));
        }
        det.append(body);
        if (pending) {
            HTMLElement bar = el("md-linear-progress");
            Js.asPropertyMap(bar).set("indeterminate", true);
            bar.className = "runbar";
            bar.setAttribute("aria-label", tr("row.working"));
            det.append(bar);
        }
        return det;
    }

    /** 요약 마크 — 어떻게 끝났나. 스프라이트가 없는 페이지라 원본의 폴백 글리프를 그대로 쓴다. */
    private static HTMLElement mark(String who, boolean hasOk, boolean ok, boolean note) {
        String glyph = null, cls = null;
        if ("tool".equals(who)) {
            if (!hasOk) { glyph = "\u2699"; cls = "spin"; }
            else if (ok) { glyph = "\u2713"; cls = "ok"; }
            else if (note) { glyph = "\u26A0"; cls = "note"; }
            else { glyph = "\u2717"; cls = "bad"; }
        } else if ("result".equals(who)) { glyph = "\u2713"; cls = "ok"; }
        else if ("failed".equals(who)) { glyph = "\u2717"; cls = "bad"; }
        if (glyph == null) return null;
        HTMLElement m = el("span");
        m.className = "mk " + cls;
        m.setAttribute("aria-hidden", "true");
        m.textContent = glyph;
        return m;
    }

    /** 접힌 행의 한 줄 — 열지 않고도 판단할 수 있어야 한다(원본 summaryFor의 이식). */
    private String summaryLine(JsPropertyMap<Object> r, String who, boolean hasOk) {
        String text = str(r, "text");
        if ("tool".equals(who)) {
            String args = str(r, "args");
            String asked = !str(r, "diff").isEmpty() ? pathOf(args) : Rows.oneLine(args, 60);
            String said = hasOk ? Rows.firstLine(decodeToolText(str(r, "out")), 44) : "";
            return str(r, "tool") + (asked.isEmpty() ? "" : " " + asked)
                    + (said.isEmpty() ? "" : "  \u27F6 " + said);
        }
        if ("council".equals(who)) return text.split("\n")[0];
        if ("shell".equals(who)) return "! " + text;
        if ("thinking".equals(who)) return tr("row.reasoning") + " \u00B7 " + Rows.oneLine(text, 80);
        return Rows.oneLine(text, 88);
    }

    private static HTMLElement foldKey(String key) {
        HTMLElement k = el("div");
        k.className = "foldk";
        k.textContent = tr(key);
        return k;
    }

    /**
     * 접힌 행 안의 한 덩어리 — 디프면 줄마다 클래스, <b>아는 키를 가진 객체면 인자표</b>,
     * 아니면 디코드된 텍스트 한 덩어리.
     *
     * 인자표가 있어야 하는 이유: 도구 인자는 이미 객체이고, 그것을 한 줄의 JSON으로 보이면
     * 경로도 명령도 본문도 escape 된 따옴표 사이에 묻힌다. 표로 펴면 이름은 고랑에, 값은 그
     * 옆에, 여러 줄인 값은 제 블록에 선다(운영 pairsInto의 계약 .args/.argk/.argv).
     */
    private static HTMLElement block(String raw) {
        if (Rows.looksLikeDiff(raw)) return pre(raw, true);
        java.util.List<String[]> pairs = Rows.jsonPairs(raw);
        if (pairs == null) return pre(raw, false);
        HTMLElement box = el("div");
        box.className = "args";
        for (String[] kv : pairs) {
            HTMLElement k = el("div");
            k.className = "argk";
            k.textContent = kv[0];
            box.append(k);
            // 어느 쪽이든 pre다 — 인자는 경로나 명령이나 본문이고, 셋 다 공백이 접히면 안 된다.
            // 한 줄짜리는 제 블록이 필요 없을 뿐이다.
            HTMLElement v = el("pre");
            v.className = "argv" + (kv[1].contains("\n") ? " tall" : "");
            v.textContent = kv[1];
            box.append(v);
        }
        return box;
    }

    /** 본문 블록 — 디프면 줄마다 클래스, 아니면 디코드된 텍스트 한 덩어리(pre). */
    private static HTMLElement pre(String raw, boolean asDiff) {
        if (asDiff) return diffPre(raw);
        HTMLElement pre = el("pre");
        pre.textContent = decodeToolText(raw);
        return pre;
    }

    private static HTMLElement diffPre(String text) {
        HTMLElement pre = el("pre");
        pre.className = "diff";
        String body = text == null ? "" : text;
        if (body.endsWith("\n")) body = body.substring(0, body.length() - 1);
        for (String line : body.split("\n", -1)) {
            HTMLElement row = el("span");
            row.className = Rows.diffLineClass(line);
            row.textContent = line + "\n";
            pre.append(row);
        }
        return pre;
    }

    /** 결과의 JSON 인코딩을 그것이 뜻하는 텍스트로 — 원본 decodeToolText의 이식. */
    private static String decodeToolText(String text) {
        if (text == null) return "";
        String trimmed = text.trim();
        if (trimmed.isEmpty() || (trimmed.charAt(0) != '"' && trimmed.charAt(0) != '[')) return text;
        try {
            Object v = elemental2.core.Global.JSON.parse(trimmed);
            if (v instanceof String) return (String) v;
            if (elemental2.core.JsArray.isArray(v)) {
                elemental2.core.JsArray<Object> arr = Js.uncheckedCast(v);
                StringBuilder b = new StringBuilder();
                for (int i = 0; i < arr.length; i++) {
                    if (i > 0) b.append('\n');
                    Object x = arr.getAt(i);
                    b.append(x == null || !"object".equals(Js.typeof(x))
                            ? String.valueOf(x) : elemental2.core.Global.JSON.stringify(x));
                }
                return b.toString();
            }
        } catch (Exception ignore) { /* JSON이 아니면 온 그대로 */ }
        return text;
    }

    /** 호출이 대는 파일 — 디프 위에 놓을 그 경로(원본 pathOf). */
    private static String pathOf(String args) {
        try {
            Object v = elemental2.core.Global.JSON.parse(args == null || args.isEmpty() ? "{}" : args);
            Object path = Js.asPropertyMap(v).get("path");
            return path instanceof String ? (String) path : "";
        } catch (Exception e) { return ""; }
    }

    private static HTMLElement tag(String key) {
        HTMLElement t = el("span");
        t.className = "pendtag";
        t.textContent = " \u00B7 " + tr(key);
        return t;
    }

    private static String stored(String key) {
        try {
            Object ls = Js.asPropertyMap(DomGlobal.window).get("localStorage");
            if (ls == null) return null;
            Object v = Js.asPropertyMap(ls).get(key);
            return v == null ? null : String.valueOf(v);
        } catch (Exception e) { return null; }
    }

    private static void store(String key, String val) {
        try {
            Object ls = Js.asPropertyMap(DomGlobal.window).get("localStorage");
            if (ls != null) Js.asPropertyMap(ls).set(key, val);
        } catch (Exception ignore) { /* storage can be denied */ }
    }

    private static String rowSig(Object row) {
        if (row == null) return "";
        JsPropertyMap<Object> r = Js.uncheckedCast(row);
        return str(r, "who") + str(r, "text") + str(r, "tool") + r.get("ok") + r.get("pending")
                + str(r, "out").length() + str(r, "args").length();
    }

    private static String str(JsPropertyMap<Object> r, String key) {
        Object v = r.get(key);
        return v == null ? "" : String.valueOf(v);
    }

    private static String hhmm(String rfc3339) {
        JsDate d = new JsDate(rfc3339);
        double h = d.getHours(), m = d.getMinutes();
        if (Double.isNaN(h)) return "";
        return (h < 10 ? "0" : "") + (int) h + ":" + (m < 10 ? "0" : "") + (int) m;
    }

    /**
     * 새 말이 왔을 때 따라 내려갈 것인가 — <b>흐르는 상자를 보고</b> 정한다.
     *
     * 페이지만 보고 있었다: 넓은 창에서 전사는 제 상자 안에서 흐르는데(가운데 기둥은 높이가
     * 정해져 있다) 그 상자의 자리를 페이지에게 물으니, 전사가 상자를 넘긴 순간부터 따라가기가
     * 꺼졌다 — 새 턴이 와도 화면은 첫 줄에 머물렀다(실측 1280px: 운영은 바닥에 붙어 85,
     * 이 콘솔은 0).
     */
    private boolean atBottom() {
        Element s = scroller();
        return s == null || s.scrollHeight - s.scrollTop - clientHeight(s) < 80;
    }

    /** 전사가 제 안에서 흐르면 그 상자가, 아니면 페이지가 스크롤러다(폰이 그렇다). */
    private Element scroller() {
        return log.scrollHeight - clientHeight(log) > 4 ? log : DomGlobal.document.scrollingElement;
    }

    private void toBottom() {
        Element s = scroller();
        if (s != null) s.scrollTop = s.scrollHeight;
    }

    private static double clientHeight(Element e) { return Js.coerceToDouble(Js.asPropertyMap(e).get("clientHeight")); }

    private static HTMLElement el(String tag) { return Js.uncheckedCast(DomGlobal.document.createElement(tag)); }
}
