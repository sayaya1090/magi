package dev.sayaya.magi.client.interfaces;

import dev.sayaya.magi.bridge.Tips;
import dev.sayaya.magi.bridge.Windows;
import dev.sayaya.magi.bridge.FleetAgent;
import dev.sayaya.magi.bridge.GoSharing;
import dev.sayaya.magi.component.Ages;
import dev.sayaya.magi.component.Dialogs;
import dev.sayaya.magi.component.Spans;
import dev.sayaya.magi.client.domain.Versions;
import dev.sayaya.magi.client.usecase.FleetCommander;
import elemental2.dom.DomGlobal;
import elemental2.dom.HTMLElement;
import elemental2.dom.KeyboardEvent;
import jsinterop.base.Js;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.function.Consumer;

import static dev.sayaya.magi.bridge.Labels.stateWord;
import static dev.sayaya.magi.bridge.Labels.tr;
import static dev.sayaya.magi.client.interfaces.Dom.cell;
import static dev.sayaya.magi.client.interfaces.Dom.el;

/**
 * 행 하나를 그리는 법 전부 — 기존 콘솔 card()/answerBox()/rowActions()의 이식.
 *
 * 스트림이 3초마다 목록을 다시 밀어도, 아무것도 안 변한 행은 이미 화면에 있는 그 행이다:
 * cardSig가 행이 그리는 전부를 서명하고, 서명이 같으면 노드를 재사용한다. 입력 중인 답
 * 필드와 조준 중인 호버를 지키는 캐시다. 서명에서 빠진 필드는 갱신이 멎는 필드가 되므로,
 * 행에 새 값을 그리게 되면 서명에도 넣는다.
 */
@Singleton
public class CardListElement {
    private final FleetCommander commander;
    private final Dialogs dialogs;
    private final Map<String, String> wasState = new HashMap<>();
    private final Map<String, Memo> shownCards = new HashMap<>();
    /**
     * 거절당한 명령의 사유 — <b>그 카드의 것</b>이라 카드 열쇠로 쥔다.
     *
     * <p>멈추라는 명령이 거절당하면 명단만 다시 서고, 그 컴패니언은 계속 working이다 —
     * 「멈추는 중」과 구별이 안 된다. 명단을 다시 읽는 것이 이 화면을 칠하므로 사유를
     * <b>먼저</b> 쥐고 나서 읽는다(접근·설정 화면과 같은 순서).</p>
     */
    private final Map<String, String> refusals = new HashMap<>();

    private static final class Memo {
        final String sig; final HTMLElement node;
        Memo(String sig, HTMLElement node) { this.sig = sig; this.node = node; }
    }

    @Inject
    public CardListElement(FleetCommander commander, Dialogs dialogs) {
        this.commander = commander;
        this.dialogs = dialogs;
    }

    /** 명단에서 사라진 것은 기억할 가치도 사라진 것이다 — 탭 수명만큼 자라는 맵 방지. */
    public void prune(FleetAgent[] list) {
        Set<String> alive = new HashSet<>(), aliveSockets = new HashSet<>();
        for (FleetAgent a : list) { alive.add(key(a)); aliveSockets.add(a.socket); }
        shownCards.keySet().removeIf(k -> !alive.contains(k));
        refusals.keySet().removeIf(k -> !alive.contains(k));
        wasState.keySet().removeIf(s -> !aliveSockets.contains(s));
    }

    /**
     * 한 행. after는 행동(인터럽트/답) 뒤의 재조회, jumpNext는 답한 뒤 다음 대기 행으로의
     * 이동 — 답하기는 브라우즈가 아니라 큐다.
     */
    public HTMLElement row(FleetAgent a, String newestVer, Runnable after, Consumer<String> jumpNext) {
        String sig = sig(a, newestVer);
        Memo had = shownCards.get(key(a));
        if (had != null && had.sig.equals(sig)) return had.node;
        HTMLElement node = card(a, newestVer, after, jumpNext);
        shownCards.put(key(a), new Memo(sig, node));
        return node;
    }

    private static String key(FleetAgent a) { return (a.peer == null ? "" : a.peer) + " " + a.socket; }

    private String sig(FleetAgent a, String newestVer) {
        StringBuilder r = new StringBuilder();
        // 쉰 시간(idle)은 여기 없다 — <b>매 초 달라지는 값</b>이라, 넣으면 아무 일도 없는 행이
        // 초당 한 번씩 새 노드가 되어 깜빡인다(실측: 멈춰 있는 ops 행이 그랬다).
        //
        // 빼는 대신 <b>이 서명 밖에서</b> 늙힌다: 나이 칸은 마지막 소식의 순간을 이고, 창의
        // 시계가 그 칸의 글자만 고쳐 쓴다(Ages). 서 있던 노드를 그대로 두는 것과 그 노드의
        // 나이가 흐르는 것이 이제 같이 참이다.
        //
        // 「다음 진짜 변화 때 함께 새로 그려진다」고 여기 적혀 있었는데 <b>둘 다 틀렸다</b>:
        // 운영의 cardSig는 idle을 넣고 있고(page.js), 그런데도 그 프레임이 오지 않아 운영도
        // 같이 얼어 있었다. 그리고 조용한 행에는 「다음 진짜 변화」가 오지 않는다 — 나이를
        // 보는 이유가 바로 아무 일도 안 일어났다는 것이므로.
        for (Object v : new Object[]{a.state, a.name, a.role, a.team, a.hub, a.workdir, a.session,
                a.steps, a.task, a.doing, a.asking, a.askId, a.askKind, a.planDone, a.planTotal,
                a.host, a.addr, a.pid, a.peer, a.live, a.permission, a.user, a.version, newestVer}) {
            r.append(v).append('\1');
        }
        if (a.report != null) for (FleetAgent.ReportSection s : a.report) r.append(s.key).append(':').append(s.text).append('|');
        // 사유도 이 카드의 모습이다 — 빼면 거절이 와도 캐시된 옛 카드가 그대로 선다.
        r.append('\1').append(refusals.getOrDefault(key(a), ""));
        return r.toString();
    }

    private HTMLElement card(FleetAgent a, String newestVer, Runnable after, Consumer<String> jumpNext) {
        HTMLElement row = el("a");
        // 상태 변화는 딱 한 번 씻긴다(noticed) — 첫 등장은 전부 새것이라 아무 말도 아니다.
        String before = wasState.get(a.socket);
        boolean news = before != null && !before.equals(a.state);
        wasState.put(a.socket, a.state);
        row.className = "card " + a.state + " state" + (a.here ? " here" : "") + (news ? " noticed" : "");
        row.setAttribute("data-socket", a.socket == null ? "" : a.socket);
        // 행은 컴패니언 화면으로 가는 문이다 — 이동(주소)은 셸의 것이라 GoSharing으로
        // 청한다. 원본 규칙 그대로 elsewhere가 아닌 행만: 남의 파일시스템의 소켓 경로는
        // 여기서 열 수 없는 것이 정직한 모양이다. 셸 없이 단독으로 떴으면 문이 없다.
        if (!a.elsewhere) {
            // 진짜 앵커다 — 가운데 클릭과 복사한 주소가 살아 있어야 하고, 커서도 손가락이다
            // (실측: href 없는 행은 default 커서였다). 이동은 셸의 문으로 간다.
            final String sock = a.socket;
            final String peerOf = a.peer;
            row.setAttribute("href", Windows.here() + "?d=" + elemental2.core.Global.encodeURIComponent(sock)
                    + (peerOf == null || peerOf.isEmpty() ? ""
                       : "&p=" + elemental2.core.Global.encodeURIComponent(peerOf)));
            row.addEventListener("click", evt -> {
                elemental2.dom.MouseEvent me = Js.uncheckedCast(evt);
                // 수식 키는 브라우저의 것 — 새 탭·새 창은 앵커가 원래 하던 일이다.
                if (me.metaKey || me.ctrlKey || me.shiftKey || me.button != 0) return;
                evt.preventDefault();
                GoSharing.go(sock, peerOf);
            });
        }

        HTMLElement badge = cell("badge", stateWord(a.state));
        // 제 플랜의 어디까지 왔나 — 진행 막대가 아니라 개수: 투두는 일정표가 아니다.
        if (a.planTotal > 0) badge.append(cell("plan", a.planDone + "/" + a.planTotal));
        String load = carrying(a);
        if (!load.isEmpty()) badge.append(cell("load", load));
        row.append(badge);

        HTMLElement who = cell("who-col", null);
        who.append(cell("name", a.name));
        if (a.role != null && !a.role.isEmpty()) who.append(cell("role", a.role));
        who.append(cell("path", a.workdir));
        row.append(who);

        // 뭘 하는가. 막힌 행은 질문이 대신 선다 — 그게 알아야 할 것이고, 답 버튼은
        // 행이 아니라 질문 밑에 산다.
        HTMLElement doing = cell("doing", null);
        if (a.asking != null && !a.asking.isEmpty()) {
            doing.append(cell("asking", "⏸ " + a.asking), answerBox(a, after, jumpNext));
        } else if (a.task != null && !a.task.isEmpty()) {
            doing.append(cell("last", a.task));
            // 요청 아래, 그 안의 툴이 보고 중일 때: 10분째 한 호출 안의 턴이 멈춘 건지
            // 묻는 사람에게 답하는 줄(working에서만 온다 — fleet.Agent.Doing).
            if (a.doing != null && !a.doing.isEmpty()) {
                HTMLElement n = cell("note", null);
                HTMLElement gl = el("span");
                gl.className = "gl spin";
                gl.textContent = "⏳";
                gl.setAttribute("aria-hidden", "true");
                n.append(gl, DomGlobal.document.createTextNode(" " + a.doing));
                doing.append(n);
            }
        }
        row.append(doing);

        // 스텝 수는 폰에서 제 이름표를 스스로 단다(colk) — 열 머리가 안 그려지는 폭에서.
        HTMLElement steps = cell("num r", a.steps > 0 ? String.valueOf(a.steps) : "—");
        steps.append(cell("colk", tr("field.steps")));
        row.append(steps);
        HTMLElement idle = cell("num r", ago(a.idle));
        Ages.on(idle, a.idle);
        row.append(idle);

        HTMLElement host = cell("host", null);
        HTMLElement b = el("b");
        // 페더레이션이면 온 콘솔 이름이 먼저, 아니면 인스턴스(account@host) — 계정이 다르면
        // 정책도 세션 저장소도 다른 딴 플릿이다. 옛 데몬은 host만 준다.
        b.textContent = a.peer != null && !a.peer.isEmpty() ? a.peer
                : a.instance != null && !a.instance.isEmpty() ? a.instance
                : a.host != null && !a.host.isEmpty() ? a.host : tr("map.here");
        host.append(b);
        if (a.addr != null && !a.addr.isEmpty()) host.append(el("br"), DomGlobal.document.createTextNode(a.addr));
        if (a.here) host.append(el("br"), DomGlobal.document.createTextNode("this directory"));
        if (a.version != null && !a.version.isEmpty()) {
            HTMLElement ver = cell("ver", a.version);
            if (!newestVer.isEmpty() && Versions.compare(a.version, newestVer) < 0) {
                ver.classList.add("behind");
                ver.append(cell("vhint", tr("ver.behind", "v", newestVer)));
            }
            host.append(el("br"), ver);
        }
        row.append(host);

        row.append(rowActions(a, after));
        String no = refusals.get(key(a));
        if (no != null && !no.isEmpty()) {
            // 사유는 <b>이 카드</b>에 선다 — 누른 단추가 여기 있고, 거절되면 카드는 눌리기 전
            // 그대로라(여전히 working) 다른 데 세워 봐야 무엇이 안 됐는지 가리키지 못한다.
            // 서버의 문장을 그대로 옮긴다 — 우리가 지어낼 수 있는 말이 아니다.
            HTMLElement said = cell("refused", no);
            said.setAttribute("role", "alert");
            row.append(said);
        }
        HTMLElement why = grounds(a);
        if (why != null) row.append(why);
        return row;
    }

    private static String ago(int s) { return s < 0 ? "" : tr("time.ago", "d", Spans.dur(s)); }

    /** 이웃이 넘긴 짐: 손에 든 하나와 그 뒤의 줄. 없으면 조용하다(빈 0은 소음 기둥이다). */
    private static String carrying(FleetAgent a) {
        List<String> parts = new ArrayList<>();
        if (a.handling) parts.add(tr("load.in_hand"));
        if (a.waiting > 0) parts.add(tr("load.waiting", "n", String.valueOf(a.waiting)));
        return String.join(", ", parts);
    }

    /** 결정의 근거 — 질문이 아니라 그 뒤의 작업이라 별도 블록. 없으면 null(빈 상자는 거짓말). */
    private HTMLElement grounds(FleetAgent a) {
        if (a.report == null || a.report.length == 0) return null;
        HTMLElement box = cell("grounds span", null);
        int kept = 0;
        for (FleetAgent.ReportSection sec : a.report) {
            if (sec == null || sec.text == null || sec.text.isEmpty()) continue;
            HTMLElement s = cell("gsec", null);
            s.append(cell("gk", sec.key), cell("gv", sec.text));
            box.append(s);
            kept++;
        }
        return kept > 0 ? box : null;
    }

    /** 세우기, 그리고 그것뿐. 행을 열지 않고도 멈출 수 있어야 폭주가 30초를 더 못 번다. */
    private HTMLElement rowActions(FleetAgent a, Runnable after) {
        HTMLElement box = cell("actions", null);
        if (!(a.live && ("working".equals(a.state) || "waiting".equals(a.state)))) return box;
        HTMLElement stopBtn = el("md-icon-button");
        stopBtn.className = "stop";
        // 대상으로 이름 붙인다: 다섯 행의 "Interrupt" 다섯 개는 리더에게 같은 단어 다섯 번이다.
        String stopName = tr("action.for_companion", "action", tr("action.interrupt"), "name", a.name == null ? "" : a.name);
        stopBtn.setAttribute("aria-label", stopName);
        Tips.on(stopBtn, stopName);
        stopBtn.innerHTML = "<svg viewBox=\"0 0 24 24\" width=\"20\" height=\"20\" aria-hidden=\"true\">"
                + "<rect x=\"7\" y=\"7\" width=\"10\" height=\"10\" rx=\"1.5\" fill=\"currentColor\"/></svg>";
        stopBtn.addEventListener("click", evt -> {
            evt.preventDefault();
            evt.stopPropagation();
            dialogs.stop(a.name, () -> commander.interrupt(a, no -> {
                if (no == null || no.isEmpty()) refusals.remove(key(a));
                else refusals.put(key(a), no);
                after.run();
            }));
        });
        box.append(stopBtn);
        return box;
    }

    /** 막힌 질문의 답 상자. 옵션 목록·자유 입력·퍼미션 네 결정의 세 모양. */
    private HTMLElement answerBox(FleetAgent a, Runnable after, Consumer<String> jumpNext) {
        HTMLElement box = el("div");
        box.className = "answer";
        // 사유는 멈춤이 거부됐을 때와 <b>같은 줄</b>에 선다. 예전엔 버렸고 그 이유를 "카드에는
        // 적을 줄이 없다"고 적어 두었는데, 이제 있다 — 줄이 생겼으니 버릴 까닭도 사라졌다.
        // 거부된 답은 다음 대기 행으로 넘어가지 않는다: 방금 세운 사유에서 눈을 떼게 하는
        // 이동이고, 답이 서지 못한 자리는 바로 그 카드다.
        Consumer<String> send = text -> commander.answer(a, text, why -> {
            boolean refused = why != null && !why.isEmpty();
            if (refused) refusals.put(key(a), why);
            else refusals.remove(key(a));
            after.run();
            if (!refused) jumpNext.accept(a.socket);
        });
        if ("question".equals(a.askKind) && a.askOptions != null && a.askOptions.length > 0) {
            // 목록이 제안이다: 가운데 정렬 — 이건 질문에 대한 툴바가 아니라 질문의 답이다.
            box.classList.add("choices");
            for (String opt : a.askOptions) {
                HTMLElement b = el("md-outlined-button");
                b.textContent = opt;
                b.addEventListener("click", evt -> { evt.preventDefault(); evt.stopPropagation(); send.accept(opt); });
                box.append(b);
            }
            // 목록이 왔다고 해서 <b>쓰는 자리</b>가 사라지지는 않는다: 누르거나, 목록에 없는
            // 것을 쓰거나 — 같은 제안의 두 반쪽이다(운영도 둘 다 낸다). 목록만 내놓으면 답이
            // 목록 밖일 때 사람은 위로 스크롤해 컴포저에 옮겨 적어야 했다.
            for (HTMLElement n : textAnswer(send)) box.append(n);
        } else if ("question".equals(a.askKind)) {
            for (HTMLElement n : textAnswer(send)) box.append(n);
        } else {
            // 퍼미션: 터미널이 늘 가졌던 네 결정, 전부 한 무게 — 콘솔이 사람 대신 기울면 안 된다.
            HTMLElement acts = cell("bgroup", null);
            box.append(acts);
            String[][] decisions = {{"action.allow", "allow"}, {"action.always", "always"},
                                    {"action.keep", "persist"}, {"action.deny", "deny"}};
            for (String[] d : decisions) {
                HTMLElement b = el("md-outlined-button");
                b.textContent = tr(d[0]);
                if (a.name != null && !a.name.isEmpty()) {
                    b.setAttribute("aria-label", tr("action.for_companion", "action", tr(d[0]), "name", a.name));
                }
                final String decision = d[1];
                b.addEventListener("click", evt -> { evt.preventDefault(); evt.stopPropagation(); send.accept(decision); });
                acts.append(b);
            }
        }
        return box;
    }

    /** 한 줄의 답과 보내기 버튼. 비면 disabled — 눌리는데 아무것도 안 하는 셋째 상태는 없다. */
    private HTMLElement[] textAnswer(Consumer<String> send) {
        HTMLElement i = el("md-outlined-text-field");
        i.setAttribute("label", tr("label.answer"));
        HTMLElement b = el("md-filled-button");
        b.textContent = tr("action.answer");
        Runnable arm = () -> {
            if (fieldValue(i).trim().isEmpty()) b.setAttribute("disabled", "");
            else b.removeAttribute("disabled");
        };
        arm.run();
        i.addEventListener("input", evt -> arm.run());
        // 행이 링크가 되어도 이 상자 안의 탭·클릭은 항해가 아니다(원본의 preventDefault 규칙).
        i.addEventListener("click", evt -> { evt.preventDefault(); evt.stopPropagation(); i.focus(); });
        i.addEventListener("keydown", evt -> {
            KeyboardEvent ke = Js.uncheckedCast(evt);
            if ("Enter".equals(ke.key) && !Js.isTruthy(Js.asPropertyMap(ke).get("isComposing"))) {
                evt.preventDefault(); evt.stopPropagation();
                String said = fieldValue(i).trim();
                if (!said.isEmpty()) send.accept(said);
            }
        });
        b.addEventListener("click", evt -> {
            evt.preventDefault(); evt.stopPropagation();
            String said = fieldValue(i).trim();
            if (!said.isEmpty()) send.accept(said);
        });
        return new HTMLElement[]{i, b};
    }

    private static String fieldValue(HTMLElement field) {
        Object v = Js.asPropertyMap(field).get("value");
        return v == null ? "" : String.valueOf(v);
    }
}
