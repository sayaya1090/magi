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
    fun completeCode(path: String, prefix: String, suffix: String): String? {
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
            r.out
        }
    }

    companion object {
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
            val r = c.exchange(Request(method = "open-file", name = path, text = text))
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
