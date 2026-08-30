package dev.sayaya.magi.bridge;

import elemental2.dom.DomGlobal;
import elemental2.dom.HTMLElement;

import static dev.sayaya.magi.bridge.Labels.tr;

/**
 * 되돌릴 수 없는 일 앞의 두 번째 누름 — 버튼이 제자리에서 "확인?"으로 무장하고, 5초 안에
 * 다시 누르지 않으면 스스로 풀린다(운영 arm).
 *
 * 여기 있는 이유는 이것이 <b>세 벌로 갈라져 있었기</b> 때문이다. 세 화면이 같은 이름의
 * private 메서드를 각자 갖고 있었고, 셋이 이미 서로 달랐다:
 *
 * <ul>
 *   <li>지식 판은 {@link Icons#reword}로 말을 갈아 표를 잃지 않고, 버튼의 aria-label도 함께
 *       갈았다 — 맞게 쓴 한 벌.</li>
 *   <li>접근 제어 판은 textContent를 그냥 덮어써서, aria-label을 단 버튼(사람 이름이 들어
 *       있다)이 무장해도 <b>읽는 기계에는 이름이 그대로</b>였다. 눈으로 보는 사람에게만 있는
 *       안전장치는 안전장치가 아니고, 하필 그 버튼이 남의 접근을 걷어내는 버튼이다.</li>
 *   <li>워크스페이스 판의 한 벌은 아무도 부르지 않는 죽은 코드였다(그 판은 dialogs.confirm을
 *       쓴다). 쓰지 않는 세 번째 사본이 옆에 놓여 있는 것이 다음 사람이 잘못된 것을 고르는
 *       방식이다.</li>
 * </ul>
 *
 * 그래서 맞게 쓴 한 벌만 남기고 여기로 올린다. 이 콘솔에서 되돌릴 수 없는 일에 두 번째
 * 누름을 요구하는 자리는 전부 이 한 곳을 지난다.
 */
public final class Confirm {
    private Confirm() {}

    /** 무장이 풀리기까지 — 사람이 다른 일을 하러 간 뒤에도 버튼이 겨눈 채로 남지 않게. */
    private static final int DISARM_MS = 5000;

    /**
     * 두 번 눌러야 도는 버튼. 첫 누름은 말과 (있다면) 읽히는 이름을 "확인?"으로 갈고,
     * 둘째 누름에서야 {@code act}가 돈다.
     *
     * @param btn  무장할 버튼 — 표(data-mark)를 달고 있어도 잃지 않는다({@link Icons#reword}).
     * @param word 평소의 말. 풀릴 때 이것으로 돌아온다.
     * @param act  둘째 누름에 도는 일.
     */
    public static void arm(HTMLElement btn, String word, Runnable act) {
        Icons.reword(btn, word);
        // 읽히는 이름은 눈에 보이는 말을 이긴다: aria-label이 붙은 버튼은 말만 갈면 화면에서만
        // 무장하고 귀에는 그대로다. 그래서 붙어 있을 때만, 붙어 있는 그것을 함께 간다.
        final String named = named(btn);
        final boolean[] armed = {false};
        final double[] timer = {-1};
        btn.addEventListener("click", evt -> {
            if (armed[0]) {
                DomGlobal.clearTimeout(timer[0]);
                disarm(btn, word, named, armed);
                act.run();
                return;
            }
            armed[0] = true;
            btn.className += " armed";
            Icons.reword(btn, tr("action.confirm"));
            if (named != null) btn.setAttribute("aria-label", tr("action.confirm") + " — " + named);
            timer[0] = DomGlobal.setTimeout(a -> disarm(btn, word, named, armed), DISARM_MS);
        });
    }

    /**
     * 이 버튼에 붙어 있는 <b>읽히는</b> 이름 — 없으면 null.
     *
     * 두 자리를 다 본다. md-* 버튼은 호스트의 aria-label을 제 것으로 가져가면서 그 자리에
     * {@code data-aria-label}만 남기고(실측: 그림자 속 &lt;button&gt;이 그 값을 단다), 그래서
     * 컴포넌트가 깨어난 <b>뒤에</b> 물어보면 aria-label은 이미 없다. 한 자리만 보면 무장이
     * 귀에 닿느냐가 이 버튼이 언제 업그레이드됐느냐에 달리게 된다.
     */
    private static String named(HTMLElement btn) {
        String direct = btn.getAttribute("aria-label");
        return direct != null ? direct : btn.getAttribute("data-aria-label");
    }

    private static void disarm(HTMLElement btn, String word, String named, boolean[] armed) {
        armed[0] = false;
        btn.className = btn.className.replace(" armed", "");
        Icons.reword(btn, word);
        if (named != null) btn.setAttribute("aria-label", named);
    }
}
