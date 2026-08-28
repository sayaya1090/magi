package dev.sayaya.magi.client.domain;

import java.util.HashMap;
import java.util.Iterator;
import java.util.Map;

/**
 * 갱신 버튼이 기억하는 것 — 소켓마다 <b>지난번에 무슨 일이 있었나</b>. DOM을 모른다.
 *
 * 기억이 버튼 곁이 아니라 여기 있는 이유(운영이 이 상태를 모듈 스코프에 둔 그 이유): 명단은
 * 몇 초마다 흐르고 그때마다 사실판이 다시 그려진다. 받는 중에 다시 그리면 새 버튼이 서고,
 * 끝난 fetch는 <b>사라진 버튼</b>을 다시 켜려 한다 — 잠긴 채로 굳는다. 그래서 상태는 그리는
 * 것보다 오래 산다.
 *
 * 세 가지만 안다:
 *   · 하는 중인가(busy) — 잠그는 근거이자, 같은 소켓에 두 번 보내지 않는 근거.
 *   · 무슨 말을 들었나(line) — 데몬이 답한 그대로다. 거부도 답이다("그 기계에서 하라").
 *   · 다시 눌러 볼 만한가(retry) — <b>아무도 아무 말도 하지 않았을 때</b>만 참이다.
 *     사유가 있는 거부에 버튼을 다시 세우면 같은 사유를 다시 받으라는 말이 된다.
 */
public final class Updates {
    private static final class Said {
        final boolean working;
        final String text;
        final boolean retry;
        Said(boolean working, String text, boolean retry) {
            this.working = working; this.text = text; this.retry = retry;
        }
    }

    private final Map<String, Said> per = new HashMap<>();

    /** 보냈다 — 답이 올 때까지 이 소켓은 잠긴다. */
    public void began(String socket, String working) {
        per.put(key(socket), new Said(true, working, false));
    }

    /**
     * 답이 왔다. said가 비면 회선이 끊긴 것이다 — 그때만 대신할 말을 세우고 다시 눌러 볼 수 있게 둔다.
     */
    public void ended(String socket, String said, String failed) {
        String heard = said == null ? "" : said.trim();
        per.put(key(socket), new Said(false, heard.isEmpty() ? failed : heard, heard.isEmpty()));
    }

    public boolean busy(String socket) {
        Said s = per.get(key(socket));
        return s != null && s.working;
    }

    /** 그 자리에 설 한 줄 — 없으면 빈 문자열이다(아직 아무 일도 없었다). */
    public String line(String socket) {
        Said s = per.get(key(socket));
        return s == null || s.text == null ? "" : s.text;
    }

    /**
     * 버튼이 서는가. 뒤처지지 않은 빌드에는 서지 않는다 — 최신인 것을 최신으로 만드는 버튼은
     * 눌러도 일어날 일이 없다. 이미 한 번 말을 들었으면, 다시 눌러 볼 만한 경우에만 그 말 곁에 남는다.
     */
    public boolean button(String socket, boolean behind) {
        Said s = per.get(key(socket));
        if (s != null && s.text != null && !s.text.isEmpty()) return s.retry && behind;
        return behind;
    }

    /**
     * 끝난 것만 잊는다 — 다른 컴패니언을 보러 떠날 때. 끝난 줄은 할 말을 다 했고, 다음 방문은
     * 깨끗하게 시작하는 것이 맞다. 받는 중인 것은 남긴다: 잊으면 돌아왔을 때 버튼이 다시 서고,
     * 아직 도는 갱신에 한 번 더 보내게 된다.
     */
    public void forgetFinished() {
        for (Iterator<Map.Entry<String, Said>> it = per.entrySet().iterator(); it.hasNext(); ) {
            if (!it.next().getValue().working) it.remove();
        }
    }

    private static String key(String socket) { return socket == null ? "" : socket; }
}
