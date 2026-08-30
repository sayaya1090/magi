package dev.sayaya.magi.bridge;

import elemental2.dom.DomGlobal;
import elemental2.dom.HTMLScriptElement;
import jsinterop.base.Js;
import jsinterop.base.JsPropertyMap;

/**
 * 모듈 스크립트를 문서에 들이는 한 가지 방법 — 셸이 화면을 들일 때도, 컴패니언 패널이
 * 제 타입 UI를 들일 때도 같은 규칙이다.
 *
 * 경로는 `/ui/` 절대이고(상대는 프록시로 새 나간다), 한 창에서 한 번만 들인다 —
 * 들였다는 사실도 창에 적는다: 모듈마다 static이 따로라 제 안에서만 세면 같은 스크립트를
 * 두 번 넣는다(언어 팩에서 실측한 그 페더레이션의 그림자).
 *
 * ⚠ 이름은 오퍼레이터가 설치한 카탈로그에서만 온다. 컴패니언이나 워크스페이스가 실어 온
 * 경로를 그대로 들이지 않는다 — `.magi/plugins`가 지목 없이는 안 도는 것과 같은 경계다.
 */
public final class ModuleInject {
    private static final String KEY = "__magi_modules";

    private ModuleInject() {}

    /** 스타일시트 없는 모듈 — 대부분의 화면은 console.css만으로 산다. */
    public static void ensure(String module) { ensure(module, false); }

    /**
     * 모듈 하나를 들인다. styles가 참이면 제 스타일시트(/ui/&lt;name&gt;.css)도 함께 건다.
     *
     * 그 판단이 여기 있는 이유: 스크립트를 들이는 쪽이 부모이고, 어느 모듈이 무엇을 싣는지는
     * 부모의 카탈로그가 아는 사실이다. 자식에게 "네 시트는 네가 걸어라"라고 시키면, 잊은
     * 자식은 스타일 없이 뜨고 그 실패는 자식 코드 어디에도 적혀 있지 않다.
     */
    public static void ensure(String module, boolean styles) {
        if (module == null || module.isEmpty()) return;
        JsPropertyMap<Object> win = Js.asPropertyMap(DomGlobal.window);
        JsPropertyMap<Object> seen = win.has(KEY)
                ? Js.uncheckedCast(win.get(KEY)) : JsPropertyMap.of();
        win.set(KEY, seen);
        if (seen.has(module)) return;
        seen.set(module, true);
        HTMLScriptElement s = (HTMLScriptElement) DomGlobal.document.createElement("script");
        s.src = "/ui/" + module + "/" + module + ".nocache.js";
        DomGlobal.document.head.append(s);
        if (styles) Stylesheet.ensure(module);
    }
}
