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
 *
 * **이름만 맞고 값이 안 실려도 여기는 초록이었다.** 인자를 하나씩 빈 값으로 바꿔 재 보니
 * (2026-08-29) 이 파일이 지키던 것은 **메서드 이름**이었지 그 옆에 실린 것이 아니었다 —
 * `name` 과 `text` 를 통째로 비워도 여섯 자리가 안 죽었다. 그중 제일 나쁜 것이 `open-file` 의
 * `text` 다: 코어의 `app.SetOpenFile` 은 **빈 텍스트를 「닫혔다」로 읽으므로** 그 변이는 버퍼를
 * 미는 일을 통째로 지우는 일로 바꿔 놓는데, 화면에서는 아무 일도 안 일어난 것과 구별이 안 된다.
 * 그래서 아래 시험들은 메서드 이름 옆에 **실려 간 값**도 같이 못박는다.
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
        // 어느 파일인지가 안 실리면 데몬은 다른 파일의 문맥으로 완성한다 — 나오는 글자는
        // 그럴듯해서 틀린 줄을 모른다.
        assertTrue(f.seen[0].contains("\"name\":\"a.kt\""), f.seen[0])
    }

    @Test
    fun `룩오버의 데몬 메서드는 콘솔 라우트 이름이 아니다`() {
        val f = Fake(listOf("""{"ok":true,"out":"3번 줄: 널 검사가 없다"}""")); f.start()
        val got = Assist(f.opener()).lookOver("a.kt", "val x = y!!")
        f.close()
        assertEquals("3번 줄: 널 검사가 없다", got)
        assertTrue(f.seen[0].contains("\"method\":\"look-over\""), "look 이 아니라 look-over: ${f.seen[0]}")
        assertTrue(f.seen[0].contains("\"name\":\"a.kt\""), "어느 파일을 보는지가 안 실렸다: ${f.seen[0]}")
        assertTrue(f.seen[0].contains("\"text\":\"val x = y!!\""), "볼 글자가 안 실렸다: ${f.seen[0]}")
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
        assertTrue(f.seen[0].contains("\"text\":\"이 함수\""), "이어붙일 앞말이 안 실렸다: ${f.seen[0]}")
        assertTrue(f.seen[1].contains("\"name\":\"a.kt\""), f.seen[1])
        // 이 한 줄이 없으면 **미는 것**과 **지우는 것**이 이 시험에서 같아 보인다(위 KDoc).
        assertTrue(f.seen[1].contains("\"text\":\"저장 안 한 내용\""), "저장 안 한 내용이 안 실렸다: ${f.seen[1]}")
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
        // 이름이 재는 것과 같아야 한다: 이 자리는 **없는 경로**(나간 것)이지 죽은 데몬이 아니다.
        // 그 둘은 이 저장소가 방금 갈라 놓은 갈래이고, 한쪽 이름을 다른 쪽에 붙여 두면 다음
        // 사람이 이 시험을 「죽은 데몬도 조용하다」의 근거로 읽는다.
        val gone = Assist({ DaemonClient.connect(java.nio.file.Paths.get("/nope/none.sock")) })
        assertNull(gone.suggest("무엇"))
        assertEquals(0, gone.inFlight, "실패해도 대기 카운트가 남으면 스피너가 안 꺼진다")
    }
    /**
     * **거부는 「할 말 없음」이 아니다.**
     *
     * 문이 못 하겠다고 할 때는 `err` 로 오고, 완성기가 그냥 아무 말도 안 했을 때는 `ok` 에
     * `reason` 으로 온다. [Assist] 가 `reason` 만 읽던 동안 설정 화면은 거부를 영영 못 그렸다 —
     * 「자동완성이 왜 죽었나」의 답이 왕복에 실려 왔는데 아무도 안 읽는 칸에 있었다.
     */
    @Test
    fun `문이 거부하면 그 사유가 남는다`() {
        Assist.lastRefused = null
        Assist.lastEmpty = "off"
        val f = Fake(listOf("""{"error":"this daemon cannot complete code"}"""))
        f.start()
        val out = Assist({ DaemonClient.connect(f.path) }).completeCode("A.kt", "fun x(", "")
        assertNull(out, "거부에 글자가 딸려 올 리 없다")
        assertEquals("this daemon cannot complete code", Assist.lastRefused,
            "문이 왜 못 하는지 말했는데 화면이 읽을 자리에 안 남았다")
        assertNull(Assist.lastEmpty, "거부는 「할 말 없음」 칸을 비운다 — 두 사유가 같이 서면 화면이 둘 다 말한다")
    }

    @Test
    fun `잘 된 왕복은 앞의 거부를 지운다`() {
        Assist.lastRefused = "this daemon cannot complete code"
        val f = Fake(listOf("""{"ok":true,"out":"1)"}"""))
        f.start()
        Assist({ DaemonClient.connect(f.path) }).completeCode("A.kt", "fun x(", "")
        assertNull(Assist.lastRefused, "잘 되는 동안 남아 있는 사유는 늙는다")
    }

    /**
     * 훑어보기는 **누른 자리에 판이 있다.** 그래서 거부를 삼키면 그 판이 「할 말이 없다」고 적고,
     * 그건 데몬이 한 말과 다른 말이다. 답으로 돌려줘야 부른 쪽이 그릴 수 있다.
     */
    @Test
    fun `훑어보기의 거부는 답으로 돌아온다`() {
        Assist.lastRefused = null
        val f = Fake(listOf("""{"error":"this daemon cannot look over a file"}"""))
        f.start()
        val said = Assist({ DaemonClient.connect(f.path) }).lookOver("A.kt", "package x")
        assertEquals("this daemon cannot look over a file", said,
            "거부를 삼키면 판이 「할 말이 없다」고 적는다 — 데몬은 왜 못 하는지 말했다")
    }

    /** 타이핑 중에 도는 셋도 사유는 남긴다 — 그 자리에서 안 그릴 뿐이다. */
    @Test
    fun `조용한 문들도 거부 사유를 남긴다`() {
        for (call in listOf<Pair<String, (Assist) -> Any?>>(
            "suggest" to { a -> a.suggest("고쳐 줘") },
            "open-file" to { a -> a.setOpenFile("A.kt", "package x") },
        )) {
            Assist.lastRefused = null
            val f = Fake(listOf("""{"error":"this daemon cannot ${call.first}"}"""))
            f.start()
            call.second(Assist({ DaemonClient.connect(f.path) }))
            assertEquals("this daemon cannot ${call.first}", Assist.lastRefused,
                "${call.first} 이 거부당했는데 아무 데도 안 남았다")
        }
    }
}
