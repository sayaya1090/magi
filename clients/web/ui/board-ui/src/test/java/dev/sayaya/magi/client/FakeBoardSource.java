package dev.sayaya.magi.client;

import dev.sayaya.magi.client.usecase.BoardSource;
import elemental2.core.Global;
import elemental2.core.JsDate;
import jsinterop.base.Js;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.function.Consumer;

/**
 * 두 팀(core=둘·docs=하나)과 오늘의 일들 — 지금 도는 것 하나, 라벨 단 것 하나,
 * 어제로 걸친 밤일 하나(오늘과 어제 양쪽에 보여야 한다). 타임스탬프는 Z 없이 —
 * 보드의 하루는 읽는 사람의 시계다(dayOf가 로컬로 접는다).
 */
@Singleton
public class FakeBoardSource implements BoardSource {
    private final String today;
    private final String yesterday;

    @Inject
    public FakeBoardSource() {
        JsDate now = new JsDate();
        double local = JsDate.now() - now.getTimezoneOffset() * 60000d;
        today = new JsDate(local).toISOString().substring(0, 10);
        yesterday = new JsDate(local - 86400000d).toISOString().substring(0, 10);
    }

    @Override
    public void fleet(Consumer<Object> cb) {
        cb.accept(Global.JSON.parse(
                "[{\"socket\":\"/a\",\"name\":\"build\",\"team\":\"core\",\"state\":\"working\",\"idle\":1}," +
                "{\"socket\":\"/b\",\"name\":\"test\",\"team\":\"core\",\"state\":\"idle\",\"idle\":10}," +
                "{\"socket\":\"/c\",\"name\":\"docs\",\"state\":\"idle\",\"idle\":5}]"));
    }

    @Override
    public void history(String socket, String peer, Consumer<Object> cb) {
        if ("/a".equals(socket)) {
            cb.accept(Global.JSON.parse(
                    "[{\"id\":\"s1\",\"title\":\"fix the retry storm\",\"started\":\"" + today + "T09:00:00\"," +
                    "\"model\":\"gpt-oss:120b\",\"labels\":[\"retries\",\"cache\"]," +
                    "\"ended\":\"" + today + "T09:02:00\"}," +
                    "{\"id\":\"s2\",\"title\":\"overnight soak\",\"started\":\"" + yesterday + "T23:00:00\"," +
                    "\"ended\":\"" + today + "T01:10:00\"}]"));
        } else if ("/b".equals(socket)) {
            cb.accept(Global.JSON.parse(
                    "[{\"id\":\"s3\",\"title\":\"still going\",\"started\":\"" + today + "T08:00:00\"," +
                    "\"ended\":\"\",\"current\":true}]"));
        } else {
            cb.accept(Global.JSON.parse("[]"));
        }
    }
}
