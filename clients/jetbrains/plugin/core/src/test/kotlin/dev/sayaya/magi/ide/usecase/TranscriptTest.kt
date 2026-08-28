package dev.sayaya.magi.ide.usecase

import dev.sayaya.magi.ide.model.LogEvent
import dev.sayaya.magi.ide.model.Request
import dev.sayaya.magi.ide.model.Response
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

/**
 * 전사 스트림의 계약을 가짜 데몬으로 시험한다.
 *
 * 여기서 보는 것 셋. **순서**(데몬이 보낸 차례 그대로 오는가), **커서 통보가 이벤트보다 먼저
 * 도착하는가**(늦게 오면 화면이 이미 그린 뒤라 지울 수 없다), 그리고 **사람이 끈 것이 고장으로
 * 보이지 않는가**.
 */
class TranscriptTest {

    /** 대본대로 프레임을 뱉는 가짜. 소켓은 안 쓴다 — 전송은 [DaemonClient] 시험이 본다. */
    private class Scripted(private val frames: List<Response>) : Daemon {
        var asked: Request? = null
        var closed = false
        override fun exchange(request: Request) = Response(ok = true)
        override fun stream(request: Request, each: (Response) -> Boolean) {
            asked = request
            for (f in frames) if (!each(f)) return
        }
        override fun close() { closed = true }
    }

    /** 진짜 소켓처럼 군다 — 읽기가 막혀 있다가 닫히면 던진다. */
    private class Blocking : Daemon {
        private val shut = CountDownLatch(1)
        var closed = false
        override fun exchange(request: Request) = Response(ok = true)
        override fun stream(request: Request, each: (Response) -> Boolean) {
            shut.await()
            throw java.io.IOException("socket closed")
        }
        override fun close() { closed = true; shut.countDown() }
    }

    private class Collect : Transcript.Sink {
        val seen = mutableListOf<String>()
        val done = CountDownLatch(1)
        var end: End? = null
        override fun frame(e: LogEvent) { synchronized(seen) { seen += "e${e.seq}" } }
        override fun note(why: String) { synchronized(seen) { seen += "note:$why" } }
        override fun ended(end: End) { this.end = end; done.countDown() }
    }

    private fun run(frames: List<Response>, since: Long? = null): Pair<Collect, Scripted> {
        val fake = Scripted(frames)
        val sink = Collect()
        Transcript({ fake }, "s_1").follow(sink, since)
        assertTrue(sink.done.await(5, TimeUnit.SECONDS), "스트림이 안 끝났다")
        return sink to fake
    }

    @Test
    fun `보낸 차례 그대로 온다 — 재생이 먼저면 재생이 먼저 보인다`() {
        val (sink, _) = run(listOf(ev(1), ev(2), ev(3)))
        assertEquals(listOf("e1", "e2", "e3"), sink.seen)
        assertEquals(End.ByDaemon, sink.end, "데몬이 닫은 것은 고장이 아니다")
    }

    @Test
    fun `커서 통보는 첫 이벤트보다 먼저 보인다`() {
        // 데몬이 그 순서로 보낸다. 늦게 오면 화면이 이미 그린 뒤라 지울 수 없다.
        val (sink, _) = run(listOf(Response(ok = true, why = "커서를 못 믿겠다"), ev(1), ev(2)))
        assertEquals("note:커서를 못 믿겠다", sink.seen.first())
        assertEquals(listOf("e1", "e2"), sink.seen.drop(1))
    }

    @Test
    fun `커서를 안 주면 since 가 아예 안 실린다`() {
        // 빈 값을 실어 보내면 "전량"이 아니라 "0 을 요청"이 되고, 그 둘은 코어에서 같은 뜻이지만
        // 와이어에 없는 것과 0 인 것을 굳이 갈라 보내면 다음 사람이 규칙을 하나 더 외워야 한다.
        val (_, fake) = run(listOf(ev(1)))
        assertNull(fake.asked?.since)
        assertEquals("transcript", fake.asked?.method)
    }

    @Test
    fun `에러 프레임은 스트림을 끝낸다`() {
        val (sink, _) = run(listOf(ev(1), Response(ok = false, error = "문이 없다")))
        assertEquals(listOf("e1"), sink.seen)
        assertEquals(End.Broken("문이 없다"), sink.end)
    }

    @Test
    fun `사람이 끄면 연결이 닫히고 사람이 끝냈다고 말한다`() {
        // 대본이 빈 가짜로는 못 재는 것이 있다. 그 가짜는 `stream` 이 곧바로 돌아와서 워커가
        // **사람이 닫기 전에** 끝나 버리고, 그러면 [End.ByDaemon] 이 이겨서 시험이 운에 걸린다.
        // 그래서 진짜 소켓처럼 **닫힐 때까지 막혀 있다가 던지는** 가짜를 쓴다.
        val fake = Blocking()
        val sink = Collect()
        Transcript({ fake }, "s_1").follow(sink).close()
        assertTrue(sink.done.await(5, TimeUnit.SECONDS))
        assertTrue(fake.closed, "연결을 안 닫았다")
        // 이 갈래가 창의 재접속을 막는다. 여기서 [End.Broken] 이 나오면 닫은 창이 되살아난다.
        assertEquals(End.ByUs, sink.end)
    }

    @Test
    fun `아무도 안 껐는데 끊긴 것은 사람이 끈 것과 다르게 온다`() {
        // 데몬이 죽거나 소켓이 끊긴 자리다. 창은 이걸 받고 다시 붙어야 하고, 그래서 사람이 끈
        // 것과 **반드시 갈려야 한다** — 뭉치면 둘 중 하나는 틀린 일을 한다.
        val broken = object : Daemon {
            override fun exchange(request: Request) = Response(ok = true)
            override fun stream(request: Request, each: (Response) -> Boolean) = throw java.io.IOException("연결이 끊겼다")
            override fun close() {}
        }
        val sink = Collect()
        Transcript({ broken }, "s_1").follow(sink)
        assertTrue(sink.done.await(5, TimeUnit.SECONDS))
        assertEquals(End.Broken("연결이 끊겼다"), sink.end)
    }

    @Test
    fun `조각은 뒤따를 사실과 같은 말이라 줄을 안 받는다`() {
        // 코어가 하는 중인 말을 조각으로 흘리고(`part.delta`, 자리가 없어 seq 0) 끝나면 같은
        // 말을 사실로 앉힌다. 둘 다 새 줄로 쌓으면 답이 두 번 뜬다.
        assertTrue(Transcript.echoesFact(LogEvent(seq = 0, type = "part.delta")))
        assertFalse(Transcript.echoesFact(LogEvent(seq = 7, type = "part.appended")))
    }

    @Test
    fun `조각이 아닌 버스 이벤트는 줄을 받는다`() {
        // 재생에 안 실리는 것은 조각과 같지만 **되풀이가 아니다** — 사실로 뒤따르는 같은 말이
        // 없다. 자리가 0 이라는 이유로 싸잡아 버리면 화면이 권한 요청을 못 보게 된다.
        assertFalse(Transcript.echoesFact(LogEvent(seq = 0, type = "permission.requested")))
        assertFalse(Transcript.echoesFact(LogEvent(seq = 0, type = "tool.progress")))
    }

    @Test
    fun `조각도 싱크에는 그대로 온다 — 거르는 것은 화면이다`() {
        // 이 층은 정책을 안 담는다(`Wire.kt` 의 LogEvent 주석). 여기서 미리 버리면 §8 의 깊은
        // 렌더가 흐르는 말을 그릴 방법을 잃는다 — 술어는 주되 프레임은 다 넘긴다.
        val (sink, _) = run(listOf(Response(ok = true, event = LogEvent(seq = 0, type = "part.delta")), ev(1)))
        assertEquals(listOf("e0", "e1"), sink.seen)
    }

    @Test
    fun `물음이 움직인 넷은 다시 물어보라는 신호다`() {
        // 창이 열릴 때 한 번만 묻던 판에서는 이 넷이 와도 아무 일도 안 났다. 답한 쪽 둘까지 넣는
        // 이유는 다른 창이 먼저 답하면 이 창의 단추가 지나간 물음을 가리키기 때문이다.
        for (t in listOf("permission.requested", "question.requested", "permission.decided", "question.answered")) {
            assertTrue(Transcript.movesPrompt(LogEvent(seq = 0, type = t)), t)
        }
    }

    @Test
    fun `물음과 무관한 것은 다시 묻게 하지 않는다`() {
        // 매 프레임마다 status 를 왕복하면 전사 하나가 소켓 왕복 수백 개가 된다.
        for (t in listOf("part.appended", "part.delta", "tool.progress", "council.verdict")) {
            assertFalse(Transcript.movesPrompt(LogEvent(seq = 0, type = t)), t)
        }
    }

    private fun ev(seq: Long) = Response(ok = true, event = LogEvent(seq = seq, type = "part.appended"))
}
