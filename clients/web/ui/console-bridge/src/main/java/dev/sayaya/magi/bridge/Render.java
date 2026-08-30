package dev.sayaya.magi.bridge;

import elemental2.dom.HTMLElement;
import jsinterop.annotations.JsFunction;

/**
 * 화면 모듈이 셸에 건네는 "이 프레임에 그려라" 콜백 (handbook domain.Render와 동일 계약).
 * JsFunction이라 GWT 모듈 경계(별도 컴파일 산출물)를 넘어 호출된다.
 */
@JsFunction
public interface Render {
    boolean onInvoke(HTMLElement frame);
}
