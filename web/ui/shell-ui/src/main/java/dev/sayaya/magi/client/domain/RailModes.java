package dev.sayaya.magi.client.domain;

/**
 * 레일의 두 기둥이 언제 어떤 모습인가 — handbook MenuRailMode/ToolRailMode의 순수 번역.
 *
 * magi의 드로어 상태는 둘이다: 접힘(기본)과 열림(body[nav=open]). handbook의 HIDE/OVERLAY,
 * 모바일 드릴인은 폰 탭바와 함께 잔여다. 규칙:
 *
 * - 접힘 + 도구 ≤1 → 메뉴 COLLAPSE(아이콘 기둥) · 툴 HIDE — 오늘의 기본 모습.
 * - 접힘 + 도구 >1 → 메뉴 HIDE · 툴 COLLAPSE: **접힌 기둥이 툴 레일이 된다.** 손끝이
 *   레일 위면(피크) 툴 EXPAND(라벨 노출). 닫기(←)를 누르면 dismissed — 메뉴 기둥으로
 *   복귀하되 선택은 유지된다(handbook CloseToolRailButton의 데스크톱 동작).
 * - 열림 → 메뉴 EXPAND(라벨·문장), 도구 >1이면 툴 EXPAND가 둘째 기둥으로 선다.
 *
 * dismissed는 닫기 버튼의 것이다 — 접힌 기둥에서만 뜻이 있고, 드로어를 열거나 문맥
 * (선택·도구 목록)이 바뀌면 걷힌다: 새 문맥은 새 판단이다.
 */
public final class RailModes {
    public enum State { EXPAND, COLLAPSE, HIDE }

    private RailModes() {}

    public static State menu(boolean drawerOpen, int toolCount, boolean dismissed) {
        if (drawerOpen) return State.EXPAND;
        boolean toolsTakeTheRail = toolCount > 1 && !dismissed;
        return toolsTakeTheRail ? State.HIDE : State.COLLAPSE;
    }

    public static State tool(boolean drawerOpen, int toolCount, boolean hovering, boolean dismissed) {
        if (toolCount <= 1) return State.HIDE;
        // 열린 드로어는 dismissed를 무시한다 — 닫기(←)는 접힌 기둥의 것이고, 열어 본
        // 사람에게 도구를 숨기는 건 다른 결정이다(handbook의 직접 전이도 다음 갱신에 걷힌다).
        if (drawerOpen) return State.EXPAND;
        if (dismissed) return State.HIDE;
        return hovering ? State.EXPAND : State.COLLAPSE;
    }
}
