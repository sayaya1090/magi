package dev.sayaya.magi.client.usecase;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.ArrayList;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Set;
import java.util.function.Consumer;

/**
 * 회의실의 상태 — 무엇을 보고 있고(목록인가 방인가), 무엇을 적는 중이고, 무엇을 골랐나.
 *
 * 폴이 화면을 다시 그려도 사람이 쓰던 것은 스토어가 들고 있다: 주제도 고른 이들도 하던
 * 말도. 운영이 그것을 전역 변수로 들고 있는 이유와 같다 — 두 초마다 다시 그리는 판에서
 * DOM에만 있던 값은 문장 한가운데서 사라진다.
 */
@Singleton
public class MeetingStore extends dev.sayaya.magi.bridge.Told {
    private final MeetingSource source;

    private String room = null;          // 주소가 대는 회의(?m=), 없으면 목록
    private Object one = null;           // 그 회의
    private boolean gone = false;        // 대는 방이 없다
    private Object rooms = null;         // 열린/끝난 회의들
    private Object fleet = null;         // 부를 수 있는 이들
    private String topic = "";           // 쓰던 주제
    private final Set<String> picked = new LinkedHashSet<>();
    private String saying = "";          // 방에서 쓰던 말
    private final Set<String> handed = new LinkedHashSet<>();   // 이미 건넨 결론

    @Inject
    public MeetingStore(MeetingSource source) { this.source = source; }



    /**
     * 이 화면이 그리는 것 — 어느 방을 보고 있고, 그 방과 목록이 무엇이며, 부를 수 있는 이들이
     * 누구인가. 쓰던 주제와 하던 말은 여기 없다: 그것은 사람의 손에 있는 것이라, 폴이 그것을
     * 이유로 판을 다시 세우면 문장 한가운데서 글자가 사라진다.
     */
    public dev.sayaya.rx.Observable<String> drawn() { return when(this::sig); }

    private String sig() {
        StringBuilder b = new StringBuilder(room == null ? "" : room).append('|').append(gone)
                .append('|').append(String.join(",", picked));
        b.append('|').append(one == null ? "" : elemental2.core.Global.JSON.stringify(one));
        b.append('|').append(rooms == null ? "" : elemental2.core.Global.JSON.stringify(rooms));
        // 명단에서 이 화면이 쓰는 것은 부를 수 있는 이들의 이름과 자리뿐이다.
        jsinterop.base.JsArrayLike<Object> all = jsinterop.base.Js.uncheckedCast(fleet);
        for (int i = 0; all != null && i < all.getLength(); i++) {
            jsinterop.base.JsPropertyMap<Object> a = jsinterop.base.Js.uncheckedCast(all.getAt(i));
            b.append(a.get("socket")).append(',').append(a.get("name")).append(',')
             .append(a.get("live")).append(',').append(a.get("team")).append(';');
        }
        return b.toString();
    }

    public String room() { return room; }
    public Object one() { return one; }
    public boolean gone() { return gone; }
    public Object rooms() { return rooms; }
    public Object fleet() { return fleet; }
    public String topic() { return topic; }
    public String saying() { return saying; }
    public Set<String> picked() { return picked; }
    public boolean handedTo(String who) { return handed.contains(room + "|" + who); }

    public void topic(String t) { topic = t == null ? "" : t; }
    public void saying(String t) { saying = t == null ? "" : t; }

    public void pick(String socket) {
        if (!picked.remove(socket)) picked.add(socket);
    }

    /** 명단에서 사라진 이의 선택은 함께 사라진다 — 안 그러면 죽은 소켓으로 회의를 연다. */
    public void keepOnly(Set<String> alive) { picked.retainAll(alive); }

    /** 주소가 바뀌었다 — 방을 떠나면 그 방의 것들도 잊는다. */
    public void aim(String id) {
        String want = id == null || id.isEmpty() ? null : id;
        if (eq(want, room)) return;
        room = want;
        one = null;
        gone = false;
        saying = "";
        // 방을 옮기면 발자국도 두고 간다 — 지난 회의의 「지금 하는 것」이 새 화면에 비치면
        // 그것은 지금이 아니다.
        forgetLive();
        dev.sayaya.magi.bridge.RoomSharing.aim(want);
        told();
        read();
    }

    /** 지금 보는 것을 다시 읽는다 — 폴이 부른다. */
    public void read() {
        if (room == null) {
            source.fleet(list -> { fleet = list; told(); });
            source.rooms(list -> { rooms = list; told(); });
            return;
        }
        source.room(room, m -> {
            gone = m == null;
            if (m != null) one = m;
            told();
        });
    }

    public void convene(Consumer<String> failed, Consumer<String> made) {
        source.convene(topic, picked.toArray(new String[0]), m -> {
            topic = "";
            picked.clear();
            made.accept(idOf(m));
        }, failed);
    }

    /** 사유는 <b>통했을 때도</b> 올려 보낸다(빈 말로) — 그래야 앞서 선 거절이 걷힌다. */
    public void say(String text, Consumer<String> said) {
        source.say(room, text, null, false, why -> {
            if (why != null && !why.isEmpty()) { said.accept(why); return; }
            said.accept("");
            saying = "";
            read();
        });
    }

    /** 바닥 잡기 — 쥐고 있는 동안엔 아무도 끼어들지 않는다. 같은 값을 다시 보내지 않는다. */
    private boolean holding = false;
    /** 마지막으로 바닥을 주장한 시각(ms) — 서버의 시계를 다시 감기 위해 든다. */
    private double heldAt = 0;

    public void hold(boolean on) {
        if (holding == on || room == null) return;
        holding = on;
        heldAt = now();
        source.say(room, null, null, on, why -> { });
    }

    /**
     * 쥐고 있는 바닥의 시계를 다시 감는다 — <b>작문 중에 밑에서 잠기지 않게</b>.
     *
     * 서버는 말 없이 잡고만 있는 바닥을 90초 뒤 놓는다(닫힌 탭이 방을 영영 얼리지 않게 하는
     * 안전장치다). 그런데 발언권을 먼저 잡고 나서 쓰는 방식에서는 <b>오래 생각하는 사람</b>이
     * 정확히 그 모양이 된다 — 2분을 고민하면 쓰던 상자가 잠긴다. 그래서 글자가 들어올 때마다
     * 시계를 다시 감되, 매 키 입력마다 보내지는 않는다(그건 방을 향한 잡음이다).
     */
    public void keepHold() {
        if (!holding || room == null || now() - heldAt < 40_000) return;
        heldAt = now();
        source.say(room, null, null, true, why -> { });
    }

    private static double now() { return elemental2.dom.DomGlobal.performance.now(); }

    public void call(String who) { source.say(room, null, who, false, why -> read()); }

    /**
     * 끝내기·다시 열기 — 거절당하면 <b>다시 읽지 않는다</b>. 이 화면의 사유는 저장소가 아니라
     * 눌린 그 상자가 쥐고 있어서(`.meetnote`), 다시 읽으면 그 상자를 헐어 사유가 함께 사라진다.
     * {@link #say}가 이미 그렇게 하고 있다 — 다른 화면들이 「사유를 쥐고 나서 다시 읽는」 것과
     * 반대로 보이지만, 규칙은 하나다: <b>사유를 세운 것을 다시 읽기가 지우게 두지 않는다.</b>
     */
    public void close(Consumer<String> said) {
        source.close(room, why -> {
            if (why != null && !why.isEmpty()) { said.accept(why); return; }
            said.accept("");
            read();
        });
    }

    public void reopen(String text, Consumer<String> said) {
        source.reopen(room, text, why -> {
            if (why != null && !why.isEmpty()) { said.accept(why); return; }
            said.accept("");
            read();
        });
    }

    /**
     * 결론 하나를 그 컴패니언에게 건넨다. 실패의 사유는 그대로 올려 보낸다 — 성공만 알리면
     * 누른 사람은 갔다고 믿는다. 성공은 <b>다시 그리지 않는다</b>: 방의 내용은 그대로이고,
     * 답은 누른 그 자리가 보인다.
     */
    public void hand(String who, Consumer<String> landed) {
        source.hand(room, who, why -> {
            if (why != null && !why.isEmpty()) { landed.accept(why); return; }
            handed.add(room + "|" + who);
            landed.accept("");
        });
    }

    public void rowsOf(String socket, String inRoom, Consumer<Object> cb) {
        source.roomRows(socket, inRoom, cb);
    }

    /**
     * 「지금 하는 것」 판이 그리는 것 — 참가자 이름 → 그 방의 현재 전량.
     *
     * <b>sig()에 넣지 않는다.</b> 화면은 그 시그니처가 바뀔 때 방을 통째로 다시 짓는데(그 가드가
     * 2초 폴의 churn을 막는 자리다), 여기 넣으면 방 프레임이 올 때마다 회의가 헐린다 — 열어 둔
     * 접기가 닫히고, 스크롤이 튀고, 사람이 쓰던 칸이 흔들린다. 그래서 이 버퍼는 흐름 밖에 서고
     * 판만 제 손으로 다시 그린다.
     *
     * 프레임은 델타가 아니라 그 사람의 전량이다(서버가 연결마다 seen을 새로 센다) — 그래서
     * 이어 붙이지 않고 갈아 끼운다.
     */
    private final java.util.LinkedHashMap<String, Object> live = new java.util.LinkedHashMap<>();

    /** 방 프레임 하나를 받는다. 그 사람의 판이 달라졌으면 참. */
    public boolean live(Object frame) {
        if (frame == null) return false;
        jsinterop.base.JsPropertyMap<Object> f = jsinterop.base.Js.asPropertyMap(frame);
        Object who = f.get("who");
        if (who == null) return false;
        String key = String.valueOf(who);
        Object rows = f.get("rows");
        Object had = live.get(key);
        String before = had == null ? "" : elemental2.core.Global.JSON.stringify(had);
        String after = rows == null ? "" : elemental2.core.Global.JSON.stringify(rows);
        if (before.equals(after)) return false;
        live.put(key, rows);
        return true;
    }

    /**
     * 이 화면을 떠났다 — 회선의 조준을 푼다.
     *
     * 방(room)은 그대로 둔다: 돌아왔을 때 같은 회의를 다시 그려야 하고, 조준은 마운트가
     * 다시 건다. 여기서 푸는 것은 <b>남의 자원</b>이다 — 아무도 안 보는 회의의 방을 데몬이
     * 계속 읽고 있을 이유가 없다.
     */
    public void leave() {
        if (room == null) return;
        dev.sayaya.magi.bridge.RoomSharing.aim(null);
    }

    /**
     * 돌아왔다 — 같은 방이어도 조준을 다시 건다.
     *
     * aim()은 방이 그대로면 일찍 반환하는데(그 가드가 폴의 churn을 막는다), 떠날 때 조준을
     * 풀어 두었으므로 그 길로 돌아오면 아무도 다시 걸어 주지 않는다. 셸 쪽이 같은 값이면
     * 무시하므로 무조건 걸어도 회선은 안 흔들린다.
     */
    public void rejoin() { dev.sayaya.magi.bridge.RoomSharing.aim(room); }

    /** 그 사람이 지금까지 한 일, 아직 아무것도 없으면 null. */
    public Object liveOf(String who) { return who == null ? null : live.get(who); }

    /** 방을 옮기면 발자국도 두고 간다 — 지난 회의의 것이 새 화면에 비치면 안 된다. */
    public void forgetLive() { live.clear(); }

    private static String idOf(Object m) {
        if (m == null) return "";
        Object v = jsinterop.base.Js.asPropertyMap(m).get("id");
        return v == null ? "" : String.valueOf(v);
    }

    private static boolean eq(String a, String b) { return a == null ? b == null : a.equals(b); }
}
