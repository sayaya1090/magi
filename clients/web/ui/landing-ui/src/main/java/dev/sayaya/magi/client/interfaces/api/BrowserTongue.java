package dev.sayaya.magi.client.interfaces.api;

import dev.sayaya.magi.bridge.Prefs;
import dev.sayaya.magi.client.usecase.TongueSource;
import elemental2.dom.DomGlobal;
import jsinterop.base.Js;
import jsinterop.base.JsArrayLike;

import javax.inject.Inject;
import javax.inject.Singleton;

/**
 * 브라우저에게 묻는다 — 그리고 콘솔과 <b>같은 자리</b>에 적는다.
 *
 * 자리가 localStorage 의 'lang' 인 것은 콘솔의 계약이다(bridge/Labels). 랜딩이 제 열쇠를
 * 따로 쓰면 한 브라우저에서 두 페이지가 다른 말을 하게 된다 — 여기서 한국어를 고르고 데모를
 * 열면 영어가 나오는 그 모양이다.
 */
@Singleton
public class BrowserTongue implements TongueSource {
    @Inject
    public BrowserTongue() {}

    @Override
    public String stored() { return Prefs.text("lang", ""); }

    @Override
    public String[] preferred() {
        // 사적 창이나 오래된 브라우저에서는 없을 수 있다 — 없으면 빈 배열이고, 규칙이 영어로 문다.
        JsArrayLike<Object> langs = Js.uncheckedCast(DomGlobal.navigator.languages);
        if (langs == null) return new String[0];
        String[] out = new String[langs.getLength()];
        for (int i = 0; i < out.length; i++) out[i] = String.valueOf(langs.getAt(i));
        return out;
    }

    @Override
    public void keep(String code) { Prefs.keepText("lang", code); }
}
