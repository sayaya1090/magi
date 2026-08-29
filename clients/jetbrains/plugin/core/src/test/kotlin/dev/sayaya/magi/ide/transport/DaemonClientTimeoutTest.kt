package dev.sayaya.magi.ide.transport

import dev.sayaya.magi.ide.model.Request
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.net.StandardProtocolFamily
import java.net.UnixDomainSocketAddress
import java.nio.channels.ServerSocketChannel
import java.nio.file.Files
import java.nio.channels.Channels
import kotlin.concurrent.thread

/**
 * 워치독의 골든 두 벌 — 시한 경로(웨지가 DaemonGone 으로 돌아온다)와 취소 경로(시한 전의 답은
 * 성공하고 같은 연결의 둘째 교환도 산다). 한쪽만 재면 cancel 을 지우는 회귀가 조용히 지나간다.
 */
class DaemonClientTimeoutTest {

    @Test
    fun `시한 안에 답하면 워치독이 물러난다 — 같은 연결의 둘째 교환도 산다`() {
        // 반대편 골든(리뷰 F4): cancel 을 지우는 회귀·무장 순서 회귀를 시한 경로 시험은 못 잡는다.
        val path = Files.createTempDirectory("magi-ok").resolve("d.sock")
        val server = ServerSocketChannel.open(StandardProtocolFamily.UNIX).apply {
            bind(UnixDomainSocketAddress.of(path))
        }
        thread(isDaemon = true) {
            runCatching {
                server.accept().use { ch ->
                    val r = Channels.newReader(ch, Charsets.UTF_8).buffered()
                    val w = Channels.newWriter(ch, Charsets.UTF_8).buffered()
                    repeat(2) {
                        r.readLine() ?: return@use
                        w.write("{\"ok\":true}\n"); w.flush()
                    }
                }
            }
        }
        DaemonClient.connect(path, patienceMs = 2_000).use { c ->
            assertTrue(c.exchange(Request(method = "status")).ok, "시한 전의 답은 성공이다")
            assertTrue(c.exchange(Request(method = "status")).ok,
                "취소된 워치독이 뒤늦게 연결을 닫으면 여기가 죽는다 — 둘째 교환이 산다는 것이 취소의 증명")
        }
        server.close()
    }

    @Test
    fun `웨지된 데몬은 시한 안에 DaemonGone 으로 돌아온다`() {
        val path = Files.createTempDirectory("magi-wedge").resolve("d.sock")
        val server = ServerSocketChannel.open(StandardProtocolFamily.UNIX).apply {
            bind(UnixDomainSocketAddress.of(path))
        }
        thread(isDaemon = true) { runCatching { server.accept() } /* 받고 아무것도 안 한다 */ }
        val t0 = System.nanoTime()
        val err = runCatching {
            DaemonClient.connect(path, patienceMs = 400).use { it.exchange(Request(method = "status")) }
        }.exceptionOrNull()
        val ms = (System.nanoTime() - t0) / 1_000_000
        server.close()
        assertTrue(err is DaemonClient.DaemonGone, "웨지는 무한 대기가 아니라 DaemonGone 이어야 한다: $err")
        assertTrue(err!!.message!!.contains("시한"), "사유에 시한이 이름으로 선다: ${err.message}")
        assertTrue(ms in 300..5_000, "시한(400ms) 언저리에 돌아와야 한다 — 실측 ${ms}ms")
    }
}
