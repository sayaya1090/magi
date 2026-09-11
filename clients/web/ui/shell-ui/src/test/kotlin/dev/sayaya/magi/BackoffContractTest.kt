package dev.sayaya.magi

import dev.sayaya.magi.bridge.Backoff
import io.kotest.core.spec.style.StringSpec
import io.kotest.matchers.shouldBe
import java.io.File

/**
 * **물러서는 시간의 정본은 이 파일이 아니라 `clients/contract/lifecycle-policy.json` 이다.**
 *
 * 두 편집기(`LaunchesTest`·`launches.test.ts`)가 이미 그 파일을 읽는다. 이 콘솔만 회선이
 * 끊기면 **고정 1.5초로 무한히** 다시 붙으러 갔다 — 지터도 물러섬도 없이(실측 2026-09-11,
 * `FetchRosterSource.open` 의 error 처리). 그러면 서버가 한 번 재시작할 때 열려 있던 **모든
 * 탭이 같은 순간에** 몰린다. SSE 는 탭마다 회선 하나라 그 쏠림이 편집기보다 크다.
 *
 * GWT 로 컴파일되는 코드는 실행 중에 파일을 못 읽으므로 수는 [Backoff] 에 박힌다. 그것이
 * 계약과 갈리지 않는 것을 여기서 잰다 — **양 끝(0.0·1.0)을 직접 넣어** 경계를 본다. 무작위로
 * 굴리는 시험은 상한이 지터에 먹히는 것(30초 단계가 36초가 되는 것)을 못 짚는다.
 */
internal class BackoffContractTest : StringSpec({
    val contract = generateSequence(File(System.getProperty("user.dir"))) { it.parentFile }
        .first { File(it, "clients/contract/lifecycle-policy.json").isFile }
        .let { File(it, "clients/contract/lifecycle-policy.json").readText() }

    fun num(key: String): Double =
        Regex("\"$key\"\\s*:\\s*([0-9.]+)").find(contract)!!.groupValues[1].toDouble()

    fun steps(): List<Triple<Int, Long, Long>> =
        Regex("""\{"attempt":\s*(\d+),\s*"min":\s*(\d+),\s*"max":\s*(\d+)\}""")
            .findAll(contract)
            .map { Triple(it.groupValues[1].toInt(), it.groupValues[2].toLong(), it.groupValues[3].toLong()) }
            .toList()

    "물러서는 시간이 계약의 범위 안에 있고, 상한을 넘지 않는다" {
        val cases = steps()
        // ⚠ 바닥. 계약을 못 읽으면 아래가 0 번 돌고 그 침묵이 통과처럼 보인다.
        (cases.size >= 6) shouldBe true
        for ((n, lo, hi) in cases) {
            Backoff.delayMs(n, 0.0).toLong() shouldBe lo
            Backoff.delayMs(n, 1.0).toLong() shouldBe hi
            (Backoff.delayMs(n, 1.0) <= num("backoffCapMs")) shouldBe true
        }
        Backoff.JITTER shouldBe num("jitterFraction")
        Backoff.CAP_MS.toDouble() shouldBe num("backoffCapMs")
    }

    /**
     * 그리고 **재연결이 그것을 지난다.** 계약이 옳아도 회선이 옛 상수를 쓰면 시험은 영원히
     * 초록이고 화면은 옛 규칙대로 돈다 — 이 트리가 되풀이해 겪은 그 자리다. 브라우저를 띄우지
     * 않고 소스로 재되, 옛 값이 돌아오는 것도 같이 막는다.
     */
    "회선이 제 상수를 다시 들지 않는다" {
        val src = generateSequence(File(System.getProperty("user.dir"))) { it.parentFile }
            .first { File(it, "clients/web/ui/shell-ui").isDirectory }
            .let {
                File(it, "clients/web/ui/shell-ui/src/main/java/dev/sayaya/magi/client/" +
                    "interfaces/api/FetchRosterSource.java").readText()
            }
        src.contains("Backoff.delayMs(") shouldBe true
        // 고정 1.5초가 그 자리였다.
        Regex("""setTimeout\([^;]*?,\s*1500\s*\)""").containsMatchIn(src) shouldBe false
        // 붙으면 셈이 0 으로 — 안 지우면 한참 뒤의 첫 끊김이 30초를 기다린다.
        //
        // ⚠ **「어딘가에 있다」로는 못 잰다.** 처음엔 파일 전체에서 `attempt = 0` 을 찾았는데,
        // 같은 줄이 조준을 바꾸는 `reopen()` 에도 있어서 **붙는 자리에서 통째로 지워도
        // 통과했다.** open 리스너 안을 본다.
        val opened = src.substringAfter("""addEventListener("open"""", "").substringBefore("});")
        (opened.isNotEmpty()) shouldBe true
        opened.contains("attempt = 0") shouldBe true
    }
})
