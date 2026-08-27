package dev.sayaya.magi.client.domain;

/**
 * 아직 말이 되는 가장 큰 단위 하나로 줄인 시간 — s/m/h/d는 어느 언어 팩에서도 같다.
 * "4s"에 맞춘 표 열은 "4 seconds"를 못 담는다.
 */
public final class Spans {
    private Spans() {}

    public static String dur(int s) {
        return s < 60 ? s + "s" : s < 3600 ? Math.round(s / 60.0) + "m"
                : s < 86400 ? Math.round(s / 3600.0) + "h" : Math.round(s / 86400.0) + "d";
    }
}
