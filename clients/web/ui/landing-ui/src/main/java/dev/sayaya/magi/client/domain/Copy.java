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
        say("lang.switch", "Change language", "언어 변경");

        say("nav.what", "The problem", "문제");
        say("nav.council", "The council", "카운슬");
        say("nav.record", "The record", "기록");
        say("nav.fleet", "Companions", "다중 에이전트");
        say("nav.start", "How to start", "실행 방법");
        say("nav.clients", "Supported clients", "지원 환경");


        say("hero.eyebrow", "Terminal coding agent", "터미널 코딩 에이전트");
        say("hero.tagline",
            "A coding agent that cannot declare itself finished.",
            "스스로 「다 했다」고 선언할 수 없는 코딩 에이전트.");
        say("hero.lede",
            "Most agent loops end when the model stops calling tools. magi requires the agent to explicitly "
            + "declare it is done. Three council members then review the record — what ran, real exit codes, "
            + "and disk changes — before accepting the declaration.",
            "대부분의 에이전트는 모델이 툴 호출을 멈추면 턴을 끝낸다. magi는 에이전트가 먼저 끝났다고 "
            + "<b>선언</b>해야 한다. 그러면 세 명의 카운슬이 무엇이 실행됐고 어떻게 끝났는지, 디스크에서 바뀐 "
            + "것이 무엇인지 기록을 읽고 승인한다.");
        say("hero.install", "Installation", "설치 안내");
        say("hero.note",
            "Apache-2.0 · Pure Go, single static binary · Runs with Ollama or any OpenAI-compatible API",
            "Apache-2.0 · CGO 없는 순수 Go 단일 바이너리 · Ollama 및 OpenAI 호환 API 지원");

        say("cta.start", "How to start", "실행 방법");
        say("cta.demo", "Open the live demo", "라이브 데모 열기");
        say("cta.manual", "Read the manual", "안내서 읽기");
        say("cta.arch", "Architecture", "아키텍처");
        say("cta.repo", "GitHub", "GitHub");

        say("sec.what.head", "When does a turn truly end?", "턴은 언제 진짜로 끝나는가?");
        say("sec.what.lede",
            "If a turn ends implicitly when the model stops calling tools, a stalled agent looks identical "
            + "to a finished one. This causes two common failures: agents stopping halfway, and agents "
            + "never stopping.",
            "모델이 툴 호출을 멈출 때 턴이 끝난다고 암묵적으로 넘기면, 생각하다 만 턴과 진짜 끝난 턴이 똑같아 "
            + "보인다. 그래서 도중에 멈추는 에이전트와 영영 멈추지 않는 에이전트가 생긴다.");
        say("figure.title", "A rejected completion claim", "반려되는 종료 선언");
        say("figure.caption",
            "The record does not support the agent's claim, so it is sent back to work.",
            "기록이 주장을 뒷받침하지 못하면 선언은 반려되고 에이전트는 하던 일로 돌아간다.");

        say("sec.council.head", "Three lenses, one decision", "세 명의 카운슬과 닫는 호출");
        say("sec.council.lede",
            "Where standard loops end, three council members vote done, reject, or abstain. A pure function "
            + "tallies these votes into a final decision. They all read the same record, but each evaluates "
            + "it through a different lens and starts looking from a different angle.",
            "루프가 끝날 지점에서 세 멤버가 done, reject, abstain 중 하나에 투표하고, 순수 함수가 이 표를 "
            + "집계한다. 셋은 같은 기록을 읽지만 판정하는 기준(렌즈)과 먼저 훑는 곳(경로)이 다르다.");
        say("council.melchior.lens", "correctness", "정확성");
        say("council.melchior.walk",
            "The task's exact words (values, formats, names) and the premises of the work.",
            "요청된 정확한 값, 형식, 이름과 작업이 딛고 선 전제 확인.");
        say("council.balthasar.lens", "verification", "검증");
        say("council.balthasar.walk",
            "The exact moment each required action ran and the actual output it returned.",
            "작동해야 할 명령들이 실제로 실행된 순간과 그 결과 검토.");
        say("council.casper.lens", "completeness", "완결성");
        say("council.casper.walk",
            "Every distinct requirement requested, even those mentioned only in passing.",
            "스치듯 한 번 언급된 부분까지 포함해 과제가 요구한 모든 사항 점검.");
        say("council.walk.head", "A walk before the verdict", "판정보다 앞서는 훑기");
        say("council.walk.body",
            "Before voting, each member checks the requirements line by line. Each is marked SATISFIED or "
            + "UNSATISFIED based strictly on verbatim tool outputs, or NO-EVIDENCE. This evaluation is placed "
            + "<b>before</b> the verdict in the schema, preventing retroactive justification. The agent's own "
            + "summary is ignored.",
            "멤버는 판정을 내리기 전에 요구사항을 한 줄씩 짚는다. 툴 실행 결과에서 발췌한 원문을 근거로 "
            + "SATISFIED, UNSATISFIED, 또는 NO-EVIDENCE를 매긴다. 이 검토는 스키마에서 판정보다 <b>앞</b>에 "
            + "놓여 결론에 맞춰 근거를 억지로 끼워 맞출 수 없다. 에이전트가 스스로 쓴 요약은 근거가 되지 못한다.");
        say("council.close.head", "The closing call", "닫는 호출");
        say("council.close.body",
            "The final tally reads all three reviews. Its decision only clamps in one direction: it can turn "
            + "a done into a continue, but never the reverse. A gate that can be talked into opening is not a gate.",
            "세 멤버가 투표한 뒤 닫는 호출이 세 의견을 모아 읽는다. 이 결론은 한 방향으로만 닫힌다. done을 "
            + "continue로 뒤집을 수는 있지만 그 반대는 불가능하다. 설득해서 열 수 있는 게이트는 게이트가 아니다.");
        say("tally.col.rule", "Rule", "규칙");
        say("tally.col.when", "Finishes when…", "이럴 때 끝난다");
        say("tally.majority.when",
            "a strict majority of voting members say done; a tie continues",
            "투표한 멤버의 과반이 done (동수면 계속)");
        say("tally.unanimous.when", "every member says done", "멤버 전원이 done");
        say("tally.quorum.when", "at least k members say done", "최소 k명이 done");
        say("tally.weighted.when", "the done-weight share meets threshold θ", "done 가중치 비중이 임계값 θ 이상");
        say("tally.veto.when",
            "a named member can refuse any finish on its own",
            "지목된 멤버가 혼자서 완료를 거부할 수 있음");
        say("tally.note",
            "Ambiguous outcomes always resolve to continue. A member that errors, times out, or returns "
            + "unparseable data simply abstains. Flaky models can weaken the vote but never freeze the loop.",
            "모호한 결과는 늘 계속(continue)으로 처리된다. 에러, 시간 초과, 파싱 실패를 낸 멤버는 "
            + "기권한다. 불안정한 모델은 투표를 약하게 만들 수는 있어도 루프를 멈출 수는 없다.");

        say("sec.record.head", "The record, not the claim", "주장이 아니라 기록");
        say("sec.record.lede",
            "Members do not judge the agent by its own summary. They verify against magi's internal record. "
            + "Because magi executes every tool call, it knows:",
            "멤버는 에이전트가 적어낸 요약에 기대지 않고 magi가 직접 남긴 기록을 근거로 판정한다. 모든 "
            + "툴 실행을 magi가 직접 처리하므로 이미 다음 사실들을 알고 있다:");
        say("record.1",
            "Every command that ran and its true exit code, including which pipe stage failed.",
            "실행된 모든 명령과 실제 종료 상태 (파이프의 어느 단계가 실패했는지 포함).");
        say("record.2",
            "The exact file edits made during the turn, shown as before-and-after diffs.",
            "해당 턴에 에이전트가 수정한 파일들의 수정 전후 변경 사항(diff).");
        say("record.3",
            "A fresh workspace scan on completion: newly modified files, running background jobs, and paths "
            + "the agent claims to have written that are missing.",
            "종료 선언 시점의 워크스페이스 상태: 작업 시작 후 수정된 파일, 아직 도는 백그라운드 작업, "
            + "썼다고 기록됐지만 디스크에 없는 경로.");
        say("record.line",
            "A predefined test can misunderstand the work. A record of executed commands cannot lie about what ran.",
            "미리 작성된 검사 코드는 작업 내용을 잘못 정해 검사할 수 있지만, 시스템이 직접 남긴 실행 기록은 틀릴 수 없다.");

        say("sec.fleet.head", "More than one agent", "여럿을 함께 띄우기");
        say("sec.fleet.lede",
            "A magi bound to a workspace is a <b>companion</b>. Give it a role in the project config, and it "
            + "becomes addressable by its purpose.",
            "워크스페이스에 묶인 magi 인스턴스를 <b>컴패니언</b>이라 부른다. 프로젝트 설정에 역할과 이름을 "
            + "적어두면 목적에 따라 부를 수 있다.");
        say("fleet.1.t", "companions", "companions");
        say("fleet.1.b",
            "Lists all companions and what they have learned, making specialists visible to the rest.",
            "다른 컴패니언들과 각자가 익힌 내용을 나열해 누가 어떤 일에 전문성이 있는지 보여준다.");
        say("fleet.2.t", "hand_off", "hand_off");
        say("fleet.2.b",
            "Hands off a piece of work and carries on. The result arrives in your conversation when finished.",
            "작업 일부를 다른 컴패니언에게 넘기고 본인 일을 계속한다. 결과는 끝나는 대로 대화에 합류한다.");
        say("fleet.3.t", "meetings", "회의");
        say("fleet.3.b",
            "Puts several companions in a read-only room to discuss a question until everyone knows their task.",
            "여러 컴패니언을 한 질문에 모아 각자 할 일을 파악할 때까지 논의하게 한다.");
        say("fleet.4.t", "no open port", "열린 포트 없음");
        say("fleet.4.b",
            "Every daemon logs a record beside its socket, which acts as the membership list. Across machines, "
            + "these records sync over ssh, so magi opens no ports and needs no credentials.",
            "모든 데몬이 자기 소켓 옆에 기록을 남기며 이것이 멤버십 목록이 된다. 머신 간 동기화도 ssh로 "
            + "처리되므로 별도로 열린 포트나 자격 증명이 필요 없다.");

        say("shot.companions.cap",
            "All local companions: their current status and tasks waiting for your input.",
            "내 머신의 모든 컴패니언 목록: 각자의 상태와 사용자 입력을 기다리는 항목들.");
        say("shot.detail.cap",
            "A single companion's live transcript with real exit codes and a command pending approval.",
            "컴패니언의 상세 화면: 실제 종료 코드가 포함된 실시간 로그와 사용자 승인을 기다리는 명령.");
        say("shot.meeting.cap",
            "Meetings: multiple companions discuss a problem and produce individual action plans.",
            "회의 화면: 여러 컴패니언이 문제를 논의하고 각자의 작업 계획을 도출한다.");
        say("shot.board.cap",
            "Tasks organized into cards, separated by team, and grouped by agent-assigned labels.",
            "팀별로 정리된 하루의 작업 보드: 에이전트가 남긴 라벨을 기준으로 묶인 카드들.");
        say("shot.demo", "Open the demo", "데모 열기");

        say("sec.clients.head", "One companion, multiple clients", "하나의 컴패니언, 다양한 클라이언트");
        say("sec.clients.lede",
            "Clients are external programs that attach to a companion using the same socket. If a client can "
            + "access the log file, it reads it directly instead of asking the daemon.",
            "클라이언트는 컴패니언에 붙는 외부 프로그램이다. 모두 같은 소켓으로 통신하며, 로그 파일에 "
            + "접근할 수 있는 클라이언트는 데몬을 거치지 않고 직접 로그를 읽는다.");
        say("seat.shipped", "shipped", "출시됨");
        say("seat.building", "being built", "짓는 중");
        say("seat.terminal.t", "The terminal", "터미널");
        say("seat.terminal.b",
            "magi runs the engine and TUI together. magi --attach connects to an existing daemon in the "
            + "workspace. magi -p runs a single turn and exits.",
            "magi는 엔진과 TUI 화면을 함께 띄운다. magi --attach는 현재 워크스페이스에 도는 데몬에 접속하고, "
            + "magi -p는 한 턴만 실행한 뒤 종료된다.");
        say("seat.console.t", "The browser console", "브라우저 콘솔");
        say("seat.console.b",
            "magi-web displays all companions across connected machines on one screen. It binds locally and "
            + "requires no separate login.",
            "magi-web은 현재 머신과 연결된 모든 머신의 컴패니언을 한 화면에 보여준다. 로컬 루프백에 "
            + "바인드되며 자체 로그인을 요구하지 않는다.");
        say("seat.jetbrains.t", "JetBrains plugin", "JetBrains 플러그인");
        say("seat.jetbrains.b",
            "An IDE window acts as a companion. The transcript lives in the tool window, and the IDE itself "
            + "becomes a tool for the agent to read and edit open buffers.",
            "IDE 창 하나가 컴패니언 역할을 한다. 툴 윈도우에 대화가 표시되고, 에이전트가 IDE에 열린 버퍼를 "
            + "직접 읽고 수정한다.");
        say("seat.vscode.t", "VS Code extension", "VS Code 확장");
        say("seat.vscode.b",
            "Provides the transcript, plans, approvals, and composer in VS Code, connecting to the same "
            + "universal socket.",
            "VS Code에서도 동일하게 작동한다. 모든 클라이언트가 쓰는 소켓을 통해 대화, 계획, 승인 기능이 "
            + "연동된다.");
        say("seat.visualstudio.t", "Visual Studio extension", "Visual Studio 확장");
        say("seat.visualstudio.b",
            "The core layer is ready, but the IDE UI is not yet built. It is listed here as a planned addition.",
            "코어 통신부는 완성되었고 IDE UI 층은 아직 짓는 중이다. 작업이 예정된 항목을 명확히 밝히기 위해 "
            + "남겨둔다.");
        say("seat.powerpoint.t", "PowerPoint add-in", "PowerPoint 애드인");
        say("seat.powerpoint.b",
            "The task pane binds to the deck's companion, exposing slide-editing tools via MCP. Every open "
            + "deck has its own conversation.",
            "문서별 컴패니언에 작업창이 붙고 슬라이드 편집 도구가 MCP를 통해 제공된다. 열려 있는 덱마다 독립적인 "
            + "대화가 유지된다.");
        say("seat.excel.t", "Excel add-in", "Excel 애드인");
        say("seat.excel.b",
            "The identical pane, but controlling sheets, ranges, and formulas instead of slides.",
            "같은 작업창에서 슬라이드 대신 시트, 범위, 수식을 다룬다.");
        say("seat.word.t", "Word add-in", "Word 애드인");
        say("seat.word.b",
            "Applies the same model to paragraphs and styles. All three Office add-ins run from a single "
            + "process and port.",
            "문단과 스타일 편집에 같은 방식을 쓴다. 세 개의 Office 애드인이 하나의 프로세스와 포트를 공유한다.");

        say("sec.feature.head", "What you get", "무엇을 얻나");
        say("feature.termination.t", "Consensus termination", "합의 종료");
        say("feature.termination.b",
            "Three members vote on completion. A pure, unit-tested function tallies votes. If rejected, "
            + "their feedback becomes the next prompt.",
            "세 멤버가 종료 여부를 투표하고 단위 테스트된 함수가 집계한다. 반려되면 지적된 내용이 합쳐져 다음 "
            + "작업 지시로 들어간다.");
        say("feature.walk.t", "A walk before the verdict", "판정보다 앞서는 훑기");
        say("feature.walk.b",
            "Requirements are mapped line-by-line to verbatim tool outputs before the schema permits a final vote.",
            "요구사항마다 툴 실행 결과를 원문 그대로 대조한 뒤에야 투표할 수 있다.");
        say("feature.record.t", "A record, not a claim", "주장이 아니라 기록");
        say("feature.record.b",
            "Logs every command, true exit codes (including failed pipe stages), written files, and scans the "
            + "workspace on every completion claim.",
            "파이프라인 실패를 포함한 실제 명령 종료 코드와 파일 수정 기록을 남기고, 종료를 선언할 때마다 "
            + "워크스페이스를 다시 검사한다.");
        say("feature.console.t", "A console for many agents", "여럿을 보는 콘솔");
        say("feature.console.b",
            "Monitor all companions from a browser. You can interrupt tasks, answer questions, approve commands, "
            + "read their memories, and receive push notifications.",
            "브라우저에서 모든 컴패니언을 감독한다. 명령 승인, 답변 입력, 학습 내용 조회가 가능하며 막히면 "
            + "푸시 알림을 보낸다.");
        say("feature.loop.t", "An inspectable loop", "열어 볼 수 있는 루프");
        say("feature.loop.b",
            "Every turn is saved as an event-sourced JSONL log, making operations like /rewind, /fork, /replay, "
            + "and /loopdiff natively supported.",
            "모든 턴이 추가 전용 JSONL로 기록된다. 덕분에 턴 되돌리기, 분기, 재실행, 비교 작업이 기본적으로 지원된다.");
        say("feature.binaries.t", "Self-contained binaries", "자기완결 바이너리");
        say("feature.binaries.b",
            "Built in pure Go with no CGO dependencies. The agent and console are separate static binaries that "
            + "run anywhere.",
            "CGO 없이 순수 Go로 짠 정적 바이너리. 에이전트와 콘솔 프로그램만 복사하면 어디서든 바로 실행된다.");

        say("feature.companions.t", "Companions and hand-off", "컴패니언과 인계");
        say("feature.companions.b",
            "Assign a role to a workspace to address it by its purpose. Use hand_off to delegate work to "
            + "specialists while continuing the main task. Meetings align multiple companions on a plan.",
            "워크스페이스에 역할을 지정해 전문 분야별로 다룰 수 있다. hand_off로 작업을 넘기고 다른 일을 진행하며, "
            + "회의 기능으로 여러 컴패니언의 작업 방향을 조율한다.");
        say("feature.knowledge.t", "What the team has learned", "팀이 익힌 것");
        say("feature.knowledge.b",
            "Skills and memories are scoped to a companion, team, or everyone. The shared wiki updates in "
            + "place, preserving retired pages with context.",
            "스킬과 기억은 컴패니언, 팀, 전체 범위로 나뉘어 공유된다. 위키 문서는 제자리에서 수정되며, 오래된 "
            + "내용은 폐기 사유와 함께 보존된다.");
        say("feature.tools.t", "Tools, MCP and plugins", "도구와 MCP, 플러그인");
        say("feature.tools.b",
            "Includes 24 built-in tools and supports external MCP servers. Editor plugins and add-ins act as "
            + "temporary MCP servers while open. Lua plugins offer capability bundles.",
            "24개의 내장 도구를 제공하며 외부 MCP 서버와도 연결된다. 에디터 플러그인 등은 켜진 동안만 연결을 "
            + "유지하며, Lua 플러그인으로 새로운 기능을 추가할 수 있다.");
        say("feature.guard.t", "Permissions and sandboxes", "권한과 샌드박스");
        say("feature.guard.b",
            "A profile is two axes — permission and sandbox. Secret paths like .env are a floor nothing "
            + "crosses, and shell commands are read for destruction, egress and pipes into a shell. A "
            + "delete that aims outside the workspace asks first, and what an edit overwrote is held by a "
            + "hard link.",
            "프로파일은 권한과 샌드박스 두 축이다. .env 같은 시크릿 경로는 아무도 넘지 못하는 바닥이고, "
            + "셸 명령은 파괴·외부 전송·셸로 가는 파이프를 두고 읽힌다. 워크스페이스 밖을 겨눈 삭제는 "
            + "먼저 묻고, 편집이 덮어쓴 내용은 하드링크가 붙들어 둔다."
            + "외부 유출 시도를 스캔한다. 덮어쓰인 파일은 하드 링크로 백업된다.");
        say("feature.complete.t", "Completion and the next instruction", "자동완성과 다음 지시");
        say("feature.complete.b",
            "Provides autocomplete and prompt suggestions based on your history. Uses a lightweight routing "
            + "profile so typing is never delayed by the agent loop.",
            "이전 프롬프트 이력을 바탕으로 다음 명령을 제안하고 자동 완성한다. 가벼운 LLM 프로파일로 처리되어 "
            + "키보드 입력이 밀리지 않는다.");
        say("feature.update.t", "A fleet that keeps itself current", "스스로 최신을 지키는 플릿");
        say("feature.update.b",
            "Daemons securely update themselves by verifying checksums, running a safe rollback pre-flight check, "
            + "and restarting in-place without losing conversations. Never across machines, never over your own "
            + "source builds.",
            "데몬은 체크섬을 검증해 내려받고, 새 빌드가 서지 않으면 되돌리는 예행을 거친다. 다시 떠도 "
            + "진행 중인 대화는 그대로다. 머신을 건너서는 절대 하지 않고, 직접 빌드한 것도 덮지 않는다."
            + "대화는 유지되며 직접 빌드한 소스는 덮어쓰지 않는다.");

        say("sec.start.head", "How to start", "실행 방법");
        say("sec.start.lede",
            "Go 1.26+ to build, and an OpenAI-compatible backend. The default model runs on Ollama's "
            + "free cloud tier — no GPU, one sign-in — and any local model works too.",
            "빌드에 Go 1.26+, 그리고 OpenAI 호환 백엔드 하나. 기본 모델은 Ollama 의 무료 클라우드 "
            + "티어에서 돈다 — GPU 없이, 로그인 한 번 — 로컬 모델도 그대로 된다.");

        say("start.req", "Requirements", "필요한 것");
        say("start.build", "Build from source", "소스에서 빌드");
        say("start.run", "Run", "실행");
        say("start.more",
            "Read the manual for configuration, profiles, sandboxes, plugins, MCP, and managing the fleet.",
            "설정, 프로파일, 샌드박스, 플러그인, MCP, 플릿 관리 등 자세한 내용은 안내서를 참고한다.");

        say("foot.note",
            "This page is built from the very repository it describes. The demo uses the real console UI "
            + "backed by browser-side mocks — no server needed.",
            "이 랜딩 페이지는 설명 대상인 magi 저장소에서 직접 빌드되었다. 옆에 있는 콘솔 데모는 실제 UI 코드를 "
            + "돌리며, 서버 없이 브라우저 단에서 목(mock) 데이터로 반응한다.");
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
