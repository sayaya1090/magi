package dev.sayaya.magi.bridge;

import dev.sayaya.rx.Observable;
import dev.sayaya.rx.subject.BehaviorSubject;

import static dev.sayaya.rx.subject.BehaviorSubject.behavior;

/**
 * "무언가 달라졌다" — 값이 아니라 <b>소식</b>만 흐르는 스토어의 밑감.
 *
 * 이 콘솔의 스토어 여덟은 같은 모양이었다: 구독자 목록 하나, 부를 때 돌리는 루프 하나,
 * 그리고 늦게 온 구독자에게 지금 상태를 한 번 더 알려 주는 규칙 하나. 여덟 벌을 손으로
 * 쓰면 여덟 번 조금씩 다르게 쓰게 된다(실제로 어떤 것은 구독 즉시 부르고 어떤 것은
 * 부르지 않았다). 그 셋을 여기 한 번 적는다.
 *
 * 흐르는 값은 몇 번째 소식인지(1, 2, 3…)다. 값 자체를 나르지 않는 이유는 이 스토어들이
 * 여러 답을 한 상자에 들고 있고(목록·질의·읽는 중), 판이 그중 무엇을 볼지는 판이 알기
 * 때문이다 — 나르는 것은 "이제 다시 읽어라"뿐이다.
 */
public class Told {
    private final BehaviorSubject<Integer> _this = behavior(0);

    /** 지금 한 번, 그리고 소식이 있을 때마다. */
    public void subscribe(Runnable o) { _this.subscribe(n -> o.run()); }

    /** 소식이 있을 때만 — 첫 구독의 즉시 호출을 원치 않는 자리(운영의 그 스토어 둘). */
    public void onChange(Runnable o) { _this.filter(n -> n > 0).subscribe(n -> o.run()); }

    /** 달라졌다. */
    protected void told() { _this.next(_this.getValue() + 1); }

    /** 파생 흐름이 필요한 자리를 위해 — 조각을 잘라 distinctUntilChanged를 걸 수 있다. */
    protected Observable<Integer> stream() { return _this; }
}
