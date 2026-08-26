package dev.sayaya.magi.client.interfaces;

import elemental2.dom.DomGlobal;
import elemental2.dom.HTMLElement;
import jsinterop.annotations.JsPackage;
import jsinterop.annotations.JsType;
import jsinterop.base.Js;

import javax.inject.Inject;
import javax.inject.Singleton;

import static dev.sayaya.magi.bridge.Labels.tr;

/**
 * 인터럽트 확인 — 가이드의 모양 그대로: 구체적 질문의 헤드라인, 무르는 쪽이 왼쪽,
 * 확정 라벨은 일어날 일을 말한다("OK" 금지). 모달이라 하나면 충분하고, 둘이면
 * 무르는 쪽이 반대편으로 흘러갈 자리만 는다.
 */
@Singleton
public class StopDialogElement {
    private HTMLElement dialog, head, body, cancel, go;

    @Inject
    public StopDialogElement() {}

    @JsType(isNative = true, namespace = JsPackage.GLOBAL, name = "Object")
    private interface MdDialog {
        void show();
        void close(String returnValue);
    }

    public void ask(String who, Runnable run) {
        if (dialog == null) build();
        head.textContent = who != null && !who.isEmpty() ? tr("stop.headline", "name", who) : tr("stop.headline_plain");
        body.textContent = tr("stop.body");
        cancel.textContent = tr("action.keep_running");
        go.textContent = tr("action.interrupt");
        MdDialog dlg = Js.uncheckedCast(dialog);
        cancel.onclick = e -> { dlg.close("cancel"); return null; };
        go.onclick = e -> { dlg.close("go"); run.run(); return null; };
        dlg.show();
    }

    private void build() {
        dialog = el("md-dialog");
        dialog.setAttribute("type", "alert");
        head = el("div"); head.setAttribute("slot", "headline");
        body = el("div"); body.setAttribute("slot", "content");
        HTMLElement actions = el("div"); actions.setAttribute("slot", "actions");
        cancel = el("md-text-button");
        go = el("md-text-button");
        go.className = "armed";
        actions.append(cancel, go);
        dialog.append(head, body, actions);
        DomGlobal.document.body.append(dialog);
    }

    private static HTMLElement el(String tag) { return Js.uncheckedCast(DomGlobal.document.createElement(tag)); }
}
