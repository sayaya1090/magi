package dev.sayaya.magi.client.interfaces;

import dev.sayaya.magi.bridge.FleetAgent;
import dev.sayaya.magi.bridge.Icons;
import elemental2.dom.DomGlobal;
import elemental2.dom.HTMLElement;
import elemental2.dom.KeyboardEvent;
import jsinterop.base.Js;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.function.Consumer;

import static dev.sayaya.magi.bridge.Labels.tr;

/**
 * 기다리는 컴패니언에게 답하는 상자 — 명단의 행에서도, 그 컴패니언 제 화면의 도크에서도
 * 같은 것이 나온다. 두 곳이 같은 코드인 이유는 "무엇을 물었나"에 따라 답의 모양이 셋으로
 * 갈리기 때문이다: 한 곳에서만 고쳐지면 같은 질문이 두 화면에서 다르게 보인다.
 *
 * <ul>
 *   <li><b>퍼미션</b> — 결정 넷(허용·그만 묻기·저장·거절). 전부 <b>한 무게</b>다: 하나를
 *       도드라지게 하면 콘솔이 사람 대신 기운다. 대신 각자 표(✓ ⃠ 💾 ⃠)를 단다 — 되돌릴 수
 *       없는 누름이고, 낱말 길이는 언어마다 달라도 그림은 한눈에 갈린다.</li>
 *   <li><b>목록이 딸린 질문</b> — 그 보기들. 가운데로 모은다: 질문에 대한 툴바가 아니라
 *       질문의 답이라서.</li>
 *   <li><b>맨 질문</b> — 한 줄 답과 보내기.</li>
 * </ul>
 */
@Singleton
public class AnswerBox {

    @Inject
    public AnswerBox() {}

    /** freeText=true면 목록이 딸린 질문에도 "그밖에"를 둔다 — 목록은 제안이지 전부가 아니다. */
    public HTMLElement of(FleetAgent a, Consumer<String> send, boolean freeText) {
        HTMLElement box = el("div");
        box.className = "answer";
        if ("question".equals(a.askKind) && a.askOptions != null && a.askOptions.length > 0) {
            box.classList.add("choices");
            for (String opt : a.askOptions) {
                HTMLElement b = el("md-outlined-button");
                b.textContent = opt;
                b.addEventListener("click", evt -> {
                    evt.preventDefault(); evt.stopPropagation(); send.accept(opt);
                });
                box.append(b);
            }
            if (freeText) {
                HTMLElement more = el("md-text-button");
                more.textContent = tr("ask.other");
                more.addEventListener("click", evt -> {
                    evt.preventDefault(); evt.stopPropagation();
                    HTMLElement[] pair = textAnswer(send);
                    more.replaceWith(pair[0], pair[1]);
                    pair[0].focus();
                });
                box.append(more);
            }
        } else if ("question".equals(a.askKind)) {
            for (HTMLElement n : textAnswer(send)) box.append(n);
        } else {
            HTMLElement acts = el("div");
            acts.className = "bgroup";
            box.append(acts);
            String[][] decisions = {{"action.allow", "allow", "#i-sl-check"},
                                    {"action.always", "always", "#i-sl-bell-slash"},
                                    {"action.keep", "persist", "#i-sl-floppy-disk"},
                                    {"action.deny", "deny", "#i-sl-ban"}};
            for (String[] d : decisions) {
                HTMLElement b = el("md-outlined-button");
                b.append(Icons.orGlyph(d[2], mark(d[1]), "mk"), DomGlobal.document.createTextNode(" " + tr(d[0])));
                if (a.name != null && !a.name.isEmpty()) {
                    b.setAttribute("aria-label", tr("action.for_companion", "action", tr(d[0]), "name", a.name));
                }
                final String decision = d[1];
                b.addEventListener("click", evt -> {
                    evt.preventDefault(); evt.stopPropagation(); send.accept(decision);
                });
                acts.append(b);
            }
        }
        return box;
    }

    /** 그림이 없는 빌드에서도 표는 남는다 — 아이콘은 구 콘솔에서 빌려 온 것이라 없을 수 있다. */
    private static String mark(String decision) {
        switch (decision) {
            case "allow": return "✓";
            case "always": return "⃠";
            case "persist": return "⌸";
            default: return "✕";
        }
    }

    /** 한 줄의 답과 보내기 버튼. 비면 disabled — 눌리는데 아무것도 안 하는 셋째 상태는 없다. */
    public HTMLElement[] textAnswer(Consumer<String> send) {
        HTMLElement i = el("md-outlined-text-field");
        i.setAttribute("label", tr("label.answer"));
        HTMLElement b = el("md-filled-button");
        b.textContent = tr("action.answer");
        Runnable arm = () -> {
            if (value(i).trim().isEmpty()) b.setAttribute("disabled", "");
            else b.removeAttribute("disabled");
        };
        arm.run();
        i.addEventListener("input", evt -> arm.run());
        // 행이 링크가 되어도 이 상자 안의 탭·클릭은 항해가 아니다(운영의 preventDefault 규칙).
        i.addEventListener("click", evt -> { evt.preventDefault(); evt.stopPropagation(); i.focus(); });
        i.addEventListener("keydown", evt -> {
            KeyboardEvent ke = Js.uncheckedCast(evt);
            if ("Enter".equals(ke.key) && !Js.isTruthy(Js.asPropertyMap(ke).get("isComposing"))) {
                evt.preventDefault(); evt.stopPropagation();
                String said = value(i).trim();
                if (!said.isEmpty()) send.accept(said);
            }
        });
        b.addEventListener("click", evt -> {
            evt.preventDefault(); evt.stopPropagation();
            String said = value(i).trim();
            if (!said.isEmpty()) send.accept(said);
        });
        return new HTMLElement[]{i, b};
    }

    private static String value(HTMLElement field) {
        Object v = Js.asPropertyMap(field).get("value");
        return v == null ? "" : String.valueOf(v);
    }

    private static HTMLElement el(String tag) { return Js.uncheckedCast(DomGlobal.document.createElement(tag)); }
}
