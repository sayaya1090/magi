package dev.sayaya.magi.client.usecase;

import dev.sayaya.magi.client.domain.Destination;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.ArrayList;
import java.util.List;
import java.util.function.Consumer;

/**
 * 1단 위의 손끝 — 열린 드로어에서 어느 문 위에 마우스가 있는가(handbook의 호버 피크).
 * null은 "아무 데도 아님": 2단은 그때 선택된 목적지로 돌아간다. 레일 전체를 벗어날 때만
 * 리셋한다 — 1단에서 2단으로 건너가는 길에 끊기면 피크가 쓸 수 없는 물건이 된다(호버 터널).
 */
@Singleton
public class MenuHover {
    private final List<Consumer<Destination>> observers = new ArrayList<>();
    private Destination current = null;

    @Inject
    public MenuHover() {}

    public void subscribe(Consumer<Destination> o) { observers.add(o); }

    public Destination current() { return current; }

    public void next(Destination d) {
        if (current == d) return;
        current = d;
        for (Consumer<Destination> o : observers) o.accept(d);
    }
}
