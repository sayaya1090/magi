package dev.sayaya.magi.client.domain;

import java.util.ArrayList;
import java.util.List;

/**
 * 모델 고르개가 무엇을 내놓아야 하는가.
 *
 * <p>목록은 그 <b>데몬</b>이 답한 것이다. 하지만 지금 돌고 있는 모델이 그 목록에 없을 수 있다 —
 * 클라우드 모델 위에 있는 컴패니언에게 로컬 백엔드가 제 것 일곱 개를 답하는 식으로. 그때 고르개는
 * 켜 둘 값을 찾지 못하고 <b>빈 칸</b>이 되며, 사람은 자기 컴패니언이 무엇으로 돌고 있는지 모른 채
 * 그것을 바꾸라는 말을 듣는다.
 *
 * <p>그래서 규칙은 "목록이 비면 지금 것"이 아니라 <b>"지금 것은 언제나 고를 수 있다"</b>이다.
 * 화면 밖으로 꺼내 둔 이유는 이 결정이 DOM과 아무 상관이 없어서다 — 브라우저를 세우지 않고 잰다.
 */
public final class ModelChoices {
    private ModelChoices() {}

    /**
     * 내놓을 이름들. 순서는 답한 순서를 지키고, 지금 것이 그 안에 없을 때만 맨 앞에 세운다.
     *
     * @param answered 데몬이 답한 이름들(없을 수 있다)
     * @param now      지금 켜져 있는 이름(비어 있을 수 있다)
     */
    public static List<String> offer(List<String> answered, String now) {
        List<String> out = new ArrayList<>();
        boolean has = false;
        if (answered != null) {
            for (String n : answered) {
                if (n == null || n.isEmpty()) continue;
                if (n.equals(now)) has = true;
                // 같은 이름을 두 번 내놓지 않는다. 목록은 백엔드가 답한 것이고, 그것이 한 이름을
                // 두 번 말하지 않으리라는 보장은 어디에도 없다 — 고르개에 같은 줄이 둘이면 사람은
                // 그 둘이 다른 것이라고 읽는다.
                if (!out.contains(n)) out.add(n);
            }
        }
        if (now != null && !now.isEmpty() && !has) out.add(0, now);
        return out;
    }
}
