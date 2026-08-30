package dev.sayaya.magi.client.usecase;

import dev.sayaya.magi.bridge.CardSharing;
import elemental2.dom.HTMLElement;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * 이 화면이 열어 둔 카드 전부 — <b>한 곳에서</b> 부모에게 건넨다.
 *
 * 카드 줄은 창에 하나이고 건네는 것은 배열이라, 두 판이 각자 건네면 나중 것이 앞 것을 지운다
 * (워크스페이스가 파일을 건네고 전사가 표결을 건네면, 파일 탭이 조용히 사라진다). 그래서 파는
 * 쪽마다 제 몫을 이 자리에 놓고, 합쳐 보내는 일은 여기서 한 번만 한다.
 */
@Singleton
public class OpenCards {
    private final Map<String, List<HTMLElement>> byOwner = new LinkedHashMap<>();

    @Inject
    public OpenCards() {}

    /** 이 주인의 카드는 이것들이다 — 그 주인의 이전 몫을 대신한다. */
    public void set(String owner, List<HTMLElement> cards) {
        byOwner.put(owner, new ArrayList<>(cards));
        publish();
    }

    private void publish() {
        List<Object> all = new ArrayList<>();
        for (List<HTMLElement> some : byOwner.values()) all.addAll(some);
        CardSharing.provide(all.toArray(new Object[0]));
    }
}
