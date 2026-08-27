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
public class MeetingStore {
    private final MeetingSource source;
    private final List<Runnable> obs = new ArrayList<>();

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

    public void subscribe(Runnable o) { obs.add(o); }

    private void tell() { for (Runnable o : new ArrayList<>(obs)) o.run(); }

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
        tell();
        read();
    }

    /** 지금 보는 것을 다시 읽는다 — 폴이 부른다. */
    public void read() {
        if (room == null) {
            source.fleet(list -> { fleet = list; tell(); });
            source.rooms(list -> { rooms = list; tell(); });
            return;
        }
        source.room(room, m -> {
            gone = m == null;
            if (m != null) one = m;
            tell();
        });
    }

    public void convene(Consumer<String> failed, Consumer<String> made) {
        source.convene(topic, picked.toArray(new String[0]), m -> {
            topic = "";
            picked.clear();
            made.accept(idOf(m));
        }, failed);
    }

    public void say(String text, Consumer<String> failed) {
        source.say(room, text, null, false, why -> {
            if (why != null && !why.isEmpty()) { failed.accept(why); return; }
            saying = "";
            read();
        });
    }

    /** 바닥 잡기 — 쓰는 동안엔 아무도 끼어들지 않는다. 같은 값을 다시 보내지 않는다. */
    private boolean holding = false;

    public void hold(boolean on) {
        if (holding == on || room == null) return;
        holding = on;
        source.say(room, null, null, on, why -> { });
    }

    public void call(String who) { source.say(room, null, who, false, why -> read()); }

    public void close() { source.close(room, this::read); }

    public void reopen(String why) { source.reopen(room, why, this::read); }

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

    private static String idOf(Object m) {
        if (m == null) return "";
        Object v = jsinterop.base.Js.asPropertyMap(m).get("id");
        return v == null ? "" : String.valueOf(v);
    }

    private static boolean eq(String a, String b) { return a == null ? b == null : a.equals(b); }
}
