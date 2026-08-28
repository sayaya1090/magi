package dev.sayaya.magi.ide.usecase

import dev.sayaya.magi.ide.model.Ask
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
 * 가짜 데몬 하나를 세우고 왕복 전체를 건다.
 *
 * 실물 없이 계약을 시험할 수 있어야 모듈을 둘로 나눈 값이 난다(설계 문서 §1). 여기서 보는 것은
 * 프레이밍(한 줄에 객체 하나), 락스텝(요청 하나에 답 하나), 그리고 **부르는 쪽이 메서드를 고르지
 * 않는다**는 규칙이다.
 */
class CompanionTest {

    /** 받은 요청 줄을 그대로 모으고, 미리 정한 답을 순서대로 돌려준다. */
    private class FakeDaemon(private val replies: List<String>) {
        val seen = mutableListOf<String>()
        val path = Files.createTempDirectory("magi-fake").resolve("d.sock")
        private val server = ServerSocketChannel.open(StandardProtocolFamily.UNIX).apply {
            bind(UnixDomainSocketAddress.of(path))
        }

        fun start() = thread(isDaemon = true) {
            server.accept().use { ch ->
                val r = Channels.newReader(ch, Charsets.UTF_8).buffered()
                val w = Channels.newWriter(ch, Charsets.UTF_8).buffered()
                for (reply in replies) {
                    val line = r.readLine() ?: return@use
                    seen += line
                    w.write(reply); w.write("\n"); w.flush()
                }
            }
        }

        fun close() = server.close()
    }

    @Test
    fun `사실 장은 데몬이 말한 것만 담는다`() {
        val fake = FakeDaemon(listOf(
            """{"ok":true,"doing":"go test ./...","permission":"auto","session":"s_9","waiting":{"id":"c1","kind":"permission","what":"bash","reason":"rm"}}""",
        ))
        fake.start()
        val f = DaemonClient.connect(fake.path).use { Companion(it, "s_1").facts() }
        fake.close()
        assertEquals("go test ./...", f.doing)
        assertEquals("auto", f.permission)
        // 세션은 데몬이 말한 것이 이긴다. 부르는 쪽이 들고 있던 값은 낡을 수 있다.
        assertEquals("s_9", f.session)
        assertTrue(fake.seen[0].contains(""""method":"status""""))
        // 두 화면(`FactsToolWindow`·`StatusBar`)이 「사람을 기다리는 중」을 이 칸 **하나**로
        // 판단한다. 안 옮겨 실으면 사람이 막고 선 것이 화면에서 **쉬는 중**과 같아 보인다 —
        // 화면의 마지막 말이 영영 거짓이 되는 그 종류다.
        assertEquals("c1", f.waiting?.id)
        assertEquals("permission", f.waiting?.kind)
    }

    @Test
    fun `모름과 없음을 섞지 않는다`() {
        // 데몬이 doing 도 permission 도 안 실어 보냈다. 그것은 "쉬는 중"도 "모드 없음"도 아니다 —
        // 화면이 그 둘을 같은 글자로 그리면 모르는 것을 아는 척하게 된다(§0.5-7).
        val fake = FakeDaemon(listOf("""{"ok":true}"""))
        fake.start()
        val f = DaemonClient.connect(fake.path).use { Companion(it, "s_1").facts() }
        fake.close()
        assertNull(f.doing)
        assertNull(f.permission)
        // 데몬이 세션을 안 말하면 부르는 쪽이 들고 있던 것으로 물러선다 — 빈 문자열이 아니다.
        assertEquals("s_1", f.session)
    }

    @Test
    fun `빈 문자열은 값이 아니라 모름이다`() {
        // 데몬이 필드를 빈 값으로 채워 보내는 경우. 있는 것처럼 세면 화면에 빈칸이 그려지고,
        // 사람은 그것을 "이 컴패니언은 아무것도 안 한다"로 읽는다.
        val fake = FakeDaemon(listOf("""{"ok":true,"doing":"","permission":"","session":""}"""))
        fake.start()
        val f = DaemonClient.connect(fake.path).use { Companion(it, "s_1").facts() }
        fake.close()
        assertNull(f.doing)
        assertNull(f.permission)
        assertEquals("s_1", f.session)
    }

    @Test
    fun `턴이 돌면 steer, 안 돌면 submit 을 고른다 — 데몬에게 물어서`() {
        // status(도는 중) → steer, status(쉬는 중) → submit
        val fake = FakeDaemon(listOf(
            """{"ok":true,"doing":"go build ./..."}""",
            """{"ok":true}""",
            """{"ok":true}""",
            """{"ok":true}""",
        ))
        fake.start()
        DaemonClient.connect(fake.path).use { c ->
            val comp = Companion(c, "s_1")
            comp.say("타이핑 중에 보낸 말")
            comp.say("쉬는 중에 보낸 말")
        }
        fake.close()

        assertTrue(fake.seen[0].contains("\"method\":\"status\""), fake.seen[0])
        assertTrue(fake.seen[1].contains("\"method\":\"steer\""), "턴이 도는데 steer 가 아니다: ${fake.seen[1]}")
        assertTrue(fake.seen[2].contains("\"method\":\"status\""), fake.seen[2])
        assertTrue(fake.seen[3].contains("\"method\":\"submit\""), "쉬는데 submit 이 아니다: ${fake.seen[3]}")
        // 고른 메서드만 보면 **빈 말**을 보내도 초록이다. 사람이 친 글이 실렸는지 같이 본다.
        assertTrue(fake.seen[1].contains("\"text\":\"타이핑 중에 보낸 말\""), fake.seen[1])
        assertTrue(fake.seen[3].contains("\"text\":\"쉬는 중에 보낸 말\""), fake.seen[3])
    }

    @Test
    fun `사람을 기다리는 중이면 그것도 열린 턴이다`() {
        val fake = FakeDaemon(listOf(
            """{"ok":true,"waiting":{"id":"c1","kind":"permission","what":"bash","reason":"rm"}}""",
            """{"ok":true}""",
        ))
        fake.start()
        DaemonClient.connect(fake.path).use { Companion(it, "s_1").say("답 대신 지시") }
        fake.close()
        assertTrue(fake.seen[1].contains("\"method\":\"steer\""),
            "대기 중은 턴 안이므로 steer 여야 한다: ${fake.seen[1]}")
    }

    @Test
    fun `퍼미션과 질문은 메서드도 필드도 다르다`() {
        val fake = FakeDaemon(listOf("""{"ok":true}""", """{"ok":true}"""))
        fake.start()
        DaemonClient.connect(fake.path).use { c ->
            val comp = Companion(c, "s_1")
            comp.always("call-7")
            comp.answer("call-8", "두 번째 선택지")
        }
        fake.close()

        assertTrue(fake.seen[0].contains("\"method\":\"permission\""), fake.seen[0])
        assertTrue(fake.seen[0].contains("\"decision\":\"always\""), "결정은 코어 낱말 그대로: ${fake.seen[0]}")
        assertTrue(fake.seen[0].contains("\"callId\":\"call-7\""), fake.seen[0])
        assertTrue(fake.seen[1].contains("\"method\":\"answer\""), fake.seen[1])
        assertTrue(fake.seen[1].contains("\"answer\":\"두 번째 선택지\""), fake.seen[1])
        // 퍼미션 쪽은 `callId` 를 이미 보고 있었는데 이쪽은 안 봤다 — 재서 나온 비대칭이다.
        assertTrue(fake.seen[1].contains("\"callId\":\"call-8\""),
            "어느 질문의 답인지가 안 실리면 데몬은 **다른 프롬프트**의 답으로 읽는다: ${fake.seen[1]}")
    }

    @Test
    fun `대기 중인 프롬프트를 읽어 온다`() {
        val fake = FakeDaemon(listOf(
            """{"ok":true,"waiting":{"id":"c9","kind":"question","what":"어느 쪽으로 갈까",""" +
                """"options":["A","B"],"index":1,"total":2}}"""
        ))
        fake.start()
        val w = DaemonClient.connect(fake.path).use { Companion(it, "s_1").waiting() }
        fake.close()

        assertEquals("c9", w?.id)
        assertEquals(Ask.Choose(listOf("A", "B")), w?.ask)
        assertEquals(listOf("A", "B"), w?.options)
        assertEquals(2, w?.total)
    }
}
