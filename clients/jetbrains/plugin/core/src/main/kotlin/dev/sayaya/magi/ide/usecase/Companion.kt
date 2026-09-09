package dev.sayaya.magi.ide.usecase

import dev.sayaya.magi.ide.model.FileRef
import dev.sayaya.magi.ide.model.Request
import dev.sayaya.magi.ide.model.Response
import dev.sayaya.magi.ide.model.Waiting

/**
 * 붙어 있는 컴패니언에게 말을 걸고, 그가 묻는 것에 답한다.
 *
 * 여기 있는 것은 **전사가 없어도 되는 것들**이다. 전사는 데몬에 `transcript` 문이 생겨야 오지만
 * (설계 문서 §3), 지시를 보내는 것과 프롬프트에 답하는 것은 지금 있는 메서드로 된다. 그래서
 * 먼저 붙인다 — 사람이 기다리는 화면을 빈 채로 두지 않는 것이 §2의 규칙이기도 하다.
 */
class Companion(
    private val client: Daemon,
    /**
     * 이 컴패니언이 매인 대화. **빌 수 있다** — 대화를 안 고르고도 되는 일(목록·새 대화·
     * 갈아타기)이 있고, 그 일을 하려고 붙을 때는 고를 대화가 아직 없다. 빈 채로 대화 문을
     * 부르는 것은 [send] 가 막는다.
     */
    private val session: String,
) {

    /**
     * 모든 교환이 지나는 한 자리.
     *
     * **빈 대화로 대화 문을 부르지 못하게 막는다.** 대화를 안 고르고 붙은 컴패니언이 `say` 나
     * `interrupt` 를 부르면 `session: ""` 이 전선에 나가고, 데몬이 그것을 어떻게 읽든 그건
     * 부르는 쪽이 **모르는 채로 정해지는 것**이다. 여기서 사유를 실어 거절하면 그 자리가
     * 화면에 문장으로 뜬다 — 못 하게 막는 것이 나중에 재는 것보다 싸다.
     */
    private fun send(request: Request): Response =
        if (request.session?.isBlank() == true) {
            Response(ok = false, error = "이 컴패니언은 대화를 고르지 않고 붙었다 — `${request.method}` 는 대화 문이다")
        } else {
            client.exchange(request)
        }

    /**
     * 사람이 친 것을 보낸다.
     *
     * 턴이 돌고 있으면 `steer`, 아니면 `submit` 이다. 둘은 같은 문의 두 철자가 아니다 —
     * `submit` 은 **새 최상위 요청**이라 코어가 `resetForNewTopLevel` 을 돌린다(계획을 비우고,
     * 턴 노트와 선언 게이트를 되돌린다). 도는 턴에 그것을 하면 사람이 한 마디 거들었을 뿐인데
     * 그 턴의 계획이 사라진다.
     *
     * [turnOpen] 은 **전사를 흘려보고 있는 쪽이 아는 사실**이다(`Rows.open` — 답 없는
     * `prompt.submitted` 가 서 있나). 그것을 아는 부르는 쪽은 넘기고, 모르는 쪽은 null 로 두면
     * [turnIsOpen] 이 데몬에게 물어본다 — 다만 그 물음은 **켜졌을 때만 참**이다(아래).
     */
    fun say(text: String, refs: List<FileRef> = emptyList(), turnOpen: Boolean? = null): Response {
        val method = if (turnOpen ?: turnIsOpen()) "steer" else "submit"
        return send(Request(
            method = method, session = session, text = text,
            refs = refs.takeIf { it.isNotEmpty() },
        ))
    }

    /** 지금 사람을 기다리고 있으면 그 프롬프트, 아니면 null. */
    fun waiting(): Waiting? = status().waiting

    /**
     * 퍼미션 프롬프트에 답한다. 결정은 코어가 쓰는 낱말 그대로 — `allow` | `deny` | `always`.
     * 불리언으로 옮기지 않는 이유는 한 결정에 어휘가 둘이면 언젠가 갈라져서다(데몬 쪽 주석).
     */
    fun allow(callId: String) = decide(callId, "allow")
    fun deny(callId: String) = decide(callId, "deny")
    fun always(callId: String) = decide(callId, "always")

    private fun decide(callId: String, decision: String): Response =
        send(Request(method = "permission", session = session, callId = callId, decision = decision))

    /** 선택지가 있는 질문에 답한다. 퍼미션과 메서드가 다르다(`answer`). */
    fun answer(callId: String, answer: String): Response =
        send(Request(method = "answer", session = session, callId = callId, answer = answer))

    /** 돌고 있는 턴을 세운다. */
    fun interrupt(): Response = send(Request(method = "interrupt", session = session))

    /**
     * 손을 붙인다 — IDE 가 내놓는 도구를 이 컴패니언에게 준다.
     *
     * 이름은 **고정**이다([McpName]). 거절은 삼키지 않고 그대로 올린다: 이미 붙어 있다는 것과
     * 이름이 겹친다는 것은 사람이 할 일이 다르고, §4 의 표가 그 갈래를 적는다. 특히 같은
     * 워크스페이스를 IDE 둘로 열면 **먼저 붙은 쪽만 손**이 되고 둘째는 거절을 받는데, 그 사실이
     * 화면에 보여야 한다(§7 의 다섯째 시나리오).
     */
    fun attachHand(url: String, headers: Map<String, String>): Response =
        send(Request(method = "mcp-attach", name = McpName.VALUE, url = url, headers = headers))

    /** 손을 뗀다. 창이 닫히거나 IDE 가 나갈 때 — 안 떼면 데몬이 죽은 주소를 계속 들고 있는다. */
    fun detachHand(): Response =
        send(Request(method = "mcp-detach", name = McpName.VALUE))

    private fun status(): Response = send(Request(method = "status", session = session))

    /**
     * 플릿 — 이 머신이 이름 댈 수 있는 컴패니언들. 세션을 안 싣는 것은 이 물음이 대화가 아니라
     * **머신**에 대한 것이라서다(`internal/adapter/daemon/roster.go` 의 `answerRoster`). 가십은 발견까지 — 조종은 그
     * 컴패니언의 자기 소켓으로(계약 경계, docs/CLIENTS.md §2).
     */
    fun roster(): Response = send(Request(method = "roster"))

    /** 작업 — 도는 백그라운드, 자식, 그리고 다음에 돌 대기열(사람 말 먼저, 그다음 건넨 일). */
    fun jobs(): Response = send(Request(method = "jobs", session = session))

    /**
     * 워크스페이스 파일 찾기 — `@` 멘션의 목록. 읽기 전용 넷 중 `glob` 을 `tool` 문으로 돌린다
     * (`internal/app/query.go` 의 `ReadOnlyTool` — 워크스페이스 감옥·denyFloor 전부 코어 규칙).
     *
     * `out` 은 줄바꿈 목록이 **아니라 JSON 배열 한 줄**이다(리뷰 실측: glob 의 okJSON 을
     * toolText 가 원문으로 통과 — 웹 정본도 배열로 파싱한다, `clients/web/server/files.go`). 첫 판은
     * 줄바꿈이라 적었고 시험이 그 허구를 축복했다 — 그린은 맞음의 증명이 아니다. 경로는
     * 워크스페이스-상대·슬래시. 파싱 실패·거절은 빈 목록.
     */
    fun globFiles(pattern: String): List<String> {
        val r = send(Request(
            method = "tool", name = "glob",
            args = kotlinx.serialization.json.buildJsonObject {
                put("pattern", kotlinx.serialization.json.JsonPrimitive(pattern))
            },
        ))
        if (!r.ok) return emptyList()
        return runCatching {
            dev.sayaya.magi.ide.model.Wire.json.decodeFromString(
                kotlinx.serialization.builtins.ListSerializer(kotlinx.serialization.serializer<String>()),
                r.out ?: "[]",
            )
        }.getOrDefault(emptyList())
    }

    /** 고칠 수 있는 설정 키들과 지금 값 — 데몬이 열거한다. */
    fun configGet(): Response = send(Request(method = "config-get"))

    /** 프로파일 모양 키가 가리킬 수 있는 것들 — 콤보를 채운다. */
    fun profiles(): Response = send(Request(method = "profiles"))

    /**
     * 설정 키 하나를 쓴다. [tier] 는 어느 층에 쓸지 — 비우면 데몬이 정한다.
     *
     * 빈 [value] 는 **지우기**다(그 층에서 그 키를 뺀다) — 「빈 문자열로 설정」이 아니다.
     */
    fun configSet(key: String, value: String, tier: String = ""): Response =
        send(Request(method = "config-set", name = key, text = value, tier = tier))

    /**
     * 이 데몬이 무엇을 할 수 있는가 — 핸드셰이크의 `caps`.
     *
     * **부르기 전에 안다.** 편집기를 그릴지 말지는 문이 있는지에 달렸고, 「없는 문을 부르고
     * 거부를 읽는」 방식으로는 <b>낡은 빌드</b>와 <b>거부하는 엔진</b>을 못 가른다 — 화면은 그
     * 둘에 다른 말을 해야 한다. 이 값은 데몬이 도는 동안 안 바뀌므로 부르는 쪽이 한 번만
     * 읽고 기억하면 된다.
     */
    fun about(): Response = send(Request(method = "about"))

    /**
     * 예약 하나를 쓴다 — 없으면 만들고, 같은 이름이 있으면 고친다.
     *
     * [enabled] 가 null 이면 스위치는 그대로다. 말만 고치는 편집이 꺼 둔 잡을 도로 켜면 안 되고,
     * 그래서 와이어에서도 이 값은 세 갈래다.
     */
    fun setCron(
        name: String,
        schedule: String,
        prompt: String,
        enabled: Boolean? = null,
        command: String = "",
        timeout: String = "",
    ): Response =
        send(Request(method = "cron-set", name = name, schedule = schedule,
            text = prompt, enabled = enabled,
            command = command.ifBlank { null }, timeout = timeout.ifBlank { null }))

    /** 예약 하나를 지운다. */
    fun removeCron(name: String): Response = send(Request(method = "cron-remove", name = name))

    /** 이 워크스페이스의 대화들. 최근 활동 순 — 차례는 데몬이 정했다. */
    fun sessions(): Response = send(Request(method = "sessions"))

    /**
     * 한 세션이 띄운 서브에이전트 대화들 — 회의 방도 여기 든다(참가자는 회의를 자식 세션에서 연다).
     *
     * `jobs().children` 과 겹치지만 겹치는 것이 전부는 아니다: 그쪽은 지금 도는 것과 방금 끝난
     * 것의 인메모리 등록부라 데몬이 재시작하면 사라지고, 이쪽은 로그의 사실이라 지난 것도 답한다.
     * 화면은 둘을 합쳐 그린다 — 도는 것은 등록부가 더 잘 알고(진행·오류), 과거는 이 문만 안다.
     *
     * 부모 id 는 **필수**다(데몬이 빈 값을 거부한다): 「지금 대화」는 부르는 쪽마다 다른 사실이라
     * 데몬이 대신 정해 주면 누가 묻느냐에 따라 다른 질문에 답하게 된다.
     */
    fun children(parent: String = session): Response =
        send(Request(method = "children", session = parent))

    /**
     * 커밋 메시지 초안 — 스테이지된 변경에서, 워크스페이스의 하우스 스타일 템플릿을 얹어서
     * (`internal/adapter/daemon/doors.go` 의 `answerGitMsg` → `DraftCommit`). 답은 `out` 에
     * 실리고, 빈 답은 실패가 아니라 「스테이지가 없다」일 수 있다(`git.go` 가 명시한 갈래).
     *
     * **text 를 안 싣는다** — 그 자리는 힌트가 아니라 **일회용 규칙 오버라이드**다: 비어 있지
     * 않으면 저장된 `[templates] commit` 을 밀어내고 "이 프로젝트의 규칙"으로 주입된다
     * (`internal/app/git.go` — 웹 커밋 카드의 규칙 입력이 쓰는 그 자리). 칸의 글을 실었다가
     * 트레일러 규칙이 조용히 빠지는 초안을 만들었다(리뷰 실측). session 도 안 싣는다 — 데몬이
     * 안 읽는다(현재 세션 기준으로 짓는다).
     */
    fun draftCommit(): Response = send(Request(method = "git-msg"))

    /** 예약들. 고장 먼저 그다음 임박순 — 차례는 데몬이 정했다. */
    fun cron(): Response = send(Request(method = "cron"))

    /**
     * 이 대화의 창이 얼마나 찼나 — **지금** 묻는다.
     *
     * 같은 사실이 스트림의 `context.usage` 로도 오지만 그것은 transient 라 재생이 없다. 도는
     * 대화에 붙은 창은 턴이 한 번 돌기 전까지 그 값을 못 본다 — 문은 그 자리에서 답한다.
     * 광고(`context`)가 있는 데몬에만 물을 것.
     */
    fun context(): Response = send(Request(method = "context", session = session))

    /** 도는 백그라운드 하나를 세운다. removed=false 는 실패가 아니라 이미-없음이다. */
    fun killJob(id: String): Response =
        send(Request(method = "job-kill", name = id))

    /**
     * 다른 대화로 옮긴다. 워크스페이스에 없는 id 는 데몬이 거부한다 — **id 를 지어내지 않는다**,
     * 새 대화는 [newSession] 이 정식 동사다.
     */
    fun resume(target: String): Response =
        send(Request(method = "resume", session = target))

    /**
     * 새 대화 — 생성과 이동이 한 동사다(만들어졌는데 현재가 아닌 세션은 아무도 원한 적 없는 행).
     * 턴이 도는 중이면 거부된다 — 인터럽트 먼저.
     */
    fun newSession(): Response = send(Request(method = "session-new"))

    /**
     * 설정 화면의 동사들 — 남는 상태를 데몬에 쓴다(설계 문서 docs/UI.ko.md §5). 값을 IDE 에
     * 한 벌 더 두지 않으므로 화면은 열 때 읽고, 적용이 쓰고, 다시 읽어 확인한다(§5.1).
     *
     * 넷 다 데몬 어휘 그대로다: 모델·백엔드·승인은 `name` 한 칸에 실린다(`doors.go` 의
     * `dispatch` — `SetModel(sid, r.Name)`), 승인 모드의 낱말은 코어의 것이다
     * (`internal/app/routing.go` 의 `SetPermission`: ask | auto | allow | deny).
     */
    fun models(): Response = send(Request(method = "models", session = session))
    fun setModel(name: String): Response =
        send(Request(method = "set-model", session = session, name = name))
    fun useBackend(name: String): Response =
        send(Request(method = "use-backend", session = session, name = name))
    fun setPermission(mode: String): Response =
        send(Request(method = "set-permission", session = session, name = mode))
    fun reloadCron(): Response = send(Request(method = "reload-cron", session = session))

    /**
     * 행동의 동사들 — 한 번 하고 끝나는 것이라 설정 화면이 아니라 제목표시줄 기어 메뉴로 간다
     * (docs/UI.ko.md §5 의 갈래). resume 은 여기 없다: 와이어가 목적지 세션을 요구하는데
     * 고르는 화면이 아직 없다 — 지어낸 목록으로 단추를 만들면 틀린 답을 보낸다.
     */
    fun compact(): Response = send(Request(method = "compact", session = session))
    fun rewind(n: Int = 1): Response =
        send(Request(method = "rewind", session = session, n = n))

    /**
     * 우측 판의 **사실 장** — 지금 무엇을 하고 있고, 어떤 승인 모드이고, 어느 대화인가.
     *
     * `docs/UI.md` §2.2 가 콘솔에서 그 카드에 담는 것과 같은 질문이되, **오늘 데몬이 답할 수 있는
     * 것만** 담는다. 컨텍스트·압축·계획은 읽기 문이 생겨야 온다(설계 문서 §3·§5).
     *
     * 모델은 일부러 안 담는다. 응답의 `models` 는 **고를 수 있는 목록**이지 지금 쓰는 것이 아니라,
     * 첫 항목을 현재 모델로 그리면 그럴듯하게 틀린다.
     */
    fun facts(): Facts = status().let {
        Facts(
            doing = it.doing?.takeIf { d -> d.isNotBlank() },
            permission = it.permission?.takeIf { p -> p.isNotBlank() },
            session = it.session?.takeIf { x -> x.isNotBlank() } ?: session,
            waiting = it.waiting,
            model = it.model?.takeIf { m -> m.isNotBlank() },
            backend = it.backend?.takeIf { b -> b.isNotBlank() },
            user = it.user?.takeIf { u -> u.isNotBlank() },
            council = it.council,
        )
    }

    /**
     * 한 번의 `status` 로 읽히는 것들. 화면이 이것을 그린다.
     *
     * 필드가 널 가능인 것은 **모름과 없음이 다르기 때문**이다 — `doing` 이 null 이면 "이 데몬이 안
     * 말해 줬다"이고 화면은 그것을 "쉬는 중"으로 그리면 안 된다(§0.5-7).
     */
    data class Facts(
        val doing: String?,
        val permission: String?,
        val session: String,
        val waiting: Waiting?,
        /** 이 대화가 올라탄 모델, 데몬이 말했으면. 모름은 null 이고 빈 문자열이 아니다. */
        val model: String?,
        /** 요청이 나가는 곳(base URL), 데몬이 말했으면. */
        val backend: String?,
        /**
         * 사람을 뭐라고 부를지 — 무언가 이름을 바꿔 줬으면.
         *
         * SSO 류 플러그인이 인증된 이름을 `magi.set_user_label` 로 심고, 엔진이 그것을 들고,
         * `status` 가 답한다. 런타임 사실이라 설정 파일이 아니라 이 전선으로만 온다. 코어는
         * **빈 라벨을 아예 안 보내고**(그때 화면은 제 낙하 낱말을 쓴다), 그래서 이 칸은
         * 있거나 없거나이지 빈 문자열이 되지 않는다.
         *
         * 안 읽고 있었다(2026-09-09 실측). 와이어에는 칸이 있었는데 여기까지 안 날라서, 누가
         * 로그인했든 전사의 사람 행은 늘 "You" 였다.
         */
        val user: String?,
        /**
         * 도는 턴을 카운슬에 선언하며 끝내나. **세 갈래다** — true·false·모름(낡은 데몬이 안
         * 말함). 모름을 「꺼짐」으로 그리지 않으려고 null 을 그대로 나른다.
         */
        val council: Boolean?,
    )

    /**
     * 전사를 안 보는 부르는 쪽을 위한 **떨어진 탐침**. 참이면 확실히 열려 있고, **거짓은
     * 「모른다」다.**
     *
     * 한 방향으로만 맞는다. `waiting` 은 사람을 기다린다는 뜻이고 `doing` 은 **분 단위로 도는
     * 툴이 남긴 진행 쪽지**라, 값이 있으면 무언가 돌고 있는 것은 맞다. 그런데 그 쪽지를 남기는
     * 자리를 세어 봤다 — 빌트인 도구 파일 50개 중 **하나**(`wait_for`)와, 접기·카운슬·넘기기·
     * 재시도 같은 예외 경로들뿐이다. `read`·`bash`·`edit` 로 착실히 도는 평범한 턴은 둘 다
     * 비어 있으므로, 이 탐침은 **도는 턴 대부분을 놓친다**. `status` 문에는 「턴이 돌고 있다」를
     * 뜻하는 칸이 아예 없다(`answerStatus`).
     *
     * 그래서 거짓일 때 `submit` 으로 떨어지는 것은 계산된 선택이다: 모를 때 새 턴을 여는 쪽이,
     * 안 도는 대화에 `steer` 를 걸어 **앞 턴의 계획과 게이트를 물려받는** 쪽보다 낫다.
     * 아는 부르는 쪽은 [say] 의 `turnOpen` 으로 사실을 넘긴다.
     */
    private fun turnIsOpen(): Boolean {
        val s = status()
        return s.waiting != null || !s.doing.isNullOrBlank()
    }
}
