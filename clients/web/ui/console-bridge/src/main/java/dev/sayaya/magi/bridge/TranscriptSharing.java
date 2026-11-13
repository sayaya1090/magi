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

    private static final String RECOVERY = "__magi_recovery_subscribe";

    /** 재시작이 남긴 것(recovery 프레임의 상태 객체) — 모르면 null. */
    @JsFunction
    public interface RecoveryFn { void call(Object stateOrNull); }

    @JsFunction
    public interface RecoverySubscribeFn { void call(RecoveryFn cb); }

    /**
     * 셸 측: 복구 상태의 문을 따로 건다. host(rows, turn)에 셋째 인자를 보태지 않은 것은 그 두
     * 문만 거는 호출자(데모·테스트 셸)가 이미 있어서다 — 문 하나가 늘었다고 그들이 깨질 까닭이 없다.
     */
    public static void hostRecovery(RecoverySubscribeFn recovery) {
        Js.asPropertyMap(DomGlobal.window).set(RECOVERY, recovery);
    }

    public static void subscribeRecovery(RecoveryFn cb) {
        JsPropertyMap<Object> win = Js.asPropertyMap(DomGlobal.window);
        if (!win.has(RECOVERY)) return;
        Js.<RecoverySubscribeFn>cast(win.get(RECOVERY)).call(cb);
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
