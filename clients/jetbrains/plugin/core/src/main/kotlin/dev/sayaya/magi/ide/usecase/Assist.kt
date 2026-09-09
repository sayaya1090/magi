package dev.sayaya.magi.ide.usecase

import dev.sayaya.magi.ide.model.Request
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import java.util.concurrent.atomic.AtomicInteger

/**
 * 모델이 곁에서 거드는 넷. 콘솔이 `/look` `/complete` `/open-file` `/suggest` 로 내놓는 것과
 * 같은 것을 IDE 로 옮긴 것이고, 부르는 메서드도 같다.
 *
 * **연결을 따로 판다.** 이것들은 모델 호출이라 초 단위로 걸리는데, 락스텝 연결 하나를 물고
 * 있으면 그동안 그 연결의 다른 교환이 전부 선다. 콘솔이 같은 이유로 풀링된 연결 대신
 * `alone()` 을 쓴다(`clients/web/server/files.go` 의 완성 라우트들). 그래서 여기는 열려 있는
 * 클라이언트가 아니라 **여는 방법**을 받는다.
 */
class Assist(
    private val open: () -> Daemon,
    /** 켜고 끄는 것은 magi 쪽 `[autocomplete]` 가 정한다. 플러그인이 두 번째 스위치를 만들지 않는다. */
    private val enabled: () -> Boolean = { true },
) {

    /**
     * 지금 몇 개가 날아가 있나. **불리언이 아니라 세는** 이유가 있다. 손으로 부른 요청과
     * 디바운스가 부른 요청이 같은 표시를 공유하는데, 불리언이면 먼저 끝난 한쪽이 아직 도는
     * 다른 쪽의 대기 표시를 꺼 버린다. 웹에서 그 결함을 한 번 겪었다.
     */
    private val flight = AtomicInteger(0)

    val inFlight: Int get() = flight.get()

    /** 코드 완성. 커서 앞뒤를 준다. 양쪽이 다 비면 부르지 않는다 — 콘솔도 그 자리에서 끊는다. */
    fun completeCode(path: String, rawPrefix: String, rawSuffix: String): String? {
        // 부르는 자리가 버퍼를 통째로 줘도 소켓에는 커서 근처만 간다 — 코어가 어차피 자른다.
        val (prefix, suffix) = nearCursor(rawPrefix, rawSuffix)
        if ((prefix.trim() + suffix.trim()).isEmpty()) return null
        // 커서 양쪽은 args 에 JSON 으로 간다. Text 하나로는 한쪽밖에 못 싣는다는 것이
        // internal/adapter/daemon/client.go 의 CompleteCode 주석이 밝히는 사유다.
        val args = buildJsonObject {
            put("prefix", JsonPrimitive(prefix))
            put("suffix", JsonPrimitive(suffix))
        }
        return call { c ->
            val r = c.exchange(Request(method = "complete", name = path, args = args))
            // 왜 빈손인지를 **기억한다.** 매 타건마다 말하면 잡음이라 설정 화면이 읽는다 —
            // 고치는 자리가 거기이기 때문이다(라우팅 키가 같은 화면에 서 있다).
            note(r.error, if (r.out.isNullOrEmpty()) r.reason else null)
            withoutEcho(r.out, prefix)
        }
    }

    companion object {
        /**
         * 모델이 **커서 앞을 다시 뱉은 만큼**을 벗긴다.
         *
         * 완성기는 커서 앞뒤를 다 받고 가운데만 말하기로 돼 있는데, 앞의 꼬리를 같이 되뱉는 일이
         * 있다. 그대로 그리면 회색 글씨에 이미 화면에 있는 글자가 한 번 더 서고, **받아들이면
         * 그 글자가 실제로 두 번 들어간다.** 짝인 VS Code 는 그 사실을 관측해 벗기고 있었고
         * (*"It sometimes re-emits the tail of the prefix"*), 이 판은 아무것도 안 벗겼다.
         * 코어도 안 벗긴다 — 코드 펜스만 걷고 그대로 준다(`internal/app/complete.go`).
         *
         * **가장 긴 겹침**을 벗긴다: 짧은 쪽부터 끊으면 남은 앞부분이 다시 중복이 된다. 완성이
         * 앞의 꼬리와 통째로 같으면 남는 것이 없고, 그건 아무 말도 안 한 것이라 빈 값이 맞다.
         *
         * 꼬리를 200자로 끊는 것도 같은 판의 규칙이다 — 그보다 긴 되뱉음은 완성이 아니라 파일을
         * 다시 쓰는 것이고, 온 버퍼를 훑는 값은 그 드문 경우에 비해 비싸다.
         */
        /**
         * 커서 한쪽에 실어 보낼 글자 수의 상한.
         *
         * 코어가 완성 프롬프트를 한쪽 **24KB** 로 자르고 그 사유를 적어 뒀다 —
         * *"A person can open a 40,000-line file in the console and the buffer travels on every
         * pause in typing; an unbounded prompt here is somebody's context window and their bill."*
         * 자르는 것은 코어이므로 그보다 많이 보내는 것은 **버려질 바이트를 소켓에 싣는 일**이다.
         *
         * 바이트가 아니라 글자로 센다. 코어의 자는 바이트라 한글이 섞이면 코어가 한 번 더 조이지만,
         * 여기서 막으려는 것은 「무한」이라 그 차이는 상관없다.
         */
        const val SIDE_CAP = 24 * 1024

        /** [SIDE_CAP] 을 넘는 만큼은 **커서에서 먼 쪽**을 버린다 — 완성에 쓰이는 것은 가까운 쪽이다. */
        @JvmStatic
        internal fun nearCursor(prefix: String, suffix: String): Pair<String, String> =
            prefix.takeLast(SIDE_CAP) to suffix.take(SIDE_CAP)

        /**
         * 주변 맥락으로 실어 보낼 글자 수의 상한 — 코어의 `ambientCap` 과 같은 수.
         *
         * 바이트 상한에 **글자**로 자른다. 글자는 바이트보다 적을 수 없으니 코어가 남길 바이트만큼은
         * 언제나 실려 가고, 남는 조각이 같다.
         */
        const val AMBIENT_CAP = 8 * 1024

        /** 버퍼의 **머리** — 코어가 남기는 쪽이다. 꼬리를 남기면 모델이 다른 파일을 본다. */
        @JvmStatic
        internal fun ambientHead(text: String): String = text.take(AMBIENT_CAP)

        @JvmStatic
        internal fun withoutEcho(out: String?, prefix: String): String? {
            val t = out?.replace("\r", "") ?: return out
            if (t.isBlank()) return t
            val tail = prefix.takeLast(200)
            for (n in minOf(tail.length, t.length) downTo 1) {
                if (tail.endsWith(t.take(n))) return t.drop(n)
            }
            return t
        }

        /**
         * 마지막 완성이 빈손이었던 사유, 있으면.
         *
         * **인스턴스가 아니라 여기 산다.** 이 클래스는 호출마다 새로 만들어지므로(에디터의
         * 완성 자리를 보라) 인스턴스 필드에 적으면 그 자리에서 사라진다 — 기억할 것은 한 자리에
         * 있어야 읽는 쪽이 하나다.
         *
         * 글자가 나온 완성은 이 값을 지운다. 한 번 못 뜬 사유가 잘 되는 동안에도 화면에 남아
         * 있으면 그 문장이 늙는다 — 이 트리가 오늘 다섯 번 겪은 그 부류다.
         */
        @Volatile
        @JvmStatic
        var lastEmpty: String? = null
            internal set

        /**
         * 마지막 거들기 왕복이 **거부**당한 사유, 있으면 — 문이 제 말로 말한 문장 그대로.
         *
         * [lastEmpty] 와 갈라 둔다: 저쪽은 이 화면이 아는 코드(`off`·`unrouted`…)라 번역된
         * 문장으로 그리고, 이쪽은 데몬의 산문이라 **그대로** 그린다. 한 칸에 섞으면 번역 열쇠를
         * 못 찾은 문장이 `set.complete.why.this daemon cannot…` 으로 찍힌다.
         */
        @Volatile
        @JvmStatic
        var lastRefused: String? = null
            internal set

        /**
         * [lastEmpty] 로 올 수 있는 **코드의 전부** — 코어 `internal/app/complete.go` 의
         * `CompleteReason` 이 정하고 여기는 옮겨 적기만 한다(`WireConformanceTest` 가 견준다).
         *
         * 목록이 필요한 이유는 **모르는 코드**다. 이 화면은 코드로 번들 열쇠를 지어
         * (`set.complete.why.<코드>`) 문장을 찾는데, 새 데몬이 다섯째 코드를 보내면 그 열쇠가
         * 없어 플랫폼 경로가 답하고 화면에는 `!set.complete.why.throttled!` 같은 **배관**이
         * 뜬다. 사유를 알리려던 자리가 사유 대신 제 구현을 보이는 것이다.
         *
         * 그래서 아는 코드만 문장으로 바꾸고 **나머지는 데몬의 낱말 그대로** 보인다 — 짝인
         * VS Code 가 같은 자리에서 정한 규칙이고("모르는 코드는 날것으로"), 이 트리가 [lastRefused]
         * 를 따로 둔 이유와도 같다.
         */
        @JvmStatic
        val emptyReasons: Set<String> = setOf("off", "unrouted", "nothing-asked", "no-answer")

        /**
         * 못 뜬 사유를 그릴 때 쓸 **번들 열쇠**, 아는 코드일 때만. 모르면 null 이고 그때 화면은
         * 코드를 그대로 보인다.
         */
        @JvmStatic
        fun emptyKey(code: String?): String? =
            code?.takeIf { it in emptyReasons }?.let { "set.complete.why.$it" }
    }

    /**
     * 이번 왕복이 왜 빈손인지를 한 자리에 적는다.
     *
     * **거부와 「할 말 없음」은 다른 칸으로 온다.** 문이 못 하겠다고 할 때는 `err` 이고
     * (`this daemon cannot complete code`), 완성기가 그냥 아무 말도 안 했을 때는 `OK` 에
     * `reason` 이다. 여기가 `reason` 만 읽던 동안 설정 화면은 **거부를 영영 못 그렸다** —
     * 「자동완성이 왜 죽었나」의 답이 아무도 안 읽는 칸에 들어 있었다.
     *
     * 네 문이 전부 같은 인터페이스([Reviewer])에 걸리므로 자리도 하나다: 하나가 거부하면
     * 넷 다 거부한다. 성공한 왕복은 지운다 — 잘 되는 동안 남아 있는 사유는 늙는다.
     */
    private fun note(err: String?, why: String?) {
        val bad = err?.takeIf { it.isNotBlank() }
        lastRefused = bad
        lastEmpty = if (bad == null) why?.takeIf { it.isNotBlank() } else null
    }

    /** 컴포저 제안. 사람이 치던 지시를 어떻게 끝낼지. */
    fun suggest(prefix: String): String? {
        if (prefix.isBlank()) return null
        return call { c ->
            val r = c.exchange(Request(method = "suggest", text = prefix))
            note(r.error, null)
            r.out
        }
    }

    /**
     * 룩오버. 모델이 어깨너머로 읽고 몇 가지를 짚는다. 데몬 메서드는 `look-over` 다 —
     * 콘솔의 라우트 이름(`/look`)과 다르므로 그쪽을 보고 옮기면 틀린다.
     */
    fun lookOver(path: String, text: String): String? {
        if (text.isBlank()) return null
        return call { c ->
            val r = c.exchange(Request(method = "look-over", name = path, text = text))
            note(r.error, null)
            // 거부는 **답으로 돌려준다.** 이 문은 사람이 눌러서 열리고, 누른 자리에 판이 있다 —
            // 삼키면 그 판이 「할 말이 없다」고 적고, 데몬은 왜 못 하는지 말했는데 아무도 안 읽는다.
            r.out?.takeIf { it.isNotBlank() } ?: r.error?.takeIf { it.isNotBlank() }
        }
    }

    /**
     * 지금 열어 둔 버퍼를 알린다. 모델 호출이 아니고 기록도 아니다 — 다음 턴이 주변 맥락으로
     * 볼 뿐이다. 사람이 저장을 안 한 채 두면 `read` 툴은 디스크를 읽으므로, 이걸 안 보내면
     * 에이전트가 낡은 내용을 추론한다.
     */
    fun setOpenFile(path: String, text: String): Boolean =
        call { c ->
            // **머리만.** 이 문은 사람이 아무것도 안 누르는 주변 맥락이라 버퍼가 바뀔 때마다
            // 나가는데, 코어는 그것의 머리 8KB 만 저장한다(`ambientCap`) — 그 주석이 사유를 이름
            // 댄다: *"holding the whole of a 40MB buffer per session for the daemon's life is
            // memory for nothing."* **저장할 때** 자르므로 코어의 메모리는 안전했고 소켓만 내내
            // 파일 전체를 실었다. 모델이 보는 것은 안 바뀐다 — 코어가 남기는 것이 머리다.
            val r = c.exchange(Request(method = "open-file", name = path, text = ambientHead(text)))
            note(r.error, null)
            if (r.ok) "y" else null
        } != null

    private fun <T> call(work: (Daemon) -> T?): T? {
        if (!enabled()) return null
        flight.incrementAndGet()
        return try {
            open().use(work)
        } catch (_: Exception) {
            // 거들기는 실패해도 조용하다. 사람이 타이핑하는 중에 뜨는 에러 상자는 도움이 아니고,
            // 이 넷 중 무엇도 못 왔다고 해서 작업이 막히지 않는다. 대기 표시만 내려간다.
            null
        } finally {
            flight.decrementAndGet()
        }
    }
}
