package dev.sayaya.magi.client.interfaces.api;

import dev.sayaya.magi.bridge.Console;
import dev.sayaya.magi.client.usecase.WordSource;
import elemental2.core.Global;
import elemental2.dom.Response;
import jsinterop.base.Js;
import jsinterop.base.JsPropertyMap;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.LinkedHashMap;
import java.util.Map;

/**
 * 팩을 읽는다 — <b>상대 경로로</b>.
 *
 * 콘솔의 Labels 는 `/i18n/…` 를 절대로 부르는데, 그것은 그 콘솔이 사이트의 뿌리에 살기 때문이다.
 * 이 페이지는 프로젝트 사이트(`/<repo>/`) 아래에 서므로 앞의 슬래시는 도메인 뿌리로 새 나간다 —
 * 데모를 낼 때 emitDemo 가 같은 이유로 경로를 전부 상대로 고쳐 쓴다.
 */
@Singleton
public class FetchWords implements WordSource {
    @Inject
    public FetchWords() {}

    @Override
    public void load(String code, Taken taken) {
        Console.raw("i18n/landing." + code + ".json", null)
                .then(Response::text)
                .then(body -> {
                    taken.take(flatten(Global.JSON.parse(body)));
                    return null;
                })
                // 못 읽어도 끝난 것으로 둔다. 페이지는 키 폴백으로 뜨고, 그것이 빈 화면보다 낫다.
                .catch_(err -> {
                    taken.take(null);
                    return null;
                });
    }

    /** 팩은 평평한 문자열 지도다 — 그 모양이 아니면 없는 것으로 친다(빈 팩 = 키 폴백). */
    private static Map<String, String> flatten(Object parsed) {
        if (parsed == null) return null;
        JsPropertyMap<Object> map = Js.uncheckedCast(parsed);
        Map<String, String> out = new LinkedHashMap<>();
        map.forEach(key -> {
            Object v = map.get(key);
            if (v instanceof String) out.put(key, (String) v);
        });
        return out;
    }
}
