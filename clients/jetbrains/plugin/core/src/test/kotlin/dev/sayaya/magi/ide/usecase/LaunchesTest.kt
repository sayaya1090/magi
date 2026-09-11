package dev.sayaya.magi.ide.usecase

import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.boolean
import kotlinx.serialization.json.double
import kotlinx.serialization.json.int
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.long
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.io.File

/**
 * **계약은 이 파일이 아니라 `clients/contract/lifecycle-policy.json` 이다.**
 *
 * 두 편집기가 같은 규칙을 따라야 하는데, 각자의 시험에 각자 사례를 적으면 「둘 다 초록인데 서로
 * 다른 규칙」이 된다 — 이 정책이 정확히 그 상태였다(2026-09-11 실측: 이쪽은 평생 3회·60초 간격,
 * VS Code 는 60초 이동 구간 3회). 그래서 사례를 한 파일에 두고 양쪽이 **그 파일을** 읽는다.
 * `docs/CLIENT_LIFECYCLE` §7 이 「공유 fixture 는 동작 계약을 담고 특정 구현의 소스 문자열을
 * 요구하지 않는다」고 적은 그 모양이다.
 *
 * 시계는 가상이다. 예산 하나가 60초짜리라 실제로 기다리는 시험은 쓸 수 없고, 안 재는 시험은
 * 없는 것과 같다.
 */
class LaunchesTest {

    private val contract: JsonObject by lazy {
        // core 의 test 는 `plugin/core` 에서 돈다. 네 칸 위가 저장소 뿌리다.
        val root = generateSequence(File(System.getProperty("user.dir"))) { it.parentFile }
            .first { File(it, "clients/contract/lifecycle-policy.json").isFile }
        Json.parseToJsonElement(File(root, "clients/contract/lifecycle-policy.json").readText()).jsonObject
    }

    private fun policy(): Launches {
        val p = contract["policy"]!!.jsonObject
        fun l(k: String) = p[k]!!.jsonPrimitive.long
        fun i(k: String) = p[k]!!.jsonPrimitive.int
        return Launches(
            windowMs = l("windowMs"), spawnsPerWindow = i("spawnsPerWindow"),
            failuresToBlock = i("failuresToBlock"), graceMs = l("graceMs"), stableMs = l("stableMs"),
        )
    }

    @Test
    fun `계약의 사례를 전부 통과한다`() {
        val cases = contract["cases"]!!.jsonArray
        assertTrue(cases.size >= 10, "사례가 ${cases.size}개뿐이다 — 계약 파일을 못 읽었거나 비었다")
        for (case in cases) {
            val c = case.jsonObject
            val name = c["name"]!!.jsonPrimitive.content
            val l = policy()
            for (step in c["steps"]!!.jsonArray) {
                val s = step.jsonObject
                when {
                    s.containsKey("ask") -> {
                        val at = s["ask"]!!.jsonPrimitive.long
                        val manual = s["manual"]?.jsonPrimitive?.boolean ?: false
                        val want = s["want"]!!.jsonPrimitive.content
                        val got = l.may(at, manual).name.lowercase()
                        assertEquals(want, got, "«$name» ${at}ms 에서")
                    }
                    s.containsKey("spawned") -> l.spawned(s["spawned"]!!.jsonPrimitive.long)
                    s.containsKey("ready") -> l.ready(s["ready"]!!.jsonPrimitive.long)
                    s.containsKey("connected") -> l.connected(s["connected"]!!.jsonPrimitive.long)
                    s.containsKey("stable") -> l.stable(s["stable"]!!.jsonPrimitive.long)
                    s.containsKey("lost") -> l.lost(s["lost"]!!.jsonPrimitive.long)
                    s.containsKey("failed") -> l.failed(s["failed"]!!.jsonPrimitive.long)
                    s.containsKey("replaced") -> l.replaced(s["replaced"]!!.jsonPrimitive.long)
                    s.containsKey("replaceFailed") -> l.replaceFailed(s["replaceFailed"]!!.jsonPrimitive.long)
                    s.containsKey("userStopped") -> l.userStopped(s["userStopped"]!!.jsonPrimitive.long)
                    else -> throw AssertionError("«$name» 에 모르는 단계가 있다: $s")
                }
            }
        }
    }

    /**
     * 물러서는 시간도 계약이 정한다. **양 끝을 직접 넣어 경계를 잰다** — 무작위로 굴려
     * 「대충 맞다」고 말하는 시험은 30초 단계에 지터가 얹혀 36초가 되는 것을 못 짚는다.
     */
    @Test
    fun `물러서는 시간이 계약의 범위 안에 있고, 상한을 넘지 않는다`() {
        val steps = contract["backoff"]!!.jsonObject["steps"]!!.jsonArray
        assertTrue(steps.size >= 6, "백오프 사례가 ${steps.size}개뿐이다")
        val cap = contract["policy"]!!.jsonObject["backoffCapMs"]!!.jsonPrimitive.long
        for (step in steps) {
            val s = step.jsonObject
            val n = s["attempt"]!!.jsonPrimitive.int
            val lo = s["min"]!!.jsonPrimitive.long
            val hi = s["max"]!!.jsonPrimitive.long
            assertEquals(lo, Backoff.delayMs(n, 0.0), "시도 $n 의 아래 끝")
            assertEquals(hi, Backoff.delayMs(n, 1.0), "시도 $n 의 위 끝")
            assertTrue(Backoff.delayMs(n, 1.0) <= cap, "시도 $n 이 상한 ${cap}ms 를 넘는다")
        }
        // 지터 비율도 계약의 것이다 — 구현이 제 값을 들고 있으면 계약과 갈린다.
        assertEquals(
            contract["policy"]!!.jsonObject["jitterFraction"]!!.jsonPrimitive.double,
            Backoff.JITTER,
            "지터 비율이 계약과 다르다",
        )
    }
}
