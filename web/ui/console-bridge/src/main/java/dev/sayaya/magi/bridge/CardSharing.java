package dev.sayaya.magi.bridge;

import elemental2.dom.DomGlobal;
import jsinterop.annotations.JsFunction;
import jsinterop.base.Js;
import jsinterop.base.JsPropertyMap;

/**
 * 가운데 기둥의 <b>카드 자리</b> — 사실판 옆에 나란히 서는 것들.
 *
 * 왜 자식이 제자리에 그리지 않고 등록하나: 그 자리에 무엇이 서 있는지 고르는 것은 탭 줄이고,
 * 탭 줄에는 부모의 사실판도 함께 선다. 한 자리에 둘이 그리면 누가 지금 보이는지 아무도 모른다.
 * 그래서 자식은 "이런 카드가 있다"고 말하고, 부모가 그 줄을 그리고 하나를 고른다.
 *
 * 열린 파일이 하나도 없으면 줄은 서지 않는다 — 고를 것이 하나뿐인 탭 줄은 furniture다.
 */
public final class CardSharing {
    private static final String KEY = "__magi_cards";
    private static final String OBS = "__magi_cards_obs";
    private static final String ALONE = "__magi_cards_alone";
    private static final String BACK = "__magi_cards_back";
    private static final String SHOWS = "__magi_cards_showing";
    private static final String SHOWS_OBS = "__magi_cards_showing_obs";
    private static final String ASK = "__magi_cards_ask";
    private static final String ASK_OBS = "__magi_cards_ask_obs";

    private CardSharing() {}

    @JsFunction
    public interface Runner { void call(); }

    @JsFunction
    public interface Listener { void call(Object cards); }

    /**
     * 자식: 지금 열려 있는 카드 전부 — <b>노드의 배열</b>. 없으면 빈 배열(그때 줄이 걷힌다).
     *
     * 카드는 <b>Element이면서 닫힐 수 있는 것</b>이다. 규약은 노드 자신이 진다: <b>id</b>가 그
     * 카드가 무엇인지(경로 같은 것)이고, <b>title</b>이 탭에 적히는 짧은 이름이며, 얹어 둔
     * <b>close()</b>가 닫는 법이다(closable). 필드를 실은 객체를 주고받지 않는 이유가 이것이다:
     * 노드는 이미 이름과 신원을 담는 자리를 갖고 있고, 그 자리를 쓰면 새 계약이 늘지 않는다.
     */
    public static void provide(Object cards) {
        JsPropertyMap<Object> win = Js.asPropertyMap(DomGlobal.window);
        win.set(KEY, cards);
        elemental2.core.JsArray<Listener> ls = heard(OBS);
        for (int i = 0; i < ls.length; i++) ls.getAt(i).call(cards);
    }

    public static Object current() { return Js.asPropertyMap(DomGlobal.window).get(KEY); }

    /**
     * 부모: 바뀌면 다시 그린다 — <b>듣는 쪽은 여럿이다</b>.
     *
     * 한 자리에 하나만 두고 있었다. 지금은 CompanionElement 하나뿐이라 터지지 않지만, 카드
     * 줄을 듣고 싶은 둘째가 생기는 순간 첫째가 조용히 지워진다 — {@link #onShowing}이 같은
     * 이유로 이미 목록이다.
     */
    public static void onChange(Listener l) { heard(OBS).push(l); }

    /**
     * 부모: 이 카드가 <b>혼자 서 있는가</b>. 폰에서는 기둥이 하나라, 카드가 트리를 대신해 그
     * 자리에 선다 — 그러면 돌아갈 문이 필요하고, 그 문은 카드의 머리 줄에 있어야 한다(운영도
     * 파일 바 안이다). 자리 배치는 부모만 알고, 머리 줄은 자식만 안다: 그래서 사실 하나를
     * 건네고 문 여는 일은 부모가 받는다.
     */
    public static void stand(boolean alone, Runner back) {
        JsPropertyMap<Object> win = Js.asPropertyMap(DomGlobal.window);
        win.set(ALONE, alone);
        win.set(BACK, back);
    }

    /** 자식: 지금 이 카드가 혼자 선 자리인가(부모가 답한다; 아무도 안 답하면 아니다). */
    public static boolean alone() {
        Object v = Js.asPropertyMap(DomGlobal.window).get(ALONE);
        return v != null && Js.isTruthy(v);
    }

    /** 자식: 이 자리를 원래 내용에 돌려준다 — 무엇이 원래 내용인지는 부모가 안다. */
    public static void toList() {
        Object b = Js.asPropertyMap(DomGlobal.window).get(BACK);
        if (b != null) Js.<Runner>cast(b).call();
    }

    /**
     * 카드 하나 — 자식이 <b>만든 노드</b>를 그대로 건넨다.
     *
     * 그릴 콜백이 아니라 노드인 이유: 카드가 여럿이면 부모는 그중 하나를 세우고 나머지를
     * 떼어 두기만 하면 되고, 다시 그릴 때가 언제인지는 자식이 안다(제 스토어가 바뀔 때).
     * 콜백이면 "언제 다시 불러야 하는가"라는 규약이 하나 더 생긴다.
     */
    /** 자식: 이 노드를 닫을 수 있는 것으로 만든다 — 무엇을 지울지는 그것을 만든 쪽만 안다. */
    public static elemental2.dom.Element closable(elemental2.dom.Element card, Runner close) {
        Js.asPropertyMap(card).set("close", close);
        return card;
    }

    /**
     * 자식: <b>이것을 보여 달라</b>. 트리에서 이미 열어 둔 파일을 다시 누르는 것이 그 말이다 —
     * 새 카드가 아니니 부모의 "방금 열린 것" 규칙에 걸리지 않고, 이 문이 없으면 눌러도 아무
     * 일이 없다.
     *
     * <b>부탁과 보고를 한 자리에 두지 않는 이유</b>: 한때 이 둘이 {@link #showing} 하나였다.
     * 그러면 부모가 적은 보고를 다음 그리기가 부탁으로 되읽을 수 있고, 실제로 그랬다 —
     * 카드가 하나도 없는 갈래에서 부모가 "facts"를 적어 두면, 방금 닫은 카드를 다시 열었을 때
     * 그 "facts"가 부탁 자리에 앉아 이겼다(눌러서 연 카드 대신 사실판이 섰다). 자리를 가르면
     * 그 일은 <b>표현할 수 없게</b> 된다: 부모는 부탁을 적을 수 없고, 제가 적은 보고를 부탁으로
     * 읽을 자리도 없다.
     *
     * 같은 값이어도 늘 알린다 — 부탁은 상태가 아니라 <b>한 번 일어난 일</b>이라, 같은 파일을
     * 두 번 누르는 것은 두 번의 부탁이다. 되풀이가 스택을 태우지 않는 것은 이 부탁이 {@link
     * #showing(String) 이뤄졌다는 보고}와 함께 지워지기 때문이다.
     */
    public static void ask(String key) {
        JsPropertyMap<Object> win = Js.asPropertyMap(DomGlobal.window);
        win.set(ASK, key);
        elemental2.core.JsArray<Runner> ls = listeners(ASK_OBS);
        for (int i = 0; i < ls.length; i++) ls.getAt(i).call();
    }

    /**
     * 부모: 누가 부탁한 것(없으면 빈 문자열).
     *
     * 읽는 것만으로는 지워지지 않는다 — <b>이뤄질 때</b> 지워진다({@link #showing(String)}).
     * 읽자마자 비우게 했더니, 아직 카드가 서지 않은 파일을 부탁한 경우 그 부탁이 카드가
     * 생기기 전에 증발했다(실측: 팔레트에서 안 열린 파일을 고르면 옆 파일이 그대로 서 있다 —
     * 본문이 도착해야 카드가 생기는데, 부탁은 그 전 그리기에서 이미 없어진다).
     */
    public static String asked() {
        Object v = Js.asPropertyMap(DomGlobal.window).get(ASK);
        return v == null ? "" : String.valueOf(v);
    }

    /**
     * 부모: 지금 <b>어느 카드가 보이는가</b>(사실판이면 "facts"). 자식의 목록은 그 표시를 따라야
     * 한다 — 파일 셋을 열어 두고 그중 하나를 보는 중인데 트리에서 셋 다 골라진 것처럼 보이면,
     * 지금 읽고 있는 것이 무엇인지 화면이 말하지 못한다.
     *
     * 이것은 <b>보고 전용</b>이다. 부탁은 {@link #ask}로 온다.
     */
    public static void showing(String key) {
        JsPropertyMap<Object> win = Js.asPropertyMap(DomGlobal.window);
        // 이뤄진 부탁은 여기서 지운다 — 아래 dedupe보다 먼저다(이미 그것을 보고 있었더라도
        // 부탁은 이뤄진 것이므로, 지우지 않으면 다음 그리기마다 같은 말을 되풀이한다).
        Object want = win.get(ASK);
        if (want != null && String.valueOf(want).equals(key)) win.set(ASK, null);
        Object had = win.get(SHOWS);
        // 바뀐 때만 알린다. 자식은 이 소식에 다시 그리고, 다시 그리면 제 카드를 다시 건네고,
        // 그러면 부모가 줄을 다시 그린다 — 같은 값에도 알리면 그 셋이 서로를 부르며 스택을
        // 태운다(실측: Maximum call stack size exceeded, 워크스페이스가 통째로 빈 화면).
        if (had != null && String.valueOf(had).equals(key)) return;
        win.set(SHOWS, key);
        elemental2.core.JsArray<Runner> ls = listeners(SHOWS_OBS);
        for (int i = 0; i < ls.length; i++) ls.getAt(i).call();
    }

    /**
     * 부모: 누가 무엇을 청하면 다시 그린다.
     *
     * 종도 값과 함께 갈라야 한다. 부탁이 보고의 종을 울리게 두었을 때, 부모는 그 종에 아예 걸려
     * 있지 않았고(부모가 거는 것은 {@link #onChange}) 부탁은 <b>자식이 제 부탁의 메아리를 듣고
     * 카드를 다시 건네 주기 때문에</b> 도착했다. 그 자식의 리스너는 부탁을 나르려고 등록된 것이
     * 아니므로, 그것이 "정말 달라졌을 때만 다시 그린다"로 똑똑해지는 날 부탁은 전달을 멈춘다.
     * 폰의 대화 탭에서는 이미 그렇다 — 왼쪽 판이 닫혀 있으면 자식의 render가 이르게 돌아간다.
     */
    public static void onAsk(Runner l) { listeners(ASK_OBS).push(l); }

    /** 자식: 지금 보이는 카드의 신원(없으면 빈 문자열) — 부모가 적은 <b>보고</b>다. */
    public static String showing() {
        Object v = Js.asPropertyMap(DomGlobal.window).get(SHOWS);
        return v == null ? "" : String.valueOf(v);
    }

    /**
     * 그것이 바뀌면 다시 그린다 — <b>듣는 쪽은 여럿이다</b>.
     *
     * 한 자리에 하나만 두고 있었다: 자식(트리)이 걸면 부모(탭 줄)의 것이 지워져, 트리에서 이미
     * 열린 파일을 다시 눌러도 그 탭이 서지 않았다(실측: 눌러도 아무 일이 없다). 창에 걸리는
     * 문은 여러 모듈이 함께 쓰는 문이라, 한 명만 들을 수 있으면 그것은 문이 아니다.
     */
    public static void onShowing(Runner l) { listeners(SHOWS_OBS).push(l); }

    /**
     * 창에 두는 목록은 <b>자바가 아니라 자바스크립트</b> 배열이다.
     *
     * 모듈마다 따로 컴파일되므로(페더레이션) 한 모듈이 만든 java.util.List를 다른 모듈이 제
     * 타입으로 캐스팅해 쓰면 없는 메서드를 부른다(실측: "ti(...).Ib is not a function" —
     * 파일을 누르는 순간 화면이 죽었다). 창을 건너는 것은 순수 JS 값이어야 한다.
     */
    private static elemental2.core.JsArray<Listener> heard(String key) {
        JsPropertyMap<Object> win = Js.asPropertyMap(DomGlobal.window);
        Object had = win.get(key);
        if (had != null) return Js.uncheckedCast(had);
        elemental2.core.JsArray<Listener> made = new elemental2.core.JsArray<>();
        win.set(key, made);
        return made;
    }

    private static elemental2.core.JsArray<Runner> listeners(String key) {
        JsPropertyMap<Object> win = Js.asPropertyMap(DomGlobal.window);
        Object had = win.get(key);
        if (had != null) return Js.uncheckedCast(had);
        elemental2.core.JsArray<Runner> made = new elemental2.core.JsArray<>();
        win.set(key, made);
        return made;
    }

    /** 부모: 이 카드를 닫는다. 닫는 법이 없는 카드는 닫히지 않는다(사실판이 그렇다). */
    public static void close(elemental2.dom.Element card) {
        Object c = card == null ? null : Js.asPropertyMap(card).get("close");
        if (c != null) Js.<Runner>cast(c).call();
    }

    /** 부모: 이 카드를 닫을 수 있는가 — 닫는 문(×)을 그릴지 정한다. */
    public static boolean closable(elemental2.dom.Element card) {
        return card != null && Js.asPropertyMap(card).get("close") != null;
    }
}
