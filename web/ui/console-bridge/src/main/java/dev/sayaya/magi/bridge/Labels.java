package dev.sayaya.magi.bridge;

import elemental2.dom.DomGlobal;
import elemental2.dom.Response;
import jsinterop.base.Js;
import jsinterop.base.JsPropertyMap;

/**
 * 언어 팩 — 기존 콘솔과 같은 파일(/i18n/language.{en,ko}.json)을 BFF에서 읽는다.
 * 문구가 한 곳에서 오므로 두 콘솔이 다른 말을 할 수 없다.
 *
 * tr(): 키가 없으면 키 자신이 폴백(빈칸 대신 "번역 빠짐"이 보이도록 — 기존 콘솔 규칙).
 * stateWord(): tr과 달리 원어 상태어가 폴백(행에 "state.gone"을 적지 않기 위해).
 */
public final class Labels {
    private static JsPropertyMap<Object> pack = Js.cast(jsinterop.base.JsPropertyMap.of());

    private Labels() {}

    /** 브라우저 선호 언어 순서로 팩을 고른다(en/ko만 존재). 실패해도 done은 부른다 — 키 폴백으로 뜬다. */
    public static void load(Runnable done) {
        String want = pick();
        DomGlobal.fetch("/i18n/language." + want + ".json")
                .then(Response::text)
                .then(body -> {
                    pack = Js.cast(elemental2.core.Global.JSON.parse(body));
                    done.run();
                    return null;
                })
                .catch_(err -> { done.run(); return null; });
    }

    private static String pick() {
        var langs = DomGlobal.navigator.languages;
        if (langs != null) for (int i = 0; i < langs.getLength(); i++) {
            String l = String.valueOf(langs.getAt(i));
            if (l.startsWith("ko")) return "ko";
            if (l.startsWith("en")) return "en";
        }
        return "en";
    }

    public static String tr(String key) {
        Object v = pack.get(key);
        return v == null ? key : String.valueOf(v);
    }

    /** {name} 꼴 변수 치환. 홀수 인자는 (이름, 값) 쌍. */
    public static String tr(String key, String... vars) {
        String out = tr(key);
        for (int i = 0; i + 1 < vars.length; i += 2) out = out.replace("{" + vars[i] + "}", vars[i + 1]);
        return out;
    }

    public static String stateWord(String s) {
        Object v = pack.get("state." + (s == null ? "" : s));
        return v != null ? String.valueOf(v) : (s == null ? "" : s);
    }
}
