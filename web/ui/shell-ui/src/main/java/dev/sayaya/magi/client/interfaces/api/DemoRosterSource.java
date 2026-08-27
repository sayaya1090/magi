package dev.sayaya.magi.client.interfaces.api;

import dev.sayaya.magi.bridge.FleetAgent;
import dev.sayaya.magi.client.usecase.RosterSource;
import elemental2.core.Global;
import elemental2.dom.DomGlobal;
import jsinterop.base.Js;

import javax.inject.Inject;
import javax.inject.Singleton;

/**
 * 데몬 없이 도는 셸 — 정적 데모의 명단과 전사.
 *
 * 명단은 <b>이 모듈의 것</b>이다: /fleet과 /events의 주인이 셸 하나이므로, 그 목도 여기
 * 하나뿐이다. 다른 화면들은 이 명단을 브리지로 받는다 — 진짜 콘솔에서와 똑같이.
 */
@Singleton
public class DemoRosterSource implements RosterSource {
    private Listener listener;
    private String aimed = null;

    @Inject
    public DemoRosterSource() {}

    @Override
    public void start(Listener l) {
        listener = l;
        l.link(true);
        push();
    }

    @Override
    public void aim(String socket, String peer) {
        aimed = socket;
        push();
    }

    @Override
    public void facts(java.util.function.Consumer<Object> consoleInfo, java.util.function.Consumer<Object> caps) {
        consoleInfo.accept(Global.JSON.parse("{\"user\":\"you\",\"host\":\"devbox\",\"version\":\"dev\"," +
                "\"embed\":\"nomic-embed-text\"}"));
        // 데모의 사람은 전부 할 수 있다 — 무엇이 가려지는지가 아니라 무엇이 있는지를 보이는 자리다.
        caps.accept(Global.JSON.parse("[\"read\",\"answer\",\"prompt\",\"curate\",\"configure\",\"shell\",\"admin\"]"));
    }

    @Override
    public void refresh() { push(); }

    private void push() {
        listener.roster(Js.uncheckedCast(Global.JSON.parse(FLEET)));
        if (aimed == null || aimed.isEmpty()) {
            listener.transcript(null);
            listener.turn(false, 0);
            return;
        }
        listener.transcript(Global.JSON.parse(TRANSCRIPT));
        // 기다리는 컴패니언의 턴은 열려 있지 않다 — 무엇을 물었는지가 화면의 사실이다.
        listener.turn(!aimed.contains("docs"), 42);
    }

    /** 아홉이면 표가 표처럼 읽힌다 — 하나짜리 데모는 이 화면이 무엇을 위한 것인지 못 보여 준다. */
    private static final String FLEET = "["
            + "{\"socket\":\"/demo/build.sock\",\"name\":\"build\",\"role\":\"keeps the build green\","
            + "\"team\":\"core\",\"host\":\"devbox\",\"instance\":\"you@devbox\",\"workdir\":\"/Users/you/work/app\","
            + "\"session\":\"s_demo1\",\"pid\":4242,\"version\":\"dev\",\"live\":true,\"here\":true,"
            + "\"state\":\"working\",\"steps\":3,\"idle\":4,\"model\":\"gpt-oss:120b\",\"permission\":\"ask\","
            + "\"task\":\"run the migration and report what changed\","
            + "\"doing\":\"bash: psql -f migrations/0421.sql (12s)\",\"planDone\":1,\"planTotal\":3},"
            + "{\"socket\":\"/demo/docs.sock\",\"name\":\"docs\",\"role\":\"writes the manuals\","
            + "\"team\":\"core\",\"host\":\"devbox\",\"instance\":\"you@devbox\",\"workdir\":\"/Users/you/work/docs\","
            + "\"session\":\"s_demo2\",\"pid\":4243,\"version\":\"dev\",\"live\":true,\"here\":true,"
            + "\"state\":\"waiting\",\"steps\":7,\"idle\":31,\"model\":\"gpt-oss:120b\",\"permission\":\"ask\","
            + "\"asking\":\"may I drop the deprecated pages?\",\"askId\":\"call_demo\",\"askKind\":\"permission\","
            + "\"askIndex\":1,\"askTotal\":1,\"task\":\"prune the manual\","
            + "\"report\":[{\"key\":\"why\",\"text\":\"they redirect to the new guide and nothing links them\"}]},"
            + "{\"socket\":\"/demo/design.sock\",\"name\":\"design\",\"role\":\"the design system\","
            + "\"team\":\"frontend\",\"host\":\"devbox\",\"instance\":\"you@devbox\",\"workdir\":\"/Users/you/work/design\","
            + "\"session\":\"s_demo3\",\"live\":true,\"here\":true,\"state\":\"waiting\",\"steps\":2,\"idle\":96,"
            + "\"asking\":\"which surface should the empty state sit on?\",\"askId\":\"call_demo2\","
            + "\"askKind\":\"question\",\"askOptions\":[\"surface\",\"surface-container\"],\"askIndex\":1,\"askTotal\":2},"
            + "{\"socket\":\"/demo/infra.sock\",\"name\":\"infra\",\"role\":\"the deploy pipeline\","
            + "\"team\":\"platform\",\"host\":\"devbox\",\"instance\":\"you@devbox\",\"workdir\":\"/Users/you/work/infra\","
            + "\"live\":true,\"here\":true,\"state\":\"idle\",\"steps\":0,\"idle\":900,\"task\":\"nothing since the release\"},"
            + "{\"socket\":\"/demo/review.sock\",\"name\":\"review\",\"role\":\"reads the diffs\","
            + "\"team\":\"core\",\"host\":\"devbox\",\"instance\":\"you@devbox\",\"live\":true,\"here\":true,"
            + "\"state\":\"working\",\"steps\":11,\"idle\":2,\"task\":\"look over the migration\","
            + "\"doing\":\"read: internal/app/loop.go\",\"waiting\":2,\"handling\":true},"
            + "{\"socket\":\"/demo/watch.sock\",\"name\":\"watch\",\"role\":\"keeps an eye on CI\","
            + "\"team\":\"platform\",\"host\":\"devbox\",\"instance\":\"you@devbox\",\"live\":true,\"here\":true,"
            + "\"state\":\"idle\",\"steps\":0,\"idle\":120},"
            + "{\"socket\":\"/demo/data.sock\",\"name\":\"data\",\"role\":\"the warehouse\",\"team\":\"data\","
            + "\"host\":\"devbox\",\"instance\":\"you@devbox\",\"live\":true,\"here\":true,\"state\":\"lost\","
            + "\"steps\":4,\"idle\":600,\"task\":\"backfill the events table\"},"
            + "{\"socket\":\"/demo/mobile.sock\",\"name\":\"mobile\",\"role\":\"the phone app\","
            + "\"team\":\"frontend\",\"host\":\"laptop\",\"instance\":\"sam@laptop\",\"live\":true,"
            + "\"state\":\"working\",\"steps\":1,\"idle\":8,\"task\":\"the tab bar on small phones\"},"
            + "{\"socket\":\"/demo/away.sock\",\"name\":\"away\",\"role\":\"on another machine\","
            + "\"team\":\"core\",\"host\":\"buildbox\",\"instance\":\"sam@buildbox\",\"elsewhere\":true,"
            + "\"live\":true,\"state\":\"working\",\"steps\":5,\"idle\":15}]";

    /** 전사는 화면이 그릴 줄 아는 것을 한 번씩 보인다: 말·생각·도구(성공과 실패)·디프. */
    private static final String TRANSCRIPT = "["
            + "{\"who\":\"user\",\"text\":\"run the migration and tell me what changed\"},"
            + "{\"who\":\"thinking\",\"text\":\"read the migration first\\nthen ask before running it\"},"
            + "{\"who\":\"tool\",\"tool\":\"bash\",\"args\":\"{\\\"command\\\":\\\"cat migrations/0421.sql\\\"}\","
            + "\"out\":\"alter table sessions add column title text;\",\"ok\":true},"
            + "{\"who\":\"tool\",\"tool\":\"edit\",\"args\":\"{\\\"path\\\":\\\"internal/app/loop.go\\\"}\","
            + "\"diff\":\"--- a/internal/app/loop.go\\n+++ b/internal/app/loop.go\\n@@ -1 +1 @@\\n-old\\n+new\",\"ok\":false},"
            + "{\"who\":\"assistant\",\"text\":\"one statement, additive — asking before running it\"}]";
}
