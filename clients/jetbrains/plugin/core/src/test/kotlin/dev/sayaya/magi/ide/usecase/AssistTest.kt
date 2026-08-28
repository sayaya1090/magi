package dev.sayaya.magi.ide.usecase

import dev.sayaya.magi.ide.transport.DaemonClient
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.net.StandardProtocolFamily
import java.net.UnixDomainSocketAddress
import java.nio.channels.Channels
import java.nio.channels.ServerSocketChannel
import java.nio.file.Files
import kotlin.concurrent.thread

/**
 * 거들기 넷이 데몬에게 **콘솔과 같은 말**을 하는지 본다.
 *
 * 여기서 한 번 틀렸다: 메서드 이름을 콘솔 라우트(`/look`)에서 가져와 `look` 이라 썼는데
 * 데몬 메서드는 `look-over` 다. 그리고 완성의 커서 양쪽은 `text` 가 아니라 `args` 에 간다.
 * 둘 다 조용히 안 되는 종류라 여기에 못박는다.
 */
class AssistTest {

    private class Fake(private val replies: List<String>) {
        val seen = mutableListOf<String>()
        val path = Files.createTempDirectory("magi-assist").resolve("d.sock")
        private val server = ServerSocketChannel.open(StandardProtocolFamily.UNIX)
            .apply { bind(UnixDomainSocketAddress.of(path)) }

        fun start() = thread(isDaemon = true) {
            repeat(replies.size) { i ->
                server.accept().use { ch ->
                    val r = Channels.newReader(ch, Charsets.UTF_8).buffered()
                    val w = Channels.newWriter(ch, Charsets.UTF_8).buffered()
                    r.readLine()?.let { seen += it }
                    w.write(replies[i]); w.write("\n"); w.flush()
                }
            }
        }

        fun close() = server.close()
        fun opener(): () -> DaemonClient = { DaemonClient.connect(path) }
    }

    @Test
    fun `완성은 커서 양쪽을 args 로 싣는다`() {
        val f = Fake(listOf("""{"ok":true,"out":"println()"}""")); f.start()
        val got = Assist(f.opener()).completeCode("a.kt", "fun main() { ", " }")
        f.close()
        assertEquals("println()", got)
        assertTrue(f.seen[0].contains("\"method\":\"complete\""), f.seen[0])
        assertTrue(f.seen[0].contains("\"args\":{"), "커서 양쪽은 args 로 가야 한다: ${f.seen[0]}")
        assertTrue(f.seen[0].contains("\"prefix\":\"fun main() { \""), f.seen[0])
        assertTrue(f.seen[0].contains("\"suffix\":\" }\""), f.seen[0])
    }

    @Test
    fun `룩오버의 데몬 메서드는 콘솔 라우트 이름이 아니다`() {
        val f = Fake(listOf("""{"ok":true,"out":"3번 줄: 널 검사가 없다"}""")); f.start()
        val got = Assist(f.opener()).lookOver("a.kt", "val x = y!!")
        f.close()
        assertEquals("3번 줄: 널 검사가 없다", got)
        assertTrue(f.seen[0].contains("\"method\":\"look-over\""), "look 이 아니라 look-over: ${f.seen[0]}")
    }

    @Test
    fun `제안과 열린 파일 알림`() {
        val f = Fake(listOf("""{"ok":true,"out":" 를 리팩터링해줘"}""", """{"ok":true}""")); f.start()
        val a = Assist(f.opener())
        assertEquals(" 를 리팩터링해줘", a.suggest("이 함수"))
        assertTrue(a.setOpenFile("a.kt", "저장 안 한 내용"))
        f.close()
        assertTrue(f.seen[0].contains("\"method\":\"suggest\""), f.seen[0])
        assertTrue(f.seen[1].contains("\"method\":\"open-file\""), f.seen[1])
    }

    @Test
    fun `빈 입력이면 데몬을 부르지 않는다`() {
        val f = Fake(emptyList()); f.start()
        val a = Assist(f.opener())
        assertNull(a.completeCode("a.kt", "  ", " "))
        assertNull(a.suggest("   "))
        assertNull(a.lookOver("a.kt", ""))
        f.close()
        assertTrue(f.seen.isEmpty(), "빈 입력에 요청이 나갔다: ${f.seen}")
    }

    @Test
    fun `꺼져 있으면 부르지 않고, 실패해도 조용하다`() {
        val f = Fake(emptyList()); f.start()
        assertNull(Assist(f.opener(), enabled = { false }).suggest("무엇"))
        f.close()
        assertTrue(f.seen.isEmpty())
        // 데몬이 없는 자리를 열게 해도 예외가 새지 않는다 — 타이핑 중에 뜨는 에러 상자는 도움이 아니다.
        val dead = Assist({ DaemonClient.connect(java.nio.file.Paths.get("/nope/none.sock")) })
        assertNull(dead.suggest("무엇"))
        assertEquals(0, dead.inFlight, "실패해도 대기 카운트가 남으면 스피너가 안 꺼진다")
    }
}
