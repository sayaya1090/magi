package dev.sayaya.magi.client.interfaces;

import dev.sayaya.magi.bridge.GoSharing;
import dev.sayaya.magi.bridge.Icons;
import dev.sayaya.magi.bridge.Tips;
import dev.sayaya.magi.bridge.Windows;
import dev.sayaya.magi.client.domain.Rooms;
import dev.sayaya.magi.client.usecase.MeetingStore;
import elemental2.dom.DomGlobal;
import elemental2.dom.HTMLElement;
import elemental2.dom.KeyboardEvent;
import jsinterop.base.Js;
import jsinterop.base.JsArrayLike;
import jsinterop.base.JsPropertyMap;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;

import static dev.sayaya.magi.bridge.Labels.tr;

/**
 * 회의실 — 이 콘솔이 컴패니언 여럿에게 <b>한 번에</b> 묻는 자리.
 *
 * 두 얼굴이다: 주소에 ?m= 이 없으면 <b>여는 화면</b>(무엇을 물을지, 누구에게, 그리고 지금
 * 열려 있거나 끝난 회의들), 있으면 <b>그 방</b>(주제·명단·오간 말·한 마디·마무리).
 *
 * 마크업 이름은 운영 콘솔의 것이다(#meet, .meetbox/.meethead/.meetroster/.meetsaid/…):
 * console.css가 그 이름으로 입힌다.
 *
 * 폴이 두 초마다 다시 그리므로, <b>쓰는 중이면 그리지 않는다</b>. 사람이 주제를 쓰는 중에
 * 폼을 다시 지으면 캐럿이 문장 밖으로 튕긴다 — 방에서는 바닥을 쥐고 있어 그 사이 아무 말도
 * 끼어들 수 없으니, 멈춰 있어도 잃는 것이 없다.
 */
@Singleton
public class MeetingElement {
    private final MeetingStore store;
    private final HTMLElement root = el("div");
    private boolean wired = false;
    private HTMLElement topicField = null;
    private HTMLElement sayField = null;
    private String drawnShape = "";
    /** 지금 서 있는 「작업 중」 판 — 프레임이 올 때 회의 본문 대신 이것만 갈아 끼운다. */
    private HTMLElement nowBox = null;
    // 거절의 사유는 <b>이 화면이</b> 쥔다 — 어느 상자에서 눌렀는지, 그때 어느 방이었는지까지.
    private String refusal = "";
    private String refusalAt = "";
    private String refusalRoom = "";

    @Inject
    public MeetingElement(MeetingStore store) { this.store = store; }

    public void mount(HTMLElement frame) {
        root.id = "meet";
        frame.replaceChildren(root);
        aim();
        // 떠날 때 푼 조준을 다시 건다 — aim()은 같은 방이면 일찍 반환하므로 이 줄이 없으면
        // 두 번째 방문부터 「지금 하는 것」이 영영 조용하다.
        store.rejoin();
        if (wired) { store.read(); return; }
        wired = true;
        // 말이 바뀌면 이 판도 다시 칠한다 — 언어를 간 사람이 화면을 옮겨 다니며 옛말을
        // 만나지 않게(운영 labels$의 그 구독).
        dev.sayaya.magi.bridge.Labels.onPack(this::render);
        // 이 화면이 그리는 것만 — 폴이 같은 답을 다시 가져왔다고 판을 새로 세우지 않는다
        // (실측: 10초에 20번, 그 사이 사람이 쓰던 말이 사라진다).
        store.drawn().subscribe(sig -> render());
        // 주소가 방을 바꾸면 그리로 — 셸이 흘리는 주소를 듣는다(뒤로가기도 이 길로 온다).
        DomGlobal.window.addEventListener("popstate", evt -> aim());
        // 두 초마다: 회의는 남이 말해서 바뀐다 — 이 화면만 스트림 밖이라 폴이 그 자리를 대신한다.
        // 화면을 떠나면 회선의 조준도 푼다. 셸의 라우터는 pushState로도 옮기므로 popstate만
        // 믿으면 아무도 안 보는 회의의 방 프레임이 계속 흐른다 — 떠난 것을 아는 가장 싼 자리가
        // 이 틱이다(이미 돌고 있고, 마운트 여부를 이미 묻고 있다).
        DomGlobal.setInterval(args -> {
            if (mounted()) store.read();
            else store.leave();
        }, 2000);
        // 「지금 하는 것」은 폴이 아니라 셸의 회선으로 온다 — 회의 본문과 같은 한 줄에 실려
        // 오는 것이 서버 계약이고(main.go의 roomFrames), 여기 폴을 하나 더 다는 것은 같은
        // 것을 두 번 묻는 일이다. 프레임이 판을 바꿨을 때만 다시 그린다: store.drawn()을
        // 거치지 않으므로 회의 본문은 헐리지 않는다.
        dev.sayaya.magi.bridge.RoomSharing.subscribe(f -> {
            if (mounted() && store.live(f)) paintNow();
        });
        store.read();
    }

    private boolean mounted() { return root.isConnected; }

    private void aim() { store.aim(Windows.query("m")); }

    // ── 그리기 ────────────────────────────────────────────────────────────────

    private void render() {
        if (store.room() == null) { drawConvene(); return; }
        if (store.gone()) { drawGone(); return; }
        if (store.one() != null) drawRoom(Js.uncheckedCast(store.one()));
    }

    /** 여는 화면: 무엇을 묻고 누구에게, 그리고 지금 도는 방들. */
    private void drawConvene() {
        // 주제를 쓰는 중이면 그대로 둔다 — 아래 목록을 새로 하겠다고 캐럿을 뺏지 않는다.
        if (typingIn(topicField)) return;
        drawnShape = "";
        HTMLElement box = cell("meetbox", null);
        box.append(head(tr("meet.title"), toFleet()));
        box.append(cell("meetwhy", tr("meet.why")));
        box.append(cell("meetends", tr("meet.ends")));

        HTMLElement topic = el("md-outlined-text-field");
        topic.className = "meettopicfield";
        topic.setAttribute("label", tr("meet.topic"));
        topic.setAttribute("type", "textarea");
        topic.setAttribute("rows", "2");
        value(topic, store.topic());
        topicField = topic;
        topic.addEventListener("input", evt -> { store.topic(value(topic)); arm(box); });
        box.append(topic);

        box.append(cell("meetlbl", tr("meet.who")));
        List<JsPropertyMap<Object>> here = callable();
        box.append(who(here));
        HTMLElement go = el("md-filled-button");
        go.className = "meetgo";
        go.textContent = tr("meet.start");
        Icons.mark(go, "#i-sl-comments");
        go.addEventListener("click", evt -> whileItRuns(go, () ->
                store.convene(why -> note("convene", why), id -> {
                    note("convene", "");
                    if (id == null || id.isEmpty()) { store.read(); return; }
                    GoSharing.viewWith("meet", "m", id);
                })));
        box.append(go, cell("meetnote", ""));
        for (HTMLElement one : roomLists()) box.append(one);
        root.replaceChildren(box);
        arm(box);
        sayRefusal();
    }

    /** 부를 수 있는 이들만 — 남의 기계의 컴패니언은 이 콘솔이 한 번도 걸어 본 적 없는 행이다. */
    private List<JsPropertyMap<Object>> callable() {
        List<JsPropertyMap<Object>> out = new ArrayList<>();
        JsArrayLike<Object> list = Js.uncheckedCast(store.fleet());
        if (list == null) return out;
        Set<String> alive = new LinkedHashSet<>();
        for (int i = 0; i < list.getLength(); i++) {
            JsPropertyMap<Object> a = Js.uncheckedCast(list.getAt(i));
            if (bool(a, "elsewhere") || !str(a, "peer").isEmpty()) continue;
            out.add(a);
            alive.add(str(a, "socket"));
        }
        // 명단에서 사라진 이의 선택은 함께 사라진다 — 칩도 없이 죽은 소켓이 남아 회의를 열었다.
        store.keepOnly(alive);
        return out;
    }

    /**
     * 누구에게 물을 것인가. <b>주인별로 묶고</b>(계정 경계가 더 단단하다), <b>팀은 색</b>으로
     * 가른다 — 넷일 땐 한 줄이면 되지만 열다섯이면 고를 수가 없다. 색은 유일한 말이 아니라,
     * 팀 이름은 툴팁에도 있다.
     */
    private HTMLElement who(List<JsPropertyMap<Object>> here) {
        HTMLElement box = cell("meetwho", null);
        List<String> teams = new ArrayList<>();
        for (JsPropertyMap<Object> a : here) {
            String t = str(a, "team");
            if (!t.isEmpty() && !teams.contains(t)) teams.add(t);
        }
        teams.sort(String::compareTo);
        Map<String, List<JsPropertyMap<Object>>> groups = new LinkedHashMap<>();
        for (JsPropertyMap<Object> a : here) {
            String owner = !str(a, "instance").isEmpty() ? str(a, "instance") : str(a, "host");
            groups.computeIfAbsent(owner, k -> new ArrayList<>()).add(a);
        }
        // 칩 하나는 한 벌이 아니다 — 부를 이가 둘도 안 되면 고를 것이 없고, 아래 줄이 그것을 말한다.
        if (here.size() < 2) return box;
        for (Map.Entry<String, List<JsPropertyMap<Object>>> g : groups.entrySet()) {
            if (groups.size() > 1 && !g.getKey().isEmpty()) box.append(cell("meetowner", g.getKey()));
            HTMLElement set = el("md-chip-set");
            for (JsPropertyMap<Object> a : g.getValue()) {
                String socket = str(a, "socket");
                HTMLElement c = el("md-filter-chip");
                int slot = teams.indexOf(str(a, "team"));
                c.className = "meetpick" + (slot >= 0 ? " tm" + slot : "");
                c.setAttribute("label", str(a, "name"));
                String says = joinBits(str(a, "team").isEmpty() ? ""
                        : tr("meet.of_team", "team", str(a, "team")), str(a, "role"));
                // 빈 말이면 붙지 않는다(Tips.on의 계약).
                Tips.on(c, says);
                Js.asPropertyMap(c).set("selected", store.picked().contains(socket));
                c.addEventListener("click", evt -> {
                    store.pick(socket);
                    Js.asPropertyMap(c).set("selected", store.picked().contains(socket));
                    arm(null);
                });
                set.append(c);
            }
            box.append(set);
        }
        return box;
    }

    /** 지금 도는 방과, 결론이 남은 끝난 방. */
    private List<HTMLElement> roomLists() {
        List<HTMLElement> out = new ArrayList<>();
        JsArrayLike<Object> list = Js.uncheckedCast(store.rooms());
        if (list == null) return out;
        List<JsPropertyMap<Object>> going = new ArrayList<>(), done = new ArrayList<>();
        for (int i = 0; i < list.getLength(); i++) {
            JsPropertyMap<Object> m = Js.uncheckedCast(list.getAt(i));
            if (!bool(m, "closed")) going.add(m);
            else if (len(m, "tasks") > 0) done.add(m);
        }
        for (Object[] pair : new Object[][]{{"meet.open", going}, {"meet.finished", done}}) {
            @SuppressWarnings("unchecked")
            List<JsPropertyMap<Object>> rooms = (List<JsPropertyMap<Object>>) pair[1];
            if (rooms.isEmpty()) continue;
            out.add(head(tr((String) pair[0]), null));
            HTMLElement l = cell("meetlist", null);
            for (JsPropertyMap<Object> m : rooms) l.append(roomRow(m));
            out.add(l);
        }
        return out;
    }

    private HTMLElement roomRow(JsPropertyMap<Object> m) {
        HTMLElement a = el("a");
        a.className = "meetrow state";
        String id = str(m, "id");
        a.setAttribute("href", "?v=meet&m=" + id);
        a.addEventListener("click", evt -> {
            elemental2.dom.MouseEvent me = Js.uncheckedCast(evt);
            if (me.metaKey || me.ctrlKey || me.shiftKey) return;   // 새 탭은 새 탭으로
            evt.preventDefault();
            GoSharing.viewWith("meet", "m", id);
        });
        a.append(cell("meettitle", str(m, "topic")));
        StringBuilder names = new StringBuilder();
        JsArrayLike<Object> sp = Js.uncheckedCast(m.get("speakers"));
        if (sp != null) for (int i = 0; i < sp.getLength(); i++) {
            JsPropertyMap<Object> s = Js.uncheckedCast(sp.getAt(i));
            if (bool(s, "person")) continue;
            if (names.length() > 0) names.append(", ");
            names.append(str(s, "name"));
        }
        a.append(cell("meetmeta", where(m) + " · " + names));
        return a;
    }

    private void drawGone() {
        HTMLElement box = cell("meetbox", null);
        box.append(head(tr("meet.title"), null));
        HTMLElement empty = cell("empty", null);
        empty.append(cell("emptywhat", tr("meet.gone")), cell("emptyhow", tr("meet.gone_how")));
        box.append(empty);
        root.replaceChildren(box);
        sayRefusal();
    }


    // ── 방 ────────────────────────────────────────────────────────────────────

    /**
     * 그 방. 주제와 명단은 붙박이고, 오간 말이 그 아래로 흐른다 — 다섯 바퀴 넷이면 여러
     * 화면이라, 무엇을 묻는 방인지와 지금 누가 말하는지는 어느 지점에서든 보여야 한다.
     */
    private void drawRoom(JsPropertyMap<Object> m) {
        // 쓰는 중이면 멈춘다. 바닥을 쥐고 있어 그 사이 아무 말도 끼어들지 못하니 잃는 것이 없다.
        if (typingIn(sayField)) return;
        // 달라진 게 없으면 다시 그리지 않는다 — 두 초마다 통째로 다시 지으면 칩도 상자도
        // 버튼도 매번 새것이 된다(워크스페이스 판에서 겪은 그 churn).
        String shape = String.valueOf(elemental2.core.Global.JSON.stringify(m));
        if (shape.equals(drawnShape) && root.childElementCount > 0) return;
        drawnShape = shape;

        HTMLElement box = cell("meetbox", null);
        box.append(head(tr("meet.title"), back()));
        HTMLElement headBox = cell("meethead", null);
        HTMLElement topic = el("h3");
        topic.className = "meettopic";
        topic.textContent = str(m, "topic");
        headBox.append(topic, cell("meetmeta", where(m)));

        List<JsPropertyMap<Object>> speakers = speakers(m);
        boolean getting = !bool(m, "opened") && !bool(m, "closed");
        int set = 0;
        for (JsPropertyMap<Object> sp : speakers) if (bool(sp, "ready") || !str(sp, "trouble").isEmpty()) set++;
        if (getting) {
            headBox.append(cell("meetgetting",
                    tr("meet.getting", "n", String.valueOf(set), "of", String.valueOf(speakers.size()))));
        }
        String holder = str(m, "holder");
        boolean composing = !holder.isEmpty() && named(speakers, holder) != null;
        boolean heldByYou = !holder.isEmpty() && !composing;
        if (getting || bool(m, "collecting") || (!bool(m, "closed") && !composing && !heldByYou)) {
            HTMLElement bar = el("md-linear-progress");
            bar.className = "meetbar-progress";
            if (getting && !speakers.isEmpty()) {
                Js.asPropertyMap(bar).set("value", (double) set / speakers.size());
            } else {
                Js.asPropertyMap(bar).set("indeterminate", true);
            }
            bar.setAttribute("aria-label", !bool(m, "opened") ? tr("meet.getting_ready")
                    : bool(m, "collecting") ? tr("meet.collecting")
                    : tr("meet.waiting_on", "who", !holder.isEmpty() ? holder
                            : nextName(speakers).isEmpty() ? tr("meet.somebody") : nextName(speakers)));
            headBox.append(bar);
        }
        if (!str(m, "trouble").isEmpty()) {
            headBox.append(cell("meettrouble", tr("meet.trouble", "why", str(m, "trouble"))));
        }
        headBox.append(roster(m, speakers));
        nowBox = nowPanel(m, speakers);
        headBox.append(nowBox);
        box.append(headBox);
        box.append(said(m, speakers));
        sayField = null;
        if (!bool(m, "closed")) box.append(sayBox(m));
        if (bool(m, "closed") && len(m, "tasks") > 0) box.append(conclusions(m));
        if (bool(m, "closed")) box.append(reopenBox(m));
        root.replaceChildren(box);
        sayRefusal();
    }

    /** 명단 — 색으로 갈리고, 지금 쥔 이는 선택되어 있다. 누르면 그 이를 지명한다. */
    private HTMLElement roster(JsPropertyMap<Object> m, List<JsPropertyMap<Object>> speakers) {
        HTMLElement box = el("md-chip-set");
        box.className = "meetroster";
        Map<String, String> tints = tints(m);
        boolean closed = bool(m, "closed"), opened = bool(m, "opened");
        String holder = str(m, "holder");
        JsArrayLike<Object> all = Js.uncheckedCast(m.get("speakers"));
        for (int i = 0; all != null && i < all.getLength(); i++) {
            JsPropertyMap<Object> s = Js.uncheckedCast(all.getAt(i));
            String name = str(s, "name");
            boolean person = bool(s, "person");
            boolean holding = holder.equals(name);
            boolean waiting = !opened && !person;
            boolean ready = bool(s, "ready");
            String trouble = str(s, "trouble");
            HTMLElement c = el("md-filter-chip");
            c.className = "meetsp " + orEmpty(tints.get(name))
                    + (holding ? " holding" : "")
                    + (bool(s, "next") && !holding ? " next" : "")
                    + (person ? " person" : "")
                    + (num(s, "passes") >= 2 ? " resting" : "")
                    + (waiting && !ready && trouble.isEmpty() ? " getting" : "")
                    + (waiting && ready ? " set" : "")
                    + (!trouble.isEmpty() ? " lost" : "");
            c.setAttribute("label", name);
            Js.asPropertyMap(c).set("selected", holding);
            String what = !trouble.isEmpty() ? trouble
                    : waiting && !ready ? tr("meet.getting_ready")
                    : waiting ? tr("meet.ready")
                    : holding ? tr("meet.holding")
                    : bool(s, "next") ? tr("meet.next")
                    : num(s, "passes") >= 2 ? tr("meet.resting")
                    : person ? tr("meet.you") : "";
            Tips.on(c, what.isEmpty() ? tr("meet.call", "who", name) : name + " — " + what);
            c.setAttribute("aria-label", what.isEmpty() ? name : name + " — " + what);
            if (closed) {
                c.setAttribute("disabled", "");
            } else {
                c.addEventListener("click", evt -> {
                    // 고른 것은 하나뿐이다 — 컴포넌트가 제 선택을 되돌리기 전에 한 번 더 못박는다.
                    elemental2.dom.NodeList<elemental2.dom.Element> chips =
                            box.querySelectorAll("md-filter-chip");
                    for (int j = 0; j < chips.getLength(); j++) {
                        Js.asPropertyMap(chips.getAt(j)).set("selected", chips.getAt(j) == c);
                    }
                    store.call(name);
                });
            }
            box.append(c);
        }
        return box;
    }

    /** 오간 말 — 바퀴마다 줄이 서고, 말한 이의 색이 붙는다. 넘긴 차례는 넘겼다고 적는다. */
    private HTMLElement said(JsPropertyMap<Object> m, List<JsPropertyMap<Object>> speakers) {
        HTMLElement box = cell("meetsaid", null);
        Map<String, String> tints = tints(m);
        Map<String, String> rooms = new LinkedHashMap<>(), sockets = new LinkedHashMap<>();
        JsArrayLike<Object> all = Js.uncheckedCast(m.get("speakers"));
        for (int i = 0; all != null && i < all.getLength(); i++) {
            JsPropertyMap<Object> s = Js.uncheckedCast(all.getAt(i));
            rooms.put(str(s, "name"), str(s, "room"));
            sockets.put(str(s, "name"), str(s, "socket"));
        }
        JsArrayLike<Object> said = Js.uncheckedCast(m.get("said"));
        int round = 0;
        Map<String, Integer> turn = new LinkedHashMap<>();
        for (int i = 0; said != null && i < said.getLength(); i++) {
            JsPropertyMap<Object> u = Js.uncheckedCast(said.getAt(i));
            int r = num(u, "round");
            if (r != round) {
                round = r;
                box.append(cell("meetlap", tr("meet.lap", "n", String.valueOf(round))));
            }
            String who = str(u, "who");
            boolean pass = bool(u, "pass");
            HTMLElement line = cell("meetline " + orElse(tints.get(who), "you") + (pass ? " passed" : ""), null);
            line.append(cell("meetwho2", who));
            turn.merge(who, 1, Integer::sum);
            if (pass) {
                line.append(cell("meettext", str(u, "text").isEmpty() ? tr("meet.passed")
                        : tr("meet.passed_why", "why", str(u, "text"))));
            } else {
                // 회의에서 오간 말도 전사와 같은 규칙으로 그린다 — 여기 도착하는 것은 모델이
                // 쓴 글이고, `txt` 클래스가 이 페이지의 마크다운 스타일을 이미 입고 있다.
                line.append(dev.sayaya.magi.bridge.Markdown.into(cell("meettext txt", null), str(u, "text")));
            }
            String room = orEmpty(rooms.get(who)), socket = orEmpty(sockets.get(who));
            if (!room.isEmpty() && !socket.isEmpty()) {
                line.append(workingBox(who, socket, room, turn.get(who) - 1));
            }
            box.append(line);
        }
        if (said == null || said.getLength() == 0) box.append(cell("meetwait", tr("meet.waiting")));
        return box;
    }

    /**
     * 지금 하고 있는 것 — 주제 아래에, 접지 않고, 계속 흐른다.
     *
     * 발언 줄에 붙는 {@link #workingBox}와 반대 방향이다: 저쪽은 <b>지나간</b> 한 마디가 어떻게
     * 나왔는지를 눌러서 펴는 것이고, 이쪽은 <b>아직 오지 않은</b> 한 마디가 지금 어디까지 왔는지다.
     * 회의의 대부분은 아무 줄도 늘지 않는 시간이라, 이 판이 없으면 느린 모델과 죽은 화면이
     * 화면에서 같은 모양이다.
     *
     * 누구 것을 그리는지는 {@link Rooms#workingNow}가 정한다 — 준비 중에는 전원, 열린 방에서는
     * 바닥을 쥔 하나. 그 규칙은 브라우저 없이 재진다.
     */
    private HTMLElement nowPanel(JsPropertyMap<Object> m, List<JsPropertyMap<Object>> speakers) {
        HTMLElement box = cell("meetnow", null);
        int n = speakers.size();
        String[] names = new String[n];
        boolean[] person = new boolean[n], ready = new boolean[n];
        for (int i = 0; i < n; i++) {
            JsPropertyMap<Object> sp = speakers.get(i);
            names[i] = str(sp, "name");
            person[i] = bool(sp, "person");
            ready[i] = bool(sp, "ready");
        }
        String[] busy = Rooms.workingNow(names, person, ready, str(m, "holder"),
                bool(m, "opened"), bool(m, "closed"));
        if (busy.length == 0) {
            box.setAttribute("hidden", "");
            return box;
        }
        Map<String, String> tints = tints(m);
        for (String who : busy) box.append(nowOne(who, orElse(tints.get(who), "sp0")));
        return box;
    }

    /** 한 사람의 판. 아직 아무것도 안 했으면 그렇게 적는다 — 빈 상자는 고장처럼 읽힌다. */
    private HTMLElement nowOne(String who, String tint) {
        HTMLElement one = cell("meetnowone " + tint, null);
        one.append(cell("meetnowwho", tr("meet.doing_now", "who", who)));
        HTMLElement rows = cell("meetnowrows", null);
        rows.setAttribute("data-who", who);
        JsArrayLike<Object> list = Js.uncheckedCast(store.liveOf(who));
        int drawn = 0;
        for (int i = 0; list != null && i < list.getLength(); i++) {
            JsPropertyMap<Object> r = Js.uncheckedCast(list.getAt(i));
            // 결론은 회의 줄에 선다 — workingBox와 같은 규칙이다. 회의가 던진 질문(user)도
            // 뺀다: 그것은 주제 바로 위에 이미 적혀 있다.
            String role = str(r, "who");
            if ("assistant".equals(role) || "user".equals(role)) continue;
            HTMLElement line = cell("row " + role, null);
            line.append(cell("who", role));
            line.append(cell("txt", oneLine(r)));
            rows.append(line);
            drawn++;
        }
        if (drawn == 0) rows.append(cell("dnote", tr("meet.thinking")));
        one.append(rows);
        return one;
    }

    /**
     * 프레임이 왔다 — 회의 본문은 그대로 두고 이 판만 갈아 끼운다.
     *
     * 판을 store.drawn()에 태우지 않는 이유가 여기 있다: 그 흐름은 방 전체를 다시 짓고, 그러면
     * 사람이 쓰던 칸과 열어 둔 접기가 프레임마다 날아간다. 아직 판이 서지 않았으면(방을 그리기
     * 전) 아무것도 하지 않는다 — 다음 render가 버퍼에서 그린다.
     */
    private void paintNow() {
        if (nowBox == null || !nowBox.isConnected) return;
        JsPropertyMap<Object> m = Js.uncheckedCast(store.one());
        if (m == null) return;
        HTMLElement fresh = nowPanel(m, speakers(m));
        nowBox.replaceWith(fresh);
        nowBox = fresh;
        // 새 줄은 아래에 붙는다 — 따라가지 않으면 판이 자란 만큼 사람이 뒤처진다.
        elemental2.dom.NodeList<elemental2.dom.Element> rows = fresh.querySelectorAll(".meetnowrows");
        for (int i = 0; i < rows.length; i++) {
            HTMLElement one = Js.uncheckedCast(rows.getAt(i));
            one.scrollTop = one.scrollHeight;
        }
    }

    /**
     * 그 한 마디를 하는 동안 그 컴패니언이 무엇을 했는가 — 눌러야 열린다.
     *
     * 회의의 한 줄은 결론이고, 그 결론에 이르기까지의 도구질은 그 컴패니언의 제 방에 있다.
     * 늘 펼쳐 두면 회의가 전사 열 벌이 된다.
     */
    private HTMLElement workingBox(String who, String socket, String room, int nth) {
        HTMLElement box = cell("meetwork", null);
        HTMLElement rows = cell("meetworkrows", null);
        rows.setAttribute("hidden", "");
        HTMLElement b = el("md-text-button");
        b.className = "meetworkgo";
        b.append(Icons.shape("#i-sl-chevron-down", "mk"),
                DomGlobal.document.createTextNode(" " + tr("meet.working")));
        b.addEventListener("click", evt -> {
            if (!rows.hasAttribute("hidden")) { rows.setAttribute("hidden", ""); return; }
            if (rows.childElementCount > 0) { rows.removeAttribute("hidden"); return; }
            store.rowsOf(socket, room, got -> {
                rows.replaceChildren();
                JsArrayLike<Object> list = Js.uncheckedCast(got);
                boolean[] isUser = new boolean[list == null ? 0 : list.getLength()];
                for (int i = 0; i < isUser.length; i++) {
                    isUser[i] = "user".equals(str(Js.uncheckedCast(list.getAt(i)), "who"));
                }
                int[] span = Rooms.turnSpan(isUser, nth);
                int drawn = 0;
                for (int i = span[0]; i < span[1]; i++) {
                    JsPropertyMap<Object> r = Js.uncheckedCast(list.getAt(i));
                    if ("assistant".equals(str(r, "who"))) continue;   // 결론은 이미 회의 줄에 있다
                    HTMLElement line = cell("row " + str(r, "who"), null);
                    line.append(cell("who", str(r, "who")));
                    line.append(cell("txt", oneLine(r)));
                    rows.append(line);
                    drawn++;
                }
                if (drawn == 0) rows.append(cell("dnote", tr("meet.working_gone")));
                rows.removeAttribute("hidden");
            });
        });
        box.append(b, rows);
        return box;
    }

    /** 한 마디 — 쓰는 동안 바닥을 쥔다(그래야 그 사이 아무도 끼어들지 않는다). */
    private HTMLElement sayBox(JsPropertyMap<Object> m) {
        HTMLElement box = cell("meetsay", null);
        HTMLElement f = el("md-outlined-text-field");
        f.id = "meetSay";
        f.setAttribute("label", tr("meet.say"));
        f.setAttribute("type", "textarea");
        f.setAttribute("rows", "2");
        value(f, store.saying());
        sayField = f;
        f.addEventListener("input", evt -> {
            store.saying(value(f));
            store.hold(!value(f).trim().isEmpty());
        });
        f.addEventListener("blur", evt -> { if (value(f).trim().isEmpty()) store.hold(false); });
        HTMLElement send = el("md-filled-button");
        send.append(Icons.shape("#i-sl-paper-plane-top", "mk"),
                DomGlobal.document.createTextNode(" " + tr("meet.send")));
        send.addEventListener("click", evt -> {
            String text = value(f).trim();
            if (text.isEmpty()) return;
            store.say(text, why -> note("say", why));
            value(f, "");
        });
        HTMLElement leave = el("md-text-button");
        leave.append(Icons.shape("#i-sl-chevron-left", "mk"),
                DomGlobal.document.createTextNode(" " + tr("meet.leave")));
        Tips.on(leave, tr("meet.leave_why"));
        leave.addEventListener("click", evt -> GoSharing.viewWith("meet", "m", ""));
        HTMLElement stop = el("md-text-button");
        stop.append(Icons.shape("#i-sl-flag-checkered", "mk"),
                DomGlobal.document.createTextNode(" " + tr("meet.wrap")));
        // 사유는 <b>이 상자</b>에 선다 — 끝내기 단추가 여기 있고, 바로 위의 보내기가 거절당할
        // 때 쓰는 자리와 같다. 방 목록 화면의 `.meetnote`는 남의 판이다.
        stop.addEventListener("click", evt -> whileItRuns(stop, () -> store.close(why -> note("say", why))));
        box.append(f, send, leave, stop);
        return box;
    }

    /** 결론 — 누구에게 무엇이 남았나. 그리고 그것을 그 컴패니언에게 건네는 문. */
    private HTMLElement conclusions(JsPropertyMap<Object> m) {
        HTMLElement box = cell("meettasks", null);
        box.append(head(tr("meet.tasks"), null));
        JsArrayLike<Object> tasks = Js.uncheckedCast(m.get("tasks"));
        for (int i = 0; tasks != null && i < tasks.getLength(); i++) {
            JsPropertyMap<Object> t = Js.uncheckedCast(tasks.getAt(i));
            String who = str(t, "who"), what = str(t, "what");
            HTMLElement row = cell("meettask" + (what.isEmpty() ? " nothing" : ""), null);
            row.append(cell("meettaskwho", who));
            row.append(what.isEmpty() ? cell("meettaskwhat", tr("meet.task_none"))
                    : dev.sayaya.magi.bridge.Markdown.into(cell("meettaskwhat txt", null), what));
            if (!what.isEmpty()) {
                if (store.handedTo(who)) {
                    row.append(sent(m, who));
                } else {
                    HTMLElement go = el("md-text-button");
                    go.append(Icons.shape("#i-sl-paper-plane-top", "mk"),
                            DomGlobal.document.createTextNode(" " + tr("meet.hand")));
                    // 건넨 뒤 그 자리에서 바뀐다 — 방의 내용은 그대로라 다시 그릴 일이 없고,
                    // 그리는 쪽이 아니라 누른 자리가 답을 보이는 것이 옳다(운영도 replaceWith).
                    go.addEventListener("click", evt -> whileItRuns(go,
                            () -> store.hand(who, why -> {
                                note("tasks", why);
                                if (why == null || why.isEmpty()) go.replaceWith(sent(m, who));
                            })));
                    row.append(go);
                }
            }
            box.append(row);
        }
        return box;
    }

    /** 건넸다는 표시와, 그리로 가는 길 — 보냈다는 말만 하고 어디로 갔는지 안 알려주지 않는다. */
    private HTMLElement sent(JsPropertyMap<Object> m, String who) {
        HTMLElement box = cell("meetsent", null);
        box.append(cell("sentsaid", tr("meet.handed")));
        // 어디로 갔는지는 그 방의 명단이 안다 — 보냈다고만 말하고 길을 안 알려주지 않는다.
        JsArrayLike<Object> all = Js.uncheckedCast(m.get("speakers"));
        for (int i = 0; all != null && i < all.getLength(); i++) {
            JsPropertyMap<Object> sp = Js.uncheckedCast(all.getAt(i));
            if (!who.equals(str(sp, "name")) || bool(sp, "person") || str(sp, "socket").isEmpty()) continue;
            HTMLElement go = el("a");
            go.className = "sentgo";
            go.textContent = tr("meet.go_there");
            final String socket = str(sp, "socket");
            go.setAttribute("href", "?d=" + socket);
            go.setAttribute("aria-label", tr("meet.go_there_named", "name", who));
            go.addEventListener("click", evt -> { evt.preventDefault(); GoSharing.go(socket, null); });
            box.append(go);
            break;
        }
        return box;
    }

    /** 다시 열기 — 무엇이 남았는지 적어 넣고 그 방을 다시 돌린다. */
    private HTMLElement reopenBox(JsPropertyMap<Object> m) {
        HTMLElement box = cell("meetsay", null);
        HTMLElement f = el("md-outlined-text-field");
        f.setAttribute("label", tr("meet.reopen_why"));
        f.setAttribute("type", "textarea");
        f.setAttribute("rows", "2");
        HTMLElement go = el("md-filled-tonal-button");
        go.append(Icons.shape("#i-sl-play", "mk"),
                DomGlobal.document.createTextNode(" " + tr("meet.reopen")));
        go.addEventListener("click", evt -> whileItRuns(go, () ->
                store.reopen(value(f), why -> note("say", why))));
        box.append(f, go);
        return box;
    }

    // ── 조각들 ────────────────────────────────────────────────────────────────

    /** 어느 단계인가 — 규칙은 domain/Rooms의 것이고, 여기서는 그 말을 고른다. */
    private String where(JsPropertyMap<Object> m) {
        String key = Rooms.stageKey(bool(m, "closed"), bool(m, "held"), bool(m, "collecting"),
                bool(m, "spent"), len(m, "tasks"), num(m, "round"), num(m, "max"));
        if (key.isEmpty()) return "";
        if ("meet.round".equals(key)) {
            return tr(key, "n", String.valueOf(num(m, "round")), "of", String.valueOf(num(m, "max")));
        }
        return tr(key);
    }

    private Map<String, String> tints(JsPropertyMap<Object> m) {
        Map<String, String> by = new LinkedHashMap<>();
        JsArrayLike<Object> all = Js.uncheckedCast(m.get("speakers"));
        int i = 0;
        for (int k = 0; all != null && k < all.getLength(); k++) {
            JsPropertyMap<Object> s = Js.uncheckedCast(all.getAt(k));
            if (bool(s, "person")) continue;
            by.put(str(s, "name"), Rooms.tint(i++));
        }
        return by;
    }

    private List<JsPropertyMap<Object>> speakers(JsPropertyMap<Object> m) {
        List<JsPropertyMap<Object>> out = new ArrayList<>();
        JsArrayLike<Object> all = Js.uncheckedCast(m.get("speakers"));
        for (int i = 0; all != null && i < all.getLength(); i++) {
            JsPropertyMap<Object> s = Js.uncheckedCast(all.getAt(i));
            if (!bool(s, "person")) out.add(s);
        }
        return out;
    }

    private static JsPropertyMap<Object> named(List<JsPropertyMap<Object>> list, String name) {
        for (JsPropertyMap<Object> s : list) if (name.equals(str(s, "name"))) return s;
        return null;
    }

    private static String nextName(List<JsPropertyMap<Object>> list) {
        for (JsPropertyMap<Object> s : list) if (bool(s, "next")) return str(s, "name");
        return "";
    }

    private static String oneLine(JsPropertyMap<Object> row) {
        String text = str(row, "text");
        if (text.isEmpty()) text = str(row, "tool");
        if (text.isEmpty()) text = str(row, "out");
        int nl = text.indexOf('\n');
        if (nl >= 0) text = text.substring(0, nl);
        return text.length() > 160 ? text.substring(0, 159) + "…" : text;
    }

    /** 이 화면의 머리 — 운영의 sectionHead와 같은 마크업(h2 + 곁의 문). */
    private HTMLElement head(String words, HTMLElement beside) {
        HTMLElement box = cell("sectionhead", null);
        HTMLElement h = el("h2");
        h.textContent = words;
        box.append(h);
        if (beside != null) box.append(beside);
        return box;
    }

    /** 명단으로 — 회의는 플릿에 대한 일이라, 그 곁에 돌아갈 길을 둔다(운영의 그 자리). */
    private HTMLElement toFleet() {
        HTMLElement b = el("md-text-button");
        b.textContent = tr("nav.companions");
        Icons.mark(b, "#i-sl-chevron-left");
        b.addEventListener("click", evt -> GoSharing.view("fleet"));
        return b;
    }

    private HTMLElement back() {
        HTMLElement b = el("md-text-button");
        b.append(Icons.shape("#i-sl-chevron-left", "mk"),
                DomGlobal.document.createTextNode(" " + tr("meet.back")));
        b.addEventListener("click", evt -> GoSharing.viewWith("meet", "m", ""));
        return b;
    }

    /** 열 수 있나 — 버튼의 상태와 그 아래 한 줄이 같은 사실에서 나온다. */
    private void arm(HTMLElement box) {
        HTMLElement scope = box == null ? root : box;
        elemental2.dom.Element go = scope.querySelector(".meetgo");
        elemental2.dom.Element note = scope.querySelector(".meetnote");
        boolean ready = Rooms.canConvene(store.topic(), store.picked().size());
        if (go != null) {
            if (ready) go.removeAttribute("disabled"); else go.setAttribute("disabled", "");
        }
        // 이 줄은 <b>안내만</b> 싣는다 — 아직 열 수 없다는 말. 서버가 거절하며 한 말은 제
        // 줄(`.refused`)에 서므로, 둘이 한 자리를 다투지 않는다. 한때는 여기 `data-fixed`를
        // 찍어 안내가 사유를 덮지 못하게 막았는데, 그 표시를 지우는 곳이 없어 안내가 영영
        // 돌아오지 못했다 — 두 말을 한 줄에 태운 것이 애초의 잘못이었다.
        if (note != null) {
            String key = Rooms.blockedKey(store.topic(), store.picked().size(), callableCount());
            note.textContent = ready || key.isEmpty() ? "" : tr(key);
        }
    }

    private int callableCount() {
        JsArrayLike<Object> list = Js.uncheckedCast(store.fleet());
        int n = 0;
        for (int i = 0; list != null && i < list.getLength(); i++) {
            JsPropertyMap<Object> a = Js.uncheckedCast(list.getAt(i));
            if (!bool(a, "elsewhere") && str(a, "peer").isEmpty()) n++;
        }
        return n;
    }

    /**
     * 거절의 사유는 사람 눈에 보이는 자리에 — 조용히 삼키면 눌린 줄 알고 기다린다. 그리고
     * 그 말은 <b>이 화면이</b> 쥔다, 노드가 아니라: 이 판은 두 초마다 폴이 다시 세우므로
     * 노드에 적어 둔 말은 데이터가 조금 달라지는 순간 함께 헐린다. 예전엔 그 노드에
     * `data-fixed`를 찍어 안내가 덮지 못하게 막았는데, 그 표시를 <b>지우는 곳이 없어</b>
     * 사유의 수명이 남의 사정으로 정해졌다 — 아무 일 없으면 영영 서서 안내를 막고, 무언가
     * 바뀌면 두 초 만에 사라졌다.
     *
     * 빈 말은 걷는다는 뜻이다: 통한 누름이 앞서 선 거절을 지운다.
     */
    private void note(String at, String why) {
        refusal = why == null ? "" : why.length() > 120 ? why.substring(0, 120) : why;
        refusalAt = refusal.isEmpty() ? "" : at;
        refusalRoom = refusal.isEmpty() ? "" : orEmpty(store.room());
        sayRefusal();
    }

    /** 사유가 설 상자 — 누른 단추가 있던 그 상자다(여는 화면은 폼, 방 안은 말하는 칸과 결론). */
    private static String placeOf(String at) {
        return "say".equals(at) ? ".meetsay" : "tasks".equals(at) ? ".meettasks" : ".meetbox";
    }

    /** 판을 다시 세울 때마다 부른다 — 사유는 새 판에서도 제자리에 다시 선다. */
    private void sayRefusal() {
        // 방을 옮기면 남의 말이 된다 — 그 자리에서 눌러 들은 말이라 따라다니지 않는다.
        if (!refusal.isEmpty() && !refusalRoom.equals(orEmpty(store.room()))) {
            refusal = ""; refusalAt = ""; refusalRoom = "";
        }
        elemental2.dom.Element had = root.querySelector(".refused");
        elemental2.dom.Element at = refusal.isEmpty() ? null : root.querySelector(placeOf(refusalAt));
        if (at == null) { if (had != null) had.remove(); return; }
        HTMLElement box = Js.uncheckedCast(at);
        HTMLElement line = had != null ? Js.uncheckedCast(had) : cell("refused", null);
        line.setAttribute("role", "alert");
        // 같은 말이면 손대지 않는다 — role=alert는 이 줄이 바뀔 때마다 읽는 기계가 다시 읽어
        // 주는 자리인데, 이 판은 두 초마다 다시 선다.
        if (!refusal.equals(line.textContent)) line.textContent = refusal;
        if (line.parentNode != box) {
            // 안내 줄 바로 아래 — 그 위의 단추를 눌러 들은 말이라, 방 목록보다 앞에 선다.
            elemental2.dom.Element guide = box.querySelector(".meetnote");
            if (guide != null && guide.parentNode == box) box.insertBefore(line, guide.nextSibling);
            else box.append(line);
        }
    }

    /** 도는 동안은 눌리지 않는다 — 두 번 눌러 두 방이 열리지 않게. */
    private static void whileItRuns(HTMLElement btn, Runnable run) {
        String was = btn.textContent;
        btn.setAttribute("disabled", "");
        btn.textContent = tr("action.working");
        run.run();
        DomGlobal.setTimeout(args -> {
            btn.removeAttribute("disabled");
            btn.textContent = was;
        }, 600);
    }

    private static String orEmpty(String s) { return s == null ? "" : s; }

    private static String orElse(String s, String other) { return s == null || s.isEmpty() ? other : s; }

    private static HTMLElement el(String tag) { return Js.uncheckedCast(DomGlobal.document.createElement(tag)); }

    static HTMLElement cell(String cls, String text) {
        HTMLElement e = el("div");
        e.className = cls;
        if (text != null) e.textContent = text;
        return e;
    }

    private static String str(JsPropertyMap<Object> m, String k) {
        Object v = m == null ? null : m.get(k);
        return v == null ? "" : String.valueOf(v);
    }

    private static boolean bool(JsPropertyMap<Object> m, String k) {
        return m != null && Js.isTruthy(m.get(k));
    }

    private static int num(JsPropertyMap<Object> m, String k) {
        Object v = m == null ? null : m.get(k);
        return v == null ? 0 : (int) Js.coerceToDouble(v);
    }

    private static int len(JsPropertyMap<Object> m, String k) {
        JsArrayLike<Object> a = Js.uncheckedCast(m.get(k));
        return a == null ? 0 : a.getLength();
    }

    private static String joinBits(String a, String b) {
        if (a.isEmpty()) return b;
        if (b.isEmpty()) return a;
        return a + " · " + b;
    }

    private static String value(HTMLElement f) {
        Object v = Js.asPropertyMap(f).get("value");
        return v == null ? "" : String.valueOf(v);
    }

    private static void value(HTMLElement f, String v) { Js.asPropertyMap(f).set("value", v); }

    /** 그 상자에 쓰는 중인가 — 폴이 사람의 문장을 지우지 않게. */
    private static boolean typingIn(HTMLElement f) {
        if (f == null || !f.isConnected) return false;
        elemental2.dom.Element now = DomGlobal.document.activeElement;
        return now == f || (now != null && f.contains(now));
    }
}
