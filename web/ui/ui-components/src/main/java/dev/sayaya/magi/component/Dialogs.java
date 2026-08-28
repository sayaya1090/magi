package dev.sayaya.magi.component;

import dev.sayaya.magi.bridge.Icons;
import elemental2.dom.DomGlobal;
import elemental2.dom.HTMLElement;
import jsinterop.base.Js;

import javax.inject.Inject;
import javax.inject.Singleton;

import static dev.sayaya.magi.bridge.Labels.tr;

/**
 * 묻는 자리 두 가지 — 한 줄을 받는 상자와, 되돌릴 수 없는 일을 확인하는 상자.
 *
 * 여기(ui-components)에 사는 이유: 묻는 자리는 화면마다 다시 지을 것이 아니다 — 두 화면이
 * 각자 지으면 취소가 확인 왼쪽인지 오른쪽인지가 화면마다 달라진다.
 *
 * 브라우저의 prompt()를 쓰지 않는 이유는 운영이 그것을 쓰지 않는 이유와 같다: 그 상자는 이
 * 콘솔의 것이 아니라 브라우저의 것이라 말도, 모양도, 초점도 이 페이지가 정할 수 없고, 어떤
 * 브라우저에서는 아예 뜨지 않는다. 되돌릴 수 없는 일을 그런 상자에 맡길 수는 없다.
 */
@Singleton
public class Dialogs {
    @Inject
    public Dialogs() {}

    @FunctionalInterface
    public interface Said { void call(String text, String choice); }

    /**
     * 한 줄을 받는다. choices가 있으면 그 중 하나를 함께 고른다(찾기의 이름/내용처럼).
     * 빈 답은 답이 아니다 — 보내기는 그때 눌리지 않는다.
     */
    public void line(String head, String body, String label, String value,
                     String[][] choices, String chosen, Said then) {
        HTMLElement dialog = el("md-dialog");
        dialog.className = "askline";
        HTMLElement h = el("div");
        h.setAttribute("slot", "headline");
        h.textContent = head;
        HTMLElement content = el("div");
        content.setAttribute("slot", "content");
        if (body != null && !body.isEmpty()) content.append(cell("asksay", body));
        HTMLElement field = el("md-outlined-text-field");
        field.setAttribute("label", label);
        Js.asPropertyMap(field).set("value", value == null ? "" : value);
        content.append(field);
        final String[] pick = {chosen};
        if (choices != null) {
            HTMLElement set = el("md-chip-set");
            set.className = "askwhere";
            for (String[] c : choices) {
                HTMLElement chip = el("md-filter-chip");
                chip.className = "wherechip";
                chip.setAttribute("data-in", c[0]);
                chip.setAttribute("label", tr(c[1]));
                Js.asPropertyMap(chip).set("selected", c[0].equals(chosen));
                final String key = c[0];
                chip.addEventListener("click", evt -> {
                    pick[0] = key;
                    elemental2.dom.NodeList<elemental2.dom.Element> all = set.querySelectorAll("md-filter-chip");
                    for (int i = 0; i < all.getLength(); i++) {
                        Js.asPropertyMap(all.getAt(i)).set("selected",
                                key.equals(all.getAt(i).getAttribute("data-in")));
                    }
                });
                set.append(chip);
            }
            content.append(set);
        }
        HTMLElement actions = el("div");
        actions.setAttribute("slot", "actions");
        HTMLElement cancel = el("md-text-button");
        cancel.textContent = tr("action.cancel");
        cancel.addEventListener("click", evt -> close(dialog));
        HTMLElement go = el("md-filled-button");
        go.textContent = label;
        go.addEventListener("click", evt -> {
            String said = value(field).trim();
            if (said.isEmpty()) return;
            close(dialog);
            then.call(said, pick[0]);
        });
        actions.append(cancel, go);
        dialog.append(h, content, actions);
        DomGlobal.document.body.append(dialog);
        open(dialog);
        DomGlobal.setTimeout(a -> Js.<HTMLElement>uncheckedCast(field).focus(), 30);
    }

    /**
     * 되돌릴 수 없는 일 — 무엇이 사라지는지 이름을 대고 묻는다. 무르는 쪽은 "취소"이고
     * 그 표는 ✕다(운영 아홉 자리 중 일곱이 이 짝).
     */
    public void confirm(String head, String body, String doIt, String doMark, Runnable then) {
        confirm(head, body, null, "#i-sl-xmark", doIt, doMark, null, then);
    }

    /**
     * 무르는 쪽도 제 낱말과 제 표를 가진다 — "취소"가 늘 맞는 말은 아니다: 도는 것을 멈추겠냐고
     * 물을 때 아니라고 답하는 것은 취소가 아니라 <b>그대로 두기</b>이고, 그 표는 ✕가 아니라 ▶다
     * (운영 confirmStop의 keep·keepMark). ✕는 이 페이지 어디서나 "이 상자를 닫는다"는 뜻이라,
     * 계속 도는 쪽에 붙이면 무엇이 계속되는지가 아니라 상자가 사라지는 것만 말한다.
     *
     * 무르는 쪽에 할 일이 있는 자리도 있다(onKeep): 이미 움직여 버린 컨트롤 — 아니라고 할 가지로
     * 벌써 옮겨 간 브랜치 메뉴 — 은 되돌려 놓아야 하고, 무엇이 제자리인지는 부르는 쪽만 안다.
     */
    public void confirm(String head, String body, String keep, String keepMark,
                        String doIt, String doMark, Runnable onKeep, Runnable then) {
        HTMLElement dialog = el("md-dialog");
        dialog.className = "askconfirm";
        // 되돌릴 수 없는 일을 묻는 상자는 alert다 — 바깥을 눌러 흘려보낼 수 있는 상자가 아니고,
        // 읽는 기계에도 그렇게 말해야 한다(운영 #stopDialog의 그 속성).
        dialog.setAttribute("type", "alert");
        HTMLElement h = el("div");
        h.setAttribute("slot", "headline");
        h.textContent = head;
        HTMLElement content = el("div");
        content.setAttribute("slot", "content");
        // 말은 슬롯에 바로 놓는다: 이 상자의 내용은 문단 하나뿐이라 감쌀 것이 없다(운영
        // #stopBody가 그 모양). 한 줄을 받는 상자(line)에서만 산문이 입력칸·칩과 이웃해
        // 묶음(.asksay)이 쓸모가 생긴다.
        content.textContent = body;
        HTMLElement actions = el("div");
        actions.setAttribute("slot", "actions");
        HTMLElement keepBtn = el("md-text-button");
        Icons.say(keepBtn, keep == null || keep.isEmpty() ? tr("action.cancel") : keep, keepMark);
        keepBtn.addEventListener("click", evt -> { close(dialog); if (onKeep != null) onKeep.run(); });
        // 하는 쪽은 <b>채운 버튼이 아니라</b> armed다: 채워 두면 그 상자에서 가장 눈에 띄는 것이
        // 되돌릴 수 없는 쪽이 된다. 운영은 색으로만 말하고(그것도 hover가 아니라 늘), 손가락이
        // 잘못 닿기 쉬운 화면에서 그 신호가 사라지지 않게 한다.
        HTMLElement go = el("md-text-button");
        go.className = "armed";
        Icons.say(go, doIt, doMark);
        go.addEventListener("click", evt -> { close(dialog); then.run(); });
        actions.append(keepBtn, go);
        dialog.append(h, content, actions);
        DomGlobal.document.body.append(dialog);
        open(dialog);
    }

    /**
     * 도는 것을 멈추겠냐고 묻는다 — 이 콘솔에서 그 물음이 서는 자리는 둘이다(대화의 멈춤 버튼,
     * 명단 행의 멈춤). 낱말도 표도 그 둘이 같아야 하므로 여기 한 번만 적는다(운영 confirmStop).
     * 이름을 모르면 이름 자리를 비운 물음이 아니라 <b>이름 없는 물음</b>을 쓴다.
     */
    public void stop(String who, Runnable then) {
        confirm(who == null || who.isEmpty() ? tr("stop.headline_plain") : tr("stop.headline", "name", who),
                tr("stop.body"), tr("action.keep_running"), "#i-sl-play",
                tr("action.interrupt"), "#i-ss-circle-stop", null, then);
    }

    private static void close(HTMLElement d) {
        closeIt(d);
        DomGlobal.setTimeout(a -> d.remove(), 300);
    }

    private static native void open(HTMLElement d) /*-{
        if (d.show) d.show(); else d.open = true;
    }-*/;

    private static native void closeIt(HTMLElement d) /*-{
        if (d.close) d.close(); else d.open = false;
    }-*/;

    private static String value(HTMLElement f) {
        Object v = Js.asPropertyMap(f).get("value");
        return v == null ? "" : String.valueOf(v);
    }

    private static HTMLElement cell(String cls, String text) {
        HTMLElement e = el("div");
        e.className = cls;
        e.textContent = text;
        return e;
    }

    private static HTMLElement el(String tag) { return Js.uncheckedCast(DomGlobal.document.createElement(tag)); }
}
