package dev.sayaya.magi.client.usecase;

import java.util.Map;

/**
 * 이 페이지의 말을 어디서 읽어 오는가.
 *
 * 콘솔이 하는 것과 같은 모양이다(bridge/Labels): 말은 코드가 아니라 <b>팩</b>이고, 고른 말에
 * 해당하는 파일 하나를 읽는다. 자바 상수로 들고 있던 때는 문장 한 줄을 고치는 데 GWT 재컴파일이
 * 필요했고, 번역을 맡기려면 자바 파일을 건네야 했다.
 */
public interface WordSource {
    /** 못 읽으면 null 을 건넨다 — 부르는 쪽이 키 폴백으로 산다(빈 화면 금지). */
    void load(String code, Taken taken);

    interface Taken { void take(Map<String, String> pack); }
}
