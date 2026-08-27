package dev.sayaya.magi.bridge;

import elemental2.dom.DomGlobal;
import elemental2.dom.Response;
import jsinterop.base.Js;
import jsinterop.base.JsPropertyMap;

import java.util.ArrayList;
import java.util.List;

/**
 * 언어 팩 — 기존 콘솔과 같은 파일(/i18n/language.{en,ko}.json)을 BFF에서 읽는다.
 * 문구가 한 곳에서 오므로 두 콘솔이 다른 말을 할 수 없다.
 *
 * tr(): 키가 없으면 키 자신이 폴백(빈칸 대신 "번역 빠짐"이 보이도록 — 기존 콘솔 규칙).
 * stateWord(): tr과 달리 원어 상태어가 폴백(행에 "state.gone"을 적지 않기 위해).
 *
 * ⚠ 한 창에 한 번만 읽는다 — 그리고 그 "한 창"은 모듈 하나가 아니다.
 *
 * 화면은 메뉴에 따라 따로 컴파일된 모듈로 들어온다(페더레이션). 모듈마다 제 이름공간이라
 * 이 클래스의 static도 모듈 수만큼 있고, 그래서 창 하나에서 팩을 네 번 받았다(실측: 셸,
 * 플릿, 지식, 보드 각 1회). 게다가 렌더는 마운트마다 불리므로(캐시된 렌더를 다시 앉힐
 * 때도) 그 자리에서 읽으면 이동마다 한 번씩 더 샜다.
 *
 * 그래서 팩은 창에 둔다(window `__magi_labels`): 먼저 읽은 쪽이 올리고, 뒤에 오는 모듈은
 * 회선을 타지 않고 그것을 든다. 명단·전사와 같은 규칙이다 — 창에 하나면 족한 것은 창에.
 *
 * 그리고 <b>드는 일도 화면의 몫이 아니다</b>. 화면마다 제 마운트를 load()로 감싸게 하면
 * 그것은 화면이 지켜야 할 계약이 하나 느는 일이고, 잊은 화면은 제 모듈의 빈 static을 읽어
 * 키를 그대로 그린다(실측: "field.facts", "action.send"). 그래서 tr()이 스스로 창을 본다 —
 * 팩을 들여놓는 것은 부모의 일이고, 그것을 드는 것은 이 클래스의 일이다.
 */
public final class Labels {
    private static final String SHARED = "__magi_labels";
    private static JsPropertyMap<Object> pack = Js.cast(jsinterop.base.JsPropertyMap.of());
    private static boolean loaded = false;
    private static boolean loading = false;
    private static final List<Runnable> waiting = new ArrayList<>();

    private Labels() {}

    /** 브라우저 선호 언어 순서로 팩을 고른다(en/ko만 존재). 실패해도 done은 부른다 — 키 폴백으로 뜬다. */
    public static void load(Runnable done) {
        if (loaded) { done.run(); return; }          // 이미 읽었다 — 회선을 다시 타지 않는다
        // 다른 모듈이 이미 받아 창에 올려 뒀다면 그것을 든다(페더레이션: static은 모듈마다다).
        JsPropertyMap<Object> win = Js.asPropertyMap(DomGlobal.window);
        if (win.has(SHARED)) {
            pack = Js.uncheckedCast(win.get(SHARED));
            loaded = true;
            done.run();
            return;
        }
        waiting.add(done);
        if (loading) return;                          // 비행 중이면 이 줄에 서면 된다
        loading = true;
        String want = pick();
        // 호스트 창의 회선으로 — 데모의 목이 사는 곳이 거기다(Console.raw의 주석).
        Console.raw("/i18n/language." + want + ".json", null)
                .then(Response::text)
                .then(body -> {
                    pack = Js.cast(elemental2.core.Global.JSON.parse(body));
                    Js.asPropertyMap(DomGlobal.window).set(SHARED, pack);
                    settle();
                    return null;
                })
                // 못 읽어도 끝난 것으로 둔다: 키 폴백으로 뜨고, 화면마다 같은 실패를 다시
                // 시도하며 회선을 태우지 않는다. 다시 읽을 일은 reload가 맡는다.
                .catch_(err -> { settle(); return null; });
    }

    /** 언어가 바뀌었을 때만 — 팩을 버리고 다시 읽는다(그 화면들이 제 말을 다시 칠한다). */
    public static void reload(Runnable done) {
        loaded = false;
        // 창의 사본도 버린다 — 남겨 두면 다른 모듈이 낡은 말을 계속 든다.
        Js.asPropertyMap(DomGlobal.window).delete(SHARED);
        load(done);
    }

    private static void settle() {
        loaded = true;
        loading = false;
        List<Runnable> now = new ArrayList<>(waiting);
        waiting.clear();
        for (Runnable r : now) r.run();
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
        Object v = current().get(key);
        return v == null ? key : String.valueOf(v);
    }

    /**
     * 이 모듈이 들고 있는 팩 — 비어 있으면 창에 올라와 있는 것을 든다.
     *
     * 페더레이션의 그림자를 여기서 막는다: 부모가 팩을 들여놓아도 그것은 부모 모듈의
     * static이고, 자식 모듈의 static은 여전히 비어 있다. 한 번 들면 그 뒤로는 제 것을 읽는다.
     */
    private static JsPropertyMap<Object> current() {
        if (!loaded) {
            JsPropertyMap<Object> win = Js.asPropertyMap(DomGlobal.window);
            if (win.has(SHARED)) {
                pack = Js.uncheckedCast(win.get(SHARED));
                loaded = true;
            }
        }
        return pack;
    }

    /** {name} 꼴 변수 치환. 홀수 인자는 (이름, 값) 쌍. */
    public static String tr(String key, String... vars) {
        String out = tr(key);
        for (int i = 0; i + 1 < vars.length; i += 2) out = out.replace("{" + vars[i] + "}", vars[i + 1]);
        return out;
    }

    public static String stateWord(String s) {
        Object v = current().get("state." + (s == null ? "" : s));
        return v != null ? String.valueOf(v) : (s == null ? "" : s);
    }
}
