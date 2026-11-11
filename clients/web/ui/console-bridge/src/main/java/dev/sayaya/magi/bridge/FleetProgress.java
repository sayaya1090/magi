package dev.sayaya.magi.bridge;

import jsinterop.annotations.JsPackage;
import jsinterop.annotations.JsType;

/**
 * 압축 진행의 센 값 — 코어 {@code event.Progress}, 명단 행의 {@code progress}. 어림이 아니다.
 * {@code total} 이 0 이면 몇 조각인지 모른다는 뜻이고, 막대는 그때 비율을 지어내지 않는다.
 */
@JsType(isNative = true, namespace = JsPackage.GLOBAL, name = "Object")
public class FleetProgress {
    public double done;
    public double total;
    public double chars;
    public double elapsedMs;
}
