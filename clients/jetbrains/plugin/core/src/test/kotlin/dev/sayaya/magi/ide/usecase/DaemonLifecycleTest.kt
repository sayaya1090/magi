package dev.sayaya.magi.ide.usecase

import dev.sayaya.magi.ide.model.Request
import dev.sayaya.magi.ide.model.Response
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.nio.file.Path
import java.nio.file.Paths

/**
 * 데몬 수명주기의 규칙을 소켓 없이 시험한다.
 *
 * 이 시험이 여기 있는 것이 [Daemons] 포트를 만든 이유다. 전에는 이 클래스가 `DaemonClient` 를
 * 직접 잡고 있어서, 판정을 시험하려면 진짜 유닉스 소켓을 여닫아야 했다. 규칙과 전송이 갈리자
 * 규칙만 따로 돌릴 수 있게 됐다.
 *
 * 붙거나 띄우는 경로(`attachOrStart`)와 그 백오프·지터 시험은 2026-09-26 에 걷었다 — 제품이 한
 * 번도 부르지 않았다. 제품의 재기동 백오프는 `Launches` 가 하고 `LaunchesTest` 가 잰다.
 */
class DaemonLifecycleTest {
    private class Child : Process() {
        var alive = true
        var stops = 0
        override fun isAlive() = alive
        override fun destroy() { stops++; alive = false }
        override fun waitFor() = 0
        override fun exitValue() = if (alive) throw IllegalThreadStateException() else 0
        override fun getInputStream() = java.io.InputStream.nullInputStream()
        override fun getErrorStream() = java.io.InputStream.nullInputStream()
        override fun getOutputStream() = java.io.OutputStream.nullOutputStream()
    }

    @Test
    fun `closing stops owned process and refuses late download completion`() {
        val owner = DaemonProcess()
        val child = Child()
        assertEquals(child, owner.launch { child })
        assertEquals(null, owner.launch { error("duplicate launch") })
        owner.close()
        owner.close()
        assertEquals(1, child.stops)
        assertEquals(null, owner.launch { error("project already closed") })
    }

    @Test
    fun `failed child can retry and stale cleanup cannot stop its successor`() {
        val owner = DaemonProcess()
        val first = Child()
        owner.launch { first }
        first.alive = false
        val next = Child()
        assertEquals(next, owner.launch { next })
        owner.stop(first)
        assertTrue(next.alive)
        owner.close()
        assertEquals(1, next.stops)
    }

    @Test
    fun `close racing a launch still terminates the child`() {
        val owner = DaemonProcess()
        val entered = java.util.concurrent.CountDownLatch(1)
        val release = java.util.concurrent.CountDownLatch(1)
        val pool = java.util.concurrent.Executors.newFixedThreadPool(2)
        val child = Child()
        try {
            val start = pool.submit<Process?> { owner.launch {
                entered.countDown()
                check(release.await(5, java.util.concurrent.TimeUnit.SECONDS))
                child
            } }
            assertTrue(entered.await(5, java.util.concurrent.TimeUnit.SECONDS))
            val close = pool.submit { owner.close() }
            release.countDown()
            assertEquals(child, start.get(5, java.util.concurrent.TimeUnit.SECONDS))
            close.get(5, java.util.concurrent.TimeUnit.SECONDS)
            assertEquals(1, child.stops)
        } finally { release.countDown(); owner.close(); pool.shutdownNow() }
    }

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

    private fun lifecycle(f: Fake) = DaemonLifecycle(socket = sock, daemons = f)

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
}
