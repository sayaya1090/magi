package dev.sayaya.magi.bridge;

/**
 * 지금 한 턴이 도는가, 돈 지 얼마나 됐나 — 턴바가 읽는 두 값.
 *
 * 둘이 늘 함께 움직여서 한 값으로 묶는다: 흐름이 나르는 것은 값 하나이고, "같은 소식이면
 * 흐르지 않는다"도 값 하나에만 걸 수 있다. 따로 나르던 시절에는 그 판정을 부르는 쪽이
 * 두 인자를 받아 제 손으로 했다.
 */
public final class Turn {
    public final boolean open;
    public final double forSec;

    public Turn(boolean open, double forSec) {
        this.open = open;
        this.forSec = forSec;
    }

    public static final Turn NONE = new Turn(false, 0);

    @Override
    public boolean equals(Object o) {
        if (!(o instanceof Turn)) return false;
        Turn t = (Turn) o;
        return open == t.open && forSec == t.forSec;
    }

    @Override
    public int hashCode() { return (open ? 1 : 0) * 31 + (int) forSec; }
}
