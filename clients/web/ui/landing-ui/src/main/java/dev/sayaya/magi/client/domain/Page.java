package dev.sayaya.magi.client.domain;

import java.util.ArrayList;
import java.util.List;

/**
 * 페이지의 <b>모양</b> — 어떤 절이 어떤 순서로 서고, 절마다 무엇을 몇 개 묻는가.
 *
 * 말(i18n 팩)과 모양(여기)을 가르는 이유는 시험 하나 때문이다: 모양이 묻는 키를 두 말이 모두
 * 답하는지 기계가 셀 수 있다. 한쪽 말에만 문장을 더하는 것은 눈으로는 안 보이고, 그때
 * 화면에는 키 문자열이 그대로 뜬다.
 *
 * <p>여기 있는 <b>기계가 낸 글</b>(설치·빌드·실행 명령, 거절당한 선언의 전사)은 말에 들어
 * 있지 않다. 번역하면 그것은 더 이상 그 기계가 낸 글이 아니고, 붙여 넣어 돌릴 수도 없다.
 */
public final class Page {
    private Page() {}

    /** 위 띠의 문들 — 순서가 곧 화면의 순서다(각 항목이 제 절의 id 이기도 하다). */
    public static final String[] NAV = {"what", "council", "record", "fleet", "clients", "start"};

    /** 카운슬 위원 — 이름은 번역하지 않는다(사람 이름이다). 렌즈와 경로만 말이 있다. */
    public static final String[] MEMBERS = {"melchior", "balthasar", "casper"};

    /** 집계 규칙 — 왼쪽 칸은 설정에 적는 낱말 그대로라 번역하지 않는다. */
    public static final String[] TALLY = {"majority", "unanimous", "quorum", "weighted", "veto"};
    public static final String[] TALLY_RULE = {"majority", "unanimous", "quorum:k", "weighted:θ", "veto:Name"};

    /** 기록이 이미 아는 것 셋. */
    public static final String[] RECORD = {"record.1", "record.2", "record.3"};

    /** 컴패니언 절의 네 줄 — 앞의 셋은 툴 이름이라 제목을 번역하지 않는다. */
    public static final String[] FLEET = {"fleet.1", "fleet.2", "fleet.3", "fleet.4"};

    /** 무엇을 얻나. */
    public static final String[] FEATURES = {
        "termination", "walk", "record", "console", "companions", "knowledge",
        "tools", "guard", "complete", "loop", "update", "binaries",
    };

    /**
     * 사람이 앉는 자리 — 같은 컴패니언에 붙는 바깥 프로그램들(docs/CLIENTS).
     *
     * 그림이 있는 자리와 없는 자리가 있다. 없는 것은 저장소에 아직 사진이 없다는 뜻이라 빈
     * 문자열로 두고, 카드는 글만 세운다 — 남의 그림을 빌려다 채우면 그 자리가 실제로 어떻게
     * 생겼는지에 대해 거짓을 말하게 된다.
     */
    public static final String[] SEATS = {
        "terminal", "console", "jetbrains", "vscode", "visualstudio", "powerpoint", "excel", "word",
    };
    public static final String[] SEAT_IMG = {
        "img/tui-turn.png", "img/console-workspace.png", "img/seat-jetbrains.png", "",
        "", "img/seat-powerpoint.png", "", "",
    };
    /** 지어진 것인가, 짓고 있는 것인가 — 상태를 적지 않으면 목록이 약속으로 읽힌다. */
    public static final String[] SEAT_STATUS = {
        "shipped", "shipped", "shipped", "shipped", "building", "shipped", "shipped", "shipped",
    };

    /** 콘솔 그림 넷과 그것이 가리키는 데모의 자리. */
    public static final String[] SHOTS = {"companions", "detail", "meeting", "board"};
    public static final String[] SHOT_IMG = {
        "img/console-companions.png", "img/console-companion-detail.png",
        "img/console-meeting.png", "img/console-board.png",
    };
    public static final String[] SHOT_HREF = {
        "demo/", "demo/?d=%2Fdemo%2Fdesign.sock", "demo/?v=meet", "demo/?v=board",
    };

    /** 절 하나에 매인 것이 아니라 페이지 전체에 흩어져 있는 말들. */
    public static final String[] LOOSE = {
        "hero.eyebrow", "hero.tagline", "hero.lede", "hero.install", "hero.note",
        "cta.start", "cta.demo", "cta.manual", "cta.arch", "cta.repo", "lang.switch",
        "figure.title", "figure.caption",
        "council.walk.head", "council.walk.body", "council.close.head", "council.close.body",
        "tally.col.rule", "tally.col.when", "tally.note",
        "record.line", "shot.demo",
        "start.req", "start.build", "start.run", "start.more",
        "foot.note",
    };

    /** 설치 한 줄 — 붙여 넣어 돌리는 글이라 두 말이 같다. */
    public static final String INSTALL =
        "curl -fsSL https://raw.githubusercontent.com/sayaya1090/magi/main/scripts/install.sh | bash";

    public static final String BUILD =
        "make build        # CGO_ENABLED=0, version injected -> ./magi\n"
        + "make web          # the browser console        -> ./magi-web";

    public static final String RUN =
        "./magi                         # interactive TUI\n"
        + "./magi --daemon                # the engine, no UI\n"
        + "./magi --attach                # attach a terminal UI to this workspace\n"
        + "./magi-web                     # the console at 127.0.0.1:7777";

    /**
     * 거절당한 종료 선언 — 이 페이지가 있는 이유의 그림. 기계가 낸 글이라 번역하지 않는다.
     * README 의 그 예와 같은 것을 짧게 줄였다.
     */
    public static final String[] TRANSCRIPT = {
        "you > add a --dry-run flag to the deploy command",
        "",
        "  ... agent reads cmd/deploy.go, edits it, runs go build ...",
        "",
        "  council {complete: true}          the agent says it is finished",
        "",
        "  -- WHAT MAGI OBSERVED",
        "     changed:   cmd/deploy.go",
        "     ran clean: go build ./...",
        "  -- THE WORKSPACE RIGHT NOW (read just now, not from the record)",
        "     cmd/deploy.go - 4,102 bytes, modified 12s ago",
        "",
        "  Balthasar [verification]  nothing runs the new flag",
        "                            `go test ./cmd` was never run",
        "  -> not accepted; the agent keeps working",
        "",
        "  ... agent adds a test, runs go test ...",
        "",
        "  council {complete: true}   ->   accepted   turn over",
    };

    /** 이 페이지가 말에게 묻는 키 전부 — 두 말이 다 답하는지 세는 자리(시험). */
    public static List<String> keys() {
        List<String> all = new ArrayList<>();
        for (String one : LOOSE) all.add(one);
        for (String one : NAV) {
            all.add("nav." + one);
            all.add("sec." + one + ".head");
            all.add("sec." + one + ".lede");
        }
        all.add("sec.feature.head");
        for (String one : MEMBERS) {
            all.add("council." + one + ".lens");
            all.add("council." + one + ".walk");
        }
        for (String one : TALLY) all.add("tally." + one + ".when");
        for (String one : RECORD) all.add(one);
        for (String one : FLEET) {
            all.add(one + ".t");
            all.add(one + ".b");
        }
        for (String one : FEATURES) {
            all.add("feature." + one + ".t");
            all.add("feature." + one + ".b");
        }
        for (String one : SHOTS) all.add("shot." + one + ".cap");
        for (String one : SEATS) {
            all.add("seat." + one + ".t");
            all.add("seat." + one + ".b");
        }
        all.add("seat.shipped");
        all.add("seat.building");
        return all;
    }
}
