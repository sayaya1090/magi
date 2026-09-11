package dev.sayaya.magi.ide.usecase

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Assertions.assertTimeoutPreemptively
import org.junit.jupiter.api.Test
import java.io.File
import java.nio.file.Files
import java.time.Duration

/**
 * 기능 조회 — **진짜 프로세스로** 잰다.
 *
 * 페이크 프로세스로는 이 물음을 못 잰다. 재려는 것이 「막히는 읽기와 기한 중 어느 쪽이 먼저
 * 서는가」이고, 그건 파이프의 성질이지 내가 흉내 낼 수 있는 것이 아니다.
 */
class CoreProbeTest {

    private fun script(body: String): List<String> {
        val f = Files.createTempFile("probe", ".sh").toFile()
        f.writeText("#!/bin/sh\n$body\n")
        f.setExecutable(true)
        f.deleteOnExit()
        return listOf(f.absolutePath)
    }

    @Test
    fun `알린 이름을 그대로 읽는다`() {
        val got = CoreProbe.features(script("""echo '{"features":["raw-socket-v1","owned-daemon-v1"],"protocol":1}'"""))
        assertEquals(setOf("raw-socket-v1", "owned-daemon-v1"), got)
    }

    /**
     * ⚠ 이 커밋의 근거. stdout 을 **열어 둔 채 아무것도 안 쓰고** 오래 사는 탐침 —
     * 읽기가 기한보다 먼저 서면 여기서 영영 안 돌아온다.
     */
    @Test
    fun `줄을 안 보내는 탐침도 기한 안에 답한다`() {
        val hangs = script("sleep 30")
        val began = System.nanoTime()
        // ⚠ 타입 인자를 적어야 한다. `assertTimeoutPreemptively` 에는 값을 안 돌려주는
        // `Executable` 판이 같이 있고, 안 적으면 코틀린이 그쪽을 골라 `got` 이 **Unit** 이 된다 —
        // 그러면 아래 비교는 조회 결과가 아니라 Unit 을 빈 집합과 견준다.
        val got = assertTimeoutPreemptively<Set<String>>(Duration.ofSeconds(10)) {
            CoreProbe.features(hangs, timeoutMs = 300)
        }
        val tookMs = (System.nanoTime() - began) / 1_000_000
        assertEquals(emptySet<String>(), got)
        assertTrue(tookMs < 5_000, "기한 300ms 인데 ${tookMs}ms 걸렸다 — 기한이 읽기 뒤에 서 있다")
    }

    /** 그리고 남기지 않는다: 시한 초과한 자식은 죽어 있어야 한다. */
    @Test
    fun `기한에 걸린 자식을 남기지 않는다`() {
        val marker = Files.createTempFile("probe-alive", ".txt").toFile()
        marker.deleteOnExit()
        // 살아 있는 동안 계속 흔적을 갱신하는 자식. 조회가 돌아온 뒤로는 멈춰 있어야 한다.
        // ⚠ 기한 안에 감싼다. 재려는 구현이 **안 끝나는** 것이 바로 이 시험이 잡는 결함이라,
        // 안 감싸면 실패가 정지로 나타난다 — 기한이 읽기 뒤에 선 판으로 돌려 봤더니 이 줄이
        // 끝없이 도는 자식 앞에서 영영 안 돌아왔고, 빌드가 멈춘 채로 남았다(2026-09-11 실측).
        assertTimeoutPreemptively<Set<String>>(Duration.ofSeconds(10)) {
            CoreProbe.features(script("while true; do date +%s%N > ${marker.absolutePath}; sleep 0.05; done"), timeoutMs = 300)
        }
        val first = File(marker.absolutePath).readText()
        Thread.sleep(600)
        assertEquals(first, File(marker.absolutePath).readText(),
            "조회가 끝난 뒤에도 자식이 살아서 쓰고 있다 — 시한 초과에 아무도 안 치웠다")
    }

    /**
     * 구형 바이너리는 **옵션을 거절하는 것으로** 답한다 — 종료 코드 2, 빈 stdout. 그런데 빈
     * stdout 만 재면 종료 코드를 안 보는 구현도 통과한다(파싱이 어차피 실패하므로). 그래서
     * 말은 멀쩡히 하면서 거절하는 쪽으로 잰다: **종료 코드가 이긴다.**
     */
    @Test
    fun `말은 하지만 거절한 바이너리는 빈 집합이다`() {
        val refuses = script("""echo '{"features":["raw-socket-v1"]}'; exit 2""")
        assertEquals(emptySet<String>(), CoreProbe.features(refuses))
    }

    @Test
    fun `깨진 줄은 빈 집합이다`() {
        assertEquals(emptySet<String>(), CoreProbe.features(script("echo 'not json at all'")))
    }

    @Test
    fun `없는 바이너리는 빈 집합이다`() {
        assertEquals(emptySet<String>(), CoreProbe.features(listOf("/nonexistent/magi", "ide-bridge", "--features")))
    }
}
