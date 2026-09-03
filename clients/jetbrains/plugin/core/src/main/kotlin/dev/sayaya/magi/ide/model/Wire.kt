package dev.sayaya.magi.ide.model

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull

/**
 * 데몬 소켓 위의 한 줄.
 *
 * 필드 이름은 `internal/adapter/daemon/daemon.go` 의 json 태그와 같아야 한다. Go 의
 * encoding/json 은 못 맞춘 필드를 **말없이 버리므로**, 이름이 어긋나면 에러가 아니라
 * "묻지 않은 질문에 정상 응답"이 된다. 그쪽 `wire_invariant_test.go` 가 필드를 지우거나
 * 이름 바꾸는 것을 막고 있고, 이 파일이 그 짝이다.
 */
object Wire {
    /**
     * 와이어는 **추가만 허용**한다. 새 데몬이 필드를 더해도 낡은 플러그인이 깨지면 안 되므로
     * 모르는 키를 무시한다. 이건 관용이 아니라 계약이다.
     */
    val json: Json = Json {
        ignoreUnknownKeys = true
        encodeDefaults = false
        explicitNulls = false
    }
}

@Serializable
data class Request(
    val method: String,
    val session: String? = null,
    val text: String? = null,
    @SerialName("callId") val callId: String? = null,
    val decision: String? = null,
    val answer: String? = null,
    val name: String? = null,
    val looking: Boolean? = null,
    val meeting: String? = null,
    /** 예약 편집의 두 필드. [enabled] 는 **세 갈래**다 — 없음은 「스위치는 그대로」다. */
    val schedule: String? = null,
    val enabled: Boolean? = null,
    /** 설정 쓰기가 어느 층에 쓸지 — 비우면 데몬이 정한다(project 우선). */
    val tier: String? = null,
    val n: Int? = null,
    val args: JsonElement? = null,
    val ask: Boolean? = null,
    // attach 문의 인자. 어느 HTTP MCP 서버이고, 무엇을 실어 보내나.
    val url: String? = null,
    val headers: Map<String, String>? = null,
    /**
     * 전사 구독의 커서. **없거나 0 이하면 전량**이다 — 지어낸 규칙이 아니라 스토어의 것이다
     * (`jsonl.go` 의 `filterFrom` 은 `fromSeq > 0` 일 때만 자르고 seq 는 1부터 시작한다).
     * 세션이 바뀌면 이 값을 버리고 다시 전량을 받아야 한다(콘솔이 `lastSeq = -1` 로 되돌리는 그 규칙).
     *
     * **받은 seq 를 그대로 커서에 넣지 마라.** 버스 전용 이벤트는 자리가 없어 `seq == 0` 으로 온다
     * (코어 `event.go`: "transient (bus-only) events carry Seq == 0"). 그 0 을 커서로 삼으면
     * 여기 규칙에 걸려 **전량**이 되고, 데몬의 `answerable` 은 `since <= 0` 을 의심하지 않으므로
     * 거절 통보(`Response.why`) 없이 대화를 통째로 다시 보낸다 — 화면 두 벌이 아무 소리 없이
     * 생긴다. 커서를 들려면 **사실만 세어야 한다**(`seq > 0`).
     *
     * 이 클라이언트는 이제 커서를 보낸다(`MagiToolWindow` 의 lastSeq — 사실의 seq 만 센다).
     * 컴팩션이 seq 를 보존하므로 커서는 믿어도 된다는 것이 계약으로 명문화됐다(docs/CLIENTS §2);
     * 대화가 바뀌면 0 으로 되돌리고, 거절은 데몬이 이벤트보다 먼저 말한다.
     */
    val since: Long? = null,
    /**
     * submit/steer 에 싣는 첨부 — `internal/core/command/command.go` 의 `FileRef`. 코어가
     * 발췌를 렌더해 프롬프트와 함께 영속하므로(전사·재생·웹이 같은 것을 봄) 와이어는 이름과
     * 범위만 나른다. 캡(ref 16KB·전체 64KB)·워크스페이스 감옥은 코어의 계약이고, 줄범위는
     * **파싱 불가**만 파일 전체로 낙하한다 — 범위가 파일 밖이면 그 사유가 발췌 자리에 선다
     * (`internal/app/refs.go` — "사라지는 첨부 없음"의 그 갈래다).
     */
    val refs: List<FileRef>? = null,
)

/** 첨부 하나. [lines] 는 "12-40" 또는 "12"(1-기준 포함, 에디터 셈법), 빈 값=파일 전체. */
@Serializable
data class FileRef(
    val path: String,
    val lines: String? = null,
)

@Serializable
data class Response(
    val ok: Boolean = false,
    val error: String? = null,
    val waiting: Waiting? = null,
    val doing: String? = null,
    val out: String? = null,
    val exit: Int? = null,
    val permission: String? = null,
    val user: String? = null,
    val tools: List<String>? = null,
    val models: List<String>? = null,
    val why: String? = null,
    val session: String? = null,
    val version: String? = null,
    val proto: Int? = null,
    val caps: List<String>? = null,
    /** 전사 프레임 하나. `transcript` 스트림에서만 실린다. */
    val event: LogEvent? = null,
    /** 플릿 — `roster` 문의 답(`internal/adapter/daemon/roster.go` 의 `RosterRow`). */
    val roster: List<RosterRow>? = null,
    /** 작업 — `jobs` 문의 답(`internal/adapter/daemon/daemon.go` 의 `Jobs`). */
    val jobs: Jobs? = null,
    /** 이 워크스페이스의 대화들 — `sessions` 문의 답(`daemon.go` 의 `SessionRow`), 최근 활동 순. */
    val sessions: List<SessionRow>? = null,
    /**
     * 한 세션이 띄운 서브에이전트 대화들 — `children` 문의 답, 최근 활동 순.
     *
     * `jobs.children` 과 다른 목록이고 그 차이가 이 문이 있는 이유다: 그쪽은 **지금 도는 것과
     * 방금 끝난 것**의 인메모리 등록부라 데몬이 재시작하면 사라지고, 이쪽은 **로그의 사실**이라
     * 지난주 것도 답한다. 어느 쪽도 다른 쪽의 캐시가 아니다.
     *
     * null 은 「문 없음」이고 빈 목록은 「띄운 적 없음」이다 — 두 화면이 다르다.
     */
    val children: List<SessionRow>? = null,
    /** 건넨 일 하나의 지금 — `hand-state` 의 답(`daemon.go` 의 `Handover`). */
    val handover: Handover? = null,
    /** 예약들 — `cron` 문의 답(`daemon.go` 의 `CronRow`), 고장 먼저 그다음 임박순. */
    val cron: List<CronRow>? = null,
    /** 고칠 수 있는 설정 키들 — `config-get`/`config-set` 의 답(`daemon.go` 의 `ConfigItem`). */
    val config: List<ConfigItem>? = null,
    /** `job-kill`·`mcp-detach` 의 「있었는지」 — 두 번 누른 ✕ 는 거짓이 아니라 이미-없음이다. */
    val removed: Boolean = false,
)

/**
 * 건넨 일의 지금 — 넷을 뭉치면 추락이 빈 답으로 보인다(코어 주석 그대로): [answer] 는 말한 것,
 * [over] 는 더 안 온다는 것, [news] 는 그 사유.
 */
@Serializable
data class Handover(
    val done: Boolean = false,
    val answer: String? = null,
    val news: String? = null,
    val over: Boolean = false,
)

/**
 * 예약 하나. [problem] 이 실린 행이 이 판이 **표시해야 하는** 행이다 — 영영 못 도는 사유는
 * 다른 어떤 화면도 다시 언급하지 않는다(코어 `CronRow` 의 계약 그대로).
 */
@Serializable
data class CronRow(
    val name: String = "",
    val schedule: String? = null,
    val enabled: Boolean = false,
    /** 이 잡이 **묻는 말**. 없으면 화면은 잡이 있다는 것만 말하고 무엇을 하는지는 못 말한다. */
    val prompt: String? = null,
    /** RFC3339. 비어 있으면 영영 안 돈다 — 꺼졌거나 [problem] 이 사유다. */
    val next: String? = null,
    val problem: String? = null,
)

/**
 * 대화 하나. **열린 턴 여부는 일부러 없다** — 계약이 뺐다(답하려면 로그 전부를 읽어야 하고,
 * 문제되는 대화는 현재 것뿐이라 roster 행의 session 과 비교하면 된다).
 */
@Serializable
data class SessionRow(
    val id: String = "",
    val title: String? = null,
    /**
     * 자식 세션이라는 표시 — 모든 자식이 **같은 낱말**("spawn")을 기록한다(코어의 `spawnAgentName`
     * 은 상수다). 그러니 이 필드는 「무언가가 이 대화를 시켰다」까지만 말하고 그 이상은 못 한다.
     * 라이브 회의로 실측했다: 회의 방이 `agent="spawn"` 으로 돌아왔다.
     */
    val agent: String? = null,
    /**
     * **누가 열었나** — 자식들을 가르는 것은 이쪽이다. 회의 방이면 "meeting", 아니면 띄운 쪽의
     * 액터. 웹 콘솔은 이 문이 생기기 전부터 이 필드로 회의를 걸러 왔다.
     */
    val origin: String? = null,
    val model: String? = null,
    val labels: List<String>? = null,
    val created: String? = null,
    @SerialName("lastActivity") val lastActivity: String? = null,
)

/**
 * 이 워크스페이스의 작업들 — 코어 `Jobs` 를 옮긴 것. [queued] 가 **다음에 돌 것**을 돌 차례
 * 그대로 싣는다: 사람이 세워 둔 말 먼저, 그다음 남이 건넨 일 — 대기열은 둘(계약이 다르다)인데
 * 차례는 하나다(에이전트가 하나라서). 한쪽만 그리는 화면은 거짓말을 한다(코어 주석 그대로).
 */
@Serializable
data class Jobs(
    val background: List<BackgroundJob>? = null,
    val children: List<ChildJob>? = null,
    val queued: List<QueuedWork>? = null,
)

@Serializable
data class BackgroundJob(
    val id: String = "",
    val command: String? = null,
    val running: Boolean = false,
    val killed: Boolean = false,
    val exit: Int = 0,
    val started: String? = null,
    val tail: String? = null,
)

@Serializable
data class ChildJob(
    val id: String = "",
    val tool: String? = null,
    val task: String? = null,
    val started: String? = null,
    val ended: String? = null,
    val running: Boolean = false,
    val steps: Int = 0,
    val err: String? = null,
)

/** 대기 하나. [kind] 는 person(여기서 친 것) | handover(남이 청한 것 — [from] 이 그 이름). */
@Serializable
data class QueuedWork(
    val kind: String = "",
    val text: String? = null,
    val from: String? = null,
)

/**
 * 이 머신이 이름 댈 수 있는 컴패니언 하나 — `internal/adapter/daemon/roster.go` 의 `RosterRow` 를 옮긴 것.
 * 필드를 전부 옮긴다: 골라 옮기면 코어가 필드를 더할 때 여기가 조용히 덜 아는 화면이 된다.
 * `state` 어휘는 계약에 못박혔다: waiting(사람을 기다림) · working · idle.
 */
@Serializable
data class RosterRow(
    val host: String? = null,
    val socket: String = "",
    val name: String? = null,
    val role: String? = null,
    val team: String? = null,
    val hub: Boolean = false,
    val workdir: String? = null,
    val account: String? = null,
    val state: String? = null,
    val version: String? = null,
    /** 실측 행의 기록 사실 셋 — 웹이 publish 기록에서 직접 읽던 것이라 문 경유에도 손실 0. */
    val pid: Int = 0,
    val addr: String? = null,
    val started: String? = null,
    /** 목격담에 서명한 머신의 공개키. 신뢰 등급은 소비자의 몫이다. */
    val by: String? = null,
    val can: Int = 0,
    val does: List<String>? = null,
    val waiting: Int = 0,
    val handling: Boolean = false,
    /** 지금 대화. 이 머신의 행에만 실린다 — 목격담엔 구독할 길이 없어 의미도 없다. */
    val session: String? = null,
    val live: Boolean = false,
    /** true 면 남이 서명한 목격담이다 — 실측이 아니라 흐리게 그린다. */
    val sighting: Boolean = false,
    @SerialName("ageSeconds") val ageSeconds: Long = 0,
)

/**
 * 사람에게 물어 놓고 답을 기다리는 프롬프트. daemon.go 의 `Waiting`.
 *
 * 이것이 응답에 실려 오는 이유가 설계상 중요하다. `permission.requested` 는 **전이 이벤트라
 * 로그에 없으므로**, 로그만 꼬리 무는 클라이언트는 프롬프트를 영영 못 본다. 데몬이 한 곳에서
 * 다시 조립해 주고(daemon.go 의 `Waiting` 주석) 그 자리가 여기다.
 */
@Serializable
data class Waiting(
    val id: String,
    val kind: String,
    val what: String,
    val args: JsonElement? = null,
    val reason: String? = null,
    val options: List<String>? = null,
    /**
     * 편집 계열 승인에 실리는 변화 그 자체(unified diff). **뷰어가 재계산하지 않는다** — 앱이
     * 한 번 계산해 싣는 것이 계약이다(`change.EditDiff`). 그 외 호출에선 빈 값.
     */
    val diff: String? = null,
    val index: Int = 0,
    val total: Int = 0,
    val since: String? = null,
) {
    /**
     * 이 물음에 이 창이 그릴 수 있는 것.
     *
     * 처음엔 `isPermission` 하나였고 나머지는 전부 질문이었다. 그 `else` 가 **모르는 종류를 질문이라고
     * 넘겨짚는다.** 코어는 같은 자리를 반대로 가른다 — `daemon.go` 의 `Waiting.Event` 는 `"question"`
     * 이 아니면 전부 권한 물음으로 그린다. 셋째 종류가 생기면 두 창이 같은 물음을 다르게 그리고, 둘 중
     * 하나는 반드시 틀린다.
     *
     * **틀린 답이 통과하지는 않는다.** 코어의 등록부가 종류별로 갈려 있다 — `app.go` 의
     * `RespondQuestion` 은 `st.questions` 만 보고 `RespondPermission` 은 `st.perms` 만 본다. 잘못
     * 보낸 답은 채널을 못 찾고 거절된다. 문제는 **거절이 사유를 틀리게 댄다**는 것이다: "이미 답했거나
     * 만료됐다"고 하는데 사실은 그 종류에 그 답을 보낸 적이 없다. 사람은 없는 경합을 찾아 나선다.
     *
     * 그래서 여기서 넘겨짚지 않는다. **거절은 뜻을 안 정하므로 틀릴 수가 없다.** 값은 답할 줄 아는
     * 창으로 한 번 옮겨 가는 것이고, 그 대신 사유가 맞는다.
     *
     * [Ask.Undrawable] 이 둘을 함께 잡는다. 선택지 없는 질문도 단추가 안 나오는데, 그게 지금 안 나는
     * 이유는 이 파일에 없었다 — `askuser.go` 가 선택지 2개 미만을 거절해서였다. 딴 파일의 약속에
     * 기대는 대신 여기서 말이 되게 한다.
     */
    val ask: Ask get() {
        if (kind == "permission") return Ask.Permission
        if (kind != "question") return Ask.Undrawable(
            "이 창이 모르는 종류의 물음이다(kind=$kind). 답할 줄 아는 창에서 답해야 한다.")
        val opts = options.orEmpty()
        return if (opts.isEmpty()) Ask.Undrawable("선택지 없이 온 질문이다. 누를 것을 지어내지 않는다.")
        else Ask.Choose(opts)
    }

    /**
     * **무엇을 정하는 물음인가.** [ask] 가 「누를 것」을 정하고 이쪽이 「보일 것」을 정한다.
     *
     * 이게 없을 때 창은 `what` 과 `reason` 만 붙이고 [args] 는 **한 번도 안 그렸다.** 권한 물음에서
     * `reason` 마저 없으면 화면에 남는 것은 굵은 글씨 «bash» 와 허용·거부·항상 셋뿐이다 —
     * **무엇을 허가하는지 모르는 채로 누른다.** `args` 는 소켓에서 `omitempty` 라 진짜로 안 온다.
     *
     * 이 칸이 있는 이유를 코어가 이미 적어 뒀다(`daemon.go` 의 `Waiting`): *"the rest of the
     * request, **so a viewer draws the prompt rather than a description of it**"*. 도구 이름은
     * 요청의 설명이지 요청이 아니다.
     *
     * **같은 부재를 이 저장소가 다른 데서는 이름으로 부른다.** 우측 판은 승인 모드가 안 오면
     * "데몬이 안 말했다"라고 적고, 로그 줄은 인자가 없으면 그렇게 적는다. 걸린 것이 제일 큰
     * 자리만 조용했다 — 대접이 위험도와 반대로 붙어 있었다.
     */
    val subject: Subject get() {
        val a = args?.takeIf { it !is JsonNull }?.toString()
        val r = reason?.takeIf { it.isNotBlank() }
        return if (a == null && r == null) Subject.Unstated else Subject.Stated(a, r)
    }
}

/** [Waiting.subject] 의 결과. */
sealed interface Subject {

    /**
     * 정해지는 것이 실려 왔다. 적어도 하나는 있다 — 둘 다 없는 것은 [Unstated] 이고, 그 둘을
     * 같은 이름으로 두면 화면이 「빈 요청」과 「말 안 해 준 요청」을 같게 그린다.
     *
     * 둘을 안 합친다. [args] 는 정해지는 것 **그 자체**라 화면이 글자 그대로 보여야 하고
     * (`Markup` 를 거친다), [reason] 은 정책이 왜 섰는지에 대한 산문이다. 합치면 화면이
     * 어느 쪽을 그리고 있는지 모르게 된다.
     */
    data class Stated(val args: String?, val reason: String?) : Subject {
        init {
            require(args != null || reason != null) {
                "둘 다 없으면 Unstated 다 — 빈 Stated 는 화면에서 「아무것도 안 정한다」로 보인다"
            }
        }
    }

    /**
     * 무엇을 정하는지가 **안 실려 왔다.** 화면은 이것을 조용히 넘기면 안 된다.
     *
     * 조용히 넘기던 자리다: `reason ?: ""` 가 빈 줄을 그리고 단추 셋은 그대로 떴다. 사람은
     * 창이 뭘 덜 받았다는 것을 알 길이 없으니 아는 것(«bash»)만 보고 누른다.
     */
    data object Unstated : Subject
}

/** [Waiting.ask] 의 결과. 화면은 이 셋만 그린다. */
sealed interface Ask {
    /** 허용·거부·항상. */
    data object Permission : Ask

    /** 선택지 그대로. **비어 있지 않다** — 비면 [Undrawable] 이다. */
    data class Choose(val options: List<String>) : Ask

    /**
     * 그릴 단추가 없다. [why] 를 사람에게 **그대로 보인다** — 조용히 비워 두면 물음만 떠 있고,
     * 사람은 자기 창이 고장 난 줄 모르고, 컴패니언은 답을 기다리며 계속 막혀 있다.
     */
    data class Undrawable(val why: String) : Ask
}

/** 데몬이 소켓 옆에 공표하는 것. daemon.go 의 `Info`. */
@Serializable
data class Published(
    val socket: String? = null,
    val workdir: String? = null,
    val session: String? = null,
    val name: String? = null,
    val role: String? = null,
    val team: String? = null,
    val hub: Boolean = false,
    val pid: Int = 0,
    val started: String? = null,
    val host: String? = null,
    val state: String? = null,
    val version: String? = null,
)

/**
 * 로그 이벤트 하나 — `internal/core/event` 의 `Event` 를 옮긴 것.
 *
 * **여기서 렌더하지 않는다.** 이것은 사실이고, 사람이 읽는 줄로 바꾸는 것은 파생이다. 그 파생은
 * usecase 의 `Rows.kt` 가 한다 — 처음에는 웹의 `line` 짓기를 코어로 옮겨 같이 쓰려 했으나
 * `clients/web/server` 이 헐릴 예정이라 **클라이언트가 각자 짓는 것**으로 정해졌다(2026-08-29,
 * `docs/TRANSCRIPT.ko.md`). 이 층이 그대로 나르는 것은 변함없다 — 전송은 사실을, 규칙은 행을.
 */
@Serializable
data class LogEvent(
    val seq: Long = 0,
    @SerialName("sessionId") val session: String = "",
    val type: String = "",
    val actor: Actor? = null,
    val ts: String? = null,
    /** 타입마다 모양이 다르다. 이 층은 안 읽는다. */
    val data: JsonElement? = null,
)

/** 행위자. 와이어의 이름은 `id` 다(코어 `event.go` 의 `Actor`) — `name` 은 안 실려 온다. */
@Serializable
data class Actor(val kind: String = "", val name: String? = null, val id: String? = null)

/**
 * 고칠 수 있는 설정 키 하나 — 데몬이 **열거해 준다.**
 *
 * 화면이 키를 손으로 나열하지 않는 것이 이 자리의 규칙이다(모델을 정하는 자리가 여럿이고, 새
 * 키가 늘 때마다 화면이 조각조각 늘어난다). 문이 열거하므로 그 규칙이 지켜지면서도 칸은
 * 자동으로 는다 — [doc] 한 줄이 그 칸 밑에 서고, [applies] 는 「지금」과 「다음 기동」을 가른다
 * (다시 켜라고 말할 필요 없는 키에 그렇게 말하면 사람이 헛되이 껐다 켠다).
 */
@Serializable
data class ConfigItem(
    val key: String = "",
    val value: String? = null,
    /** env | project | global | 빈 값(아무도 안 정함) — 지금 값이 **어디서 왔나**. */
    val source: String? = null,
    val tier: String? = null,
    val file: String? = null,
    /** now | next start. 키마다 다르다 — 한 문장으로 뭉치면 절반이 거짓말이 된다. */
    val applies: String? = null,
    val doc: String? = null,
    /** 못 읽은 설정 층과 그 사유. 오타 난 파일과 아무 말 없는 파일은 값만 보면 같은 부재다. */
    val unreadable: String? = null,
)
