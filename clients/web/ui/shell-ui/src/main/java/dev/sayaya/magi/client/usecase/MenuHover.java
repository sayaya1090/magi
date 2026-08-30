package dev.sayaya.magi.client.usecase;

import dev.sayaya.magi.client.domain.Destination;
import dev.sayaya.rx.subject.BehaviorSubject;
import lombok.experimental.Delegate;

import javax.inject.Inject;
import javax.inject.Singleton;

import static dev.sayaya.rx.subject.BehaviorSubject.behavior;

/**
 * 1단 위의 손끝 — 열린 드로어에서 어느 문 위에 마우스가 있는가(handbook의 호버 피크).
 * null은 "아무 데도 아님": 2단은 그때 선택된 목적지로 돌아간다. 레일 전체를 벗어날 때만
 * 리셋한다 — 1단에서 2단으로 건너가는 길에 끊기면 피크가 쓸 수 없는 물건이 된다(호버 터널).
 *
 * 스토어는 <b>흐름 그 자체</b>다(handbook의 그 관용구: @Delegate BehaviorSubject): 구독하면
 * 지금 값이 즉시 오고, 같은 값이 두 번 오는 일은 여기서 끊는다.
 */
@Singleton
public class MenuHover {
    @Delegate private final BehaviorSubject<Destination> _this = behavior(null);

    @Inject
    public MenuHover() {}

    public Destination current() { return getValue(); }

    /**
     * 같은 문을 다시 대는 것은 소식이 아니다 — 여기서 끊는다.
     *
     * 거르는 자리를 스토어로 두는 이유: 손끝은 한 문 위에서 여러 번 움직이고, 그 낱낱을
     * 흘려보내면 그것을 듣는 판마다 제 손으로 같은 가드를 달아야 한다(그 가드를 하나 빠뜨린
     * 것이 사실판이 초당 1402번 다시 서던 결함이었다).
     */
    public void next(Destination d) {
        if (getValue() == d) return;
        _this.next(d);
    }
}
