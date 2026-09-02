package dev.sayaya.magi.client.domain;

/**
 * 전사의 창(window) — 페이지에 실제로 두는 행이 어디서부터인가.
 *
 * 전사는 끝없이 자란다. 전부를 DOM에 두면 새 행 하나가 붙을 때마다 브라우저가 그 전부를 다시
 * 레이아웃하고, 긴 세션에서는 그것이 화면이 굳는 이유가 된다. 그래서 끝의 일부만 페이지에 두고,
 * 그 앞은 높이만 차지하는 상자 하나로 대신한다. 스크롤바가 정직해지는 것도 그 상자 덕이다.
 *
 * 여기 있는 것은 <b>셈뿐</b>이다. 높이를 재고 상자를 세우고 스크롤을 붙잡는 것은 화면의 몫이고,
 * 그쪽은 브라우저가 있어야 잰다. 어디서부터 그릴지를 정하는 규칙은 브라우저 없이 재지므로 갈라 둔다.
 */
public final class Window {
    private Window() {}

    /** 쉬는 상태로 페이지에 두는 행 수. */
    public static final int CAP = 150;

    /**
     * 잘라내기 전에 넘어도 되는 여유.
     *
     * 한 행이 올 때마다 창을 한 칸씩 밀면 행 재사용의 매치가 <b>첫 행에서</b> 깨지고, 그러면 매
     * 프레임 창 전체를 다시 짓는다 — 창이 없애려던 비용을 창이 만드는 꼴이다. 그래서 CAP를 넘어
     * SLACK까지 자라게 두었다가 한 번에 CAP로 떨어뜨린다. 잘라내기가 드물어지고 그 비용이 분산된다.
     */
    public static final int SLACK = 50;

    /** 독자가 창 위에 닿았을 때 되불러오는 행 수. */
    public static final int REACH = 100;

    /**
     * 그린 적 없는 행의 가정 높이(px).
     *
     * 긴 세션의 첫 프레임은 수백 행을 <b>그린 적도 없이</b> 잘라낸다 — 잴 것이 없으니 이 값으로
     * 센다. 스타일시트가 같은 이유로 주는 값과 같은 수다(console.css의 {@code
     * contain-intrinsic-size:auto 3.5rem} = 56px). 한 번이라도 그려진 행은 그때 잰 실제 높이로
     * 기억하므로 이 값은 첫 프레임에만 쓰인다.
     */
    public static final int GUESS = 56;

    /**
     * 이번 프레임에 창이 시작할 자리.
     *
     * 두 가지로만 움직인다. 아래로는 <b>청크 단위</b>로 — {@code keep + SLACK}를 넘었을 때만,
     * 그것도 {@code total - keep}까지 한 번에. 위로는 독자가 청했을 때(= {@code keep}이 커졌을 때)
     * 즉시. 그래서 이 함수는 두 방향 모두에서 "지금 keep이 말하는 자리"를 돌려주되, 아래로는
     * 여유를 넘기 전까지 <b>움직이지 않는다</b>.
     *
     * @param total   전사 전체 행 수
     * @param winFrom 지금 창이 시작하는 자리
     * @param keep    지금 창이 들 의향 (= CAP에서 시작해 REACH만큼씩 자란다)
     * @return 새 winFrom (0 이상, total 이하)
     */
    public static int nextFrom(int total, int winFrom, int keep) {
        if (total <= 0) return 0;
        int target = Math.max(0, total - keep);
        if (target > winFrom) {
            // 아래로: 여유를 넘었을 때만 움직인다.
            return (total - winFrom > keep + SLACK) ? target : winFrom;
        }
        // 위로: 독자가 청한 것이므로 곧바로 들어준다.
        return target;
    }

    /**
     * 되찾기 — 독자가 창 위에 닿았다.
     *
     * {@code keep}은 <b>자라기만 하고 줄지 않는다</b>(같은 대화에 머무는 동안). 줄게 두면 다음
     * 프레임의 잘라내기가 방금 스크롤로 되찾은 것을 도로 가져가, 창이 읽고 있는 사람 밑에서 닫힌다.
     */
    public static int reach(int keep) { return keep + REACH; }

    /** 더 되찾을 것이 있는가 — 창이 이미 맨 앞이면 없다. */
    public static boolean canReach(int winFrom) { return winFrom > 0; }
}
