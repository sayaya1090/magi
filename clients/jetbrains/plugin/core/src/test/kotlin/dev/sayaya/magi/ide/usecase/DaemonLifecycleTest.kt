package dev.sayaya.magi.ide.usecase

import dev.sayaya.magi.ide.model.Request
import dev.sayaya.magi.ide.model.Response
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.nio.file.Path
import java.nio.file.Paths
import kotlin.random.Random

/**
 * 데몬 수명주기의 규칙을 소켓 없이 시험한다.
 *
 * 이 시험이 여기 있는 것이 [Daemons] 포트를 만든 이유다. 전에는 이 클래스가 `DaemonClient` 를
 * 직접 잡고 있어서, 백오프를 시험하려면 진짜 유닉스 소켓을 여닫고 진짜로 자야 했다 — 그래서
 * 시험이 하나도 없었다. 규칙과 전송이 갈리자 규칙만 따로 돌릴 수 있게 됐다.
 *
 * 자는 것과 난수도 주입한다. 시험이 벽시계를 기다리면 느린 것이 문제가 아니라 **CI 에서 가끔
 * 실패**하는 것이 문제다.
 */
class DaemonLifecycleTest {

    private val sock: Path = Paths.get("/tmp/does-not-matter.sock")

    /** 시험용 전송. 붙는 시도를 몇 번째부터 받아 줄지, 붙어 보면 무엇을 만나는지를 대본으로 준다. */
    private class Fake(
        var aliveFrom: Int = Int.MAX_VALUE,
        var reach: Reach = Reach.Refused,
        var unusable: String? = null,
    ) : Daemons {
        var attempts = 0
        var starts = 0
        override fun connect(socket: Path): Daemon {
            attempts++
            if (attempts < aliveFrom) throw java.io.IOException("아직 아무도 안 듣는다")
            return object : Daemon {
                override fun exchange(request: Request) = Response(ok = true)
                // 수명주기는 스트림을 안 쓴다. 부르면 시험이 조용히 아무것도 안 하는 대신 터지게 둔다 —
                // 안 쓰는 것을 빈 몸으로 채우면 나중에 쓰기 시작해도 아무 말이 없다.
                override fun stream(request: Request, each: (Response) -> Boolean) =
                    throw UnsupportedOperationException("수명주기는 스트림을 열지 않는다")
                override fun close() {}
            }
        }
        override fun reach(socket: Path) = reach
        override fun unusable(socket: Path) = unusable
    }

    private fun lifecycle(f: Fake, slept: MutableList<Long> = mutableListOf()) =
        DaemonLifecycle(
            socket = sock,
            start = { f.starts++ },
            daemons = f,
            sleep = { slept += it },
            random = Random(1),
        )

    // ── 판정 ──────────────────────────────────────────────────────────────────

    @Test
    fun `답하면 살아있다`() {
        assertEquals(DaemonLifecycle.Verdict.Alive, lifecycle(Fake(reach = Reach.Listening)).verdict())
    }

    @Test
    fun `소켓 파일이 없으면 질서 있게 나간 것이라 되살리지 않는다`() {
        assertEquals(DaemonLifecycle.Verdict.Left, lifecycle(Fake(reach = Reach.Absent)).verdict())
    }

    @Test
    fun `붙기를 거절당했으면 죽임을 당한 것이다`() {
        assertEquals(DaemonLifecycle.Verdict.Killed, lifecycle(Fake(reach = Reach.Refused)).verdict())
    }

    @Test
    fun `물어볼 수 없었던 것은 죽은 것이 아니고 사유가 그대로 실려 나간다`() {
        // 이 갈래가 이 커밋의 요지다. 예전엔 `alive()` 가 예외를 전부 false 로 접었고 그 false 가
        // 여기서 「죽임을 당했다」로 펴졌다 — **못 물어본 것이 데몬에 대한 긍정 진술이 됐고**,
        // 그 판정에는 재기동이 달려 있다. 갈래가 생겼으니 이제 그렇게 못 쓴다.
        //
        // 사유를 같이 재는 이유: 갈래만 나누고 `Unknown` 을 빈 몸으로 두면 「모른다」까지만 남고
        // **무엇을 만났는지가 사라진다.** 그러면 사람은 왜 안 되는지 모른 채 창을 닫았다 연다.
        val v = lifecycle(Fake(reach = Reach.CouldNotAsk("SocketException: … non-socket"))).verdict()
        assertEquals(DaemonLifecycle.Verdict.Unknown("SocketException: … non-socket"), v)
    }

    // ── 붙거나 띄우거나 ───────────────────────────────────────────────────────

    @Test
    fun `이미 있으면 띄우지 않고 붙는다`() {
        val f = Fake(aliveFrom = 1)
        val out = lifecycle(f).attachOrStart()
        assertTrue(out is DaemonLifecycle.Outcome.Attached)
        assertEquals(0, f.starts, "띄운 적이 없어야 한다")
        assertEquals(1, f.attempts, "첫 시도에 붙었어야 한다")
    }

    @Test
    fun `없으면 띄우고 붙는다`() {
        val f = Fake(aliveFrom = 3)
        val out = lifecycle(f).attachOrStart()
        assertTrue(out is DaemonLifecycle.Outcome.Attached)
        assertEquals(1, f.starts, "기동은 한 번뿐이어야 한다")
    }

    @Test
    fun `기동은 성공한 뒤 다시 부르지 않는다`() {
        // 붙기까지 여러 번 걸려도 프로세스를 여러 개 띄우면 안 된다. flock 이 막아 주지만
        // 막힌 프로세스가 매번 뜨는 것은 그 자체로 결함이다.
        val f = Fake(aliveFrom = 5)
        lifecycle(f).attachOrStart(attempts = 6)
        assertEquals(1, f.starts)
    }

    @Test
    fun `못 붙으면 빈 화면이 아니라 이유를 말한다`() {
        val f = Fake(aliveFrom = Int.MAX_VALUE)
        val out = lifecycle(f).attachOrStart(attempts = 2)
        assertTrue(out is DaemonLifecycle.Outcome.Unreachable)
        assertTrue(
            (out as DaemonLifecycle.Outcome.Unreachable).reason.contains(sock.toString()),
            "이유에 소켓 경로가 있어야 사람이 어디를 볼지 안다",
        )
    }

    @Test
    fun `열 수 없는 경로면 띄워 보지도 않는다`() {
        val f = Fake(unusable = "소켓 경로가 너무 길다")
        val out = lifecycle(f).attachOrStart()
        assertEquals(DaemonLifecycle.Outcome.Unreachable("소켓 경로가 너무 길다"), out)
        assertEquals(0, f.starts, "경로가 틀렸는데 프로세스를 띄우면 안 된다")
        assertEquals(0, f.attempts)
    }

    // ── 백오프 ────────────────────────────────────────────────────────────────

    @Test
    fun `백오프는 늘어나고 상한에서 멈춘다`() {
        val slept = mutableListOf<Long>()
        val f = Fake(aliveFrom = Int.MAX_VALUE)
        lifecycle(f, slept).attachOrStart(attempts = 8, firstBackoffMillis = 200)
        assertEquals(8, slept.size)
        assertTrue(slept[1] > slept[0], "두 번째가 첫 번째보다 길어야 한다: $slept")
        assertTrue(slept.all { it <= 3_000 }, "상한 2000ms + 지터를 넘으면 안 된다: $slept")
    }

    @Test
    fun `지터가 붙는다`() {
        // 창 셋이 한꺼번에 열리면 같은 박자로 재시도해 같은 순간에 또 부딪힌다. 그래서 매번
        // 다른 만큼 잔다 — 대기값이 전부 같으면 지터가 죽은 것이다.
        val slept = mutableListOf<Long>()
        lifecycle(Fake(aliveFrom = Int.MAX_VALUE), slept)
            .attachOrStart(attempts = 6, firstBackoffMillis = 400)
        assertTrue(slept.any { it % 100L != 0L }, "지터 없이 전부 2의 거듭제곱이면 안 된다: $slept")
    }
}
