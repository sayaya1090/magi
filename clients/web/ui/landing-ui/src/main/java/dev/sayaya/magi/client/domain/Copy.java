package dev.sayaya.magi.client.domain;

import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * 이 페이지의 말 — 두 벌.
 *
 * 한 키의 두 말을 <b>한 줄에</b> 적는다. 언어별로 파일이나 블록을 갈라 두면 문장을 고칠 때
 * 한쪽만 고치게 되고, 그 사실은 그 말을 읽는 사람에게만 보인다(콘솔에서 실측된 그 모양:
 * 설정 화면만 한국어였다). 여기서는 빠뜨리면 줄이 짧아 보이고, 그것도 놓치면
 * {@code CopyTest} 가 센다.
 *
 * <p>팩을 내려받지 않는 이유: 랜딩은 데몬 없이 파일로만 서는 페이지이고, 두 말이 다 해야
 * 60줄쯤이라 첫 화면을 그리기 전에 회선을 한 번 더 기다릴 값이 없다.
 */
public final class Copy {
    private Copy() {}

    private static final Map<String, String[]> WORDS = new LinkedHashMap<>();

    private static void say(String key, String en, String ko) { WORDS.put(key, new String[]{en, ko}); }

    static {
        say("lang.switch", "Language", "언어");

        say("nav.what", "The problem", "문제");
        say("nav.council", "The council", "카운슬");
        say("nav.record", "The record", "기록");
        say("nav.fleet", "Companions", "컴패니언");
        say("nav.start", "Quick start", "빠른 시작");

        say("hero.eyebrow", "Terminal coding agent", "터미널 코딩 에이전트");
        say("hero.tagline",
            "A coding agent that is not allowed to declare itself finished.",
            "스스로 「다 했다」고 말할 수 없는 코딩 에이전트.");
        say("hero.lede",
            "In most agent loops the turn ends when the model stops calling tools. magi ends it "
            + "differently. The agent has to declare that it is done, and three council members read "
            + "the record — what actually ran, how it really exited, what changed on disk — before "
            + "that declaration is accepted.",
            "대개의 루프는 모델이 툴을 그만 부르면 턴이 끝난다. magi 는 다르게 끝낸다. 에이전트가 "
            + "끝났다고 <b>선언</b>해야 하고, 세 위원이 기록을 — 무엇이 돌았고, 실제로 어떻게 끝났고, "
            + "디스크에서 무엇이 달라졌는지를 — 읽은 뒤에야 그 선언이 받아들여진다.");
        say("hero.install", "Install", "설치");
        say("hero.note",
            "Apache-2.0 · pure Go, CGO-free single binary · runs against Ollama or any "
            + "OpenAI-compatible endpoint",
            "Apache-2.0 · 순수 Go, CGO 없는 단일 바이너리 · Ollama 든 OpenAI 호환 엔드포인트든");

        say("cta.start", "Quick start", "빠른 시작");
        say("cta.demo", "Open the live demo", "라이브 데모 열기");
        say("cta.manual", "Read the manual", "안내서 읽기");
        say("cta.arch", "Architecture", "아키텍처");
        say("cta.repo", "GitHub", "GitHub");

        say("sec.what.head", "When is the turn actually finished?", "턴은 언제 정말로 끝났나?");
        say("sec.what.lede",
            "Leave the answer implicit — a turn ends when the model stops calling tools — and a turn "
            + "that trailed off mid-thought looks exactly like one that is genuinely done. You get "
            + "both failure modes: agents that stop three quarters of the way through, and agents "
            + "that never stop at all.",
            "답을 암묵에 두면 — 모델이 툴을 그만 부르면 턴이 끝나는 것으로 두면 — 생각하다 만 턴과 "
            + "정말로 끝난 턴이 똑같이 보인다. 그래서 두 실패가 다 나온다. 4분의 3에서 멈추는 "
            + "에이전트와, 아예 멈추지 않는 에이전트.");
        say("figure.title", "A finish being refused", "거절당하는 종료 선언");
        say("figure.caption",
            "The case worth showing: the record does not back the claim, so the turn goes back to work.",
            "보여줄 값이 있는 쪽은 이 경우다 — 기록이 주장을 받쳐 주지 않아 턴이 작업으로 되돌아간다.");

        say("sec.council.head", "Three readings, then one closing call", "세 개의 읽기, 그리고 닫는 호출");
        say("sec.council.lede",
            "At the point where the loop would otherwise end, each member votes done, reject or "
            + "abstain, and a pure tally function turns those votes into one decision. They read the "
            + "same record; what differs is the lens each judges by and where it looks first.",
            "루프가 그냥 끝났을 자리에서 위원 셋이 done · reject · abstain 에 투표하고, 순수한 집계 "
            + "함수가 그 표를 결정 하나로 바꾼다. 셋은 같은 기록을 읽는다 — 다른 것은 무엇으로 "
            + "판정하는가(렌즈)와 어디부터 훑는가(경로)뿐이다.");
        say("council.melchior.lens", "correctness", "정확성");
        say("council.melchior.walk",
            "the task's literal words — exact values, formats, names — then the premises the work rests on",
            "과제의 리터럴 문구 — 정확한 값·형식·이름 — 그다음 그 작업이 딛고 선 전제");
        say("council.balthasar.lens", "verification", "검증");
        say("council.balthasar.walk",
            "for each thing that must work, the moment it actually ran and the output that came back",
            "돌아야 하는 것 하나하나에 대해, 그것이 실제로 돈 순간과 돌아온 출력");
        say("council.casper.lens", "completeness", "완결성");
        say("council.casper.walk",
            "every distinct thing the task asked for, including the one named once in passing",
            "과제가 요구한 서로 다른 부분 전부 — 지나가듯 한 번 불린 것까지");
        say("council.walk.head", "A walk before the verdict", "판정보다 앞서는 훑기");
        say("council.walk.body",
            "Before a member may state a decision it writes one line per requirement, marked "
            + "SATISFIED or UNSATISFIED and settled by a verbatim fragment of something a tool "
            + "returned — or by NO-EVIDENCE, which is an answer too. That field sits before the "
            + "verdict in the schema, so a reading cannot be assembled backwards from a conclusion "
            + "already reached. What the agent says about its own work settles nothing.",
            "위원은 판정을 말하기 전에 요구사항 하나에 한 줄씩 SATISFIED 나 UNSATISFIED 를 쓰고, "
            + "툴이 돌려준 것에서 그대로 떼어 온 조각이나 NO-EVIDENCE 로 결론짓는다 — NO-EVIDENCE "
            + "도 답이다. 이 칸은 스키마에서 판정보다 <b>앞</b>에 놓여, 이미 내려놓은 결론에서 읽기를 "
            + "거꾸로 조립할 수 없다. 에이전트가 제 작업에 대해 하는 말은 아무것도 결론짓지 못한다.");
        say("council.close.head", "The closing call", "닫는 호출");
        say("council.close.body",
            "A tally is three readings added up, and until it is added up nobody has read all three. "
            + "One closing call does — and its conclusion is clamped one way: it can turn a done into "
            + "continue, never the reverse. A gate that one more opinion could talk into finishing is "
            + "not a gate.",
            "집계는 세 개의 읽기를 더한 것이고, 더하기 전까지 셋을 다 읽은 사람은 없다. 닫는 호출이 "
            + "그 자리다 — 그리고 그 결론은 한 방향으로 클램프돼 있다. done 을 continue 로 바꿀 수는 "
            + "있어도 그 반대는 없다. 의견 하나가 더 붙어 끝낼 수 있는 게이트는 게이트가 아니다.");
        say("tally.col.rule", "Rule", "규칙");
        say("tally.col.when", "Finishes when…", "이럴 때 끝난다");
        say("tally.majority.when",
            "a strict majority of voting members say done; a tie continues",
            "투표한 위원의 과반이 done — 동수면 계속");
        say("tally.unanimous.when", "every member says done", "위원 전부가 done");
        say("tally.quorum.when", "at least k members say done", "k 명 이상이 done");
        say("tally.weighted.when", "the done-weight share meets threshold θ", "done 쪽 가중치 비중이 임계 θ 이상");
        say("tally.veto.when",
            "a named member can refuse any finish on its own",
            "지목된 위원 하나가 어떤 종료든 혼자 거부할 수 있다");
        say("tally.note",
            "Whatever you pick, an ambiguous outcome resolves to continue. A member that errors, "
            + "times out or returns something unparseable abstains instead of blocking the gate: a "
            + "flaky model degrades the vote, it cannot freeze the loop.",
            "무엇을 고르든 모호한 결과는 continue 로 풀린다. 에러가 나거나 시간이 넘거나 못 읽을 것을 "
            + "돌려준 위원은 게이트를 막는 대신 기권한다 — 흔들리는 모델은 표를 약하게 할 뿐 루프를 "
            + "얼리지 못한다.");

        say("sec.record.head", "The record, not the claim", "주장이 아니라 기록");
        say("sec.record.lede",
            "Members do not judge the agent's summary of its work. They judge it against what magi "
            + "itself recorded — and magi grants every tool call, so it already knows:",
            "위원은 에이전트가 제 작업을 요약한 말을 판정하지 않는다. magi 자신이 기록한 것에 대고 "
            + "판정한다 — magi 는 모든 툴 호출을 허가하므로 이미 알고 있다:");
        say("record.1",
            "every command that ran and how it really ended, down to which stage of a pipe failed",
            "무엇이 돌았고 실제로 어떻게 끝났는지 — 파이프의 어느 단이 실패했는지까지");
        say("record.2",
            "the agent's own edits this turn, as a per-file before → after diff",
            "이번 턴에 에이전트가 한 편집을, 파일마다 이전 → 이후로");
        say("record.3",
            "on a completion claim, a fresh read of the workspace: files modified since the task "
            + "began, background jobs still alive, and any path the record says was written that is "
            + "not on disk",
            "종료 선언에는 워크스페이스를 지금 다시 읽은 것 — 과제 시작 이후 수정된 파일, 아직 살아 "
            + "있는 백그라운드 작업, 그리고 기록은 썼다는데 디스크에 없는 경로");
        say("record.line",
            "A check written in advance can be wrong about the work. A record of what was granted "
            + "cannot be wrong about what ran.",
            "미리 써 둔 체크는 작업에 대해 틀릴 수 있다. 무엇을 허가했는지의 기록은 무엇이 돌았는지에 "
            + "대해 틀릴 수 없다.");

        say("sec.fleet.head", "More than one, and a console to watch them", "여럿, 그리고 그들을 보는 콘솔");
        say("sec.fleet.lede",
            "One magi bound to one workspace is a companion. Give it a name and a role in the repo's "
            + "own config and it becomes addressable by what it is for.",
            "워크스페이스 하나에 묶인 magi 하나가 <b>컴패니언</b>이다. 저장소의 설정에 이름과 역할을 "
            + "적으면, 무엇을 위한 것인지로 부를 수 있게 된다.");
        say("fleet.1.t", "companions", "companions");
        say("fleet.1.b",
            "lists the others, including what each workspace has learned — which is how a specialist "
            + "becomes visible to the rest.",
            "다른 컴패니언들을 나열한다 — 각 워크스페이스가 무엇을 익혔는지까지. 전문가가 나머지에게 "
            + "보이게 되는 길이다.");
        say("fleet.2.t", "hand_off", "hand_off");
        say("fleet.2.b",
            "gives one a piece of the work and carries on; the answer lands in your conversation when "
            + "it is finished.",
            "일의 한 조각을 넘기고 계속 간다. 답은 끝나는 대로 이 대화에 들어온다.");
        say("fleet.3.t", "meetings", "회의");
        say("fleet.3.b",
            "put several companions on one question, read-only, until each knows what to do.",
            "여러 컴패니언을 한 질문에 앉힌다 — 읽기 전용으로, 각자 무엇을 할지 알 때까지.");
        say("fleet.4.t", "no open port", "열린 포트 없음");
        say("fleet.4.b",
            "every daemon writes a record beside its socket, and that directory is the membership "
            + "list. Across machines it is the same records, traded over ssh — so magi opens no port "
            + "of its own and holds no credential of its own.",
            "데몬마다 제 소켓 옆에 레코드를 쓰고, 그 디렉토리가 곧 멤버십 목록이다. 머신을 건너도 같은 "
            + "레코드를 ssh 로 주고받는다 — 그래서 magi 는 제 포트를 열지 않고 제 자격증명을 갖지 않는다.");

        say("shot.companions.cap",
            "Every companion on your machines: what each is doing, and the rows waiting on an answer from you.",
            "이 머신들의 컴패니언 전부 — 각자 무엇을 하고 있는지, 그리고 당신의 답을 기다리는 줄.");
        say("shot.detail.cap",
            "One companion: the live transcript with true exit codes, and a dangerous command waiting for approval.",
            "컴패니언 하나 — 진짜 종료 코드가 적힌 라이브 전사, 그리고 승인을 기다리는 위험한 명령.");
        say("shot.meeting.cap",
            "Meetings: several companions on one question, then each conclusion goes out as work.",
            "회의 — 여러 컴패니언이 한 질문에 붙고, 각자의 결론이 일이 되어 나간다.");
        say("shot.board.cap",
            "A day of work as cards, a column per team, grouped by the label the agent gave each piece.",
            "하루치 일을 카드로 — 팀마다 한 열, 에이전트가 조각마다 붙인 라벨로 묶어서.");
        say("shot.demo", "Open the demo", "데모 열기");

        say("sec.feature.head", "What you get", "무엇을 얻나");
        say("feature.termination.t", "Consensus termination", "합의 종료");
        say("feature.termination.b",
            "Three members vote done / reject / abstain; a pure, unit-tested rule tallies them. A "
            + "reject feeds their aggregated feedback back in as the next instruction.",
            "위원 셋이 done · reject · abstain 에 투표하고, 순수하고 단위 시험된 규칙이 그것을 집계한다. "
            + "거절이면 모아진 피드백이 다음 지시가 되어 되돌아간다.");
        say("feature.walk.t", "A walk before the verdict", "판정보다 앞서는 훑기");
        say("feature.walk.b",
            "One line per requirement, settled by a verbatim tool result or by NO-EVIDENCE — before "
            + "the schema lets a member state a decision.",
            "요구사항 하나에 한 줄, 툴 결과의 원문 조각이나 NO-EVIDENCE 로 결론 — 스키마가 판정을 "
            + "허락하기 전에.");
        say("feature.record.t", "A record, not a claim", "주장이 아니라 기록");
        say("feature.record.b",
            "Which commands ran, their real exit — including which stage of a pipe failed — and which "
            + "files they wrote, plus a fresh read of the workspace on every done.",
            "무엇이 돌았는지, 파이프의 어느 단이 실패했는지까지 포함한 진짜 종료 코드, 무엇을 썼는지 — "
            + "그리고 done 마다 워크스페이스를 다시 읽은 것.");
        say("feature.console.t", "A console for many agents", "여럿을 보는 콘솔");
        say("feature.console.b",
            "Supervise every companion from a browser: interrupt, answer a question, approve a "
            + "command, read what they have learned — and get a push notification when one blocks.",
            "브라우저에서 컴패니언 전부를 감독한다 — 중단, 질문에 답, 명령 승인, 무엇을 익혔는지 읽기. "
            + "하나가 막히면 푸시로 알린다.");
        say("feature.loop.t", "An inspectable loop", "열어 볼 수 있는 루프");
        say("feature.loop.b",
            "Every turn is event-sourced to append-only JSONL, so /rewind, /fork, /replay and "
            + "/loopdiff are ordinary operations rather than features to be built.",
            "턴은 전부 추가 전용 JSONL 로 이벤트소싱된다 — 그래서 /rewind · /fork · /replay · /loopdiff "
            + "가 새로 지어야 할 기능이 아니라 평범한 조작이다.");
        say("feature.binaries.t", "Self-contained binaries", "자기완결 바이너리");
        say("feature.binaries.b",
            "Pure Go, CGO-free: the agent and the optional console, each a static binary. Copy either "
            + "anywhere and run it.",
            "순수 Go, CGO 없음 — 에이전트와 선택적 콘솔이 각각 정적 바이너리 하나다. 어디로 복사하든 "
            + "그대로 돈다.");

        say("sec.start.head", "Quick start", "빠른 시작");
        say("sec.start.lede",
            "Go 1.26+ to build, and an OpenAI-compatible backend. The default model runs on Ollama's "
            + "free cloud tier — no GPU, one sign-in — and any local model works too.",
            "빌드에 Go 1.26+, 그리고 OpenAI 호환 백엔드 하나. 기본 모델은 Ollama 의 무료 클라우드 "
            + "티어에서 돈다 — GPU 없이, 로그인 한 번 — 로컬 모델도 그대로 된다.");
        say("start.req", "Requirements", "필요한 것");
        say("start.build", "Build from source", "소스에서 빌드");
        say("start.run", "Run", "실행");
        say("start.more",
            "The manual carries the rest: configuration, profiles and sandboxes, plugins, MCP, the fleet.",
            "나머지는 안내서에 있다 — 설정, 프로파일과 샌드박스, 플러그인, MCP, 플릿.");

        say("foot.note",
            "This page is built from the repository it describes. The console demo beside it is the "
            + "real console, answered by a mock in the browser — no server, and every answer a fixture.",
            "이 페이지는 그것이 설명하는 저장소에서 함께 지어진다. 옆의 콘솔 데모는 진짜 콘솔이고, "
            + "브라우저 안의 목이 답한다 — 서버는 없고, 모든 답은 픽스처다.");
    }

    /**
     * 그 말의 그 낱말. 모르는 키는 <b>키 그대로</b> 돌려준다 — 콘솔의 tr() 과 같은 규칙이고,
     * 빠진 말이 빈칸이 아니라 눈에 띄는 문자열로 서게 하려는 것이다.
     */
    public static String word(Tongue tongue, String key) {
        String[] both = WORDS.get(key);
        if (both == null) return key;
        String said = both[tongue == Tongue.KO ? 1 : 0];
        return said == null || said.isEmpty() ? key : said;
    }

    /** 이 사전이 답할 수 있는 키 전부 — 모양이 묻지 않는 것이 남아 있는지 세는 자리(시험). */
    public static List<String> keys() { return new ArrayList<>(WORDS.keySet()); }
}
