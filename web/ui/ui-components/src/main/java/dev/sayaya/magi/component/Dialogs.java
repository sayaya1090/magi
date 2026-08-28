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

    /** 되돌릴 수 없는 일 — 무엇이 사라지는지 이름을 대고 묻는다. */
    public void confirm(String head, String body, String doIt, Runnable then) {
        confirm(head, body, doIt, null, then);
    }

    /**
     * 하는 쪽 버튼에도 표를 단다 — 무엇이 일어나는지는 낱말만이 아니라 그림으로도 말한다
     * (운영 confirmThis는 doMark 없이 불리는 자리가 없다).
     */
    public void confirm(String head, String body, String doIt, String doMark, Runnable then) {
        HTMLElement dialog = el("md-dialog");
        dialog.className = "askconfirm";
        HTMLElement h = el("div");
        h.setAttribute("slot", "headline");
        h.textContent = head;
        HTMLElement content = el("div");
        content.setAttribute("slot", "content");
        content.append(cell("asksay", body));
        HTMLElement actions = el("div");
        actions.setAttribute("slot", "actions");
        HTMLElement keep = marked(el("md-text-button"), "#i-sl-xmark", tr("action.cancel"));
        keep.addEventListener("click", evt -> close(dialog));
        HTMLElement go = marked(el("md-filled-tonal-button"), doMark, doIt);
        go.addEventListener("click", evt -> { close(dialog); then.run(); });
        actions.append(keep, go);
        dialog.append(h, content, actions);
        DomGlobal.document.body.append(dialog);
        open(dialog);
    }

    /**
     * 낱말에 표를 붙인다 — <b>붙일 표가 있을 때만</b>. 스프라이트가 없는 빌드(기여자·정적
     * 데모)에서 shape는 null을 돌려주는데, DOM의 append는 null을 "null"이라는 글자로 적는다:
     * 확인 버튼이 "null 옮기고 보내기"로 읽혔다(실측). 운영의 withMark도 같은 자리에서
     * 그림이 없으면 그냥 돌아선다.
     */
    private static HTMLElement marked(HTMLElement btn, String ref, String word) {
        elemental2.dom.Element m = ref == null || ref.isEmpty() ? null : Icons.shape(ref, "mk");
        if (m == null) { btn.textContent = word; return btn; }
        btn.append(m, DomGlobal.document.createTextNode(" " + word));
        return btn;
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
