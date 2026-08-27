package dev.sayaya.magi.bridge;

import jsinterop.annotations.JsOverlay;
import jsinterop.annotations.JsPackage;
import jsinterop.annotations.JsType;
import jsinterop.base.Js;
import jsinterop.base.JsPropertyMap;

/**
 * 셸이 화면 모듈에 건네는 "지금 무엇을 보고 있나" — handbook의 UriSharing에 대응.
 * 창 브리지(CompanionSharing)를 건너므로 네이티브 Object다 — GWT 모듈은 각자 딴
 * 이름공간으로 컴파일돼, 일반 자바 클래스는 경계를 넘는 순간 남의 클래스다.
 *
 * type은 셸이 이미 해석한 타입 카탈로그 키다(무선언 행은 "1"=코딩에이전트로 읽는다) —
 * 타입 전용 UI 모듈도 이 계약만 알고 로드된다(타입 → 모듈 해석은 셸의 몫).
 */
@JsType(isNative = true, namespace = JsPackage.GLOBAL, name = "Object")
public class CompanionContext {
    public String socket; // ?d= — 어느 컴패니언인가
    public String peer;   // ?p= — 어느 콘솔을 거쳐서인가 (없으면 null=로컬)
    public String type;   // 해석된 타입 키 — companion-ui(기본)면 "1"
    public String past;   // ?past= — null이면 지금 대화, ""면 지난 일 목록, 값이면 그 세션

    @JsOverlay
    public static CompanionContext of(String socket, String peer, String type, String past) {
        JsPropertyMap<Object> o = JsPropertyMap.of();
        o.set("socket", socket);
        o.set("peer", peer);
        o.set("type", type);
        o.set("past", past);
        return Js.uncheckedCast(o);
    }
}
