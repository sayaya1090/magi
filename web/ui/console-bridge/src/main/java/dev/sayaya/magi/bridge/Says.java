package dev.sayaya.magi.bridge;

import elemental2.dom.DomGlobal;
import jsinterop.annotations.JsFunction;
import jsinterop.base.Js;
import jsinterop.base.JsPropertyMap;

/**
 * 콘솔의 목소리 — 사람에게 <b>보여</b> 남는 한 줄과, 읽는 기계에게만 <b>들리는</b> 한 줄.
 *
 * 화면 모듈은 제 판 안에서 벌어진 일만 그린다. 그런데 거절은 판 밖의 일일 때가 있다:
 * 서버가 이유를 대는데 그 이유가 어느 필드의 것도 아니면, 화면에는 적을 자리가 없어 사람이
 * 누른 버튼이 <b>아무 일도 안 한 것</b>이 된다(실측: 신규 콘솔의 MCP 저장 거부가 그랬다).
 * 그 자리가 마스트헤드의 상태줄이고, 줄의 주인은 셸이다.
 *
 * 둘을 나눠 두는 이유는 운영이 나눠 둔 이유와 같다: {@link #note}는 <b>남는다</b>(무엇이
 * 덮어쓰거나 화면이 바뀔 때까지) — 스스로 사라지는 알림은 놓칠 수 있다는 가이드의 반대가
 * 그것이다. {@link #say}는 보이지 않고 한 번 발표되고 끝이다 — 목록이 눈앞에서 줄어드는 것을
 * 보는 사람에게는 이미 충분한 피드백이라, 그 사실을 다시 <b>그리면</b> 화면만 시끄러워진다.
 *
 * 호스트가 없으면(단독 테스트 페이지) 조용히 아무 일도 하지 않는다 — 셸 없는 화면에서
 * 상태줄은 없는 것이지 고장난 것이 아니다.
 */
public final class Says {
    private static final String NOTE = "__magi_says";
    private static final String SAY = "__magi_say";

    private Says() {}

    @JsFunction
    public interface SayFn { void call(String text); }

    /** 셸 측: 두 줄의 주인을 등록한다. */
    public static void host(SayFn note, SayFn say) {
        JsPropertyMap<Object> win = Js.asPropertyMap(DomGlobal.window);
        win.set(NOTE, note);
        win.set(SAY, say);
    }

    /** 화면 측: 보이는 자리에 적는다. 빈 문자열은 걷는다는 뜻이다(운영과 같은 계약). */
    public static void note(String text) {
        JsPropertyMap<Object> win = Js.asPropertyMap(DomGlobal.window);
        if (!win.has(NOTE)) return;
        Js.<SayFn>cast(win.get(NOTE)).call(text == null ? "" : text);
    }

    /** 화면 측: 읽는 기계에게만. */
    public static void say(String text) {
        JsPropertyMap<Object> win = Js.asPropertyMap(DomGlobal.window);
        if (!win.has(SAY)) return;
        Js.<SayFn>cast(win.get(SAY)).call(text == null ? "" : text);
    }

    public static boolean hosted() { return Js.asPropertyMap(DomGlobal.window).has(NOTE); }
}
