package dev.sayaya.magi.client;

import com.google.gwt.core.client.EntryPoint;
import elemental2.dom.DomGlobal;
import elemental2.dom.HTMLElement;
import jsinterop.base.Js;

/**
 * 부모 없이, 부모가 하는 만큼만: 세 자리를 옷 입혀 만들고 그 자리에 이 모듈을 앉힌다.
 *
 * 자리 이름과 옷은 운영 콘솔의 것과 같다 — 가운데 기둥(#stream), 왼쪽 기둥(#filecol),
 * 창 바닥의 도크(#dock .bay). 하네스가 부모를 흉내 내는 이유는 자식이 부모 없이도 도는지를
 * 재기 위해서가 아니라, <b>부모가 무엇을 책임지는지</b>를 한 곳에 적어 두기 위해서다:
 * 자식 코드에는 이 이름들이 하나도 없다.
 */
public class TestApplication implements EntryPoint {
    @Override
    public void onModuleLoad() {
        HTMLElement main = Js.uncheckedCast(DomGlobal.document.createElement("main"));
        main.id = "frame";
        HTMLElement stage = div("agentview"), filecol = div("filecol"), stream = div("stream");
        stage.append(filecol, stream);
        main.append(stage);
        DomGlobal.document.body.appendChild(main);
        HTMLElement dock = Js.<HTMLElement>uncheckedCast(DomGlobal.document.createElement("footer"));
        dock.id = "dock";
        HTMLElement bay = div(null);
        bay.className = "bay";
        dock.append(bay);
        DomGlobal.document.body.appendChild(dock);
        // 기둥은 둘 다 열어 둔다 — 이 페이지엔 손잡이(부모의 것)가 없다.
        DomGlobal.document.body.setAttribute("files", "open");
        DomGlobal.document.body.setAttribute("side", "open");
        DomGlobal.document.body.setAttribute("at", "agent");
        // 부모가 알리는 그 사실 하나를 하네스가 대신 연다 — 스펙이 사람 대신 부모 노릇을 한다.
        jsinterop.base.Js.asPropertyMap(DomGlobal.window).set("__magi_ask_publish",
                (dev.sayaya.magi.bridge.AskSharing.NextFn) ask ->
                        dev.sayaya.magi.bridge.AskSharing.publish(ask));
        // 답을 받는 것도 부모의 몫이다(/answer의 주인은 하나) — 하네스가 그 문을 대신 건다.
        dev.sayaya.magi.bridge.AskSharing.hostSend((text, landed) -> {
            Object ask = jsinterop.base.Js.asPropertyMap(DomGlobal.window).get("__magi_ask");
            String call = ask == null ? "" : String.valueOf(jsinterop.base.Js.asPropertyMap(ask).get("call"));
            String kind = ask == null ? "" : String.valueOf(jsinterop.base.Js.asPropertyMap(ask).get("kind"));
            jsinterop.base.Js.asPropertyMap(DomGlobal.window)
                    .set("__magi_test_answered", call + "/" + kind + "/" + text);
            landed.call("");
        });
        CodingTestComponent c = DaggerCodingTestComponent.create();
        c.conversation().mount(stream);
        c.conversation().mountComposer(bay);
        // 카드 자리도 부모의 것이다 — 하네스가 그 자리를 만들고, 자식이 등록한 것을 그린다.
        HTMLElement cards = div("fileview");
        stream.append(cards);
        dev.sayaya.magi.bridge.CardSharing.onChange(list -> {
            // 부모는 마지막에 열린 것을 세운다(진짜 부모는 탭 줄로 고르게 한다) — 자식이 건넨
            // 노드를 그대로 붙일 뿐이라, 그 안을 무엇으로 그렸는지는 알지 못한다.
            cards.replaceChildren();
            jsinterop.base.JsArrayLike<Object> all = jsinterop.base.Js.uncheckedCast(list);
            StringBuilder said = new StringBuilder();
            for (int i = 0; all != null && i < all.getLength(); i++) {
                elemental2.dom.Element one = jsinterop.base.Js.uncheckedCast(all.getAt(i));
                if (said.length() > 0) said.append('|');
                said.append(one.id).append('=').append(one.getAttribute("title"))
                    .append(dev.sayaya.magi.bridge.CardSharing.closable(one) ? "+x" : "");
            }
            // 스펙이 부모 노릇을 재는 자리: 무엇을 카드로 받았는지(신원·이름·닫힘)를 적어 둔다.
            jsinterop.base.Js.asPropertyMap(DomGlobal.window).set("__magi_test_cards", said.toString());
            if (all != null && all.getLength() > 0) {
                cards.append(jsinterop.base.Js.<HTMLElement>uncheckedCast(all.getAt(all.getLength() - 1)));
            }
        });
        c.workspace().mount(filecol);
    }

    private static HTMLElement div(String id) {
        HTMLElement e = Js.uncheckedCast(DomGlobal.document.createElement("div"));
        if (id != null) e.id = id;
        return e;
    }
}
