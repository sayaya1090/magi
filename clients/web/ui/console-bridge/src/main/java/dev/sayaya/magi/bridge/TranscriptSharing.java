package dev.sayaya.magi.bridge;

import elemental2.dom.DomGlobal;
import jsinterop.annotations.JsFunction;
import jsinterop.base.Js;
import jsinterop.base.JsPropertyMap;

/**
 * 대화(전사)와 턴 상태의 창 브리지 — 창당 1스트림 규칙의 나머지 반.
 *
 * 명단(RosterSharing)처럼 스트림의 소유자는 셸이다: ?d=로 조준된 /events의 기본 프레임
 * (전사 행 전체 배열)과 turn 프레임을 셸이 받아 이 문으로 흘린다. 화면 모듈은 구독만
 * 한다. 구독은 마지막 값을 재생한다 — 전사는 null("아직/못 읽음")도 흘린다: 첫 로드의
 * 실패는 화면이 말해야 한다.
 */
public final class TranscriptSharing {
    private static final String SUB = "__magi_transcript_subscribe";
    private static final String TURN = "__magi_turn_subscribe";

    private TranscriptSharing() {}

    @JsFunction
    public interface RowsFn { void call(Object rowsOrNull); }

    @JsFunction
    public interface RowsSubscribeFn { void call(RowsFn cb); }

    @JsFunction
    public interface TurnFn { void call(boolean open, double forSec); }

    @JsFunction
    public interface TurnSubscribeFn { void call(TurnFn cb); }

    /** 셸 측: 두 문을 건다. */
    public static void host(RowsSubscribeFn rows, TurnSubscribeFn turn) {
        JsPropertyMap<Object> win = Js.asPropertyMap(DomGlobal.window);
        win.set(SUB, rows);
        win.set(TURN, turn);
    }

    public static boolean hosted() { return Js.asPropertyMap(DomGlobal.window).has(SUB); }

    public static void subscribe(RowsFn cb) {
        JsPropertyMap<Object> win = Js.asPropertyMap(DomGlobal.window);
        if (!win.has(SUB)) return;
        Js.<RowsSubscribeFn>cast(win.get(SUB)).call(cb);
    }

    public static void subscribeTurn(TurnFn cb) {
        JsPropertyMap<Object> win = Js.asPropertyMap(DomGlobal.window);
        if (!win.has(TURN)) return;
        Js.<TurnSubscribeFn>cast(win.get(TURN)).call(cb);
    }
}
